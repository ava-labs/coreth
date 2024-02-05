#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

export GOGC=25

# Camino root directory
CAMINOETHVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
# export CGO_CFLAGS="-O -D__BLST_PORTABLE__"

# Load the constants
source "$CAMINOETHVM_PATH"/scripts/constants.sh

go test $@ -timeout="30m" -coverprofile="coverage.out" -covermode="atomic" $(go list ./... | grep -v /mocks | grep -v proto | grep -v tests)
