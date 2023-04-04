// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
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

package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

//go:generate go run github.com/fjl/gencodec -type Genesis -field-override genesisSpecMarshaling -out gen_genesis.go
//go:generate go run github.com/fjl/gencodec -type GenesisAccount -field-override genesisAccountMarshaling -out gen_genesis_account.go

var errGenesisNoConfig = errors.New("genesis has no chain configuration")

// Genesis specifies the header fields, state of a genesis block. It also defines hard
// fork switch-over blocks through the chain configuration.
type Genesis struct {
	Config     *params.ChainConfig `json:"config"`
	Nonce      uint64              `json:"nonce"`
	Timestamp  uint64              `json:"timestamp"`
	ExtraData  []byte              `json:"extraData"`
	GasLimit   uint64              `json:"gasLimit"   gencodec:"required"`
	Difficulty *big.Int            `json:"difficulty" gencodec:"required"`
	Mixhash    common.Hash         `json:"mixHash"`
	Coinbase   common.Address      `json:"coinbase"`
	Alloc      GenesisAlloc        `json:"alloc"      gencodec:"required"`

	// Camino genesis
	InitialAdmin                   common.Address `json:"initialAdmin"`
	FeeRewardExportMinAmount       uint64         `json:"feeRewardExportMinAmount"`
	FeeRewardExportMinTimeInterval uint64         `json:"feeRewardExportMinTimeInterval"`

	// These fields are used for consensus tests. Please don't use them
	// in actual genesis blocks.
	Number     uint64      `json:"number"`
	GasUsed    uint64      `json:"gasUsed"`
	ParentHash common.Hash `json:"parentHash"`
	BaseFee    *big.Int    `json:"baseFeePerGas"`
}

// GenesisAlloc specifies the initial state that is part of the genesis block.
type GenesisAlloc map[common.Address]GenesisAccount

func (ga *GenesisAlloc) UnmarshalJSON(data []byte) error {
	m := make(map[common.UnprefixedAddress]GenesisAccount)
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*ga = make(GenesisAlloc)
	for addr, a := range m {
		(*ga)[common.Address(addr)] = a
	}
	return nil
}

type GenesisMultiCoinBalance map[common.Hash]*big.Int

// GenesisAccount is an account in the state of the genesis block.
type GenesisAccount struct {
	Code       []byte                      `json:"code,omitempty"`
	Storage    map[common.Hash]common.Hash `json:"storage,omitempty"`
	Balance    *big.Int                    `json:"balance" gencodec:"required"`
	MCBalance  GenesisMultiCoinBalance     `json:"mcbalance,omitempty"`
	Nonce      uint64                      `json:"nonce,omitempty"`
	PrivateKey []byte                      `json:"secretKey,omitempty"` // for tests
}

// field type overrides for gencodec
type genesisSpecMarshaling struct {
	Nonce      math.HexOrDecimal64
	Timestamp  math.HexOrDecimal64
	ExtraData  hexutil.Bytes
	GasLimit   math.HexOrDecimal64
	GasUsed    math.HexOrDecimal64
	Number     math.HexOrDecimal64
	Difficulty *math.HexOrDecimal256
	BaseFee    *math.HexOrDecimal256
	Alloc      map[common.UnprefixedAddress]GenesisAccount
}

type genesisAccountMarshaling struct {
	Code       hexutil.Bytes
	Balance    *math.HexOrDecimal256
	Nonce      math.HexOrDecimal64
	Storage    map[storageJSON]storageJSON
	PrivateKey hexutil.Bytes
}

// storageJSON represents a 256 bit byte array, but allows less than 256 bits when
// unmarshaling from hex.
type storageJSON common.Hash

func (h *storageJSON) UnmarshalText(text []byte) error {
	text = bytes.TrimPrefix(text, []byte("0x"))
	if len(text) > 64 {
		return fmt.Errorf("too many hex characters in storage key/value %q", text)
	}
	offset := len(h) - len(text)/2 // pad on the left
	if _, err := hex.Decode(h[offset:], text); err != nil {
		fmt.Println(err)
		return fmt.Errorf("invalid hex storage key/value %q", text)
	}
	return nil
}

