#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# ---------------------------------------------
# Build & Test Script with Flake Handling
# Implements behavior matrix:
# 1) All run, only known flakes: retry flaky tests only
# 2) Partial panics: retry failed + missing tests
# 3) Unexpected failures: exit immediately
# 4) All pass: exit immediately
# ---------------------------------------------

# avalanche root directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORETH_PATH="${CORETH_PATH:-$(cd "$SCRIPT_DIR/.." && pwd)}"

# Load project constants
source "$CORETH_PATH/scripts/constants.sh"

# CLI flags (race detection, timeout)
NO_RACE="${NO_RACE:-}"
RACE_FLAG=""
(( NO_RACE )) || RACE_FLAG="-race"
TIMEOUT="${TIMEOUT:-600s}"
MAX_RETRIES=4

# 1) Gather all packages under test (exclude internal test dir)
mapfile -t ALL_PKGS < <(
  go list ./... |
  grep -v github.com/ava-labs/coreth/tests
)

# Known flaky tests file
KNOWN_FLAKES_FILE="$CORETH_PATH/scripts/known_flakes.txt"

# Coverage/shuffle flags matching original behavior
declare -a COVER_FLAGS=(
  "-coverprofile=coverage.out"
  "-covermode=atomic"
  "-shuffle=on"
)

# get_all_tests: list all Test* functions by grepping *_test.go files
get_all_tests() {
  local pkg="$1"
  go list -f '{{.Dir}}' "$pkg" |
    xargs grep -R --include '*_test.go' -E '^[[:space:]]*func[[:space:]]+Test' |
    sed -E 's/.*func[[:space:]]+(Test[[:alnum:]_]+).*/\1/' |
    sort -u
}

# run_and_collect:
#  - Runs each pkg, captures flaky failures and missing tests (panics)
#  - Exits on non-flaky failures
run_and_collect() {
  local pkgs=("$@")
  FLAKY_TESTS=()
  MISSING_TESTS=()

  for pkg in "${pkgs[@]}"; do
    echo "=== Package: $pkg ==="

    # Skip packages with no test files (mimics 'no test files' in Go output)
    dir=$(go list -f '{{.Dir}}' "$pkg")
    if [[ ! -d "$dir" || -z $(ls "$dir"/*_test.go 2>/dev/null) ]]; then
      echo "?   $pkg [no test files]"
      continue
    fi

    # Run tests with coverage + shuffle, tee output to inspect failures
    echo "ok?  $pkg"
    test_out=$(mktemp)
    command go test -v "${COVER_FLAGS[@]}" -timeout="$TIMEOUT" $RACE_FLAG "$pkg" \
      | tee "$test_out" || command_status=$?

    # Handle failures: separate flakes vs unexpected
    if [[ ${command_status:-0} -ne 0 ]]; then
      mapfile -t failures < <(
        grep '^--- FAIL' "$test_out" | awk '{print \$3}' | sort -u
      )
      for t in "${failures[@]}"; do
        if ! grep -Fxq "$t" "$KNOWN_FLAKES_FILE"; then
          echo "Unexpected failure (exit immediately): $t"
          exit 1
        fi
        # Collect known flaky tests for retry
        FLAKY_TESTS+=("$pkg::$t")
      done
    fi

    # Detect missing tests due to panic/skips
    mapfile -t all_tests < <(get_all_tests "$pkg")
    mapfile -t ran_tests < <(
      grep -E '^=== RUN' "$test_out" | awk '{print \$3}' | sort -u
    )
    missing=()
    for t in "${all_tests[@]}"; do
      if ! printf '%s\n' "${ran_tests[@]}" | grep -qxF "$t"; then
        missing+=("$pkg::$t")
      fi
    done
    echo "Tests ran: ${#ran_tests[@]}, missing (for retry): ${#missing[@]}"
    MISSING_TESTS+=("${missing[@]}")

    rm "$test_out"
  done
}

# Main retry loop: up to MAX_RETRIES attempts
for ((i=1; i<=MAX_RETRIES; i++)); do
  echo "--- Attempt #$i ---"
  # 1st run over all pkgs, subsequent runs will still use all pkgs but skip passed ones
  run_and_collect "${ALL_PKGS[@]}"

  # If no flakes or missing, success as per matrix row 'All tests pass'
  if [[ ${#FLAKY_TESTS[@]} -eq 0 && ${#MISSING_TESTS[@]} -eq 0 ]]; then
    echo "✅ All tests passed"
    exit 0
  fi

  # Else, build regex to retry only flaky + missing tests
  tests=("${FLAKY_TESTS[@]}" "${MISSING_TESTS[@]}")
  regex="^($(printf '%s|' "${tests[@]##*::}") )$"
  echo "Retrying only flaky + missing tests: ${tests[*]}"

  # Retry invocation preserves coverage + shuffle flags
  command go test "${COVER_FLAGS[@]}" -timeout="$TIMEOUT" $RACE_FLAG \
    -run "$regex" "${ALL_PKGS[@]}" | tee retry.out || true

  # Re-collect flaky/missing for further attempts
  run_and_collect "${ALL_PKGS[@]}"
done

# After retries exhausted: fail if still flakes/missing reflect matrix 'After Behavior'
if [[ ${#FLAKY_TESTS[@]} -gt 0 || ${#MISSING_TESTS[@]} -gt 0 ]]; then
  echo "❌ Tests still flaky or missing after $MAX_RETRIES attempts:"
  echo "   Flaky: ${FLAKY_TESTS[*]}"
  echo "   Missing: ${MISSING_TESTS[*]}"
  exit 1
fi

exit 0
