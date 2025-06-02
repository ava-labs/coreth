#!/usr/bin/env bash

set -euo pipefail

# Run AvalancheGo e2e tests from the target version against the current state of coreth.

# e.g.,
# ./scripts/tests.e2e.sh
# AVALANCHE_VERSION=v1.10.x ./scripts/tests.e2e.sh
# ./scripts/tests.e2e.sh --start-monitors          # All arguments are supplied to ginkgo
if ! [[ "$0" =~ scripts/tests.e2e.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

echo "running AvalancheGo e2e tests"
./scripts/run_task.sh test-e2e-ci -- --ginkgo.label-filter='c || uses-c' "${@}"