func (h storageJSON) MarshalText() ([]byte, error) {
	return hexutil.Bytes(h[:]).MarshalText()
}

// GenesisMismatchError is raised when trying to overwrite an existing
// genesis block with an incompatible one.
type GenesisMismatchError struct {
	Stored, New common.Hash
}

func (e *GenesisMismatchError) Error() string {
	return fmt.Sprintf("database contains incompatible genesis (have %x, new %x)", e.Stored, e.New)
}

// SetupGenesisBlock writes or updates the genesis block in db.
// The block that will be used is:
//
//	                     genesis == nil       genesis != nil
//	                  +------------------------------------------
//	db has no genesis |  main-net default  |  genesis
//	db has genesis    |  from DB           |  genesis (if compatible)
//
// The stored chain configuration will be updated if it is compatible (i.e. does not
// specify a fork block below the local head block). In case of a conflict, the
// error is a *params.ConfigCompatError and the new, unwritten config is returned.
func SetupGenesisBlock(
	db ethdb.Database, genesis *Genesis, lastAcceptedHash common.Hash, skipChainConfigCheckCompatible bool,
) (*params.ChainConfig, error) {
	if genesis == nil {
		return nil, ErrNoGenesis
	}
	if genesis.Config == nil {
		return nil, errGenesisNoConfig
	}
	// Just commit the new block if there is no stored genesis block.
	stored := rawdb.ReadCanonicalHash(db, 0)
	if (stored == common.Hash{}) {
		log.Info("Writing genesis to database")
		_, err := genesis.Commit(db)
		if err != nil {
			return genesis.Config, err
		}
		return genesis.Config, nil
	}
	// We have the genesis block in database but the corresponding state is missing.
	header := rawdb.ReadHeader(db, stored, 0)
	if _, err := state.New(header.Root, state.NewDatabase(db), nil); err != nil {
		// Ensure the stored genesis matches with the given one.
		hash := genesis.ToBlock(nil).Hash()
		if hash != stored {
			return genesis.Config, &GenesisMismatchError{stored, hash}
		}
		_, err := genesis.Commit(db)
		return genesis.Config, err
	}
	// Check whether the genesis block is already written.
	hash := genesis.ToBlock(nil).Hash()
	if hash != stored {
		return genesis.Config, &GenesisMismatchError{stored, hash}
	}
	// Get the existing chain configuration.
	newcfg := genesis.Config
	if err := newcfg.CheckConfigForkOrder(); err != nil {
		return newcfg, err
	}
	storedcfg := rawdb.ReadChainConfig(db, stored)
	if storedcfg == nil {
		log.Warn("Found genesis block without chain config")
		rawdb.WriteChainConfig(db, stored, newcfg)
		return newcfg, nil
	}

	// Check config compatibility and write the config. Compatibility errors
	// are returned to the caller unless we're already at block zero.
	// we use last accepted block for cfg compatibility check. Note this allows
	// the node to continue if it previously halted due to attempting to process blocks with
	// an incorrect chain config.
	lastBlock := ReadBlockByHash(db, lastAcceptedHash)
	// this should never happen, but we check anyway
	// when we start syncing from scratch, the last accepted block
	// will be genesis block
	if lastBlock == nil {
		return newcfg, fmt.Errorf("missing last accepted block")
	}
	height := lastBlock.NumberU64()
	timestamp := lastBlock.Time()
	if skipChainConfigCheckCompatible {
		log.Info("skipping verifying activated network upgrades on chain config")
	} else {
		compatErr := storedcfg.CheckCompatible(newcfg, height, timestamp)
		if compatErr != nil && height != 0 && compatErr.RewindTo != 0 {
			return newcfg, compatErr
		}
	}
	rawdb.WriteChainConfig(db, stored, newcfg)
	return newcfg, nil
}

