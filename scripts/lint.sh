#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/lint.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi


# Ensure bash>=4 for mapfile support; on macOS, re-exec with Homebrew bash if available
if ! declare -F mapfile >/dev/null 2>&1; then
  if [[ "$(uname -s)" == "Darwin" ]]; then
    if command -v brew >/dev/null 2>&1; then
      HB_BASH="$(brew --prefix bash 2>/dev/null)/bin/bash"
      if [[ -x "${HB_BASH}" ]]; then
        exec "${HB_BASH}" "$0" "$@"
      fi
    fi
    for p in /opt/homebrew/bin/bash /usr/local/bin/bash; do
      if [[ -x "$p" ]]; then
        exec "$p" "$0" "$@"
      fi
    done
    echo >&2 "error: bash>=4 required. Install with: brew install bash"
    exit 255
  fi
fi

# The -P option is not supported by the grep version installed by
# default on macos. Since `-o errexit` is ignored in an if
# conditional, triggering the problem here ensures script failure when
# using an unsupported version of grep.
if ! grep -P 'lint.sh' scripts/lint.sh &>/dev/null; then
  if [[ "$(uname -s)" == "Darwin" ]]; then
    # On macOS, try Homebrew gnubin path and re-check
    if command -v brew >/dev/null 2>&1; then
      GNUGREP_PATH="$(brew --prefix grep)/libexec/gnubin"
      if [[ -d "${GNUGREP_PATH}" ]]; then
        export PATH="${GNUGREP_PATH}:${PATH}"
      fi
    fi
    if ! grep -P 'lint.sh' scripts/lint.sh &>/dev/null; then
      echo >&2 "error: This script requires a recent version of gnu grep."
      echo >&2 "       On macOS, install with: brew install grep"
      echo >&2 "       Ensure GNU grep is in PATH (e.g., \"\$(brew --prefix grep)/libexec/gnubin\")."
      exit 255
    fi
  fi
fi

# Prefer pre-installed binaries and only fall back to `go run` if missing.
# This avoids repeated module downloads/compilation in pre-commit/pre-push subshells.
run_or_go() {
  local _tool="$1"
  local _module_at_version="$2"
  shift 2
  if command -v "${_tool}" >/dev/null 2>&1; then
    "${_tool}" "$@"
  else
    go run "${_module_at_version}" "$@"
  fi
}

# Read excluded directories into arrays
DEFAULT_FILES=()
UPSTREAM_FILES=()
function read_dirs {
  local upstream_folders_file="./scripts/upstream_files.txt"
  # Read the upstream_folders file into an array
  local -a upstream_folders=()
  mapfile -t upstream_folders < "$upstream_folders_file"

  # Shared find filters
  local -a find_filters=(
    -type f
    -name '*.go'
    ! -name '*.pb.go'
    ! -name 'mock_*.go'
    ! -name 'mocks_*.go'
    ! -name 'mocks.go'
    ! -name 'mock.go'
    ! -name 'gen_*.go'
    ! -path './**/*mock/*.go'
  )

  # Combined loop: build both upstream licensed find and exclude args
  local -a upstream_find_args=()
  local -a upstream_exclude_args=()
  for line in "${upstream_folders[@]}"; do
    # Skip empty or whitespace-only lines to avoid matching the entire repo
    [[ -z "${line//[[:space:]]/}" ]] && continue
    if [[ "$line" == !* ]]; then
      # Excluding files with !
      upstream_exclude_args+=(! -path "./${line:1}")
    else
      upstream_find_args+=(-path "./${line}" -o)
    fi
  done
  # Remove the last '-o' from the arrays
  unset 'upstream_find_args[${#upstream_find_args[@]}-1]'

  # Find upstream files
  mapfile -t UPSTREAM_FILES < <(
    find . \
      \( "${upstream_find_args[@]}" \) \
      -a \( "${find_filters[@]}" \) \
      "${upstream_exclude_args[@]}"
  )

  # Build exclusion args from upstream files
  default_exclude_args=()
  for f in "${UPSTREAM_FILES[@]}"; do
    default_exclude_args+=(! -path "$f")
  done

  # Now find default files (exclude already licensed ones)
  mapfile -t DEFAULT_FILES < <(find . "${find_filters[@]}" "${default_exclude_args[@]}")
}

# by default, "./scripts/lint.sh" runs all linft tests
# to run only "license_header" test
# TESTS='license_header' ./scripts/lint.sh
TESTS=${TESTS:-"golangci_lint license_header require_error_is_no_funcs_as_params single_import interface_compliance_nil require_no_error_inline_func import_testing_only_in_tests"}

function test_golangci_lint {
  run_or_go "golangci-lint" "github.com/golangci/golangci-lint/cmd/golangci-lint@v1.63" run --config .golangci.yml
}

