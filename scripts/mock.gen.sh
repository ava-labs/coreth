#!/usr/bin/env bash

set -euo pipefail

if ! [[ "$0" =~ scripts/mock.gen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# https://github.com/uber-go/mock
go install -v go.uber.org/mock/mockgen@v0.4.0

go generate -run mockgen ./...
