// (c) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
)

var errInvalidAddr = errors.New("invalid hex address")

// ParseEthAddress parses [addrStr] and returns an Ethereum address
func ParseEthAddress(addrStr string) (common.Address, error) {
	if !common.IsHexAddress(addrStr) {
		return common.Address{}, errInvalidAddr
	}
	return common.HexToAddress(addrStr), nil
}

// GetEthAddress returns the ethereum address derived from [privKey]
func GetEthAddress(privKey *secp256k1.PrivateKey) common.Address {
	return PublicKeyToEthAddress(privKey.PublicKey())
}

// PublicKeyToEthAddress returns the ethereum address derived from [pubKey]
func PublicKeyToEthAddress(pubKey *secp256k1.PublicKey) common.Address {
	return crypto.PubkeyToAddress(*(pubKey.ToECDSA()))
}
