#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# ---------------------------------------------
# Build & Test Script with Flake Handling 
# Behavior matrix:
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

# CLI flags
NO_RACE="${NO_RACE:-}"
RACE_FLAG=""
(( NO_RACE )) || RACE_FLAG="-race"
TIMEOUT="${TIMEOUT:-600s}"
MAX_RETRIES=4

# Gather all packages under test (exclude internal tests)
ALL_PKGS=()
while IFS= read -r pkg; do ALL_PKGS+=("$pkg"); done < <(
  go list ./... | grep -v github.com/ava-labs/coreth/tests
)

# Known flaky tests file
KNOWN_FLAKES_FILE="$CORETH_PATH/scripts/known_flakes.txt"

# Coverage/shuffle flags matching original behavior
COVER_PROFILE="coverage.out"
COVER_MODE="atomic"
SHUFFLE="on"

# get_all_tests: list Test* functions by grepping *_test.go files
get_all_tests() {
  local pkg="$1"
  go list -f '{{.Dir}}' "$pkg" | \
    xargs grep -R --include '*_test.go' -E '^[[:space:]]*func[[:space:]]+Test' | \
    sed -E 's/.*func[[:space:]]+(Test[[:alnum:]_]+).*/\1/' | sort -u
}

# run_and_collect:
#  - Uses go test -json to gather run/fail/skip events
#  - Captures flaky failures and missing tests (due to panics/skips)
#  - Exits on non-flaky failures
run_and_collect() {
  local pkgs=("$@")
  FLAKY_TESTS=()
  MISSING_TESTS=()

  for pkg in "${pkgs[@]}"; do
    echo "=== Package: $pkg ==="

    # Skip packages without *_test.go
    dir=$(go list -f '{{.Dir}}' "$pkg")
    if [[ ! -d "$dir" || -z $(ls "$dir"/*_test.go 2>/dev/null) ]]; then
      echo "?   $pkg [no test files]"
      continue
    fi

    # Run tests with JSON output
    echo "Running: go test -json (coverage & shuffle)"
    out_file="${pkg//\//_}.json"
    command go test -json \
      -timeout="$TIMEOUT" \
      -coverprofile="$COVER_PROFILE" \
      -covermode="$COVER_MODE" \
      -shuffle="$SHUFFLE" \
      $RACE_FLAG "$pkg" > "$out_file" 2>&1 || true

    # Parse results
    # ran_tests: tests with Action=run or skip
    ran_tests=()
    while IFS= read -r t; do ran_tests+=("$t"); done < <(
      jq -r 'select(.Package=="'$pkg'" and .Test!=null and (.Action=="run" or .Action=="skip")) | .Test' "$out_file" | sort -u
    )

    # all_failed: tests with Action=fail
    all_failed=()
    while IFS= read -r t; do all_failed+=("$t"); done < <(
      jq -r 'select(.Package=="'$pkg'" and .Test!=null and .Action=="fail") | .Test' "$out_file" | sort -u
    )

    # Separate non-flaky vs flaky
    non_flakes=()
    flakes=()
    for t in "${all_failed[@]}"; do
      if grep -Fxq "$t" "$KNOWN_FLAKES_FILE"; then
        flakes+=("$pkg::$t")
      else
        non_flakes+=("$pkg::$t")
      fi
    done

    # Exit on any non-flaky failure
    if (( ${#non_flakes[@]} )); then
      echo "Unexpected (non-flake) failure: ${non_flakes[*]}"
      exit 1
    fi

    # Collect flaky tests for retry
    FLAKY_TESTS+=("${flakes[@]}")
    echo "Flaky failures: ${#flakes[@]}"

    # Detect missing tests due to panic/skips
    all_tests=()
    while IFS= read -r t; do all_tests+=("$t"); done < <(get_all_tests "$pkg")
    missing=()
    for t in "${all_tests[@]}"; do
      if ! printf '%s\n' "${ran_tests[@]}" | grep -qxF "$t"; then
        missing+=("$pkg::$t")
      fi
    done
    MISSING_TESTS+=("${missing[@]}")
    echo "Tests ran: ${#ran_tests[@]}, missing: ${#missing[@]}"

    rm "$out_file"
  done
}

# Main retry loop
for ((i=1; i<=MAX_RETRIES; i++)); do
  echo "--- Attempt #$i ---"
  run_and_collect "${ALL_PKGS[@]}"

  # All tests passed?
  if [[ ${#FLAKY_TESTS[@]} -eq 0 && ${#MISSING_TESTS[@]} -eq 0 ]]; then
    echo "✅ All tests passed"
    exit 0
  fi

  # Prepare to retry only flakes + missing
  tests=("${FLAKY_TESTS[@]}" "${MISSING_TESTS[@]}")
  regex="^($(printf '%s|' "${tests[@]##*::}") )$"
  echo "Retrying only flaky + missing tests: ${tests[*]}"

  # Retry
  command go test -json \
    -timeout="$TIMEOUT" \
    -coverprofile="$COVER_PROFILE" \
    -covermode="$COVER_MODE" \
    -shuffle="$SHUFFLE" \
    $RACE_FLAG -run "$regex" "${ALL_PKGS[@]}" > retry.json 2>&1 || true

  # Re-collect for further attempts
  run_and_collect "${ALL_PKGS[@]}"
done

# Final exit
if [[ ${#FLAKY_TESTS[@]} -gt 0 || ${#MISSING_TESTS[@]} -gt 0 ]]; then
  echo "❌ Tests still flaky or missing after $MAX_RETRIES attempts"
  echo "  Flaky: ${FLAKY_TESTS[*]}"
  echo "  Missing: ${MISSING_TESTS[*]}"
  exit 1
fi

exit 0