// ToBlock creates the genesis block and writes state of a genesis specification
// to the given database (or discards it if nil).
func (g *Genesis) ToBlock(db ethdb.Database) *types.Block {
	if db == nil {
		db = rawdb.NewMemoryDatabase()
	}
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	if err != nil {
		panic(err)
	}

	head := &types.Header{
		Number:     new(big.Int).SetUint64(g.Number),
		Nonce:      types.EncodeNonce(g.Nonce),
		Time:       g.Timestamp,
		ParentHash: g.ParentHash,
		Extra:      g.ExtraData,
		GasLimit:   g.GasLimit,
		GasUsed:    g.GasUsed,
		BaseFee:    g.BaseFee,
		Difficulty: g.Difficulty,
		MixDigest:  g.Mixhash,
		Coinbase:   g.Coinbase,
	}

	// Configure any stateful precompiles that should be enabled in the genesis.
	g.Config.CheckConfigurePrecompiles(nil, types.NewBlockWithHeader(head), statedb)

	for addr, account := range g.Alloc {
		statedb.AddBalance(addr, account.Balance)
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
		if account.MCBalance != nil {
			for coinID, value := range account.MCBalance {
				statedb.AddBalanceMultiCoin(addr, coinID, value)
			}
		}
	}
	root := statedb.IntermediateRoot(false)
	head.Root = root

	if g.GasLimit == 0 {
		head.GasLimit = params.GenesisGasLimit
	}
	if g.Difficulty == nil {
		head.Difficulty = params.GenesisDifficulty
	}
	if g.Config != nil && g.Config.IsApricotPhase3(common.Big0) {
		if g.BaseFee != nil {
			head.BaseFee = g.BaseFee
		} else {
			head.BaseFee = big.NewInt(params.ApricotPhase3InitialBaseFee)
		}
	}
	statedb.Commit(false, false)
	if err := statedb.Database().TrieDB().Commit(root, true, nil); err != nil {
		panic(fmt.Sprintf("unable to commit genesis block: %v", err))
	}

	return types.NewBlock(head, nil, nil, nil, trie.NewStackTrie(nil), nil, false)
}

