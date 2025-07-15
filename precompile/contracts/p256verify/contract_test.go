// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p256verify

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"github.com/ava-labs/libevm/libevm/precompiles/p256verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestP256VerifyPrecompile_RequiredGas(t *testing.T) {
	precompile := &P256VerifyPrecompile{}

	// Test with valid input length (160 bytes)
	validInput := make([]byte, 160)
	gas := precompile.RequiredGas(validInput)
	assert.Equal(t, p256verify.Precompile{}.RequiredGas(validInput), gas)

	// Test with invalid input length
	invalidInput := make([]byte, 100)
	gas = precompile.RequiredGas(invalidInput)
	assert.Equal(t, p256verify.Precompile{}.RequiredGas(invalidInput), gas)
}

func TestP256VerifyPrecompile_Run(t *testing.T) {
	precompile := &P256VerifyPrecompile{}

	// Generate a valid P256 key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create a message and sign it
	message := []byte("Hello, P256!")
	hash := sha256.Sum256(message)

	r, s, err := ecdsa.Sign(rand.Reader, privateKey, hash[:])
	require.NoError(t, err)

	// Pack the input using the libevm p256verify.Pack function
	input := p256verify.Pack(hash, r, s, &privateKey.PublicKey)

	// Test valid signature
	ret, err := precompile.Run(input)
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, ret)

	// Test invalid signature (wrong input length)
	invalidInput := make([]byte, 100)
	ret, err = precompile.Run(invalidInput)
	require.NoError(t, err)
	assert.Nil(t, ret) // The libevm precompile returns nil for invalid input
}
