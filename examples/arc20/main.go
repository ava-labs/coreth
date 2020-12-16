package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	uri                    = "http://127.0.0.1:9650/ext/bc/C/rpc"
	nativeAssetBalanceAddr = common.HexToAddress("0x0100000000000000000000000000000000000001")
	nativeAssetCallAddr    = common.HexToAddress("0x0100000000000000000000000000000000000002")
	chainID                = new(big.Int).SetUint64(43112)
	privateKey             *ecdsa.PrivateKey
	address                common.Address
	erc20                  common.Address
	assetID                *big.Int
	amount                 *big.Int
	gasLimit               = uint64(700000)
	gasPrice               = params.MinGasPrice
)

func init() {
	// Private Key to sign deposit transaction
	// To export from MetaMask:
	// 1) Click ellipsis on right hand side of account page
	// 2) View Account Details
	// 3) Export Private Key
	pk, err := crypto.HexToECDSA("da777cd656c8760a7d378ae04d7dd0cd7a703c450c84e6c2faa886ca97517df7")
	if err != nil {
		panic(err)
	}
	privateKey = pk
	// Extract From Address from the private key
	address = crypto.PubkeyToAddress(privateKey.PublicKey)
	// erc20 = common.HexToAddress("0xea75d59faF258F1fdf2b94F158e54D7ad44359B6")
	// Address of ARC-20 to deposit funds in
	erc20 = common.HexToAddress("0x721F9Af7631605713133ccc502E32eA4d43CDfec")
	// AssetID as a uint256 integer as displayed in Remix of the ARC-20
	assetID = common.HexToHash("0x42f0d24c970eb6c567ea63a68b97d8e357a7fc8d9f73489c7761f1382b295db4").Big()
	amount = new(big.Int).SetUint64(100)
}

// createDepositCallData creates the callData argument to nativeAssetTransfer to move [amount]
// of [assetID] to [erc20] address and call the deposit function with signature "deposit()"
func createDepositCallData(erc20 common.Address, assetID, amount *big.Int) []byte {
	// erc20 addr, assetID, assetAmount, callData
	signatureHash := crypto.Keccak256([]byte("deposit()"))
	fmt.Printf("signatureHash: 0x%x\n", signatureHash)
	functionSignature := signatureHash[:4]
	data := make([]byte, 0, 84)
	data = append(data, erc20.Bytes()...)
	data = append(data, common.LeftPadBytes(assetID.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	data = append(data, functionSignature...) // Add this back in to trigger call to deposit
	fmt.Printf("deposit callData: 0x%x\n", data)
	return data
}

// createDepositTransaction creates a transaction to deposit native asset funds in [erc20]
func createDepositTransaction(nonce uint64, erc20 common.Address, assetID, amount *big.Int, gasLimit uint64, gasPrice *big.Int) *types.Transaction {
	callData := createDepositCallData(erc20, assetID, amount)
	return types.NewTransaction(nonce, nativeAssetCallAddr, new(big.Int), gasLimit, gasPrice, callData)
}

// deploy ARC-20 contract with specific assetID
func main() {
	client, err := ethclient.Dial(uri)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	nonce, err := client.NonceAt(ctx, address, nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Creating deposit transaction from: %s, erc20 address: %s, assetID: %d, amount: %d, nonce: %d\n", address.Hex(), erc20.Hex(), assetID, amount, nonce)
	// Create and sign deposit transaction from account that has been funded with sufficient AVAX to
	// pay gas costs and sufficient amount of the native asset to make the deposit
	tx := createDepositTransaction(nonce, erc20, assetID, amount, gasLimit, gasPrice)
	signer := types.NewEIP155Signer(chainID)
	signedTx, err := types.SignTx(tx, signer, privateKey)
	if err != nil {
		panic(err)
	}
	// Send the signed transaction to the client
	if err := client.SendTransaction(ctx, signedTx); err != nil {
		panic(err)
	}
	txHash := signedTx.Hash()
	fmt.Printf("txHash: %s\n", txHash.Hex())
}
