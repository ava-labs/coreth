#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Assumes golang 1.19+ and AWS credentials available in environment variables
# Cloning this with s5cmd takes ~10 minutes

go install github.com/peak/s5cmd/v2@master

git clone https://github.com/peak/s5cmd.git
cd s5cmd
CGO_ENABLED=0 make build
time ./s5cmd cp 's3://statesync-testing/blocks-mainnet-50m/*' .
