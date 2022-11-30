#!/usr/bin/env bash

# Set up the versions to be used
coreth_version=${CORETH_VERSION:-'v0.11.4'}
# Don't export them as they're used in the context of other calls
avalanche_version=${AVALANCHE_VERSION:-'v1.9.4-rc.0'}