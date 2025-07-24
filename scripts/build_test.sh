#!/usr/bin/env bash
set -euo pipefail

# avalanche root directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORETH_PATH="${CORETH_PATH:-$(cd "$SCRIPT_DIR/.." && pwd)}"

# Load the constants
source "$CORETH_PATH/scripts/constants.sh"

# Configure race detection
NO_RACE="${NO_RACE:-}"
RACE_FLAG=""
(( NO_RACE )) || RACE_FLAG="-race"

# Test settings
TIMEOUT="${TIMEOUT:-600s}"
MAX_RETRIES=4
ALL_PKGS=( $(go list ./... | grep -v github.com/ava-labs/coreth/tests) )

# get_all_tests: quickly list Test* functions in pkg directories
get_all_tests() {
  local pkg="$1"
  local dir
  dir=$(go list -f '{{.Dir}}' "$pkg")
  grep -R --include '*_test.go' -E '^[[:space:]]*func[[:space:]]+(Test[[:alnum:]_]+)\(' "$dir" |
    sed -E 's/^.*func[[:space:]]+(Test[[:alnum:]_]+).*/\1/' |
    sort -u
}

# run_and_collect: execute tests per-package and gather failures + missing tests
run_and_collect() {
  local pkgs=("${!1}")
  FAILED_TESTS=()
  MISSING_TESTS=()

  echo "Processing ${#pkgs[@]} packages..."
  for pkg in "${pkgs[@]}"; do
    echo "--- Package: $pkg ---"
    start_pkg=$SECONDS

    # 1) list all tests via fast grep
    echo "Listing tests..."
    all_tests=()
    while IFS= read -r t; do all_tests+=("$t"); done < <(get_all_tests "$pkg")
    echo "Found ${#all_tests[@]} tests"

    # 2) run tests with JSON output (capture but don't exit)
    echo "Running tests (JSON)..."
    out_file="${pkg//\//_}.json"
    go test -json -timeout="$TIMEOUT" $RACE_FLAG "$pkg" >"$out_file" 2>&1 || true

    # 3) collect ran and failed tests
    echo "Parsing results..."
    ran_tests=()
    while IFS= read -r t; do ran_tests+=("$t"); done < <(
      jq -r 'select(.Package=="'$pkg'" and .Test!=null and .Action=="run") | .Test' "$out_file" | sort -u
    )
    failed_tests=()
    while IFS= read -r t; do failed_tests+=("$t"); done < <(
      jq -r 'select(.Package=="'$pkg'" and .Test!=null and .Action=="fail") | .Test' "$out_file" | sort -u
    )
    echo "Ran ${#ran_tests[@]}, Failed ${#failed_tests[@]}"

    # record failures
    for t in "${failed_tests[@]}"; do
      FAILED_TESTS+=("$pkg::$t")
    done

    # detect missing (panic or skip) tests
    echo "Detecting missing tests..."
    for t in "${all_tests[@]}"; do
      if ! printf '%s\n' "${ran_tests[@]}" | grep -qxF "$t"; then
        MISSING_TESTS+=("$pkg::$t")
      fi
    done
    echo "Missing ${#MISSING_TESTS[@]} so far"

    echo "Package time: $((SECONDS - start_pkg))s"
  done
}

# Main retry loop
target_tests=()
for ((i=1; i<=MAX_RETRIES; i++)); do
  echo "=== Test attempt #$i ==="

  if [[ $i -eq 1 ]]; then
    run_and_collect ALL_PKGS[@]
  else
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

    echo "Parsing retry results..."
    run_and_collect retry.json  # deprecated: placeholder for JSON-path parser
  fi

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
