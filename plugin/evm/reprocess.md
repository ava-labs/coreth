# Reprocess Test Harness

To stress test the VM, we'd like the ability to grab the current state at a given block height N and benchmark executing a range of blocks starting at that point. This should enable us to compare the performance between different versions of the VM such as comparing the performance with Firewood vs. the original geth pathdb/hashdb + KV database (level/pebble/etc) combination.

## Quick Start

Our current target benchmark is to execute blocks over a pre-specified range for which we have a snapshotted Firewood database at the target start block.

To do this in a single script on Snoopy, you can run the following:

```bash
ssh -L 8000:localhost:80 ethchallenge.avax-dev.network
cd coreth
nohup bash -x ./bench/scripts/reprocess_snoopy.sh &
```

This will copy the existing Coreth database and Firewood DB file (~200 GB each) and then re-execute the range of blocks `(33134213, 34000000]`.

The existing database that we copy was generated prior to https://github.com/ava-labs/firewood/pull/845, which makes it incompatible with the latest version of Firewood (incompatible header checks).

Grafana has been configured on Snoopy, so after forwarding port 80, you can view the grafana dashboards locally at: `http://localhost:8000/dashboards`.

## Current DevX

Currently, the unit tests in [reprocess_test.go](./reprocess_test.go) have been set up to provide a CLI-like tool run via golang unit tests. This includes CLI flags set via `TestMain` and the following entrypoints:

- TestExportBlocks
- TestExportCode
- TestExportHeaders
- TestQueryBlock
- TestPersistedMetadata
- TestCalculatePrefix
- TestReprocessGenesis
- TestReprocessMainnetBlocks
- TestCheckSnapshot

Planning to re-write this as a more general CLI, so for now refer to the code itself for documentation.

## Building Firewood

To build Coreth or run any unit tests/benchmarks, the go toolchain must be able to find the Firewood binary. Unless Firewood packages pre-built binaries alongside the FFI package, this means that we need to build the shared binary locally, so that the Firewood FFI layer we are importing's CGO directives can find the shared library.

As a result, we can only build Coreth using a local `go mod edit -replace` to a local version of Firewood and anything that depends on it seemingly needs to do the same.

For convenience, you can run the following to clone, check out the latest version, and build Firewood within the `build/` subdirectory of Coreth.

```bash
./scripts/build_firewood.sh
```

Note: if you are working with a local version of Firewood, you can skip checking out a different branch with:

```bash
FIREWOOD_PATH=<local-firewood-path> FIREWOOD_IGNORE_CHECKOUT=true ./scripts/build_firewood.sh
```

This will build the shared library in place and update the `go.mod` file to point to it.

If you are running benchmarks and want to update the Firewood dependency, you simply need to re-compile the Firewood shared library. If the Firewood update includes any disk layout incompatibilities, then you'll need to repeat re-processing to re-generate any Firewood database files required for the benchmark.

## Using Coreth

With the current setup, you must build Firewood locally before building Coreth or running unit tests/benchmarks as it will not compile without the Firewood shared library present.

As an example, try cloning or using this branch of Coreth and running (without building Firewood):

