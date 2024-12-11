// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bind

var bindExtraTests = []struct {
	name     string
	contract string
	bytecode []string
	abi      []string
	imports  string
	tester   string
	fsigs    []map[string]string
	libs     map[string]string
	aliases  map[string]string
	types    []string
}{
	// This test checks that the NativeAssetCall proxies the caller address
	// This behavior is disabled on the network and is only to test previous
	// behavior. Note the test uses ApricotPhase2Config.
	{
		`GetSenderNativeAssetCall`,
		`
		pragma solidity >=0.8.0 <0.9.0;
		contract GetSenderNativeAssetCall {
			address _sender;
			function getSender() public view returns (address){
					return _sender;
			}
			function setSender() public {
					_sender = msg.sender;
			}
		}
		`,
		[]string{`6080604052348015600f57600080fd5b506101608061001f6000396000f3fe608060405234801561001057600080fd5b50600436106100365760003560e01c806350c36a521461003b5780635e01eb5a14610045575b600080fd5b610043610063565b005b61004d6100a5565b60405161005a919061010f565b60405180910390f35b336000806101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550565b60008060009054906101000a900473ffffffffffffffffffffffffffffffffffffffff16905090565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b60006100f9826100ce565b9050919050565b610109816100ee565b82525050565b60006020820190506101246000830184610100565b9291505056fea26469706673582212209023ce54f38e749b58f44e8da750354578080ce16df95037b7305ed7e480c36d64736f6c634300081b0033`},
		[]string{`[
			{
				"inputs": [],
				"name": "getSender",
				"outputs": [
					{
						"internalType": "address",
						"name": "",
						"type": "address"
					}
				],
				"stateMutability": "view",
				"type": "function"
			},
			{
				"inputs": [],
				"name": "setSender",
				"outputs": [],
				"stateMutability": "nonpayable",
				"type": "function"
			}
		]`},
		`
			"math/big"
			"github.com/ava-labs/coreth/accounts/abi/bind"
			"github.com/ava-labs/coreth/accounts/abi/bind/backends"
			"github.com/ava-labs/coreth/core/types"
			"github.com/ava-labs/coreth/eth/ethconfig"
			"github.com/ava-labs/coreth/ethclient/simulated"
			"github.com/ava-labs/coreth/node"
			"github.com/ava-labs/coreth/params"
			"github.com/ava-labs/libevm/crypto"
		`,
		`
			// Generate a new random account and a funded simulator
			key, _ := crypto.GenerateKey()
			auth, _ := bind.NewKeyedTransactorWithChainID(key, big.NewInt(1337))
			alloc := types.GenesisAlloc{auth.From: {Balance: big.NewInt(1000000000000000000)}}
			atApricotPhase2 := func(nodeConf *node.Config, ethConf *ethconfig.Config) {
				chainConfig := *params.TestApricotPhase2Config
				chainConfig.ChainID = big.NewInt(1337)
				ethConf.Genesis.Config = &chainConfig
			}
			b := simulated.NewBackend(alloc, simulated.WithBlockGasLimit(10000000), atApricotPhase2)
			sim := &backends.SimulatedBackend{
				Backend: b,
				Client:  b.Client(),
			}
			defer sim.Close()
			// Deploy an interaction tester contract and call a transaction on it
			_, _, interactor, err := DeployGetSenderNativeAssetCall(auth, sim)
			if err != nil {
				t.Fatalf("Failed to deploy interactor contract: %v", err)
			}
			sim.Commit(false)
			_, err = interactor.SetSender(
				&bind.TransactOpts{
					From: auth.From,
					Signer: auth.Signer,
					NativeAssetCall: &bind.NativeAssetCallOpts{
						AssetAmount: big.NewInt(0),
					},
				},
			)
			if err != nil {
				t.Fatalf("Failed to set sender: %v", err)
			}
			sim.Commit(true)
			addr, err := interactor.GetSender(nil)
			if err != nil {
				t.Fatalf("Failed to get sender: %v", err)
			}
			if addr != auth.From {
				t.Fatalf("Address mismatch: have '%v'", addr)
			}
		`,
		nil,
		nil,
		nil,
		nil,
	},
}

func init() {
	bindTests = append(bindTests, bindExtraTests...)
}
