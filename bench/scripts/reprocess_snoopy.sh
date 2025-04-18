#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Copy the FW Eth backup file to another location for testing

firewood_target_file="/home/snoopy/fw-test-$(date -Iseconds).db.eth.33m"
cp /home/snoopy/fw.db.eth.33m.bak ${firewood_target_file}
./bench/scripts/reprocess_blocks.sh /home/snoopy/blocks-mainnet-50m /home/snoopy/fw-metadata.33m ${firewood_target_file} 33000000 34000000