# automatically checks license headers
# to modify the file headers (if missing), remove "--verify" flag
# TESTS='license_header' ADDLICENSE_FLAGS="--debug" ./scripts/lint.sh
_addlicense_flags=${ADDLICENSE_FLAGS:-"--verify --debug"}
function test_license_header {
  # Run license tool
  if [[ ${#UPSTREAM_FILES[@]} -gt 0 ]]; then
    echo "Running license tool on upstream files with header for upstream..."
    # shellcheck disable=SC2086
    run_or_go "go-license" "github.com/palantir/go-license@v1.25.0" \
      --config=./license_header_for_upstream.yml \
      ${_addlicense_flags} \
      "${UPSTREAM_FILES[@]}" \
      || return 1
  fi

  if [[ ${#DEFAULT_FILES[@]} -gt 0 ]]; then
    echo "Running license tool on remaining files with default header..."
    # shellcheck disable=SC2086
    run_or_go "go-license" "github.com/palantir/go-license@v1.25.0" \
      --config=./license_header.yml \
      ${_addlicense_flags} \
      "${DEFAULT_FILES[@]}" \
      || return 1
  fi
}

function test_single_import {
  if grep -R -zo -P 'import \(\n\t".*"\n\)' "${DEFAULT_FILES[@]}"; then
    echo ""
    return 1
  fi
}

function test_require_error_is_no_funcs_as_params {
  if grep -R -zo -P 'require.ErrorIs\(.+?\)[^\n]*\)\n' "${DEFAULT_FILES[@]}"; then
    echo ""
    return 1
  fi
}

function test_require_no_error_inline_func {
  if grep -R -zo -P '\t+err :?= ((?!require|if).|\n)*require\.NoError\((t, )?err\)' "${DEFAULT_FILES[@]}"; then
    echo ""
    echo "Checking that a function with a single error return doesn't error should be done in-line."
    echo ""
    return 1
  fi
}

# Ref: https://go.dev/doc/effective_go#blank_implements
function test_interface_compliance_nil {
  if grep -R -o -P '_ .+? = &.+?\{\}' "${DEFAULT_FILES[@]}"; then
    echo ""
    echo "Interface compliance checks need to be of the form:"
    echo "  var _ json.Marshaler = (*RawMessage)(nil)"
    echo ""
    return 1
  fi
}

function test_import_testing_only_in_tests {
  NON_TEST_GO_FILES=$(
    echo "${DEFAULT_FILES[@]}" | tr ' ' '\n' |
      grep -i '\.go$' |
      grep -vi '_test\.go$' |
      grep -v '^./tests/'
  )

  IMPORT_TESTING=$(echo "${NON_TEST_GO_FILES}" | xargs grep -lP '^\s*(import\s+)?"testing"')
  IMPORT_TESTIFY=$(echo "${NON_TEST_GO_FILES}" | xargs grep -l '"github.com/stretchr/testify')
  IMPORT_FROM_TESTS=$(echo "${NON_TEST_GO_FILES}" | xargs grep -l '"github.com/ava-labs/coreth/tests/')
  IMPORT_TEST_PKG=$(echo "${NON_TEST_GO_FILES}" | xargs grep -lP '"github.com/ava-labs/coreth/.*?test"')

  # TODO(arr4n): send a PR to add support for build tags in `mockgen` and then enable this.
  # IMPORT_GOMOCK=$( echo "${NON_TEST_GO_FILES}" | xargs grep -l '"go.uber.org/mock');
  HAVE_TEST_LOGIC=$(printf "%s\n%s\n%s\n%s" "${IMPORT_TESTING}" "${IMPORT_TESTIFY}" "${IMPORT_FROM_TESTS}" "${IMPORT_TEST_PKG}")

  IN_TEST_PKG=$(echo "${NON_TEST_GO_FILES}" | grep -P '.*test/[^/]+\.go$') # directory (hence package name) ends in "test"
  # Files in /tests/ are already excluded by the `find ... ! -path`
  INTENDED_FOR_TESTING="${IN_TEST_PKG}"

  # -3 suppresses files that have test logic and have the "test" build tag
  # -2 suppresses files that are tagged despite not having detectable test logic
  UNTAGGED=$(comm -23 <(echo "${HAVE_TEST_LOGIC}" | sort -u) <(echo "${INTENDED_FOR_TESTING}" | sort -u))
  if [ -z "${UNTAGGED}" ]; then
    return 0
  fi

  echo 'Non-test Go files importing test-only packages MUST (a) be in *test package; or (b) be in /tests/ directory:'
  echo "${UNTAGGED}"
  return 1
}

function run {
  local test="${1}"
  shift 1
  echo "START: '${test}' at $(date)"
  if "test_${test}" "$@"; then
    echo "SUCCESS: '${test}' completed at $(date)"
  else
    echo "FAIL: '${test}' failed at $(date)"
    exit 255
  fi
}

echo "Running '$TESTS' at: $(date)"
read_dirs
for test in $TESTS; do
  run "${test}" "${DEFAULT_FILES[@]}"
done

echo "ALL SUCCESS!"
