// (c) 2019-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package runtime

import (
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ava-labs/coreth/accounts/abi"
	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/eth/tracers"
	"github.com/ava-labs/coreth/eth/tracers/logger"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/asm"

	// force-load js tracers to trigger registration
	_ "github.com/ava-labs/coreth/eth/tracers/js"
	"github.com/holiman/uint256"
)

func TestDefaults(t *testing.T) {
	cfg := new(Config)
	setDefaults(cfg)

	if cfg.Difficulty == nil {
		t.Error("expected difficulty to be non nil")
	}

	if cfg.GasLimit == 0 {
		t.Error("didn't expect gaslimit to be zero")
	}
	if cfg.GasPrice == nil {
		t.Error("expected time to be non nil")
	}
	if cfg.Value == nil {
		t.Error("expected time to be non nil")
	}
	if cfg.GetHashFn == nil {
		t.Error("expected time to be non nil")
	}
	if cfg.BlockNumber == nil {
		t.Error("expected block number to be non nil")
	}
}

func TestEVM(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("crashed with: %v", r)
		}
	}()

	Execute([]byte{
		byte(vm.DIFFICULTY),
		byte(vm.TIMESTAMP),
		byte(vm.GASLIMIT),
		byte(vm.PUSH1),
		byte(vm.ORIGIN),
		byte(vm.BLOCKHASH),
		byte(vm.COINBASE),
	}, nil, nil)
}

func TestExecute(t *testing.T) {
	ret, _, err := Execute([]byte{
		byte(vm.PUSH1), 10,
		byte(vm.PUSH1), 0,
		byte(vm.MSTORE),
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	}, nil, nil)
	if err != nil {
		t.Fatal("didn't expect error", err)
	}

	num := new(big.Int).SetBytes(ret)
	if num.Cmp(big.NewInt(10)) != 0 {
		t.Error("Expected 10, got", num)
	}
}

func TestCall(t *testing.T) {
	state, _ := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	address := common.HexToAddress("0x0a")
	state.SetCode(address, []byte{
		byte(vm.PUSH1), 10,
		byte(vm.PUSH1), 0,
		byte(vm.MSTORE),
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	})

	ret, _, err := Call(address, nil, &Config{State: state})
	if err != nil {
		t.Fatal("didn't expect error", err)
	}

	num := new(big.Int).SetBytes(ret)
	if num.Cmp(big.NewInt(10)) != 0 {
		t.Error("Expected 10, got", num)
	}
}

