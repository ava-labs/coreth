#!/usr/bin/env bash

# Set the PATHS
GOPATH="$(go env GOPATH)"

# Set binary location
binary_path=${CAMINOETHVM_BINARY_PATH:-"$GOPATH/src/github.com/chain4travel/caminogo/build/plugins/evm"}

# Avalabs docker hub
dockerhub_repo="avaplatform/caminogo"

# Current branch
current_branch=${CURRENT_BRANCH:-$(git describe --tags --exact-match 2> /dev/null || git symbolic-ref -q --short HEAD || git rev-parse --short HEAD)}
echo "Using branch: ${current_branch}"

# Image build id
# Use an abbreviated version of the full commit to tag the image.

# WARNING: this will use the most recent commit even if there are un-committed changes present
caminoethvm_commit="$(git --git-dir="$CAMINOETHVM_PATH/.git" rev-parse HEAD)"
caminoethvm_commit_id="${caminoethvm_commit::8}"

build_image_id=${BUILD_IMAGE_ID:-"$camino_version-$caminoethvm_commit_id"}

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"
