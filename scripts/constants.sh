#!/usr/bin/env bash

# Set the PATHS
GOPATH="$(go env GOPATH)"

# Set binary location
binary_path=${CAMINOETHVM_BINARY_PATH:-"$GOPATH/src/github.com/chain4travel/camino-node/build/plugins/evm"}

# Avalabs docker hub
dockerhub_repo="c4tplatform/caminoethvm"

CAMINOETHVM_TAG=${CAMINOETHVM_TAG:-${GITHUB_REF_NAME:-""}}

# Image build id
caminoethvm_commit=${CAMINOETHVM_COMMIT:-$( git rev-list -1 HEAD )}
# Use an abbreviated version of the full commit to tag the image.
caminoethvm_short_commit="${caminoethvm_commit::8}"
caminoethvm_tag=${CAMINOETHVM_TAG:-$( git describe --tags )}
echo "Using tag: ${caminoethvm_tag}"

# caminogo version
module=$(grep caminogo $CAMINOETHVM_PATH/go.mod)
# trim leading
module="${module#"${module%%[![:space:]]*}"}"
t=(${module//\ / })
caminogo_tag=${t[1]}

build_image_id=${BUILD_IMAGE_ID:-"$caminogo_tag-$caminoethvm_short_commit"}

# Set the CGO flags to use the portable version of BLST
#
# We use "export" here instead of just setting a bash variable because we need
# to pass this flag to all child processes spawned by the shell.
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"
