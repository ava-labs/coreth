#!/usr/bin/env bash

set -euo pipefail

# e.g.,
# ./scripts/tests.integration.sh
# ./scripts/tests.e2e.sh --ginkgo.label-filter=x                                       # All arguments are supplied to gi
if ! [[ "$0" =~ scripts/tests.integration.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

echo "building integration.test"
# Install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.4
ACK_GINKGO_RC=true ginkgo build ./tests/integration
./tests/integration/integration.test --help

# Execute in random order to identify unwanted dependency
ginkgo -v --randomize-all ./tests/integration/integration.test -- "${@}"
