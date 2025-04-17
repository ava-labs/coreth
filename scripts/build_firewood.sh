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

check_prerequisites() {
    returnval=0
    for exe in "$@"; do
        if ! command -v "$exe" >/dev/null 2>&1; then
            echo "❌ $exe is not on your path"
            returnval=1
        fi
    done

    return $returnval
}

import_firewood() {
    local firewood_remote="${1}"
    local firewood_path="${2}"
    local firewood_version="${3}"
    if [[ -d ${firewood_path} ]]; then
        echo "✅ Firewood repo directory already exists at ${firewood_path}"
        if ! [[ -f "${firewood_path}/Cargo.toml" ]]; then
            echo "❌ The directory doesn't contain a valid Cargo.toml"
            return 1
        fi
    else
        echo "Cloning Firewood from ${firewood_remote}..."
        git clone "${firewood_remote}" "${firewood_path}"
    fi

    pushd "${firewood_path}" || { echo "Failed to enter firewood_path: ${firewood_path}"; return 1; }
    if ! [[ -z $(git status --porcelain) ]]; then
        echo "❌ Refusing to touch a changed repository; please stash or commit your changes first"
        return 1
    fi

    git fetch origin "${firewood_version}" || return 1
    git checkout "${firewood_version}" || return 1
    git reset --hard "origin/${firewood_version}" || return 1
    popd

    echo "Firewood repo is at version: $(git -C ${firewood_path} rev-parse HEAD)"
}

# Takes 1 positional argument for the firewood path
build_firewood() {
    pushd "${1}" || { echo "Failed to enter FIREWOOD_PATH: ${1}"; return 1; }

    # Run cargo build
    cargo build --release --features ethhash
    local build_status=$?

    # Return to the original directory
    popd

    return ${build_status} # Return the status of the cargo build
}

check_prerequisites cargo git make cc protoc || exit 1

if [[ "${FIREWOOD_IGNORE_CHECKOUT}" == "false" ]]; then
    echo "Checking out Firewood..."
    import_firewood ${FIREWOOD_REMOTE} ${FIREWOOD_PATH} ${FIREWOOD_VERSION} || exit 1
else
    echo "Skipping Firewood checkout..."
fi

echo "Building Firewood in ${FIREWOOD_PATH}..."
build_firewood "${FIREWOOD_PATH}"
go mod edit -replace github.com/ava-labs/firewood/ffi/v2="${FIREWOOD_PATH}/ffi"
