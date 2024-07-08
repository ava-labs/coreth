#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Ensure that files not named _test.go files but importing test-related packages
# are constrained with `//go:build test`.

ROOT=$( git rev-parse --show-toplevel )
NON_TEST_GO_FILES=$( find "${ROOT}" -iname '*.go' ! -iname '*_test.go');

IMPORT_TESTING=$( echo "${NON_TEST_GO_FILES}" | xargs grep -lP '^\s*(import\s+)?"testing"');
IMPORT_TESTIFY=$( echo "${NON_TEST_GO_FILES}" | xargs grep -l '"github.com/stretchr/testify');
# TODO(arr4n): send a PR to add support for build tags in `mockgen` and then enable this.
# IMPORT_GOMOCK=$( echo "${NON_TEST_GO_FILES}" | xargs grep -l '"go.uber.org/mock');
HAVE_TEST_LOGIC=$( printf "%s\n%s" "${IMPORT_TESTING}" "${IMPORT_TESTIFY}" );

TAGGED_AS_TEST=$( echo "${NON_TEST_GO_FILES}" | xargs grep -lP '^\/\/go:build\s+(.+(,|\s+))?test[,\s]?');

# -3 suppresses files that have test logic and have the "test" build tag
# -2 suppresses files that are tagged despite not having detectable test logic
UNTAGGED=$( comm -23 <( echo "${HAVE_TEST_LOGIC}" | sort -u ) <( echo "${TAGGED_AS_TEST}" | sort -u ) );
if [ -z "${UNTAGGED}" ];
then
  exit 0;
fi

echo "Non-test Go files importing test-only packages MUST have '//go:build test' tag:";
echo "${UNTAGGED}";
exit 1;