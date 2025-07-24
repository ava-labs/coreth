#!/usr/bin/env bash
set -euo pipefail

# avalanche root directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORETH_ROOT="${CORETH_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"

# Load the constants
source "$CORETH_ROOT/scripts/constants.sh"

# Configure race detection
NO_RACE="${NO_RACE:-}"
RACE_FLAG=""
(( NO_RACE )) || RACE_FLAG="-race"

# Test settings
TIMEOUT="${TIMEOUT:-600s}"
MAX_RETRIES=4
ALL_PKGS=( $(go list ./... | grep -v github.com/ava-labs/coreth/tests) )

# run_and_collect: execute tests per-package and gather failures + missing tests
run_and_collect() {
  local pkgs=("${!1}")
  FAILED_TESTS=()
  MISSING_TESTS=()

  for pkg in "${pkgs[@]}"; do
    # 1) list all tests in this package
    mapfile -t all_tests < <(go test -list . "$pkg" | grep '^Test')

    # 2) run tests with JSON output (capture but don't exit)
    go test -json -timeout="$TIMEOUT" $RACE_FLAG "$pkg" >"${pkg//\//_}.json" 2>&1 || true

    # 3) collect ran and failed tests
    mapfile -t ran_tests < <(
      jq -r 'select(.Package=="'$pkg'" and .Test!=null and .Action=="run") | .Test' "${pkg//\//_}.json" | sort -u
    )
    mapfile -t failed_tests < <(
      jq -r 'select(.Package=="'$pkg'" and .Test!=null and .Action=="fail") | .Test' "${pkg//\//_}.json" | sort -u
    )

    # record failures
    for t in "${failed_tests[@]}"; do
      FAILED_TESTS+=("$pkg::$t")
    done

    # detect missing (panic or skip) tests
    for t in "${all_tests[@]}"; do
      if ! printf '%s
' "${ran_tests[@]}" | grep -qxF "$t"; then
        MISSING_TESTS+=("$pkg::$t")
      fi
    done
  done
}

# Main retry loop
target_tests=()
for ((i=1; i<=MAX_RETRIES; i++)); do
  echo "=== Test attempt #$i ==="

  if [[ $i -eq 1 ]]; then
    # first run on all packages
    run_and_collect ALL_PKGS[@]
  else
    # on retries: combine failed + missing tests
    if (( ${#FAILED_TESTS[@]} )); then
      target_tests=("${FAILED_TESTS[@]}" "${MISSING_TESTS[@]}")
    else
      target_tests=("${MISSING_TESTS[@]}")
    fi

    # build regex for -run
    tests_regex=$(printf "%s|" "${target_tests[@]##*::}")
    tests_regex="^(${tests_regex%|})$"

    echo "Retrying only: ${target_tests[*]}"
    go test -json -timeout="$TIMEOUT" $RACE_FLAG -run "$tests_regex" \
      "${ALL_PKGS[@]}" >retry.json 2>&1 || true

    # collect results from retry file
    run_and_collect retry.json  # if refactor to accept JSON path
  fi

  # exit conditions
  if [[ ${#FAILED_TESTS[@]} -eq 0 && ${#MISSING_TESTS[@]} -eq 0 ]]; then
    echo "✅ All tests passed on attempt #$i"
    rm -f *.json coverage.out
    exit 0
  fi

  echo "Failures: ${FAILED_TESTS[*]}"
  echo "Missing: ${MISSING_TESTS[*]}"
  echo "Retrying..."
done

echo "❌ Tests still failing after $MAX_RETRIES attempts"
exit 1
