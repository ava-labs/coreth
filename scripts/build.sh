#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

go_version_minimum="1.18.1"

go_version() {
    go version | sed -nE -e 's/[^0-9.]+([0-9.]+).+/\1/p'
}

version_lt() {
    # Return true if $1 is a lower version than than $2,
    local ver1=$1
    local ver2=$2
    # Reverse sort the versions, if the 1st item != ver1 then ver1 < ver2
    if  [[ $(echo -e -n "$ver1\n$ver2\n" | sort -rV | head -n1) != "$ver1" ]]; then
        return 0
    else
        return 1
    fi
}

if version_lt "$(go_version)" "$go_version_minimum"; then
    echo "Caminoethvm requires Go >= $go_version_minimum, Go $(go_version) found." >&2
    exit 1
fi

# Avalanche root directory
CAMINOETHVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the constants
source "$CAMINOETHVM_PATH"/scripts/constants.sh

# Initialize dependencies
if [ ! -f $CAMINOETHVM_PATH/caminogo/.git ]; then
    echo "Initializing git submodules..."
    git --git-dir $CAMINOETHVM_PATH/.git submodule update --init
fi

if [[ $# -eq 1 ]]; then
    binary_path=$1
elif [[ $# -ne 0 ]]; then
    echo "Invalid arguments to build caminoethvm. Requires either no arguments (default) or one arguments to specify binary location."
    exit 1
fi

# Build Caminoethvm, which is run as a subprocess
echo "Building Caminoethvm Version: $caminoethvm_tag; GitCommit: $caminoethvm_commit"
go build -ldflags "-X github.com/ava-labs/coreth/plugin/evm.GitCommit=$caminoethvm_short_commit -X github.com/ava-labs/coreth/plugin/evm.Version=$caminoethvm_tag" -o "$binary_path" "plugin/"*.go
