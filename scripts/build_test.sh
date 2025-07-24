#!/usr/bin/env bash
set -euo pipefail

CORETH_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"
source "$CORETH_ROOT/scripts/constants.sh"

NO_RACE="${NO_RACE:-}"
RACE_FLAG=""
(( NO_RACE )) || RACE_FLAG="-race"
TIMEOUT="${TIMEOUT:-600s}"
MAX_RETRIES=4
ALL_PKGS=( $(go list ./... | grep -v github.com/ava-labs/coreth/tests) )

run_and_collect() {
  local pkgs=("${!1}")
  FAILED_TESTS=()
  MISSING_TESTS=()

  # Iterate per-package so we can list tests
  for pkg in "${pkgs[@]}"; do
    # 1) list all tests in this pkg
    mapfile -t all_tests < <(go test -list . "$pkg" | grep '^Test')

    # 2) run tests with JSON output
    go test -json -timeout="$TIMEOUT" $RACE_FLAG "$pkg" >"${pkg//\//_}.json" 2>&1 || true

    # 3) collect which tests ran & failed
    mapfile -t ran_tests < <(
      jq -r 'select(.Package=="'"$pkg"'" and .Test!=null and .Action=="run") | "\(.Test)"' \
        "${pkg//\//_}.json" | sort -u
    )
    mapfile -t failed_tests < <(
      jq -r 'select(.Package=="'"$pkg"'" and .Test!=null and .Action=="fail") | "\(.Test)"' \
        "${pkg//\//_}.json" | sort -u
    )

    # 4) record failures
    for t in "${failed_tests[@]}"; do
      FAILED_TESTS+=("$pkg::$t")
    done

    # 5) detect missing (panic/skipped) tests
    for t in "${all_tests[@]}"; do
      if ! printf '%s\n' "${ran_tests[@]}" | grep -qxF "$t"; then
        MISSING_TESTS+=("$pkg::$t")
      fi
    done
  done
}

# Main retry loop
TARGET_PKGS=( "${ALL_PKGS[@]}" )
for ((i=1; i<=MAX_RETRIES; i++)); do
  echo ">>> Attempt #$i"
  run_and_collect TARGET_PKGS[@]

  if [[ ${#FAILED_TESTS[@]} -eq 0 && ${#MISSING_TESTS[@]} -eq 0 ]]; then
    echo "✅ All tests passed!"
    exit 0
  fi

  echo "Will retry the following tests:"
  printf '  %s\n' "${FAILED_TESTS[@]}" "${MISSING_TESTS[@]}"

  # Next pass: run only those specific tests
  TARGET_PKGS=()  # not needed when rerunning individual tests
  # build a regexp for -run
  RUN_REGEX=$(printf "%s|" "${FAILED_TESTS[@]##*::}" "${MISSING_TESTS[@]##*::}")
  RUN_REGEX="^(${RUN_REGEX%|})\$"

  # re-run them all together
  go test -json -timeout="$TIMEOUT" $RACE_FLAG -run "$RUN_REGEX" \
    "${ALL_PKGS[@]}" >retry.json 2>&1 || true

  # parse retry.json again… (you can refactor run_and_collect to accept a file)
done

echo "❌ Tests still failing after $MAX_RETRIES retries"
exit 1