func (g *Genesis) PreDeploy() error {
	eip1967Key := common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")
	adminKey := common.BytesToHash(crypto.Keccak256(append(g.InitialAdmin.Hash().Bytes(), common.Hash{}.Bytes()...)))

	// Deploy AdminProxy Contract
	implAddress := common.HexToAddress("0x010000000000000000000000000000000000000b")
	g.Alloc[common.HexToAddress("0x010000000000000000000000000000000000000a")] = GenesisAccount{
		Balance: common.Big0,
		Code:    common.Hex2Bytes("6080604052600436106100225760003560e01c8063d784d4261461004b57610039565b3661003957610037610032610074565b6100a5565b005b610049610044610074565b6100a5565b005b34801561005757600080fd5b50610072600480360381019061006d919061024d565b6100cb565b005b6000807f360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc60001b9050805491505090565b3660008037600080366000845af43d6000803e80600081146100c6573d6000f35b3d6000fd5b3073ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff1614610139576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610130906102e0565b60405180910390fd5b610142816101f3565b610181576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610178906102c0565b60405180910390fd5b60007f360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc60001b90508181558173ffffffffffffffffffffffffffffffffffffffff167fbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b60405160405180910390a25050565b600080823f90506000801b811415801561023057507fc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a47060001b8114155b915050919050565b6000813590506102478161039a565b92915050565b60006020828403121561026357610262610343565b5b600061027184828501610238565b91505092915050565b6000610287600e83610300565b915061029282610348565b602082019050919050565b60006102aa600d83610300565b91506102b582610371565b602082019050919050565b600060208201905081810360008301526102d98161027a565b9050919050565b600060208201905081810360008301526102f98161029d565b9050919050565b600082825260208201905092915050565b600061031c82610323565b9050919050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b600080fd5b7f4e6f74206120636f6e7472616374000000000000000000000000000000000000600082015250565b7f4163636573732064656e69656400000000000000000000000000000000000000600082015250565b6103a381610311565b81146103ae57600080fd5b5056fea2646970667358221220eaad273d24fb4c2d97d0a6ec9110de5905a399afa8c25558441ce29ac70ed7f964736f6c63430008070033"),
		Storage: map[common.Hash]common.Hash{
			eip1967Key: implAddress.Hash(),
			adminKey:   common.HexToHash("0x01"),
		},
	}
	// Deploy AdminImpl Contract
	// compiled with 0.8.17+commit.8df45f5f
	// with 200 optimize runs
	g.Alloc[implAddress] = GenesisAccount{
		Balance: common.Big0,
		Code:    common.Hex2Bytes("608060405234801561001057600080fd5b50600436106100a95760003560e01c80634de525d9116100715780634de525d9146101135780635c97f4a21461014b5780639a11b2e81461016e578063bb03fa1d14610181578063ce6ccfaf146101aa578063fdff0b26146101d357600080fd5b80630900f010146100ae5780630912ed77146100c357806315e812ad146100d65780633c09e2fd146100ed5780634686069814610100575b600080fd5b6100c16100bc366004610586565b6101e6565b005b6100c16100d13660046105a1565b61027b565b6001545b6040519081526020015b60405180910390f35b6100c16100fb3660046105a1565b6102b2565b6100c161010e3660046105cb565b6102e4565b6100da6101213660046105fc565b6001600160a01b039190911663ffffffff60a01b604092831c161760009081526003602052205490565b61015e6101593660046105a1565b610349565b60405190151581526020016100e4565b6100c161017c36600461062f565b61035c565b6100da61018f366004610586565b6001600160a01b031660009081526002602052604090205490565b6100da6101b8366004610586565b6001600160a01b031660009081526020819052604090205490565b6100c16101e1366004610673565b610412565b60016101f23382610464565b6102175760405162461bcd60e51b815260040161020e906106af565b60405180910390fd5b604051636bc26a1360e11b81526001600160a01b0383166004820152600a600160981b019063d784d42690602401600060405180830381600087803b15801561025f57600080fd5b505af1158015610273573d6000803e3d6000fd5b505050505050565b60016102873382610464565b6102a35760405162461bcd60e51b815260040161020e906106af565b6102ad8383610485565b505050565b60016102be3382610464565b6102da5760405162461bcd60e51b815260040161020e906106af565b6102ad83836104d8565b60026102f03382610464565b61030c5760405162461bcd60e51b815260040161020e906106af565b60018290556040518281527f1c053aa9b674900648619554980ac10e913d661c372ca30455e1e4ec0ce44071906020015b60405180910390a15050565b60006103558383610464565b9392505050565b60046103683382610464565b6103845760405162461bcd60e51b815260040161020e906106af565b6001600160a01b03841660009081526002602052604081205490846103ab578382176103b0565b831982165b6001600160a01b038716600081815260026020908152604091829020849055815186815290810184905292935090917ff64784c1c207eed151b4adc53adde03b1c3a4ecad6b8de3a65539d464b3e1add910160405180910390a2505050505050565b600861041e3382610464565b61043a5760405162461bcd60e51b815260040161020e906106af565b506001600160a01b039290921663ffffffff60a01b604092831c1617600090815260036020522055565b6001600160a01b039190911660009081526020819052604090205416151590565b6001600160a01b0382166000818152602081815260409182902080548519169055815192835282018390527fcfa5316bd1be4ceb62f363b0a162f322c33ba870641138cd8600dd4fa603fc3b910161033d565b60088111156105185760405162461bcd60e51b815260206004820152600c60248201526b556e6b6e6f776e20526f6c6560a01b604482015260640161020e565b6001600160a01b03821660008181526020818152604091829020805485179055815192835282018390527f385a9c70004a48177c93b74796d77d5ebf7e1248f9e2369624514da454cd01b0910161033d565b80356001600160a01b038116811461058157600080fd5b919050565b60006020828403121561059857600080fd5b6103558261056a565b600080604083850312156105b457600080fd5b6105bd8361056a565b946020939093013593505050565b6000602082840312156105dd57600080fd5b5035919050565b80356001600160e01b03198116811461058157600080fd5b6000806040838503121561060f57600080fd5b6106188361056a565b9150610626602084016105e4565b90509250929050565b60008060006060848603121561064457600080fd5b61064d8461056a565b92506020840135801515811461066257600080fd5b929592945050506040919091013590565b60008060006060848603121561068857600080fd5b6106918461056a565b925061069f602085016105e4565b9150604084013590509250925092565b6020808252600d908201526c1058d8d95cdcc819195b9a5959609a1b60408201526060019056fea26469706673582212209900c16d58d9001c20ae169637f321ce612885d54c14093d6930bb50869e55e264736f6c63430008110033"),
	}

	// Deploy IncentiveProxy Contract
	implAddress = common.HexToAddress("0x010000000000000000000000000000000000000d")
	g.Alloc[common.HexToAddress("0x010000000000000000000000000000000000000c")] = GenesisAccount{
		Balance: common.Big0,
		Code:    common.Hex2Bytes("6080604052600436106100225760003560e01c8063d784d4261461004b57610039565b3661003957610037610032610074565b6100a5565b005b610049610044610074565b6100a5565b005b34801561005757600080fd5b50610072600480360381019061006d919061024d565b6100cb565b005b6000807f360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc60001b9050805491505090565b3660008037600080366000845af43d6000803e80600081146100c6573d6000f35b3d6000fd5b3073ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff1614610139576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610130906102e0565b60405180910390fd5b610142816101f3565b610181576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610178906102c0565b60405180910390fd5b60007f360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc60001b90508181558173ffffffffffffffffffffffffffffffffffffffff167fbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b60405160405180910390a25050565b600080823f90506000801b811415801561023057507fc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a47060001b8114155b915050919050565b6000813590506102478161039a565b92915050565b60006020828403121561026357610262610343565b5b600061027184828501610238565b91505092915050565b6000610287600e83610300565b915061029282610348565b602082019050919050565b60006102aa600d83610300565b91506102b582610371565b602082019050919050565b600060208201905081810360008301526102d98161027a565b9050919050565b600060208201905081810360008301526102f98161029d565b9050919050565b600082825260208201905092915050565b600061031c82610323565b9050919050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b600080fd5b7f4e6f74206120636f6e7472616374000000000000000000000000000000000000600082015250565b7f4163636573732064656e69656400000000000000000000000000000000000000600082015250565b6103a381610311565b81146103ae57600080fd5b5056fea2646970667358221220eaad273d24fb4c2d97d0a6ec9110de5905a399afa8c25558441ce29ac70ed7f964736f6c63430008070033"),
		Storage: map[common.Hash]common.Hash{
			eip1967Key: implAddress.Hash(),
			adminKey:   common.HexToHash("0x01"),
		},
	}

	// Deploy IncentiveImpl Contract
	g.Alloc[implAddress] = GenesisAccount{
		Balance: common.Big0,
		Code:    common.Hex2Bytes("608060405234801561001057600080fd5b50600436106100575760003560e01c80630900f0101461005c5780630912ed77146100785780633c09e2fd146100945780635c97f4a2146100b0578063ce6ccfaf146100e0575b600080fd5b610076600480360381019061007191906104de565b610110565b005b610092600480360381019061008d919061050b565b6101de565b005b6100ae60048036038101906100a9919061050b565b610238565b005b6100ca60048036038101906100c5919061050b565b610292565b6040516100d79190610602565b60405180910390f35b6100fa60048036038101906100f591906104de565b6102a6565b604051610107919061065d565b60405180910390f35b600161011c33826102b8565b61015b576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101529061063d565b60405180910390fd5b73010000000000000000000000000000000000000c73ffffffffffffffffffffffffffffffffffffffff1663d784d426836040518263ffffffff1660e01b81526004016101a891906105be565b600060405180830381600087803b1580156101c257600080fd5b505af11580156101d6573d6000803e3d6000fd5b505050505050565b60016101ea33826102b8565b610229576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016102209061063d565b60405180910390fd5b6102338383610306565b505050565b600161024433826102b8565b610283576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161027a9061063d565b60405180910390fd5b61028d8383610390565b505050565b600061029e83836102b8565b905092915050565b60006102b182610463565b9050919050565b600080826000808673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054161415905092915050565b80196000808473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825416925050819055507fcfa5316bd1be4ceb62f363b0a162f322c33ba870641138cd8600dd4fa603fc3b82826040516103849291906105d9565b60405180910390a15050565b6103986104ab565b8111156103da576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016103d19061061d565b60405180910390fd5b806000808473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825417925050819055507f385a9c70004a48177c93b74796d77d5ebf7e1248f9e2369624514da454cd01b082826040516104579291906105d9565b60405180910390a15050565b60008060008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020549050919050565b60006001905090565b6000813590506104c381610728565b92915050565b6000813590506104d88161073f565b92915050565b6000602082840312156104f4576104f36106d1565b5b6000610502848285016104b4565b91505092915050565b60008060408385031215610522576105216106d1565b5b6000610530858286016104b4565b9250506020610541858286016104c9565b9150509250929050565b61055481610689565b82525050565b6105638161069b565b82525050565b6000610576600c83610678565b9150610581826106d6565b602082019050919050565b6000610599600d83610678565b91506105a4826106ff565b602082019050919050565b6105b8816106c7565b82525050565b60006020820190506105d3600083018461054b565b92915050565b60006040820190506105ee600083018561054b565b6105fb60208301846105af565b9392505050565b6000602082019050610617600083018461055a565b92915050565b6000602082019050818103600083015261063681610569565b9050919050565b600060208201905081810360008301526106568161058c565b9050919050565b600060208201905061067260008301846105af565b92915050565b600082825260208201905092915050565b6000610694826106a7565b9050919050565b60008115159050919050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000819050919050565b600080fd5b7f556e6b6e6f776e20526f6c650000000000000000000000000000000000000000600082015250565b7f4163636573732064656e69656400000000000000000000000000000000000000600082015250565b61073181610689565b811461073c57600080fd5b50565b610748816106c7565b811461075357600080fd5b5056fea2646970667358221220cdfcf91f1a2fb074b8fa3a34f3bc20bff7d8816f0b737b3a7a67295c3000ae6064736f6c63430008070033"),
	}

	return nil
}