func BenchmarkCall(b *testing.B) {
	// var definition = `[{"constant":true,"inputs":[],"name":"seller","outputs":[{"name":"","type":"address"}],"type":"function"},{"constant":false,"inputs":[],"name":"abort","outputs":[],"type":"function"},{"constant":true,"inputs":[],"name":"value","outputs":[{"name":"","type":"uint256"}],"type":"function"},{"constant":false,"inputs":[],"name":"refund","outputs":[],"type":"function"},{"constant":true,"inputs":[],"name":"buyer","outputs":[{"name":"","type":"address"}],"type":"function"},{"constant":false,"inputs":[],"name":"confirmReceived","outputs":[],"type":"function"},{"constant":true,"inputs":[],"name":"state","outputs":[{"name":"","type":"uint8"}],"type":"function"},{"constant":false,"inputs":[],"name":"confirmPurchase","outputs":[],"type":"function"},{"inputs":[],"type":"constructor"},{"anonymous":false,"inputs":[],"name":"Aborted","type":"event"},{"anonymous":false,"inputs":[],"name":"PurchaseConfirmed","type":"event"},{"anonymous":false,"inputs":[],"name":"ItemReceived","type":"event"},{"anonymous":false,"inputs":[],"name":"Refunded","type":"event"}]`
	var definition = `[{"inputs":[{"internalType":"string","name":"name_","type":"string"},{"internalType":"string","name":"symbol_","type":"string"}],"stateMutability":"nonpayable","type":"constructor"},{"inputs":[{"internalType":"address","name":"sender","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"owner","type":"address"}],"name":"ERC721IncorrectOwner","type":"error"},{"inputs":[{"internalType":"address","name":"operator","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"ERC721InsufficientApproval","type":"error"},{"inputs":[{"internalType":"address","name":"approver","type":"address"}],"name":"ERC721InvalidApprover","type":"error"},{"inputs":[{"internalType":"address","name":"operator","type":"address"}],"name":"ERC721InvalidOperator","type":"error"},{"inputs":[{"internalType":"address","name":"owner","type":"address"}],"name":"ERC721InvalidOwner","type":"error"},{"inputs":[{"internalType":"address","name":"receiver","type":"address"}],"name":"ERC721InvalidReceiver","type":"error"},{"inputs":[{"internalType":"address","name":"sender","type":"address"}],"name":"ERC721InvalidSender","type":"error"},{"inputs":[{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"ERC721NonexistentToken","type":"error"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"owner","type":"address"},{"indexed":true,"internalType":"address","name":"approved","type":"address"},{"indexed":true,"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"Approval","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"owner","type":"address"},{"indexed":true,"internalType":"address","name":"operator","type":"address"},{"indexed":false,"internalType":"bool","name":"approved","type":"bool"}],"name":"ApprovalForAll","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"from","type":"address"},{"indexed":true,"internalType":"address","name":"to","type":"address"},{"indexed":true,"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"Transfer","type":"event"},{"inputs":[{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"approve","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"getApproved","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"address","name":"operator","type":"address"}],"name":"isApprovedForAll","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"name","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"safeMint","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"safeTransferFrom","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"safeTransferFrom","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"operator","type":"address"},{"internalType":"bool","name":"approved","type":"bool"}],"name":"setApprovalForAll","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"bytes4","name":"interfaceId","type":"bytes4"}],"name":"supportsInterface","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"tokenURI","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"from","type":"address"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"}],"name":"transferFrom","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

	// var code = common.Hex2Bytes("6060604052361561006c5760e060020a600035046308551a53811461007457806335a063b4146100865780633fa4f245146100a6578063590e1ae3146100af5780637150d8ae146100cf57806373fac6f0146100e1578063c19d93fb146100fe578063d696069714610112575b610131610002565b610133600154600160a060020a031681565b610131600154600160a060020a0390811633919091161461015057610002565b61014660005481565b610131600154600160a060020a039081163391909116146102d557610002565b610133600254600160a060020a031681565b610131600254600160a060020a0333811691161461023757610002565b61014660025460ff60a060020a9091041681565b61013160025460009060ff60a060020a9091041681146101cc57610002565b005b600160a060020a03166060908152602090f35b6060908152602090f35b60025460009060a060020a900460ff16811461016b57610002565b600154600160a060020a03908116908290301631606082818181858883f150506002805460a060020a60ff02191660a160020a179055506040517f72c874aeff0b183a56e2b79c71b46e1aed4dee5e09862134b8821ba2fddbf8bf9250a150565b80546002023414806101dd57610002565b6002805460a060020a60ff021973ffffffffffffffffffffffffffffffffffffffff1990911633171660a060020a1790557fd5d55c8a68912e9a110618df8d5e2e83b8d83211c57a8ddd1203df92885dc881826060a15050565b60025460019060a060020a900460ff16811461025257610002565b60025460008054600160a060020a0390921691606082818181858883f150508354604051600160a060020a0391821694503090911631915082818181858883f150506002805460a060020a60ff02191660a160020a179055506040517fe89152acd703c9d8c7d28829d443260b411454d45394e7995815140c8cbcbcf79250a150565b60025460019060a060020a900460ff1681146102f057610002565b6002805460008054600160a060020a0390921692909102606082818181858883f150508354604051600160a060020a0391821694503090911631915082818181858883f150506002805460a060020a60ff02191660a160020a179055506040517f8616bbbbad963e4e65b1366f1d75dfb63f9e9704bbbf91fb01bec70849906cf79250a15056")
	var code = common.Hex2Bytes("608060405234801561000f575f80fd5b506040516111d43803806111d483398101604081905261002e916100ef565b81815f61003b83826101d8565b50600161004882826101d8565b5050505050610292565b634e487b7160e01b5f52604160045260245ffd5b5f82601f830112610075575f80fd5b81516001600160401b0381111561008e5761008e610052565b604051601f8201601f19908116603f011681016001600160401b03811182821017156100bc576100bc610052565b6040528181528382016020018510156100d3575f80fd5b8160208501602083015e5f918101602001919091529392505050565b5f8060408385031215610100575f80fd5b82516001600160401b03811115610115575f80fd5b61012185828601610066565b602085015190935090506001600160401b0381111561013e575f80fd5b61014a85828601610066565b9150509250929050565b600181811c9082168061016857607f821691505b60208210810361018657634e487b7160e01b5f52602260045260245ffd5b50919050565b601f8211156101d357805f5260205f20601f840160051c810160208510156101b15750805b601f840160051c820191505b818110156101d0575f81556001016101bd565b50505b505050565b81516001600160401b038111156101f1576101f1610052565b610205816101ff8454610154565b8461018c565b6020601f821160018114610237575f83156102205750848201515b5f19600385901b1c1916600184901b1784556101d0565b5f84815260208120601f198516915b828110156102665787850151825560209485019460019092019101610246565b508482101561028357868401515f19600387901b60f8161c191681555b50505050600190811b01905550565b610f358061029f5f395ff3fe608060405234801561000f575f80fd5b50600436106100e5575f3560e01c806370a0823111610088578063a22cb46511610063578063a22cb465146101db578063b88d4fde146101ee578063c87b56dd14610201578063e985e9c514610214575f80fd5b806370a082311461019f57806395d89b41146101c0578063a1448194146101c8575f80fd5b8063095ea7b3116100c3578063095ea7b31461015157806323b872dd1461016657806342842e0e146101795780636352211e1461018c575f80fd5b806301ffc9a7146100e957806306fdde0314610111578063081812fc14610126575b5f80fd5b6100fc6100f7366004610be2565b610227565b60405190151581526020015b60405180910390f35b610119610278565b6040516101089190610c2b565b610139610134366004610c3d565b610307565b6040516001600160a01b039091168152602001610108565b61016461015f366004610c6f565b61032e565b005b610164610174366004610c97565b61033d565b610164610187366004610c97565b6103cb565b61013961019a366004610c3d565b6103ea565b6101b26101ad366004610cd1565b6103f4565b604051908152602001610108565b610119610439565b6101646101d6366004610c6f565b610448565b6101646101e9366004610cea565b610461565b6101646101fc366004610d37565b61046c565b61011961020f366004610c3d565b610484565b6100fc610222366004610e14565b6104f5565b5f6001600160e01b031982166380ac58cd60e01b148061025757506001600160e01b03198216635b5e139f60e01b145b8061027257506301ffc9a760e01b6001600160e01b03198316145b92915050565b60605f805461028690610e45565b80601f01602080910402602001604051908101604052809291908181526020018280546102b290610e45565b80156102fd5780601f106102d4576101008083540402835291602001916102fd565b820191905f5260205f20905b8154815290600101906020018083116102e057829003601f168201915b5050505050905090565b5f61031182610522565b505f828152600460205260409020546001600160a01b0316610272565b61033982823361055a565b5050565b6001600160a01b03821661036b57604051633250574960e11b81525f60048201526024015b60405180910390fd5b5f610377838333610567565b9050836001600160a01b0316816001600160a01b0316146103c5576040516364283d7b60e01b81526001600160a01b0380861660048301526024820184905282166044820152606401610362565b50505050565b6103e583838360405180602001604052805f81525061046c565b505050565b5f61027282610522565b5f6001600160a01b03821661041e576040516322718ad960e21b81525f6004820152602401610362565b506001600160a01b03165f9081526003602052604090205490565b60606001805461028690610e45565b610339828260405180602001604052805f815250610659565b610339338383610670565b61047784848461033d565b6103c5338585858561070e565b606061048f82610522565b505f6104a560408051602081019091525f815290565b90505f8151116104c35760405180602001604052805f8152506104ee565b806104cd84610836565b6040516020016104de929190610e94565b6040516020818303038152906040525b9392505050565b6001600160a01b039182165f90815260056020908152604080832093909416825291909152205460ff1690565b5f818152600260205260408120546001600160a01b03168061027257604051637e27328960e01b815260048101849052602401610362565b6103e583838360016108c6565b5f828152600260205260408120546001600160a01b0390811690831615610593576105938184866109ca565b6001600160a01b038116156105cd576105ae5f855f806108c6565b6001600160a01b0381165f90815260036020526040902080545f190190555b6001600160a01b038516156105fb576001600160a01b0385165f908152600360205260409020805460010190555b5f8481526002602052604080822080546001600160a01b0319166001600160a01b0389811691821790925591518793918516917fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef91a4949350505050565b6106638383610a2e565b6103e5335f85858561070e565b6001600160a01b0382166106a257604051630b61174360e31b81526001600160a01b0383166004820152602401610362565b6001600160a01b038381165f81815260056020908152604080832094871680845294825291829020805460ff191686151590811790915591519182527f17307eab39ab6107e8899845ad3d59bd9653f200f220920489ca2b5937696c31910160405180910390a3505050565b6001600160a01b0383163b1561082f57604051630a85bd0160e11b81526001600160a01b0384169063150b7a0290610750908890889087908790600401610ea8565b6020604051808303815f875af192505050801561078a575060408051601f3d908101601f1916820190925261078791810190610ee4565b60015b6107f1573d8080156107b7576040519150601f19603f3d011682016040523d82523d5f602084013e6107bc565b606091505b5080515f036107e957604051633250574960e11b81526001600160a01b0385166004820152602401610362565b805181602001fd5b6001600160e01b03198116630a85bd0160e11b1461082d57604051633250574960e11b81526001600160a01b0385166004820152602401610362565b505b5050505050565b60605f61084283610a8f565b60010190505f8167ffffffffffffffff81111561086157610861610d23565b6040519080825280601f01601f19166020018201604052801561088b576020820181803683370190505b5090508181016020015b5f19016f181899199a1a9b1b9c1cb0b131b232b360811b600a86061a8153600a850494508461089557509392505050565b80806108da57506001600160a01b03821615155b1561099b575f6108e984610522565b90506001600160a01b038316158015906109155750826001600160a01b0316816001600160a01b031614155b8015610928575061092681846104f5565b155b156109515760405163a9fbf51f60e01b81526001600160a01b0384166004820152602401610362565b81156109995783856001600160a01b0316826001600160a01b03167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b92560405160405180910390a45b505b50505f90815260046020526040902080546001600160a01b0319166001600160a01b0392909216919091179055565b6109d5838383610b66565b6103e5576001600160a01b038316610a0357604051637e27328960e01b815260048101829052602401610362565b60405163177e802f60e01b81526001600160a01b038316600482015260248101829052604401610362565b6001600160a01b038216610a5757604051633250574960e11b81525f6004820152602401610362565b5f610a6383835f610567565b90506001600160a01b038116156103e5576040516339e3563760e11b81525f6004820152602401610362565b5f8072184f03e93ff9f4daa797ed6e38ed64bf6a1f0160401b8310610acd5772184f03e93ff9f4daa797ed6e38ed64bf6a1f0160401b830492506040015b6d04ee2d6d415b85acef81000000008310610af9576d04ee2d6d415b85acef8100000000830492506020015b662386f26fc100008310610b1757662386f26fc10000830492506010015b6305f5e1008310610b2f576305f5e100830492506008015b6127108310610b4357612710830492506004015b60648310610b55576064830492506002015b600a83106102725760010192915050565b5f6001600160a01b03831615801590610bc25750826001600160a01b0316846001600160a01b03161480610b9f5750610b9f84846104f5565b80610bc257505f828152600460205260409020546001600160a01b038481169116145b949350505050565b6001600160e01b031981168114610bdf575f80fd5b50565b5f60208284031215610bf2575f80fd5b81356104ee81610bca565b5f81518084528060208401602086015e5f602082860101526020601f19601f83011685010191505092915050565b602081525f6104ee6020830184610bfd565b5f60208284031215610c4d575f80fd5b5035919050565b80356001600160a01b0381168114610c6a575f80fd5b919050565b5f8060408385031215610c80575f80fd5b610c8983610c54565b946020939093013593505050565b5f805f60608486031215610ca9575f80fd5b610cb284610c54565b9250610cc060208501610c54565b929592945050506040919091013590565b5f60208284031215610ce1575f80fd5b6104ee82610c54565b5f8060408385031215610cfb575f80fd5b610d0483610c54565b915060208301358015158114610d18575f80fd5b809150509250929050565b634e487b7160e01b5f52604160045260245ffd5b5f805f8060808587031215610d4a575f80fd5b610d5385610c54565b9350610d6160208601610c54565b925060408501359150606085013567ffffffffffffffff811115610d83575f80fd5b8501601f81018713610d93575f80fd5b803567ffffffffffffffff811115610dad57610dad610d23565b604051601f8201601f19908116603f0116810167ffffffffffffffff81118282101715610ddc57610ddc610d23565b604052818152828201602001891015610df3575f80fd5b816020840160208301375f6020838301015280935050505092959194509250565b5f8060408385031215610e25575f80fd5b610e2e83610c54565b9150610e3c60208401610c54565b90509250929050565b600181811c90821680610e5957607f821691505b602082108103610e7757634e487b7160e01b5f52602260045260245ffd5b50919050565b5f81518060208401855e5f93019283525090919050565b5f610bc2610ea28386610e7d565b84610e7d565b6001600160a01b03858116825284166020820152604081018390526080606082018190525f90610eda90830184610bfd565b9695505050505050565b5f60208284031215610ef4575f80fd5b81516104ee81610bca56fea26469706673582212206111a42d5528c9d21b96b99731ef12e155e44f1cc8b7cc3b860cef73550f1b3c64736f6c634300081a0033")

	abi, err := abi.JSON(strings.NewReader(definition))
	if err != nil {
		b.Fatal(err)
	}

	args := make([][]byte, b.N)

	for i := 0; i < b.N; i++ {
		arg, err := abi.Pack("safeMint", common.HexToAddress("0x0a"), big.NewInt(1))
		if err != nil {
			b.Fatal(err)
		}
		args[i] = arg
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Execute(code, args[i], nil)
	}
}
func benchmarkEVM_Create(bench *testing.B, code string) {
	var (
		statedb, _ = state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
		sender     = common.BytesToAddress([]byte("sender"))
		receiver   = common.BytesToAddress([]byte("receiver"))
	)

	statedb.CreateAccount(sender)
	statedb.SetCode(receiver, common.FromHex(code))
	runtimeConfig := Config{
		Origin:      sender,
		State:       statedb,
		GasLimit:    10000000,
		Difficulty:  big.NewInt(0x200000),
		Time:        0,
		Coinbase:    common.Address{},
		BlockNumber: new(big.Int).SetUint64(1),
		ChainConfig: &params.ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      new(big.Int),
			ByzantiumBlock:      new(big.Int),
			ConstantinopleBlock: new(big.Int),
			DAOForkBlock:        new(big.Int),
			DAOForkSupport:      false,
			EIP150Block:         new(big.Int),
			EIP155Block:         new(big.Int),
			EIP158Block:         new(big.Int),
		},
		EVMConfig: vm.Config{},
	}
	// Warm up the intpools and stuff
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		Call(receiver, []byte{}, &runtimeConfig)
	}
	bench.StopTimer()
}

func BenchmarkEVM_CREATE_500(bench *testing.B) {
	// initcode size 500K, repeatedly calls CREATE and then modifies the mem contents
	benchmarkEVM_Create(bench, "5b6207a120600080f0600152600056")
}
func BenchmarkEVM_CREATE2_500(bench *testing.B) {
	// initcode size 500K, repeatedly calls CREATE2 and then modifies the mem contents
	benchmarkEVM_Create(bench, "5b586207a120600080f5600152600056")
}
func BenchmarkEVM_CREATE_1200(bench *testing.B) {
	// initcode size 1200K, repeatedly calls CREATE and then modifies the mem contents
	benchmarkEVM_Create(bench, "5b62124f80600080f0600152600056")
}
func BenchmarkEVM_CREATE2_1200(bench *testing.B) {
	// initcode size 1200K, repeatedly calls CREATE2 and then modifies the mem contents
	benchmarkEVM_Create(bench, "5b5862124f80600080f5600152600056")
}

func fakeHeader(n uint64, parentHash common.Hash) *types.Header {
	header := types.Header{
		Coinbase:   common.HexToAddress("0x00000000000000000000000000000000deadbeef"),
		Number:     big.NewInt(int64(n)),
		ParentHash: parentHash,
		Time:       1000,
		Nonce:      types.BlockNonce{0x1},
		Extra:      []byte{},
		Difficulty: big.NewInt(0),
		GasLimit:   100000,
	}
	return &header
}

type dummyChain struct {
	counter int
}

// Engine retrieves the chain's consensus engine.
func (d *dummyChain) Engine() consensus.Engine {
	return nil
}

// GetHeader returns the hash corresponding to their hash.
func (d *dummyChain) GetHeader(h common.Hash, n uint64) *types.Header {
	d.counter++
	parentHash := common.Hash{}
	s := common.LeftPadBytes(big.NewInt(int64(n-1)).Bytes(), 32)
	copy(parentHash[:], s)

	//parentHash := common.Hash{byte(n - 1)}
	//fmt.Printf("GetHeader(%x, %d) => header with parent %x\n", h, n, parentHash)
	return fakeHeader(n, parentHash)
}

// TestBlockhash tests the blockhash operation. It's a bit special, since it internally
// requires access to a chain reader.
func TestBlockhash(t *testing.T) {
	// Current head
	n := uint64(1000)
	parentHash := common.Hash{}
	s := common.LeftPadBytes(big.NewInt(int64(n-1)).Bytes(), 32)
	copy(parentHash[:], s)
	header := fakeHeader(n, parentHash)

	// This is the contract we're using. It requests the blockhash for current num (should be all zeroes),
	// then iteratively fetches all blockhashes back to n-260.
	// It returns
	// 1. the first (should be zero)
	// 2. the second (should be the parent hash)
	// 3. the last non-zero hash
	// By making the chain reader return hashes which correlate to the number, we can
	// verify that it obtained the right hashes where it should

	/*

		pragma solidity ^0.5.3;
		contract Hasher{

			function test() public view returns (bytes32, bytes32, bytes32){
				uint256 x = block.number;
				bytes32 first;
				bytes32 last;
				bytes32 zero;
				zero = blockhash(x); // Should be zeroes
				first = blockhash(x-1);
				for(uint256 i = 2 ; i < 260; i++){
					bytes32 hash = blockhash(x - i);
					if (uint256(hash) != 0){
						last = hash;
					}
				}
				return (zero, first, last);
			}
		}

	*/
	// The contract above
	data := common.Hex2Bytes("6080604052348015600f57600080fd5b50600436106045576000357c010000000000000000000000000000000000000000000000000000000090048063f8a8fd6d14604a575b600080fd5b60506074565b60405180848152602001838152602001828152602001935050505060405180910390f35b600080600080439050600080600083409050600184034092506000600290505b61010481101560c35760008186034090506000816001900414151560b6578093505b5080806001019150506094565b508083839650965096505050505090919256fea165627a7a72305820462d71b510c1725ff35946c20b415b0d50b468ea157c8c77dff9466c9cb85f560029")
	// The method call to 'test()'
	input := common.Hex2Bytes("f8a8fd6d")
	chain := &dummyChain{}
	ret, _, err := Execute(data, input, &Config{
		GetHashFn:   core.GetHashFn(header, chain),
		BlockNumber: new(big.Int).Set(header.Number),
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(ret) != 96 {
		t.Fatalf("expected returndata to be 96 bytes, got %d", len(ret))
	}

	zero := new(big.Int).SetBytes(ret[0:32])
	first := new(big.Int).SetBytes(ret[32:64])
	last := new(big.Int).SetBytes(ret[64:96])
	if zero.BitLen() != 0 {
		t.Fatalf("expected zeroes, got %x", ret[0:32])
	}
	if first.Uint64() != 999 {
		t.Fatalf("second block should be 999, got %d (%x)", first, ret[32:64])
	}
	if last.Uint64() != 744 {
		t.Fatalf("last block should be 744, got %d (%x)", last, ret[64:96])
	}
	if exp, got := 255, chain.counter; exp != got {
		t.Errorf("suboptimal; too much chain iteration, expected %d, got %d", exp, got)
	}
}

// benchmarkNonModifyingCode benchmarks code, but if the code modifies the
// state, this should not be used, since it does not reset the state between runs.
func benchmarkNonModifyingCode(gas uint64, code []byte, name string, tracerCode string, b *testing.B) {
	cfg := new(Config)
	setDefaults(cfg)
	cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	cfg.GasLimit = gas
	if len(tracerCode) > 0 {
		tracer, err := tracers.DefaultDirectory.New(tracerCode, new(tracers.Context), nil)
		if err != nil {
			b.Fatal(err)
		}
		cfg.EVMConfig = vm.Config{
			Tracer: tracer,
		}
	}
	var (
		destination = common.BytesToAddress([]byte("contract"))
		vmenv       = NewEnv(cfg)
		sender      = vm.AccountRef(cfg.Origin)
	)
	cfg.State.CreateAccount(destination)
	eoa := common.HexToAddress("E0")
	{
		cfg.State.CreateAccount(eoa)
		cfg.State.SetNonce(eoa, 100)
	}
	reverting := common.HexToAddress("EE")
	{
		cfg.State.CreateAccount(reverting)
		cfg.State.SetCode(reverting, []byte{
			byte(vm.PUSH1), 0x00,
			byte(vm.PUSH1), 0x00,
			byte(vm.REVERT),
		})
	}

	//cfg.State.CreateAccount(cfg.Origin)
	// set the receiver's (the executing contract) code for execution.
	cfg.State.SetCode(destination, code)
	vmenv.Call(sender, destination, nil, gas, uint256.MustFromBig(cfg.Value))

	b.Run(name, func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			vmenv.Call(sender, destination, nil, gas, uint256.MustFromBig(cfg.Value))
		}
	})
}

// BenchmarkSimpleLoop test a pretty simple loop which loops until OOG
// 55 ms
func BenchmarkSimpleLoop(b *testing.B) {
	staticCallIdentity := []byte{
		byte(vm.JUMPDEST), //  [ count ]
		// push args for the call
		byte(vm.PUSH1), 0, // out size
		byte(vm.DUP1),       // out offset
		byte(vm.DUP1),       // out insize
		byte(vm.DUP1),       // in offset
		byte(vm.PUSH1), 0x4, // address of identity
		byte(vm.GAS), // gas
		byte(vm.STATICCALL),
		byte(vm.POP),      // pop return value
		byte(vm.PUSH1), 0, // jumpdestination
		byte(vm.JUMP),
	}

	callIdentity := []byte{
		byte(vm.JUMPDEST), //  [ count ]
		// push args for the call
		byte(vm.PUSH1), 0, // out size
		byte(vm.DUP1),       // out offset
		byte(vm.DUP1),       // out insize
		byte(vm.DUP1),       // in offset
		byte(vm.DUP1),       // value
		byte(vm.PUSH1), 0x4, // address of identity
		byte(vm.GAS), // gas
		byte(vm.CALL),
		byte(vm.POP),      // pop return value
		byte(vm.PUSH1), 0, // jumpdestination
		byte(vm.JUMP),
	}

	callInexistant := []byte{
		byte(vm.JUMPDEST), //  [ count ]
		// push args for the call
		byte(vm.PUSH1), 0, // out size
		byte(vm.DUP1),        // out offset
		byte(vm.DUP1),        // out insize
		byte(vm.DUP1),        // in offset
		byte(vm.DUP1),        // value
		byte(vm.PUSH1), 0xff, // address of existing contract
		byte(vm.GAS), // gas
		byte(vm.CALL),
		byte(vm.POP),      // pop return value
		byte(vm.PUSH1), 0, // jumpdestination
		byte(vm.JUMP),
	}

	callEOA := []byte{
		byte(vm.JUMPDEST), //  [ count ]
		// push args for the call
		byte(vm.PUSH1), 0, // out size
		byte(vm.DUP1),        // out offset
		byte(vm.DUP1),        // out insize
		byte(vm.DUP1),        // in offset
		byte(vm.DUP1),        // value
		byte(vm.PUSH1), 0xE0, // address of EOA
		byte(vm.GAS), // gas
		byte(vm.CALL),
		byte(vm.POP),      // pop return value
		byte(vm.PUSH1), 0, // jumpdestination
		byte(vm.JUMP),
	}

	loopingCode := []byte{
		byte(vm.JUMPDEST), //  [ count ]
		// push args for the call
		byte(vm.PUSH1), 0, // out size
		byte(vm.DUP1),       // out offset
		byte(vm.DUP1),       // out insize
		byte(vm.DUP1),       // in offset
		byte(vm.PUSH1), 0x4, // address of identity
		byte(vm.GAS), // gas

		byte(vm.POP), byte(vm.POP), byte(vm.POP), byte(vm.POP), byte(vm.POP), byte(vm.POP),
		byte(vm.PUSH1), 0, // jumpdestination
		byte(vm.JUMP),
	}

	callRevertingContractWithInput := []byte{
		byte(vm.JUMPDEST), //
		// push args for the call
		byte(vm.PUSH1), 0, // out size
		byte(vm.DUP1),        // out offset
		byte(vm.PUSH1), 0x20, // in size
		byte(vm.PUSH1), 0x00, // in offset
		byte(vm.PUSH1), 0x00, // value
		byte(vm.PUSH1), 0xEE, // address of reverting contract
		byte(vm.GAS), // gas
		byte(vm.CALL),
		byte(vm.POP),      // pop return value
		byte(vm.PUSH1), 0, // jumpdestination
		byte(vm.JUMP),
	}

	//tracer := logger.NewJSONLogger(nil, os.Stdout)
	//Execute(loopingCode, nil, &Config{
	//	EVMConfig: vm.Config{
	//		Debug:  true,
	//		Tracer: tracer,
	//	}})
	// 100M gas
	benchmarkNonModifyingCode(100000000, staticCallIdentity, "staticcall-identity-100M", "", b)
	benchmarkNonModifyingCode(100000000, callIdentity, "call-identity-100M", "", b)
	benchmarkNonModifyingCode(100000000, loopingCode, "loop-100M", "", b)
	benchmarkNonModifyingCode(100000000, callInexistant, "call-nonexist-100M", "", b)
	benchmarkNonModifyingCode(100000000, callEOA, "call-EOA-100M", "", b)
	benchmarkNonModifyingCode(100000000, callRevertingContractWithInput, "call-reverting-100M", "", b)

	//benchmarkNonModifyingCode(10000000, staticCallIdentity, "staticcall-identity-10M", b)
	//benchmarkNonModifyingCode(10000000, loopingCode, "loop-10M", b)
}

// TestEip2929Cases contains various testcases that are used for
// EIP-2929 about gas repricings
func TestEip2929Cases(t *testing.T) {
	t.Skip("Test only useful for generating documentation")
	id := 1
	prettyPrint := func(comment string, code []byte) {
		instrs := make([]string, 0)
		it := asm.NewInstructionIterator(code)
		for it.Next() {
			if it.Arg() != nil && 0 < len(it.Arg()) {
				instrs = append(instrs, fmt.Sprintf("%v %#x", it.Op(), it.Arg()))
			} else {
				instrs = append(instrs, fmt.Sprintf("%v", it.Op()))
			}
		}
		ops := strings.Join(instrs, ", ")
		fmt.Printf("### Case %d\n\n", id)
		id++
		fmt.Printf("%v\n\nBytecode: \n```\n%#x\n```\nOperations: \n```\n%v\n```\n\n",
			comment,
			code, ops)
		Execute(code, nil, &Config{
			EVMConfig: vm.Config{
				Tracer:    logger.NewMarkdownLogger(nil, os.Stdout),
				ExtraEips: []int{2929},
			},
		})
	}

	{ // First eip testcase
		code := []byte{
			// Three checks against a precompile
			byte(vm.PUSH1), 1, byte(vm.EXTCODEHASH), byte(vm.POP),
			byte(vm.PUSH1), 2, byte(vm.EXTCODESIZE), byte(vm.POP),
			byte(vm.PUSH1), 3, byte(vm.BALANCE), byte(vm.POP),
			// Three checks against a non-precompile
			byte(vm.PUSH1), 0xf1, byte(vm.EXTCODEHASH), byte(vm.POP),
			byte(vm.PUSH1), 0xf2, byte(vm.EXTCODESIZE), byte(vm.POP),
			byte(vm.PUSH1), 0xf3, byte(vm.BALANCE), byte(vm.POP),
			// Same three checks (should be cheaper)
			byte(vm.PUSH1), 0xf2, byte(vm.EXTCODEHASH), byte(vm.POP),
			byte(vm.PUSH1), 0xf3, byte(vm.EXTCODESIZE), byte(vm.POP),
			byte(vm.PUSH1), 0xf1, byte(vm.BALANCE), byte(vm.POP),
			// Check the origin, and the 'this'
			byte(vm.ORIGIN), byte(vm.BALANCE), byte(vm.POP),
			byte(vm.ADDRESS), byte(vm.BALANCE), byte(vm.POP),

			byte(vm.STOP),
		}
		prettyPrint("This checks `EXT`(codehash,codesize,balance) of precompiles, which should be `100`, "+
			"and later checks the same operations twice against some non-precompiles. "+
			"Those are cheaper second time they are accessed. Lastly, it checks the `BALANCE` of `origin` and `this`.", code)
	}

	{ // EXTCODECOPY
		code := []byte{
			// extcodecopy( 0xff,0,0,0,0)
			byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, //length, codeoffset, memoffset
			byte(vm.PUSH1), 0xff, byte(vm.EXTCODECOPY),
			// extcodecopy( 0xff,0,0,0,0)
			byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, //length, codeoffset, memoffset
			byte(vm.PUSH1), 0xff, byte(vm.EXTCODECOPY),
			// extcodecopy( this,0,0,0,0)
			byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, //length, codeoffset, memoffset
			byte(vm.ADDRESS), byte(vm.EXTCODECOPY),

			byte(vm.STOP),
		}
		prettyPrint("This checks `extcodecopy( 0xff,0,0,0,0)` twice, (should be expensive first time), "+
			"and then does `extcodecopy( this,0,0,0,0)`.", code)
	}

	{ // SLOAD + SSTORE
		code := []byte{

			// Add slot `0x1` to access list
			byte(vm.PUSH1), 0x01, byte(vm.SLOAD), byte(vm.POP), // SLOAD( 0x1) (add to access list)
			// Write to `0x1` which is already in access list
			byte(vm.PUSH1), 0x11, byte(vm.PUSH1), 0x01, byte(vm.SSTORE), // SSTORE( loc: 0x01, val: 0x11)
			// Write to `0x2` which is not in access list
			byte(vm.PUSH1), 0x11, byte(vm.PUSH1), 0x02, byte(vm.SSTORE), // SSTORE( loc: 0x02, val: 0x11)
			// Write again to `0x2`
			byte(vm.PUSH1), 0x11, byte(vm.PUSH1), 0x02, byte(vm.SSTORE), // SSTORE( loc: 0x02, val: 0x11)
			// Read slot in access list (0x2)
			byte(vm.PUSH1), 0x02, byte(vm.SLOAD), // SLOAD( 0x2)
			// Read slot in access list (0x1)
			byte(vm.PUSH1), 0x01, byte(vm.SLOAD), // SLOAD( 0x1)
		}
		prettyPrint("This checks `sload( 0x1)` followed by `sstore(loc: 0x01, val:0x11)`, then 'naked' sstore:"+
			"`sstore(loc: 0x02, val:0x11)` twice, and `sload(0x2)`, `sload(0x1)`. ", code)
	}
	{ // Call variants
		code := []byte{
			// identity precompile
			byte(vm.PUSH1), 0x0, byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1),
			byte(vm.PUSH1), 0x04, byte(vm.PUSH1), 0x0, byte(vm.CALL), byte(vm.POP),

			// random account - call 1
			byte(vm.PUSH1), 0x0, byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1),
			byte(vm.PUSH1), 0xff, byte(vm.PUSH1), 0x0, byte(vm.CALL), byte(vm.POP),

			// random account - call 2
			byte(vm.PUSH1), 0x0, byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1),
			byte(vm.PUSH1), 0xff, byte(vm.PUSH1), 0x0, byte(vm.STATICCALL), byte(vm.POP),
		}
		prettyPrint("This calls the `identity`-precompile (cheap), then calls an account (expensive) and `staticcall`s the same"+
			"account (cheap)", code)
	}
}

