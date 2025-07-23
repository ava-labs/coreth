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

# Function to get all test names from packages
get_all_tests() {
    local packages="$1"
    go test -list=".*" ${race:-} "$@" $packages 2>/dev/null | grep "^Test" | sort
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
    test_output=$(go test -json -shuffle=on ${race:-} -timeout="${TIMEOUT:-600s}" -coverprofile=coverage.out -covermode=atomic "$@" $PACKAGES 2>&1) || command_status=$?
    
    # Extract test results for analysis
    echo "$test_output" > test.json
    
    # Get tests that actually ran and failed
    RAN_TESTS=$(echo "$test_output" | jq -r 'select(.Action == "pass" or .Action == "fail") | .Test' | sort)
    FAILED_TESTS=$(echo "$test_output" | jq -r 'select(.Action == "fail") | .Test' | sort)
    
    # Check if all expected tests ran
    MISSING_TESTS=$(comm -23 <(echo "$EXPECTED_TESTS") <(echo "$RAN_TESTS"))
    
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
    unexpected_failures=$(comm -23 <(echo "$FAILED_TESTS") <(sed 's/\r$//' ./scripts/known_flakes.txt))
    if [ -n "${unexpected_failures}" ]; then
        echo "Unexpected test failures: ${unexpected_failures}"
        exit 1
    fi

    # If only known flakes failed and no tests were missing, we can be more efficient
    if [[ -z "$MISSING_TESTS" ]]; then
        echo "Only known flakes failed and all tests ran. Retrying only failed tests..."
        # Run only the failed tests for efficiency
        for test in $FAILED_TESTS; do
            package=$(echo "$test" | cut -d'/' -f1)
            test_name=$(echo "$test" | cut -d'/' -f2)
            echo "Retrying $test_name in $package"
            go test -run "^${test_name}$" ${race:-} -timeout="${TIMEOUT:-600s}" "$@" $package
        done
    else
        echo "Some tests did not run, retrying entire suite..."
    fi
    
    echo "Test run $i failed with known flakes, retrying..."
done

# If we reach here, we have failed all retries
echo "All retry attempts failed"
exit 1
