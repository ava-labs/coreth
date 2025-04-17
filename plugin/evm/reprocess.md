# Reprocess Test Harness

To stress test the VM, we'd like the ability to grab the current state at a given block height N and benchmark executing a range of blocks starting at that point. This should enable us to compare the performance between different versions of the VM such as comparing the performance with Firewood vs. the original geth pathdb/hashdb + KV database (level/pebble/etc) combination.

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

## Using Coreth

With the current setup, you must build Firewood locally before building Coreth or running unit tests/benchmarks as it will not compile without the Firewood shared library present.
