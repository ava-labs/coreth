#!/usr/bin/env bash

set -euo pipefail

# Run AvalancheGo e2e tests from the target version against the current state of coreth.

# e.g.,
# ./scripts/tests.e2e.sh
# AVALANCHE_VERSION=v1.10.x ./scripts/tests.e2e.sh

# Coreth root directory
CORETH_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the version
source "$CORETH_PATH"/scripts/versions.sh

echo "checking out target AvalancheGo version ${avalanche_version}"
if [[ -d avalanchego ]]; then
  echo "updating existing clone"
  cd avalanchego
  git pull
  git checkout -B "${avalanche_version}"
else
  echo "creating new clone"
  git clone -b "${avalanche_version}" --single-branch https://github.com/ava-labs/avalanchego.git
  cd avalanchego
fi

echo "updating coreth dependency to point to local path"
go mod edit -replace github.com/ava-labs/coreth=../
go mod tidy

echo "building with race detection"
./scripts/build.sh -r

echo "running AvalancheGo e2e tests"
./scripts/tests.e2e.sh ./build/avalanchego
