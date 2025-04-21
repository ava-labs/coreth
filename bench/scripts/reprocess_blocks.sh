#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Assumes suitable golang version to run Coreth has been installed, Firewood has been built,
# and that a block database is available.

reprocess_blocks() {
    local sourceDbDir="${1}" # Database to fetch blocks from
    local dbDir="${2}" # Chain's database including metadata, code, etc. (also includes geth merkle trie)
    local firewoodDbFile="${3}" # Path to the Firewood database file
    local startBlock="${4}" # First block to execute - root of this block must match the last root of Firewood database
    local endBlock="${5}" # End block

    time go test -v -timeout 0 -run ^TestReprocessMainnetBlocks/firewood$ github.com/ava-labs/coreth/plugin/evm \
        -sourceDbDir=${sourceDbDir} -dbDir=${dbDir} -pruning=false \
        -startBlock=${startBlock} -endBlock=${endBlock} \
        -prefetchers=0 -useSnapshot=false -skipUpgradeCheck -logEach=100 \
        -firewoodDBFile=${firewoodDbFile} -startPProf=true -firewoodMetricsPort=3000
}

# Usage: ./reprocess_blocks.sh <sourceDbDir> <dbDir> <firewoodDbFile> <startBlock> <endBlock>
reprocess_blocks "$@"
