#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Initialize dependencies
git submodule update --init

go test $@ -timeout="30m" -coverprofile="coverage.out" -covermode="atomic" $(go list ./... | grep -v /mocks | grep -v proto | grep -v tests)