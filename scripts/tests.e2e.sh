#!/usr/bin/env bash

set -euo pipefail

# Run AvalancheGo e2e tests from the target version against the current state of coreth.

# e.g.,
# ./scripts/tests.e2e.sh
# AVALANCHE_VERSION=v1.10.x ./scripts/tests.e2e.sh
if ! [[ "$0" =~ scripts/tests.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Allow configuring the clone path to point to an existing clone
export AVALANCHEGO_CLONE_PATH="${AVALANCHEGO_CLONE_PATH:-avalanchego}"

./scripts/build_avalanchego.sh

# Always return to the coreth path on exit
CORETH_PATH="$(echo "${PWD}")"
function cleanup {
  cd "${CORETH_PATH}"
}
trap cleanup EXIT

echo "running AvalancheGo e2e tests"
cd "${AVALANCHEGO_CLONE_PATH}"
E2E_SERIAL=1 ./scripts/tests.e2e.sh --ginkgo.label-filter='c || uses-c'
