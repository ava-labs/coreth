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
KNOWN_FLAKES_FILE="$CORETH_PATH/scripts/known_flakes.txt"

# get_all_tests: quickly list Test* functions in pkg directories
ing get_all_tests() {
  local pkg="$1"
  local dir
  dir=$(go list -f '{{.Dir}}' "$pkg")
  grep -R --include '*_test.go' -E '^[[:space:]]*func[[:space:]]+(Test[[:alnum:]_]+)\(' "$dir" |
    sed -E 's/^.*func[[:space:]]+(Test[[:alnum:]_]+).*/\1/' |
    sort -u
}

# run_and_collect: execute tests per-package and gather flaky failures + missing tests
run_and_collect() {
  local pkgs=("${!1}")
  FLAKY_TESTS=()
  MISSING_TESTS=()

  echo "Processing ${#pkgs[@]} packages..."
  for pkg in "${pkgs[@]}"; do
    echo "--- Package: $pkg ---"
    start_pkg=$SECONDS

    # list all tests
    echo "Listing tests..."
    all_tests=()
    while IFS= read -r t; do all_tests+=("$t"); done < <(get_all_tests "$pkg")
    echo "Found ${#all_tests[@]} tests"

    # run tests JSON
    echo "Running tests (JSON)..."
    out_file="${pkg//\//_}.json"
    go test -json -timeout="$TIMEOUT" $RACE_FLAG "$pkg" >"$out_file" 2>&1 || true

    # parse results
    echo "Parsing results..."
    ran_tests=()
    while IFS= read -r t; do ran_tests+=("$t"); done < <(
      jq -r 'select(.Package=="'$pkg'" and .Test!=null and (.Action=="run" or .Action=="skip")) | .Test' "$out_file" | sort -u
    )

    all_failed=()
    while IFS= read -r t; do all_failed+=("$t"); done < <(
      jq -r 'select(.Package=="'$pkg'" and .Test!=null and .Action=="fail") | .Test' "$out_file" | sort -u
    )

    # separate known flakes and non-flakes
    non_flakes=()
    flakes=()
    for t in "${all_failed[@]}"; do
      if grep -Fxq "$t" "$KNOWN_FLAKES_FILE"; then
        flakes+=("$pkg::$t")
      else
        non_flakes+=("$pkg::$t")
      fi
    done

    # if any non-flaky failures, exit immediately
    if (( ${#non_flakes[@]} )); then
      echo "Unexpected failures detected (non-flaky): ${non_flakes[*]}"
      exit 1
    fi

    echo "Flaky failures: ${#flakes[@]}"
    FLAKY_TESTS+=("${flakes[@]}")

    # detect missing tests due to panic
    echo "Detecting missing tests..."
    for t in "${all_tests[@]}"; do
      if ! printf '%s\n' "${ran_tests[@]}" | grep -qxF "$t"; then
        MISSING_TESTS+=("$pkg::$t")
      fi
    done
    echo "Missing tests so far: ${#MISSING_TESTS[@]}"
    echo "Package time: $((SECONDS - start_pkg))s"
  done
}

# Main retry loop
targets=()
for ((i=1; i<=MAX_RETRIES; i++)); do
  echo "=== Test attempt #$i ==="
  if (( i == 1 )); then
    run_and_collect ALL_PKGS[@]
  else
    targets=("${FLAKY_TESTS[@]}" "${MISSING_TESTS[@]}")
    # build run regex
    regex=$(printf "%s|" "${targets[@]##*::}")
    regex="^(${regex%|})$"
    echo "Retrying flaky/missing: ${targets[*]}"
    go test -json -timeout="$TIMEOUT" $RACE_FLAG -run "$regex" "${ALL_PKGS[@]}" >retry.json 2>&1 || true
    echo "Parsing retry results..."
    run_and_collect retry.json  # adapt if needed
  fi

  # if no more flakes or missing -> success
  if (( ${#FLAKY_TESTS[@]} == 0 && ${#MISSING_TESTS[@]} == 0 )); then
    echo "✅ All tests passed on attempt #$i"
    rm -f *.json coverage.out
    exit 0
  fi

  echo "Remaining flaky: ${FLAKY_TESTS[*]}"
  echo "Remaining missing: ${MISSING_TESTS[*]}"
  echo "Retrying..."
done

echo "❌ Tests still flaky or missing after $MAX_RETRIES attempts"
exit 1