// TestColdAccountAccessCost test that the cold account access cost is reported
// correctly
// see: https://github.com/ethereum/go-ethereum/issues/22649
func TestColdAccountAccessCost(t *testing.T) {
	for i, tc := range []struct {
		code []byte
		step int
		want uint64
	}{
		{ // EXTCODEHASH(0xff)
			code: []byte{byte(vm.PUSH1), 0xFF, byte(vm.EXTCODEHASH), byte(vm.POP)},
			step: 1,
			want: 2600,
		},
		{ // BALANCE(0xff)
			code: []byte{byte(vm.PUSH1), 0xFF, byte(vm.BALANCE), byte(vm.POP)},
			step: 1,
			want: 2600,
		},
		{ // CALL(0xff)
			code: []byte{
				byte(vm.PUSH1), 0x0,
				byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1),
				byte(vm.PUSH1), 0xff, byte(vm.DUP1), byte(vm.CALL), byte(vm.POP),
			},
			step: 7,
			want: 2855,
		},
		{ // CALLCODE(0xff)
			code: []byte{
				byte(vm.PUSH1), 0x0,
				byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1),
				byte(vm.PUSH1), 0xff, byte(vm.DUP1), byte(vm.CALLCODE), byte(vm.POP),
			},
			step: 7,
			want: 2855,
		},
		{ // DELEGATECALL(0xff)
			code: []byte{
				byte(vm.PUSH1), 0x0,
				byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1),
				byte(vm.PUSH1), 0xff, byte(vm.DUP1), byte(vm.DELEGATECALL), byte(vm.POP),
			},
			step: 6,
			want: 2855,
		},
		{ // STATICCALL(0xff)
			code: []byte{
				byte(vm.PUSH1), 0x0,
				byte(vm.DUP1), byte(vm.DUP1), byte(vm.DUP1),
				byte(vm.PUSH1), 0xff, byte(vm.DUP1), byte(vm.STATICCALL), byte(vm.POP),
			},
			step: 6,
			want: 2855,
		},
		{ // SELFDESTRUCT(0xff)
			code: []byte{
				byte(vm.PUSH1), 0xff, byte(vm.SELFDESTRUCT),
			},
			step: 1,
			want: 7600,
		},
	} {
		tracer := logger.NewStructLogger(nil)
		Execute(tc.code, nil, &Config{
			EVMConfig: vm.Config{
				Tracer: tracer,
			},
		})
		have := tracer.StructLogs()[tc.step].GasCost
		if want := tc.want; have != want {
			for ii, op := range tracer.StructLogs() {
				t.Logf("%d: %v %d", ii, op.OpName(), op.GasCost)
			}
			t.Fatalf("testcase %d, gas report wrong, step %d, have %d want %d", i, tc.step, have, want)
		}
	}
}

