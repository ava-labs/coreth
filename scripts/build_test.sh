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

# MAX_RUNS bounds the attempts to retry individual tests before giving up
MAX_RUNS=4

# Function to extract failed test names from output
extract_failed_tests() {
    local output_file="$1"
    # Extract test failures and panics
    (grep "^--- FAIL" "$output_file" | awk '{print $3}' || grep -E '^\s+Test.+ \(' "$output_file" | awk '{print $1}') |
    sort -u
}

# Function to check if a test is a known flake
is_known_flake() {
    local test_name="$1"
    grep -q "^${test_name}$" ./scripts/known_flakes.txt
}

# Function to check for panics in output
detect_panics() {
    local output_file="$1"
    grep -q "panic:" "$output_file" || grep -q "fatal error:" "$output_file"
}

# Function to get all tests in a package
get_package_tests() {
    local package="$1"
    go test -list=".*" "$package" 2>/dev/null | grep "^Test" || true
}

# Function to get tests that didn't run due to panic
get_missing_tests_after_panic() {
    local output_file="$1"
    local package="$2"
    local failed_tests="$3"
    
    # Get all tests in the package
    local all_tests
    all_tests=$(get_package_tests "$package")
    
    # Find tests that didn't run (not in failed_tests and not in successful output)
    local missing_tests=""
    for test in $all_tests; do
        # Check if test is in failed_tests
        if echo "$failed_tests" | grep -q "$test"; then
            continue
        fi
        
        # Check if test passed (appears in output with PASS)
        if grep -q "PASS.*$test" "$output_file"; then
            continue
        fi
        
        # Test didn't run
        missing_tests="$missing_tests $test"
    done
    
    echo "$missing_tests"
}

# Function to run specific tests and return status
run_specific_test() {
    local test_name="$1"
    local output_file="$2"
    local package="$3"
    go test -shuffle=on ${race:-} -timeout="${TIMEOUT:-600s}" \
      -run "^${test_name}$" "$package" 2>&1 | tee "$output_file"
    return "${PIPESTATUS[0]}"
}

# Initial test run
echo "Running initial test suite..."
# shellcheck disable=SC2046
go test -shuffle=on ${race:-} -timeout="${TIMEOUT:-600s}" -coverprofile=coverage.out -covermode=atomic "$@" $(go list ./... | grep -v github.com/ava-labs/coreth/tests) | tee test.out || command_status=$?

# If initial run passes, exit successfully
if [[ ${command_status:-0} == 0 ]]; then
    rm test.out
    echo "All tests passed on first run!"
    exit 0
fi

# Extract failed tests
failed_tests=$(extract_failed_tests test.out)
echo "Failed tests: $failed_tests"

# Build a map of “test → package” by scanning each failing package just once
failing_packages=$(grep "^FAIL" test.out | awk '{print $2}' | sort -u)
echo "Failing packages: $failing_packages"

declare -A test_pkg_map
for pkg in $failing_packages; do
  for t in $(get_package_tests "$pkg"); do
    if echo "$failed_tests" | grep -x "$t" >/dev/null; then
      test_pkg_map["$t"]="$pkg"
    fi
  done
done

# Check for unexpected failures
unexpected_failures=""
for test in $failed_tests; do
    if ! is_known_flake "$test"; then
        unexpected_failures="$unexpected_failures $test"
    fi
done

if [ -n "$unexpected_failures" ]; then
    echo "Unexpected test failures: $unexpected_failures"
    exit 1
fi

# All failures are known flakes, retry them individually
echo "All failures are known flakes. Retrying failed tests individually..."

# Create a temporary file for retry output
retry_output="retry_test.out"
rm -f "$retry_output"

# Track tests that need retry
tests_to_retry="$failed_tests"

# Retry loop for failed tests
for ((attempt = 1; attempt <= MAX_RUNS; attempt++)); do
    echo "Retry attempt $attempt for tests: $tests_to_retry"
    
    still_failing=""
    panic_affected_packages=""
    
    # Retry each failed test
    for test in $tests_to_retry; do
        pkg="${test_pkg_map[$test]:-}"
        if [[ -z "$pkg" ]]; then
            echo "ERROR: no package mapping for test $test" >&2
            exit 1
        fi
        echo "Retrying test: $test in package: $pkg"
        run_specific_test "$test" "$retry_output" "$pkg"
        test_status=$?
        
        # Check if test passed
        if [[ $test_status == 0 ]]; then
            echo "Test $test passed on attempt $attempt"
            continue
        fi
        
        # Check if this is an unexpected failure
        if ! is_known_flake "$test"; then
            echo "Test $test failed and is not a known flake"
            exit 1
        fi
        
        # Check for panics
        if detect_panics "$retry_output"; then
            echo "Test $test panicked on attempt $attempt"
            package="${test%.*}"
            panic_affected_packages="$panic_affected_packages $package"
            
            # Get tests that didn't run due to panic
            missing_tests=$(get_missing_tests_after_panic "$retry_output" "$package" "$test")
            if [ -n "$missing_tests" ]; then
                echo "Tests that didn't run due to panic: $missing_tests"
                # Add missing tests to retry list
                for missing_test in $missing_tests; do
                    if ! echo "$tests_to_retry" | grep -q "$missing_test"; then
                        tests_to_retry="$tests_to_retry $missing_test"
                    fi
                done
            fi
        fi
        
        # Test still failing
        still_failing="$still_failing $test"
    done
    
    # Update tests to retry for next iteration
    tests_to_retry="$still_failing"
    
    # If no tests are still failing, we're done
    if [ -z "$tests_to_retry" ]; then
        echo "All tests passed after retries!"
        rm -f test.out "$retry_output"
        exit 0
    fi
    
    echo "Tests still failing after attempt $attempt: $tests_to_retry"
    
    # If this was the last attempt, fail
    if [[ $attempt == "$MAX_RUNS" ]]; then
        echo "Tests failed all $MAX_RUNS attempts: $tests_to_retry"
        exit 1
    fi
done

echo "All known flaky tests passed after retries!"
rm -f test.out "$retry_output"
exit 0 
