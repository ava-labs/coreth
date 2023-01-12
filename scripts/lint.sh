#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Initialize dependencies
git submodule update --init

if ! [ -x "$(command -v golangci-lint)" ]; then
  go install -v github.com/golangci/golangci-lint/cmd/golangci-lint@v1.49.0
fi
golangci-lint run --path-prefix=. --timeout 3m