func TestRuntimeJSTracer(t *testing.T) {
	jsTracers := []string{
		`{enters: 0, exits: 0, enterGas: 0, gasUsed: 0, steps:0,
	step: function() { this.steps++},
	fault: function() {},
	result: function() {
		return [this.enters, this.exits,this.enterGas,this.gasUsed, this.steps].join(",")
	},
	enter: function(frame) {
		this.enters++;
		this.enterGas = frame.getGas();
	},
	exit: function(res) {
		this.exits++;
		this.gasUsed = res.getGasUsed();
	}}`,
		`{enters: 0, exits: 0, enterGas: 0, gasUsed: 0, steps:0,
	fault: function() {},
	result: function() {
		return [this.enters, this.exits,this.enterGas,this.gasUsed, this.steps].join(",")
	},
	enter: function(frame) {
		this.enters++;
		this.enterGas = frame.getGas();
	},
	exit: function(res) {
		this.exits++;
		this.gasUsed = res.getGasUsed();
	}}`}
	tests := []struct {
		code []byte
		// One result per tracer
		results []string
	}{
		{
			// CREATE
			code: []byte{
				// Store initcode in memory at 0x00 (5 bytes left-padded to 32 bytes)
				byte(vm.PUSH5),
				// Init code: PUSH1 0, PUSH1 0, RETURN (3 steps)
				byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.RETURN),
				byte(vm.PUSH1), 0,
				byte(vm.MSTORE),
				// length, offset, value
				byte(vm.PUSH1), 5, byte(vm.PUSH1), 27, byte(vm.PUSH1), 0,
				byte(vm.CREATE),
				byte(vm.POP),
			},
			results: []string{`"1,1,952855,6,12"`, `"1,1,952855,6,0"`},
		},
		{
			// CREATE2
			code: []byte{
				// Store initcode in memory at 0x00 (5 bytes left-padded to 32 bytes)
				byte(vm.PUSH5),
				// Init code: PUSH1 0, PUSH1 0, RETURN (3 steps)
				byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.RETURN),
				byte(vm.PUSH1), 0,
				byte(vm.MSTORE),
				// salt, length, offset, value
				byte(vm.PUSH1), 1, byte(vm.PUSH1), 5, byte(vm.PUSH1), 27, byte(vm.PUSH1), 0,
				byte(vm.CREATE2),
				byte(vm.POP),
			},
			results: []string{`"1,1,952846,6,13"`, `"1,1,952846,6,0"`},
		},
		{
			// CALL
			code: []byte{
				// outsize, outoffset, insize, inoffset
				byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0,
				byte(vm.PUSH1), 0, // value
				byte(vm.PUSH1), 0xbb, //address
				byte(vm.GAS), // gas
				byte(vm.CALL),
				byte(vm.POP),
			},
			results: []string{`"1,1,981796,6,13"`, `"1,1,981796,6,0"`},
		},
		{
			// CALLCODE
			code: []byte{
				// outsize, outoffset, insize, inoffset
				byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0,
				byte(vm.PUSH1), 0, // value
				byte(vm.PUSH1), 0xcc, //address
				byte(vm.GAS), // gas
				byte(vm.CALLCODE),
				byte(vm.POP),
			},
			results: []string{`"1,1,981796,6,13"`, `"1,1,981796,6,0"`},
		},
		{
			// STATICCALL
			code: []byte{
				// outsize, outoffset, insize, inoffset
				byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0,
				byte(vm.PUSH1), 0xdd, //address
				byte(vm.GAS), // gas
				byte(vm.STATICCALL),
				byte(vm.POP),
			},
			results: []string{`"1,1,981799,6,12"`, `"1,1,981799,6,0"`},
		},
		{
			// DELEGATECALL
			code: []byte{
				// outsize, outoffset, insize, inoffset
				byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0,
				byte(vm.PUSH1), 0xee, //address
				byte(vm.GAS), // gas
				byte(vm.DELEGATECALL),
				byte(vm.POP),
			},
			results: []string{`"1,1,981799,6,12"`, `"1,1,981799,6,0"`},
		},
		{
			// CALL self-destructing contract
			code: []byte{
				// outsize, outoffset, insize, inoffset
				byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0,
				byte(vm.PUSH1), 0, // value
				byte(vm.PUSH1), 0xff, //address
				byte(vm.GAS), // gas
				byte(vm.CALL),
				byte(vm.POP),
			},
			results: []string{`"2,2,0,5003,12"`, `"2,2,0,5003,0"`},
		},
	}
	calleeCode := []byte{
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	}
	depressedCode := []byte{
		byte(vm.PUSH1), 0xaa,
		byte(vm.SELFDESTRUCT),
	}
	main := common.HexToAddress("0xaa")
	for i, jsTracer := range jsTracers {
		for j, tc := range tests {
			statedb, _ := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
			statedb.SetCode(main, tc.code)
			statedb.SetCode(common.HexToAddress("0xbb"), calleeCode)
			statedb.SetCode(common.HexToAddress("0xcc"), calleeCode)
			statedb.SetCode(common.HexToAddress("0xdd"), calleeCode)
			statedb.SetCode(common.HexToAddress("0xee"), calleeCode)
			statedb.SetCode(common.HexToAddress("0xff"), depressedCode)

			tracer, err := tracers.DefaultDirectory.New(jsTracer, new(tracers.Context), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = Call(main, nil, &Config{
				GasLimit: 1000000,
				State:    statedb,
				EVMConfig: vm.Config{
					Tracer: tracer,
				}})
			if err != nil {
				t.Fatal("didn't expect error", err)
			}
			res, err := tracer.GetResult()
			if err != nil {
				t.Fatal(err)
			}
			if have, want := string(res), tc.results[i]; have != want {
				t.Errorf("wrong result for tracer %d testcase %d, have \n%v\nwant\n%v\n", i, j, have, want)
			}
		}
	}
}

