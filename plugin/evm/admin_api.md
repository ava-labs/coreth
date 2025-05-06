---
title: C-Chain API
description: "This page is an overview of the C-Chain API associated with AvalancheGo."
---

<Callout title="Note">
Ethereum has its own notion of `networkID` and `chainID`. These have no relationship to Avalanche's view of networkID and chainID and are purely internal to the [C-Chain](/docs/quick-start/primary-network#c-chain). On Mainnet, the C-Chain uses `1` and `43114` for these values. On the Fuji Testnet, it uses `1` and `43113` for these values. `networkID` and `chainID` can also be obtained using the `net_version` and `eth_chainId` methods.
</Callout>

## Ethereum APIs

### Endpoints

#### JSON-RPC Endpoints

To interact with C-Chain via the JSON-RPC endpoint:

```sh
/ext/bc/C/rpc
```

To interact with other instances of the EVM via the JSON-RPC endpoint:

```sh
/ext/bc/blockchainID/rpc
```

where `blockchainID` is the ID of the blockchain running the EVM.

#### WebSocket Endpoints

<Callout title="info">
On the [public API node](/docs/tooling/rpc-providers), it only supports C-Chain
websocket API calls for API methods that don't exist on the C-Chain's HTTP API
</Callout>

To interact with C-Chain via the websocket endpoint:

```sh
/ext/bc/C/ws
```

For example, to interact with the C-Chain's Ethereum APIs via websocket on localhost, you can use:

```sh
ws://127.0.0.1:9650/ext/bc/C/ws
```

<Callout title="Tip" icon = {<BadgeCheck className="size-5 text-card" fill="green" />} >
On localhost, use `ws://`. When using the [Public API](/docs/tooling/rpc-providers) or another
host that supports encryption, use `wss://`.
</Callout>

To interact with other instances of the EVM via the websocket endpoint:

```sh
/ext/bc/blockchainID/ws
```

where `blockchainID` is the ID of the blockchain running the EVM.

### Standard Ethereum APIs

Avalanche offers an API interface identical to Geth's API except that it only supports the following
services:

- `web3_`
- `net_`
- `eth_`
- `personal_`
- `txpool_`
- `debug_` (note: this is turned off on the public API node.)

