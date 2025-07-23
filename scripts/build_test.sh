#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Avalanche root directory
CORETH_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Load the constants
source "$CORETH_PATH"/scripts/constants.sh

# We pass in the arguments to this script directly to enable easily passing parameters such as enabling race detection,
# parallelism, and test coverage.
# DO NOT RUN tests from the top level "tests" directory since they are run by ginkgo
race="-race"
if [[ -n "${NO_RACE:-}" ]]; then
    race=""
fi

# MAX_RUNS bounds the attempts to retry the tests before giving up
# This is useful for flaky tests
MAX_RUNS=4

get_all_tests() {
    local packages="$1"
    # Map import paths → directories matching your build tags/platform
    local dirs
    dirs=$(printf '%s\n' "$packages" | xargs go list -f '{{.Dir}}')
    # Grep for top-level Test functions in *_test.go
    grep -R --include '*_test.go' -E '^[[:space:]]*func[[:space:]]+(Test[[:alnum:]_]+)\(' \
         "$dirs" |
    sed -E 's/^.*func[[:space:]]+(Test[[:alnum:]_]+).*/\1/' |
    sort -u
}

# Get all packages to test
PACKAGES=$(go list ./... | grep -v github.com/ava-labs/coreth/tests)

for ((i = 1; i <= MAX_RUNS; i++));
do
    echo "Test run $i of $MAX_RUNS"
    
    # Get expected tests (for comparison) on first run
    if [[ $i -eq 1 ]]; then
        echo "Getting expected test list..."
        EXPECTED_TESTS=$(get_all_tests "$PACKAGES")
        echo "Expected tests: $(echo "$EXPECTED_TESTS" | wc -l | tr -d ' ') tests"
    fi
    
    # Run tests with JSON output for better tracking
    echo "Running tests..."
    test_output=$(go test -json -shuffle=on ${race:-} -timeout="${TIMEOUT:-600s}" -coverprofile=coverage.out -covermode=atomic "$@" "$PACKAGES" 2>&1) || command_status=$?
    
    # Extract test results for analysis
    echo "$test_output" > test.json
    
    # Get tests that actually ran (including panics) and skipped
    RAN_TESTS=$(echo "$test_output" \
        | jq -r '
            select(
              .Action == "run"
              or .Action == "pass"
              or .Action == "fail"
              or .Action == "skip"
            )
            | .Test
          ' 2>/dev/null \
        | sort || echo "")
 
    # Get tests that failed
    FAILED_TESTS=$(echo "$test_output" | jq -r 'select(.Action == "fail") | .Test' 2>/dev/null | sort || echo "")
    
    # Check if all expected tests ran
    MISSING_TESTS=$(comm -23 <(echo "$EXPECTED_TESTS") <(echo "$RAN_TESTS") 2>/dev/null || echo "")
    
    if [[ -n "$MISSING_TESTS" ]]; then
        echo "WARNING: Some tests did not run due to panics or other issues:"
        echo "$MISSING_TESTS"
    fi

    # If the test passed, exit
    if [[ ${command_status:-0} == 0 ]]; then
        echo "All tests passed!"
        rm -f test.json test.out
        exit 0
    else 
        unset command_status # Clear the error code for the next run
    fi

    # Check for unexpected failures
    unexpected_failures=$(comm -23 <(echo "$FAILED_TESTS") <(sed 's/\r$//' ./scripts/known_flakes.txt) 2>/dev/null || echo "")
    if [ -n "${unexpected_failures}" ]; then
        echo "Unexpected test failures: ${unexpected_failures}"
        exit 1
    fi

    # Determine which tests to retry based on what happened
    TESTS_TO_RETRY=""
    
    if [[ -z "$MISSING_TESTS" ]]; then
        # All tests ran, only retry the failed ones
        echo "All tests ran successfully. Retrying only failed tests..."
        TESTS_TO_RETRY="$FAILED_TESTS"
    else
        # Some tests didn't run due to panics, retry missing + failed tests
        echo "Some tests did not run due to panics. Retrying missing and failed tests..."
        TESTS_TO_RETRY=$(echo -e "$MISSING_TESTS\n$FAILED_TESTS" | sort -u)
    fi
    
    # Retry the specific tests that need it
    if [[ -n "$TESTS_TO_RETRY" ]]; then
        echo "Retrying tests: $TESTS_TO_RETRY"
        for test in $TESTS_TO_RETRY; do
            echo "Retrying $test"
            # find the package directory containing that test
            pkg_dir=$(grep -R --include='*_test.go' -l "func[[:space:]]\+${test}\(" "$CORETH_PATH" | head -n1)
            if [[ -n "$pkg_dir" ]]; then
              pkg=$(dirname "$pkg_dir" | xargs go list -f '{{.ImportPath}}' 2>/dev/null)
            else
              pkg="$PACKAGES"
            fi
            echo " → in package $pkg"
            # run only that test in its package
            go test -run "^${test}$" ${race:-} -timeout="${TIMEOUT:-600s}" "$@" "$pkg"
        done
    fi
    
    echo "Test run $i failed with known flakes, retrying..."
done

# If we reach here, we have failed all retries
echo "All retry attempts failed"
exit 1