func TestJSTracerCreateTx(t *testing.T) {
	jsTracer := `
	{enters: 0, exits: 0,
	step: function() {},
	fault: function() {},
	result: function() { return [this.enters, this.exits].join(",") },
	enter: function(frame) { this.enters++ },
	exit: function(res) { this.exits++ }}`
	code := []byte{byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.RETURN)}

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	tracer, err := tracers.DefaultDirectory.New(jsTracer, new(tracers.Context), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = Create(code, &Config{
		State: statedb,
		EVMConfig: vm.Config{
			Tracer: tracer,
		}})
	if err != nil {
		t.Fatal(err)
	}

	res, err := tracer.GetResult()
	if err != nil {
		t.Fatal(err)
	}
	if have, want := string(res), `"0,0"`; have != want {
		t.Errorf("wrong result for tracer, have \n%v\nwant\n%v\n", have, want)
	}
}

func BenchmarkTracerStepVsCallFrame(b *testing.B) {
	// Simply pushes and pops some values in a loop
	code := []byte{
		byte(vm.JUMPDEST),
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
		byte(vm.POP),
		byte(vm.POP),
		byte(vm.PUSH1), 0, // jumpdestination
		byte(vm.JUMP),
	}

	stepTracer := `
	{
	step: function() {},
	fault: function() {},
	result: function() {},
	}`
	callFrameTracer := `
	{
	enter: function() {},
	exit: function() {},
	fault: function() {},
	result: function() {},
	}`

	benchmarkNonModifyingCode(10000000, code, "tracer-step-10M", stepTracer, b)
	benchmarkNonModifyingCode(10000000, code, "tracer-call-frame-10M", callFrameTracer, b)
}
