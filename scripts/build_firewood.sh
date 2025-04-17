#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Coreth imports Firewood via CGO bindings. Since Firewood is implemented in Rust, the golang
# compiler cannot generate the Firewood shared library itself. Unlike with a normal CGO library
# where the golang compiler can fall back to the C compiler available via $(go env CC) to compile C
# code and then include within the golang binary, it has no fallback for Rust.
# This necessitates a two-step build process, where Coreth can only build correctly when it points to
# pre-built Firewood shared library with CGO compatible bindings.
FIREWOOD_REMOTE=${FIREWOOD_REMOTE:-"git@github.com:ava-labs/firewood.git"}
FIREWOOD_PATH=${FIREWOOD_PATH:-"./build/firewood"}
FIREWOOD_VERSION=${FIREWOOD_VERSION:-"main"}
FIREWOOD_IGNORE_CHECKOUT=${FIREWOOD_IGNORE_CHECKOUT:-"false"}

import_firewood() {
    local original_dir=$(pwd)
    local firewood_remote="${1}"
    local firewood_path="${2}"
    local firewood_version="${3}"
    if [[ -d ${firewood_path} ]]; then
        echo "Firewood repo already exists: ${firewood_path}"
    else
        echo "Cloning Firewood from GitHub..."
        git clone "${firewood_remote}" "${firewood_path}"
    fi

    cd "${firewood_path}" || { echo "Failed to enter firewood_path: ${firewood_path}"; return 1; }
    git fetch origin ${firewood_version}
    git checkout ${firewood_version}
    git reset --hard origin/${firewood_version}
    cd ${original_dir} || { echo "Failed to return to original directory: ${original_dir}"; return 1; }
    echo "Firewood repo is at version: $(git -C ${firewood_path} rev-parse HEAD)"
}

# Takes 1 positional argument for the firewood path
build_firewood() {
    local original_dir
    original_dir=$(pwd) # Save the current directory
    cd "${1}" || { echo "Failed to enter FIREWOOD_PATH: ${1}"; return 1; }

    # Run cargo build
    cargo build --release --features ethhash
    local build_status=$?

    # Return to the original directory
    cd "${original_dir}" || { echo "Failed to return to original directory: ${original_dir}"; return 1; }

    return ${build_status} # Return the status of the cargo build
}

if [[ "${FIREWOOD_IGNORE_CHECKOUT}" == "false" ]]; then
    echo "Checking out Firewood..."
    import_firewood ${FIREWOOD_REMOTE} ${FIREWOOD_PATH} ${FIREWOOD_VERSION}
else
    echo "Skipping Firewood checkout..."
fi

echo "Building Firewood in ${FIREWOOD_PATH}..."
build_firewood "${FIREWOOD_PATH}"
go mod edit -replace github.com/ava-labs/firewood/ffi/v2="${FIREWOOD_PATH}/ffi"