// Commit writes the block and state of a genesis specification to the database.
// The block is committed as the canonical head block.
func (g *Genesis) Commit(db ethdb.Database) (*types.Block, error) {
	block := g.ToBlock(db)
	if block.Number().Sign() != 0 {
		return nil, errors.New("can't commit genesis block with number > 0")
	}
	config := g.Config
	if config == nil {
		return nil, errGenesisNoConfig
	}
	if err := config.CheckConfigForkOrder(); err != nil {
		return nil, err
	}
	rawdb.WriteBlock(db, block)
	rawdb.WriteReceipts(db, block.Hash(), block.NumberU64(), nil)
	rawdb.WriteCanonicalHash(db, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(db, block.Hash())
	rawdb.WriteHeadHeaderHash(db, block.Hash())
	rawdb.WriteChainConfig(db, block.Hash(), config)
	return block, nil
}

// MustCommit writes the genesis block and state to db, panicking on error.
// The block is committed as the canonical head block.
func (g *Genesis) MustCommit(db ethdb.Database) *types.Block {
	block, err := g.Commit(db)
	if err != nil {
		panic(err)
	}
	return block
}

// GenesisBlockForTesting creates and writes a block in which addr has the given wei balance.
func GenesisBlockForTesting(db ethdb.Database, addr common.Address, balance *big.Int) *types.Block {
	g := Genesis{
		Config:  params.TestChainConfig,
		Alloc:   GenesisAlloc{addr: {Balance: balance}},
		BaseFee: big.NewInt(params.ApricotPhase3InitialBaseFee),
	}
	return g.MustCommit(db)
}

// ReadBlockByHash reads the block with the given hash from the database.
func ReadBlockByHash(db ethdb.Reader, hash common.Hash) *types.Block {
	blockNumber := rawdb.ReadHeaderNumber(db, hash)
	if blockNumber == nil {
		return nil
	}
	return rawdb.ReadBlock(db, hash, *blockNumber)
}
