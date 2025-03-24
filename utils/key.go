// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"crypto/ecdsa"
	"crypto/rand"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
)

// Key contains an ecdsa private key field as well as an address field
// obtained from converting the ecdsa public key.
type Key struct {
	Address    common.Address
	PrivateKey *ecdsa.PrivateKey
}

// NewKey generates a new key pair and returns a pointer to a [Key].
func NewKey() (*Key, error) {
	privateKeyECDSA, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Key{
		Address:    crypto.PubkeyToAddress(privateKeyECDSA.PublicKey),
		PrivateKey: privateKeyECDSA,
	}, nil
}
