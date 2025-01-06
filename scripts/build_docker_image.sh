#!/usr/bin/env bash

set -euo pipefail

# If set to non-empty, prompts the building of a multi-arch image when the image
# name indicates use of a registry.
#
# A registry is required to build a multi-arch image since a multi-arch image is
# not really an image at all. A multi-arch image (also called a manifest) is
# basically a list of arch-specific images available from the same registry that
# hosts the manifest. Manifests are not supported for local images.
#
# Reference: https://docs.docker.com/build/building/multi-platform/
PLATFORMS="${PLATFORMS:-}"

# If set to non-empty, the image will be published to the registry.
PUBLISH="${PUBLISH:-}"

# Directory above this script
CORETH_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

# Load the constants
source "$CORETH_PATH"/scripts/constants.sh

# Load the versions
source "$CORETH_PATH"/scripts/versions.sh

# WARNING: this will use the most recent commit even if there are un-committed changes present
BUILD_IMAGE_ID=${BUILD_IMAGE_ID:-"${CURRENT_BRANCH}"}

# buildx (BuildKit) improves the speed and UI of builds over the legacy builder and
# simplifies creation of multi-arch images.
#
# Reference: https://docs.docker.com/build/buildkit/
DOCKER_CMD="docker buildx build"

if [[ -n "${PUBLISH}" ]]; then
  DOCKER_CMD="${DOCKER_CMD} --push"

  echo "Pushing $DOCKERHUB_REPO:$BUILD_IMAGE_ID"

  # A populated DOCKER_USERNAME env var triggers login
  if [[ -n "${DOCKER_USERNAME:-}" ]]; then
    echo "$DOCKER_PASS" | docker login --username "$DOCKER_USERNAME" --password-stdin
  fi
fi

# Build a multi-arch image if requested
if [[ -n "${PLATFORMS}" ]]; then
  DOCKER_CMD="${DOCKER_CMD} --platform=${PLATFORMS}"
fi

echo "Building Docker Image: $DOCKERHUB_REPO:$BUILD_IMAGE_ID based of AvalancheGo@$AVALANCHE_VERSION"
${DOCKER_CMD} -t "$DOCKERHUB_REPO:$BUILD_IMAGE_ID" "$CORETH_PATH" -f "$CORETH_PATH/Dockerfile" \
  --build-arg AVALANCHE_VERSION="$AVALANCHE_VERSION" \
  --build-arg CORETH_COMMIT="$CORETH_COMMIT" \
  --build-arg CURRENT_BRANCH="$CURRENT_BRANCH"
