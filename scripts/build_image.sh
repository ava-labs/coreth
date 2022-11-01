#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Avalanche root directory
CAMINOETHVM_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the versions
source "$CAMINOETHVM_PATH"/scripts/versions.sh

# Load the constants
source "$CAMINOETHVM_PATH"/scripts/constants.sh

echo "Building Docker Image: $dockerhub_repo:$build_image_id based of $camino_version"
docker build -t "$dockerhub_repo:$build_image_id" "$CAMINOETHVM_PATH" -f "$CAMINOETHVM_PATH/Dockerfile" \
  --build-arg CAMINO_VERSION="$camino_version" \
  --build-arg CAMINOETHVM_COMMIT="$caminoethvm_commit" \
  --build-arg CURRENT_BRANCH="$current_branch"
