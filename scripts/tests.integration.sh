#!/usr/bin/env bash

set -euo pipefail

# Run Coreth EVM integration tests against the target version of AvalancheGo

# e.g.,
# ./scripts/tests.integration.sh
# ./scripts/tests.integration.sh --ginkgo.label-filter=x   # All arguments are supplied to ginkgo
if ! [[ "$0" =~ scripts/tests.integration.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Allow configuring the clone path to point to an existing clone
export AVALANCHEGO_CLONE_PATH="${AVALANCHEGO_CLONE_PATH:-avalanchego}"

echo "building integration.test"
# Install the ginkgo binary (required for test build and run)
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.1.4
ACK_GINKGO_RC=true ginkgo build ./tests/integration
./tests/integration/integration.test --help

# Only build avalanchego if using the network fixture
if [[ "${@}" =~ "--use-network-fixture" ]]; then
  ./scripts/build_avalanchego.sh
  export AVALANCHEGO_PATH="$(realpath ${AVALANCHEGO_PATH:-${AVALANCHEGO_CLONE_PATH}/build/avalanchego})"
fi

# Execute in random order to identify unwanted dependency
ginkgo -v --randomize-all ./tests/integration/integration.test -- "${@}"
