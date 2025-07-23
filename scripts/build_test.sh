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



# Get all packages to test
PACKAGES=$(go list ./... | grep -v github.com/ava-labs/coreth/tests)

for ((i = 1; i <= MAX_RUNS; i++));
do
    echo "Test run $i of $MAX_RUNS"
    
    # Run tests with JSON output for better tracking
    echo "Running tests..."
    test_output=$(go test -json -shuffle=on ${race:-} -timeout="${TIMEOUT:-600s}" -coverprofile=coverage.out -covermode=atomic "$@" "$PACKAGES" 2>&1) || command_status=$?
    
    # Extract test results for analysis
    echo "$test_output" > test.json
    
    # Debug: Check if JSON output is valid
    echo "Debug: JSON output size: $(echo "$test_output" | wc -l) lines"
    echo "Debug: First few JSON lines:"
    echo "$test_output" | head -5
    
    # Get tests that actually ran and failed
    RAN_TESTS=$(echo "$test_output" | jq -r 'select(.Action == "pass" or .Action == "fail") | .Test' 2>/dev/null | sort || echo "")
    FAILED_TESTS=$(echo "$test_output" | jq -r 'select(.Action == "fail") | .Test' 2>/dev/null | sort || echo "")
    
    # Debug: Show what we found
    echo "Debug: Found $(echo "$RAN_TESTS" | wc -l | tr -d ' ') tests that ran"
    echo "Debug: Found $(echo "$FAILED_TESTS" | wc -l | tr -d ' ') tests that failed"
    if [[ -n "$RAN_TESTS" ]]; then
        echo "Debug: First few ran tests:"
        echo "$RAN_TESTS" | head -3
    fi
    
    # Detect missing tests by analyzing package-level results
    # Check for both complete package failures and partial test failures
    MISSING_TESTS=""
    while IFS= read -r package; do
        [[ -z "$package" ]] && continue
        
        # Check if this package has any tests
        package_has_tests=$(go test -list=".*" "$package" 2>/dev/null | grep -c "^Test" || echo "0")
        
        if [[ "$package_has_tests" -gt 0 ]]; then
            # Get all expected tests for this package
            expected_package_tests=$(go test -list=".*" "$package" 2>/dev/null | grep "^Test" | sed "s|^|$package/|" || true)
            
            # Get tests that actually ran from this package
            ran_package_tests=$(echo "$RAN_TESTS" | grep "^$package/" || true)
            
            # Find missing tests by comparing expected vs ran
            missing_package_tests=$(comm -23 <(echo "$expected_package_tests") <(echo "$ran_package_tests") 2>/dev/null || echo "")
            
            if [[ -n "$missing_package_tests" ]]; then
                missing_count=$(echo "$missing_package_tests" | wc -l | tr -d ' ')
                ran_count=$(echo "$ran_package_tests" | wc -l | tr -d ' ')
                echo "WARNING: Package $package has $package_has_tests tests, $ran_count ran, $missing_count missing - likely due to panic"
                MISSING_TESTS="${MISSING_TESTS}${MISSING_TESTS:+$'\n'}${missing_package_tests}"
            fi
        fi
    done <<< "$PACKAGES"

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
        echo "Retrying $(echo "$TESTS_TO_RETRY" | wc -l) tests..."
        for test in $TESTS_TO_RETRY; do
            package=$(echo "$test" | cut -d'/' -f1)
            test_name=$(echo "$test" | cut -d'/' -f2)
            echo "Retrying $test_name in $package"
            go test -run "^${test_name}$" ${race:-} -timeout="${TIMEOUT:-600s}" "$@" "$package"
        done
    fi
    
    echo "Test run $i failed with known flakes, retrying..."
done

# If we reach here, we have failed all retries
echo "All retry attempts failed"
exit 1
