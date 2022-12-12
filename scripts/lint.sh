#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Initialize dependencies
git submodule update --init

golangci-lint run --path-prefix=. --timeout 3m