You can interact with these services the same exact way you'd interact with Geth (see exceptions below). See the
[Ethereum Wiki's JSON-RPC Documentation](https://ethereum.org/en/developers/docs/apis/json-rpc/)
and [Geth's JSON-RPC Documentation](https://geth.ethereum.org/docs/rpc/server)
for a full description of this API.

<Callout title="info">
For batched requests on the [public API node](/docs/tooling/rpc-providers) , the maximum
number of items is 40. We are working on to support a larger batch size.
</Callout>

#### Exceptions

`eth_getProof` behaves differently than geth, from release [`v0.12.2`](https://github.com/ava-labs/avalanchego/releases/tag/v1.12.2), with the following differences:

- On archival nodes (nodes with`pruning-enabled` set to `false`), queries for state proofs older than 24 hours preceding the last accepted block will be rejected by default. This can be adjusted with `historical-proof-query-window`, which defines the number of blocks before the last accepted block that can be queried for state proofs. Set this option to `0` to accept a state query for any block number.
- On pruning nodes (nodes with `pruning-enabled` set to `true`), queries for state proofs outside the 32 block window after the last accepted block are always rejected.

### Avalanche - Ethereum APIs

In addition to the standard Ethereum APIs, Avalanche offers `eth_baseFee`,
`eth_maxPriorityFeePerGas`, and `eth_getChainConfig`.

They use the same endpoint as standard Ethereum APIs:

```sh
/ext/bc/C/rpc
```

#### `eth_baseFee`

Get the base fee for the next block.

**Signature:**

```sh
eth_baseFee() -> {}
```

`result` is the hex value of the base fee for the next block.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_baseFee",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x34630b8a00"
}
```

#### `eth_maxPriorityFeePerGas`

Get the priority fee needed to be included in a block.

**Signature:**

```sh
eth_maxPriorityFeePerGas() -> {}
```

`result` is hex value of the priority fee needed to be included in a block.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_maxPriorityFeePerGas",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "0x2540be400"
}
```

For more information on dynamic fees see the [C-Chain section of the transaction fee
documentation](/docs/api-reference/standards/guides/txn-fees#c-chain-fees).

#### `eth_getChainConfig`

`eth_getChainConfig` returns chain config. This API is enabled by default with `internal-eth`
namespace.

**Signature:**

```sh
eth_getChainConfig({}) -> {chainConfig: json}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_getChainConfig",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "chainId": 43112,
    "homesteadBlock": 0,
    "daoForkBlock": 0,
    "daoForkSupport": true,
    "eip150Block": 0,
    "eip150Hash": "0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0",
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "apricotPhase1BlockTimestamp": 0,
    "apricotPhase2BlockTimestamp": 0,
    "apricotPhase3BlockTimestamp": 0,
    "apricotPhase4BlockTimestamp": 0,
    "apricotPhase5BlockTimestamp": 0
  }
}
```

## Avalanche Specific APIs

To interact with the `avax` specific RPC calls on the C-Chain:

```sh
/ext/bc/C/avax
```

To interact with other instances of the EVM AVAX endpoints:

```sh
/ext/bc/blockchainID/avax
```

### `avax.export`

<Callout title="Warning" type="warn">
Not recommended for use on Mainnet. See warning notice in [Keystore API](/docs/api-reference/keystore-api).
</Callout>

Export an asset from the C-Chain to X-Chain or P-Chain. If exporting to the X-Chain, you must call the
X-Chain's [`avm.import`](/docs/api-reference/x-chain/api#avmimport).

**Signature:**

```sh
avax.export({
    to: string,
    amount: int,
    assetID: string,
    baseFee: int,
    username: string,
    password:string,
}) -> {txID: string}
```

- `to` is the X-Chain or P-Chain address the asset is sent to.
- `amount` is the amount of the asset to send.
- `assetID` is the ID of the asset. To export AVAX use `"AVAX"` as the `assetID`.
- `baseFee` is the base fee that should be used when creating the transaction. If omitted, a
  suggested fee will be used.
- `username` is the user that controls the address that transaction will be sent from.
- `password` is `username`‘s password.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.export",
    "params" :{
        "to":"X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
        "amount": 500,
        "assetID": "2nzgmhZLuVq8jc7NNu2eahkKwoJcbFWXWJCxHBVWAJEZkhquoK",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txID": "2W5JuFENitZKTpJsy9igBpTcEeBKxBHHGAUkgsSUnkjVVGQ9i8"
  },
  "id": 1
}
```

### `avax.exportAVAX`

<Callout title="Warning" type="warn">
Not recommended for use on Mainnet. See warning notice in [Keystore API](/docs/api-reference/keystore-api).
</Callout>

**DEPRECATED—instead use** [`avax.export`](/docs/api-reference/c-chain/api#avaxexport).

Send AVAX from the C-Chain to X-Chain or P-Chain. If exporting to the X-Chain, you must call the
X-Chain's [`avm.import`](/docs/api-reference/x-chain/api#avmimport) with assetID `AVAX`
on the X-Chain to complete the transfer.

**Signature:**

```go
avax.exportAVAX({
    to: string,
    amount: int,
    baseFee: int,
    username: string,
    password:string,
}) -> {txID: string}
```

**Request:**

- `to` is X-Chain or P-Chain address the asset is sent to.
- `amount` is the amount of the asset to send.
- `baseFee` is the base fee that should be used when creating the transaction. If omitted, a
  suggested fee will be used.
- `username` is the user that controls the address that transaction will be sent from.
- `password` is `username`‘s password.

**Response:**

- `txID` is the TXID of the completed ExportTx.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.exportAVAX",
    "params" :{
        "from": ["0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"],
        "to":"X-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
        "amount": 500,
        "changeAddr": "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txID": "2ffcxdkiKXXA4JdyRoS38dd7zoThkapNPeZuGPmmLBbiuBBHDa"
  },
  "id": 1
}
```

### `avax.exportKey`

<Callout title="Warning" type="warn">
Not recommended for use on Mainnet. See warning notice in [Keystore API](/docs/api-reference/keystore-api).
</Callout>

Get the private key that controls a given address. The returned private key can be added to a user
with `avax.importKey`.

**Signature:**

```go
avax.exportKey({
    username: string,
    password:string,
    address:string
}) -> {privateKey: string}
```

**Request:**

- `username` must control `address`.
- `address` is the address for which you want to export the corresponding private key. It should be
  in hex format.

**Response:**

- `privateKey` is the CB58 encoded string representation of the private key that controls
  `address`. It has a `PrivateKey-` prefix and can be used to import a key via `avax.importKey`.
- `privateKeyHex` is the hex string representation of the private key that controls `address`. It
  can be used to import an account into Core or other wallets, like MetaMask.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.exportKey",
    "params" :{
        "username" :"myUsername",
        "password":"myPassword",
        "address": "0xc876DF0F099b3eb32cBB78820d39F5813f73E18C"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "privateKey": "PrivateKey-2o2uPgTSf3aR5nW6yLHjBEAiatAFKEhApvYzsjvAJKRXVWCYkE",
    "privateKeyHex": "0xec381fb8d32168be4cf7f8d4ce9d8ca892d77ba574264f3665ad5edb89710157"
  },
  "id": 1
}
```

### `avax.getAtomicTx`

Gets a transaction by its ID. Optional encoding parameter to specify the format for the returned
transaction. Can only be `hex` when a value is provided.

**Signature:**

```go
avax.getAtomicTx({
    txID: string,
    encoding: string, //optional
}) -> {
    tx: string,
    encoding: string,
    blockHeight: string
}
```

**Request:**

- `txID` is the transaction ID. It should be in cb58 format.
- `encoding` is the encoding format to use. Can only be `hex` when a value is provided.

**Response:**

- `tx` is the transaction encoded to `encoding`.
- `encoding` is the `encoding`.
- `blockHeight` is the height of the block which the transaction was included in.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.getAtomicTx",
    "params" :{
        "txID":"2GD5SRYJQr2kw5jE73trBFiAgVQyrCaeg223TaTyJFYXf2kPty",
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "tx": "0x000000000000000030399d0775f450604bd2fbc49ce0c5c1c6dfeb2dc2acb8c92c26eeae6e6df4502b19d891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf000000018212d6807a0ec9c1b26321418fe7a548180b5be728ce53fe7e98ab5755ed316100000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005000003a352a382400000000100000000000000018db97c7cece249c2b98bdc0226cc4c2a57bf52fc000003a3529edd17dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000010000000900000001ead19377f015422fbb8731204fcf6d6879dd05146c2d5b5594e2fea2cb420b2f40bd457b71e279e547790b28fe5482f278c76cf39b2dce5c2e6c53352fe6827d002cc7d20d",
    "encoding": "hex",
    "blockHeight": "1"
  },
  "id": 1
}
```

### `avax.getAtomicTxStatus`

Get the status of an atomic transaction sent to the network.

**Signature:**

```sh
avax.getAtomicTxStatus({txID: string}) -> {
  status: string,
  blockHeight: string // returned when status is Accepted
}
```

`status` is one of:

- `Accepted`: The transaction is (or will be) accepted by every node. Check the `blockHeight`
  property
- `Processing`: The transaction is being voted on by this node
- `Dropped`: The transaction was dropped by this node because it thought the transaction invalid
- `Unknown`: The transaction hasn't been seen by this node

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.getAtomicTxStatus",
    "params" :{
        "txID":"2QouvFWUbjuySRxeX5xMbNCuAaKWfbk5FeEa2JmoF85RKLk2dD"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "status": "Accepted",
    "blockHeight": "1"
  }
}
```

### `avax.getUTXOs`

Gets the UTXOs that reference a given address.

**Signature:**

```sh
avax.getUTXOs(
    {
        addresses: string,
        limit: int, //optional
        startIndex: { //optional
            address: string,
            utxo: string
        },
        sourceChain: string,
        encoding: string, //optional
    },
) ->
{
    numFetched: int,
    utxos: []string,
    endIndex: {
        address: string,
        utxo: string
    }
}
```

- `utxos` is a list of UTXOs such that each UTXO references at least one address in `addresses`.
- At most `limit` UTXOs are returned. If `limit` is omitted or greater than 1024, it is set to 1024.
- This method supports pagination. `endIndex` denotes the last UTXO returned. To get the next set of
  UTXOs, use the value of `endIndex` as `startIndex` in the next call.
- If `startIndex` is omitted, will fetch all UTXOs up to `limit`.
- When using pagination (that is when `startIndex` is provided), UTXOs are not guaranteed to be unique
  across multiple calls. That is, a UTXO may appear in the result of the first call, and then again
  in the second call.
- When using pagination, consistency is not guaranteed across multiple calls. That is, the UTXO set
  of the addresses may have changed between calls.
- `encoding` sets the format for the returned UTXOs. Can only be `hex` when a value is provided.

#### **Example**

Suppose we want all UTXOs that reference at least one of
`C-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5`.

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.getUTXOs",
    "params" :{
        "addresses":["C-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5"],
        "sourceChain": "X",
        "startIndex": {
            "address": "C-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
            "utxo": "22RXW7SWjBrrxu2vzDkd8uza7fuEmNpgbj58CxBob9UbP37HSB"
        },
        "encoding": "hex"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

This gives response:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "numFetched": "3",
    "utxos": [
      "0x0000a799e7448acf74ca9223159a04f93b948f99cf28509f908839532b2f85baffc300000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000003a352a38240000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c22d23171",
      "0x00006385c683d43bdbe754c224be36c5004ea7ce49c0849cadeaea6af93dae18cc7700000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000003a352a38240000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29cb81cc877",
      "0x000038137283c94582351b86c3e90808312636769e3f5c14fbf1152d6634f770695c00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000003a352a38240000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c7412490e"
    ],
    "endIndex": {
      "address": "C-avax18jma8ppw3nhx5r4ap8clazz0dps7rv5ukulre5",
      "utxo": "0x9333ef8a05f26acf2d8766f94723f749870fa2ca80c19c33cc945d79013d7c50fd023beb"
    },
    "encoding": "hex"
  },
  "id": 1
}
```

