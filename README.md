<div align="center">
  <img src="resources/camino-logo.png?raw=true">
</div>

---

# CaminoEthVm and the C-Chain

[Camino](https://docs.camino.foundation/about/platform-overview) is a network composed of multiple blockchains.
Each blockchain is an instance of a Virtual Machine (VM), much like an object in an object-oriented language is an instance of a class.
That is, the VM defines the behavior of the blockchain.
CaminoEthVm is the [Virtual Machine (VM)](https://docs.camino.foundation/about/platform-overview#virtual-machines) that defines the Contract Chain (C-Chain).
This chain implements the Ethereum Virtual Machine and supports Solidity smart contracts as well as most other Ethereum client functionality.

## Building

CaminoEthVm is a dependency of Camino-Node which is used to implement the EVM based Virtual Machine for the Camino C-Chain. In order to run with a local version of CaminoEthVm, users must update their CaminoEthVm dependency within Camino-Node to point to their local CaminoEthVm directory. If CaminoEthVm and Caino-Node are at the standard location within your GOPATH, this will look like the following:

```bash
cd $GOPATH/src/github.com/chain4travel/camino-node
go mod edit -replace github.com/chain4travel/caminoethvm=../caminoethvm
```

Note: the C-Chain originally ran in a separate process from the main Camino-Node process and communicated with it over a local gRPC connection. When this was the case, Camino-Node's build script would download CaminoEthVm, compile it, and place the binary into the `camino-node/build/plugins` directory.

## API

The C-Chain supports the following API namespaces:

- `eth`
- `personal`
- `txpool`
- `debug`

Only the `eth` namespace is enabled by default. 
To enable the other namespaces see the instructions for passing the C-Chain config to Camino-Node [here.](https://docs.camino.foundation/nodes/references/chain-config-flags)
Full documentation for the C-Chain's API can be found [here.](https://docs.camino.foundation/apis/caminogo-apis/c-chain)

## Compatibility

The C-Chain is compatible with almost all Ethereum tooling, including [Remix](https://https://remix.ethereum.org/), Metamask, Truffle and HardHat

## Differences Between Camino C-Chain and Ethereum

### Atomic Transactions

As a network composed of multiple blockchains, Camino uses *atomic transactions* to move assets between chains. CaminoEthVm modifies the Ethereum block format by adding an *ExtraData* field, which contains the atomic transactions.

### Camino Native Tokens (CNTs)

The C-Chain supports Camino Native Tokens, which are created on the X-Chain using precompiled contracts. These precompiled contracts *nativeAssetCall* and *nativeAssetBalance* support the same interface for CNTs as *CALL* and *BALANCE* do for CAM with the added parameter of *assetID* to specify the asset.

For the full documentation of precompiles for interacting with CNTs and using them in CRC-20s, see [here](https://docs.camino.foundation/build/references/camino-crc20s).

### Block Timing

Blocks are produced asynchronously in Snowman Consensus, so the timing assumptions that apply to Ethereum do not apply to CaminoEthVm. To support block production in an async environment, a block is permitted to have the same timestamp as its parent. Since there is no general assumption that a block will be produced every 10 seconds, smart contracts built on Camino should use the block timestamp instead of the block number for their timing assumptions.

A block with a timestamp more than 10 seconds in the future will not be considered valid. However, a block with a timestamp more than 10 seconds in the past will still be considered valid as long as its timestamp is greater than or equal to the timestamp of its parent block.

## Difficulty and Random OpCode

Snowman consensus does not use difficulty in any way, so the difficulty of every block is required to be set to 1. This means that the DIFFICULTY opcode should not be used as a source of randomness.

Additionally, with the change from the DIFFICULTY OpCode to the RANDOM OpCode (RANDOM replaces DIFFICULTY directly), there is no planned change to provide a stronger source of randomness. The RANDOM OpCode relies on the Eth2.0 Randomness Beacon, which has no direct parallel within the context of either CaminoEthVM or Snowman consensus. Therefore, instead of providing a weaker source of randomness that may be manipulated, the RANDOM OpCode will not be supported. Instead, it will continue the behavior of the DIFFICULTY OpCode of returning the block's difficulty, such that it will always return 1.

## Block Format

To support these changes, there have been a number of changes to the C-Chain block format compared to what exists on Ethereum.

### Block Body

* `Version`: provides version of the `ExtData` in the block. Currently, this field is always 0.
* `ExtData`: extra data field within the block body to store atomic transaction bytes.

### Block Header

* `ExtDataHash`: the hash of the bytes in the `ExtDataHash` field
* `BaseFee`: Added by EIP-1559 to represent the base fee of the block (present in Ethereum as of EIP-1559)
* `ExtDataGasUsed`: amount of gas consumed by the atomic transactions in the block
* `BlockGasCost`: surcharge for producing a block faster than the target rate
