#!/usr/bin/env bash

set -euo pipefail

# Root directory
CORETH_PATH=$(
  cd "$(dirname "${BASH_SOURCE[0]}")"
  cd .. && pwd
)

if ! [[ "$0" =~ scripts/mock.gen.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# https://github.com/uber-go/mock
go install -v go.uber.org/mock/mockgen@v0.4.0

# Load the versions
source "$CORETH_PATH"/scripts/versions.sh

# tuples of (source interface import path, comma-separated interface names, output file path)
input="scripts/mocks.mockgen.txt"
while IFS= read -r line; do
  IFS='=' read -r src_import_path interface_name output_path <<<"${line}"
  package_name=$(basename "$(dirname "$output_path")")
  echo "Generating ${output_path}..."
  mockgen -package="${package_name}" -destination="${output_path}" "${src_import_path}" "${interface_name}"
done <"$input"

echo "SUCCESS"
