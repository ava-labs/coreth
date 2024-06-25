package atx

import (
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// GetEthAddress returns the ethereum address derived from [privKey]
func GetEthAddress(privKey *secp256k1.PrivateKey) common.Address {
	return PublicKeyToEthAddress(privKey.PublicKey())
}

// PublicKeyToEthAddress returns the ethereum address derived from [pubKey]
func PublicKeyToEthAddress(pubKey *secp256k1.PublicKey) common.Address {
	return crypto.PubkeyToAddress(*(pubKey.ToECDSA()))
}