### `avax.import`

<Callout title="Warning" type="warn">
Not recommended for use on Mainnet. See warning notice in [Keystore API](/docs/api-reference/keystore-api).
</Callout>

Finalize the transfer of a non-AVAX or AVAX from X-Chain or P-Chain to the C-Chain.

**Signature:**

```go
avax.import({
    to: string,
    sourceChain: string,
    baseFee: int, // optional
    username: string,
    password:string,
}) -> {txID: string}
```

**Request:**

- `to` is the address the asset is sent to. This must be the same as the `to` argument in the
  corresponding call to the X-Chain's or P-Chain's `export`.
- `sourceChain` is the ID or alias of the chain the asset is being imported from. To import funds
  from the X-Chain, use `"X"`; for the P-Chain, use `"P"`.
- `baseFee` is the base fee that should be used when creating the transaction. If omitted, a
  suggested fee will be used.
- `username` is the user that controls the address that transaction will be sent from.
- `password` is `username`‘s password.

**Response:**

- `txID` is the ID of the completed ImportTx.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.import",
    "params" :{
        "to":"0x4b879aff6b3d24352Ac1985c1F45BA4c3493A398",
        "sourceChain":"X",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txID": "6bJq9dbqhiQvoshT3uSUbg9oB24n7Ei6MLnxvrdmao78oHR9t"
  },
  "id": 1
}
```

### `avax.importAVAX`

<Callout title="Warning" type="warn">
Not recommended for use on Mainnet. See warning notice in [Keystore API](/docs/api-reference/keystore-api).
</Callout>

**DEPRECATED—instead use** [`avax.import`](/docs/api-reference/c-chain/api#avaximport)

Finalize a transfer of AVAX from the X-Chain or P-Chain to the C-Chain.

**Signature:**

```go
avax.importAVAX({
    to: string,
    sourceChain: string,
    baseFee: int, // optional
    username: string,
    password:string,
}) -> {txID: string}
```

**Request:**

- `to` is the address the AVAX is sent to. It should be in hex format.
- `sourceChain` is the ID or alias of the chain the AVAX is being imported from. To import funds
  from the X-Chain, use `"X"`; for the P-Chain, use `"P"`.
- `baseFee` is the base fee that should be used when creating the transaction. If omitted, a
  suggested fee will be used.
- `username` is the user that controls the address that transaction will be sent from.
- `password` is `username`‘s password.

**Response:**

- `txID` is the ID of the completed ImportTx.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.importAVAX",
    "params" :{
        "to":"0x4b879aff6b3d24352Ac1985c1F45BA4c3493A398",
        "sourceChain":"X",
        "username":"myUsername",
        "password":"myPassword"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "txID": "LWTRsiKnEUJC58y8ezAk6hhzmSMUCtemLvm3LZFw8fxDQpns3"
  },
  "id": 1
}
```