```bash
./scripts/build.sh`
```

This should fail with:

```
Using branch: aaronbuchwald/prefetch-state
Building Coreth @ GitCommit: b39c2484deccd47f9e0f268559e041f97ba0f836
# github.com/ava-labs/coreth/shim/fw
shim/fw/firewood.go:11:12: undefined: firewood.Database
```

or running one of the reprocess unit tests via:

```bash
go test -timeout 30s -run ^TestReprocessGenesis$ github.com/ava-labs/coreth/plugin/evm -timeout=15s
```

This should fail with:

```
# github.com/ava-labs/coreth/shim/fw
shim/fw/firewood.go:11:12: undefined: firewood.Database
FAIL    github.com/ava-labs/coreth/plugin/evm [build failed]
FAIL
```

Now try building Firewood and re-running the same commands:

```bash
./scripts/build_firewood.sh
./scripts/build.sh
```

```bash
go test -timeout 30s -run ^TestReprocessGenesis$ github.com/ava-labs/coreth/plugin/evm -timeout=15s
```

## Benchmarking

Scripts have been added [here](../../bench/scripts) to automate the following tasks:

- [Import Blocks from S3](../../bench/scripts/import_s3_blocks.sh)
- [Reprocess a Specified Range of Blocks](../../bench/scripts/reprocess_blocks.sh)
- [Clone DB and Reprocess](../../bench/scripts/reprocess_snoopy.sh)

### Generating the Block Database

The reprocess benchmarks currently depend on a database pulled from the S3 bucket that contains a mapping of all blocks that we'd like to re-execute. This can be created from scratch by bootstrapping a new node and then using `TestExportBlocks` to export from the source DB (from running an actual node) to a new target database, where it will import only the blocks.

Since the benchmarks only depend on the first 50m block, it's not necessary to repeat this process at the moment and we can continue to re-use the block database in the S3 bucket s3://statesync-testing/blocks-mainnet-50m within the AWS experimental account.

Note: for fastest results, it's recommended to use [s5cmd](https://github.com/peak/s5cmd) instead of the AWS CLI for S3 directly.

If AWS credentials are populated, you can import the S3 Bucket via:

```bash
bash -x ./bench/scripts/import_s3_blocks.sh <targetBlockDBPath>
```

### Re-Processing from [start, end]

Once we have the block database, we can either re-process blocks directly from the genesis or we may prefer to benchmark execution of a specific range of blocks.

If we'd like to re-process from genesis, then we can run:

```bash
bash -x ./bench/scripts/reprocess_blocks.sh <blockDbDir> <dbDir> <firewoodDbFile> 0 <endBlock>
```

If instead, you want to benchmark re-executing a particular range of blocks such as [33m, 43m], then you'll first need to re-execute up to 33m to reproduce the state as of block 33m.

To do this, you can perform the following (note: this may take > 24 hrs and therefore require prepending nohup to the commands):

```bash
bash -x ./bench/scripts/reprocess_blocks.sh blocks.db metadata-db-33m.bak firewood-db-33m.bak 0 33000000
cp metadata-db-33m.bak metadata-db-33m.bench
cp firewood-db-33m.bak firewood-db-33m.bench
bash -x ./bench/scripts/reprocess_blocks.sh blocks.db metadata-db-33m.bench firewood-db-33m.bench 33000000 43000000
```

### Gotchas

The current Firewood benchmarking process requires passing around multiple databases, copying them manually, and making sure that when running a benchmark the metadata across them is fully aligned or else the benchmark may refuse to run or even appear to hang as it processes an event like a re-org (ex. I ran into a situation where the test appeared to hang and the root cause was the metadata was corrupted and it was attempting to process a re-org from height 0 to 33m).

If you see an error such as:

```
=== RUN   TestReprocessMainnetBlocks
INFO [04-21|11:15:00.357] Allocated cache and file handles         database=/home/snoopy/blocks-mainnet-50m cache=128.00MiB handles=1024 readonly=true
INFO [04-21|11:15:00.448] Using LevelDB as the backing database
INFO [04-21|11:15:00.867] Starting pprof server                    addr=http://localhost:6060/debug/pprof
    reprocess_test.go:487: Persisted metadata: Last hash: f4b404723d96ce733026496e0916ea368ad83d3dd79bd5eb9f50fe2351b3ec8b, Last root: ac4ff373fb5471154bf085c0de406b20bbf383f260da075381a79371072a92fd, Last height: 33133459
    reprocess_test.go:500: 
        	Error Trace:	/home/snoopy/coreth/plugin/evm/reprocess_test.go:500
        	Error:      	Not equal: 
        	            	expected: 0x1f99393
        	            	actual  : 0x1f99230
        	Test:       	TestReprocessMainnetBlocks
        	Messages:   	Last height does not match start block
--- FAIL: TestReprocessMainnetBlocks (0.51s)
FAIL
FAIL	github.com/ava-labs/coreth/plugin/evm	0.637s
FAIL

real	0m2.028s
user	0m2.028s
sys	0m1.060s
```

This likely indicates that the metadata's start block does not align with the last expected height. To solve this issue, you can also use the flag `-usePersistedStartBlock` to use the start block left on disk.

Separately, you can also run into an issue where the merkle root reported by Firewood does not match the last accepted state root according to the metadata db. The metadata DB does not contain a mapping from state root to block height, which makes it impossible to automatically override to a different height. If you are aware of the metadata that needs to be aligned, these issues are relatively easy to recognize, diagnose, and quickly fix, but in the long term we should switch to an automated solution and ideally more clearly tie the databases together and report better errors.
