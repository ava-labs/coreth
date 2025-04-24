#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Copy the FW Eth backup file to another location for testing
execution_target_suffix=$(date -Iseconds)-execution

data_dir="/mnt/md0/data"

blocks_db_dir="${data_dir}/blocks-mainnet-50m-pristine"
fw_db_file="${data_dir}/fw-db-file-${execution_target_suffix}"
fw_metadata_file="${data_dir}/fw-metadata-${execution_target_suffix}"

cp "${data_dir}/fw-db-file-pristine" "${fw_db_file}"
cp -r "${data_dir}/fw-metadata-pristine" "${fw_metadata_file}"

bash -x ./bench/scripts/reprocess_blocks.sh $blocks_db_dir $fw_metadata_file $fw_db_file 33134213 34000000
