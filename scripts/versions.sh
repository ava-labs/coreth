#!/usr/bin/env bash

# Set up the versions to be used
coreth_version=${CORETH_VERSION:-'v1.1.0'} # caminoethvm version
# Don't export them as they're used in the context of other calls
avalanche_version=${AVALANCHE_VERSION:-'v1.0.0-rc1'} # caminogo version
