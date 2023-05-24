#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

export GOGC=25

# Camino root directory
CAMINOETHVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Initialize dependencies
if [ ! -f caminogo/.git ]; then
    echo "Initializing git submodules..."
    git submodule update --init
fi

# Load the constants
source "$CAMINOETHVM_PATH"/scripts/constants.sh

go test $@ -timeout="30m" -coverprofile="coverage.out" -covermode="atomic" $(go list ./... | grep -v /mocks | grep -v proto | grep -v tests)