### `avax.importKey`

<Callout title="Warning" type="warn">
Not recommended for use on Mainnet. See warning notice in [Keystore API](/docs/api-reference/keystore-api).
</Callout>

Give a user control over an address by providing the private key that controls the address.

**Signature:**

```go
avax.importKey({
    username: string,
    password:string,
    privateKey:string
}) -> {address: string}
```

**Request:**

- Add `privateKey` to `username`'s set of private keys.

**Response:**

- `address` is the address `username` now controls with the private key. It will be in hex format.

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"avax.importKey",
    "params" :{
        "username" :"myUsername",
        "password":"myPassword",
        "privateKey":"PrivateKey-2o2uPgTSf3aR5nW6yLHjBEAiatAFKEhApvYzsjvAJKRXVWCYkE"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "address": "0xc876DF0F099b3eb32cBB78820d39F5813f73E18C"
  },
  "id": 1
}
```

### `avax.issueTx`

Send a signed transaction to the network. `encoding` specifies the format of the signed transaction.
Can only be `hex` when a value is provided.

**Signature:**

```sh
avax.issueTx({
    tx: string,
    encoding: string, //optional
}) -> {
    txID: string
}
```

**Example Call:**

```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     : 1,
    "method" :"avax.issueTx",
    "params" :{
        "tx":"0x00000009de31b4d8b22991d51aa6aa1fc733f23a851a8c9400000000000186a0000000005f041280000000005f9ca900000030390000000000000001fceda8f90fcb5d30614b99d79fc4baa29307762668f16eb0259a57c2d3b78c875c86ec2045792d4df2d926c40f829196e0bb97ee697af71f5b0a966dabff749634c8b729855e937715b0e44303fd1014daedc752006011b730",
        "encoding": "hex"

    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/avax
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "txID": "NUPLwbt2hsYxpQg4H2o451hmTWQ4JZx2zMzM4SinwtHgAdX1JLPHXvWSXEnpecStLj"
  }
}
```

## Admin API

This API can be used for debugging. Note that the Admin API is disabled by default. To run a node
with the Admin API enabled, use [C-Chain config flag`--coreth-admin-api-enabled:true`](/docs/nodes/chain-configs/c-chain#coreth-admin-api-enabled).

### Endpoint

```text
/ext/bc/C/admin
```

### `admin.setLogLevel`

Sets the log level of the C-Chain.

**Signature:**

```text
admin.setLogLevel({level:string}) -> {}
```

- `level` is the log level to be set.

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.setLogLevel",
    "params": {
        "level":"info"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

### `admin.startCPUProfiler`

Starts a CPU profile.

**Signature:**

```text
admin.startCPUProfiler() -> {}
```

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.startCPUProfiler",
    "params": {}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

### `admin.stopCPUProfiler`

Stops and writes a CPU profile.

**Signature:**

```text
admin.stopCPUProfiler() -> {}
```

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.stopCPUProfiler",
    "params": {}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

### `admin.memoryProfile`

Runs and writes a memory profile.

**Signature:**

```text
admin.memoryProfile() -> {}
```

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.memoryProfile",
    "params": {}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```

### `admin.lockProfile`

Runs a mutex profile writing to the `coreth_performance_c` directory.

**Signature:**

```text
admin.lockProfile() -> {}
```

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"admin.lockProfile",
    "params": {}
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C/admin
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {}
}
```
