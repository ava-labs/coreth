#!/usr/bin/env bash
set -euo pipefail

# avalanche root directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORETH_PATH="${CORETH_PATH:-$(cd "$SCRIPT_DIR/.." && pwd)}"

# Load the constants
source "$CORETH_PATH/scripts/constants.sh"

# CLI flags
NO_RACE="${NO_RACE:-}"
RACE_FLAG=""
(( NO_RACE )) || RACE_FLAG="-race"
TIMEOUT="${TIMEOUT:-600s}"
MAX_RETRIES=4
ALL_PKGS=( $(go list ./... | grep -v github.com/ava-labs/coreth/tests) )
KNOWN_FLAKES_FILE="$CORETH_PATH/scripts/known_flakes.txt"
COVER_FLAGS="-coverprofile=coverage.out -covermode=atomic -shuffle=on"

# get_all_tests: list Test functions without invoking go test
get_all_tests() { local pkg="$1"; go list -f '{{.Dir}}' "$pkg" | xargs grep -R --include '*_test.go' -E '^[[:space:]]*func[[:space:]]+Test' | sed -E 's/.*func[[:space:]]+(Test[[:alnum:]_]+).*/\1/' | sort -u; }

run_and_collect() {
  local pkgs=("${!1}")
  FLAKY_TESTS=()
  MISSING_TESTS=()

  for pkg in "${pkgs[@]}"; do
    echo "=== Package: $pkg ==="
    # skip empty
    dir=$(go list -f '{{.Dir}}' "$pkg")
    if [[ ! -d "$dir" || -z $(ls "$dir"/*_test.go 2>/dev/null) ]]; then
      echo "?   $pkg [no test files]"
      continue
    fi

    # initial test run with tee
    echo "ok?  $pkg"
    test_out="$(mktemp)"
    go test $COVER_FLAGS -timeout="$TIMEOUT" $RACE_FLAG "$@" "$pkg" | tee "$test_out" || command_status=$?
    if [[ ${command_status:-0} -ne 0 ]]; then
      # parse failures
      failures=$(grep '^--- FAIL' "$test_out" | awk '{print $3}' | sort -u)
      for t in $failures; do
        [[ $(grep -Fxq "$t" "$KNOWN_FLAKES_FILE"; echo $?) -eq 1 ]] && { echo "Unexpected failure: $t"; exit 1; }
        FLAKY_TESTS+=("$pkg::$t")
      done
    fi

    # detect missing due to panic
    all_tests=( $(get_all_tests "$pkg") )
    ran_tests=( $(grep -E '^=== RUN' "$test_out" | awk '{print $3}' | sort -u) )
    missing=()
    for t in "${all_tests[@]}"; do [[ ! " ${ran_tests[*]} " =~ " $t " ]] && missing+=("$pkg::$t"); done
    echo "Tests ran: ${#ran_tests[@]}, missing: ${#missing[@]}"
    MISSING_TESTS+=("${missing[@]}")

    rm "$test_out"
  done
}

# main loop
for ((i=1;i<=MAX_RETRIES;i++)); do
  echo "--- Attempt #$i ---"
  run_and_collect ALL_PKGS[@]
  if [[ ${#MISSING_TESTS[@]} -eq 0 && ${#FLAKY_TESTS[@]} -eq 0 ]]; then echo "✅ All tests passed"; exit 0; fi
  # retry only flaky and missing
  tests=(${FLAKY_TESTS[@]} ${MISSING_TESTS[@]})
  regex="^($(printf '%s|' "${tests[@]##*::}") )$"
  echo "Retrying: ${tests[*]}"
  go test $COVER_FLAGS -timeout="$TIMEOUT" $RACE_FLAG -run "$regex" "${ALL_PKGS[@]}" | tee retry.out || true
  mv retry.out "${test_out:-retry.out}"
done

if [[ ${#FLAKY_TESTS[@]} -gt 0 || ${#MISSING_TESTS[@]} -gt 0 ]]; then
  echo "❌ Failures after retries: ${FLAKY_TESTS[*]} ${MISSING_TESTS[*]}"
  exit 1
fi
exit 0
