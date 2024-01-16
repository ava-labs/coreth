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

# Coreth root directory
CORETH_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Allow configuring the clone path to point to an existing clone
AVALANCHEGO_CLONE_PATH="${AVALANCHEGO_CLONE_PATH:-avalanchego}"

# Load the version
source "$CORETH_PATH"/scripts/versions.sh

# Always return to the coreth path on exit
function cleanup {
  cd "${CORETH_PATH}"
}
trap cleanup EXIT

echo "checking out target AvalancheGo version ${avalanche_version}"
if [[ -d "${AVALANCHEGO_CLONE_PATH}" ]]; then
  echo "updating existing clone"
else
  echo "creating new clone"
  git clone https://github.com/ava-labs/avalanchego.git "${AVALANCHEGO_CLONE_PATH}"
fi

cd "${AVALANCHEGO_CLONE_PATH}"
git fetch origin ${avalanche_version}

ref=""
# check if given reference is a tag or branch
if git rev-parse --quiet --verify "origin/${avalanche_version}^{commit}" >/dev/null; then
  echo "checking out branch ${avalanche_version}"
  ref="origin/${avalanche_version}"
  git checkout -B "test-${avalanche_version}" "origin/${avalanche_version}"
else
  echo "checking out tag ${avalanche_version}"
  ref="${avalanche_version}"
fi

git checkout -B "test-${avalanche_version}" "${ref}"

echo "updating coreth dependency to point to ${CORETH_PATH}"
go mod edit -replace "github.com/ava-labs/coreth=${CORETH_PATH}"
go mod tidy

echo "building avalanchego"
./scripts/build.sh -r

echo "running AvalancheGo e2e tests"
E2E_SERIAL=1 ./scripts/tests.e2e.sh --ginkgo.label-filter='c || uses-c'
