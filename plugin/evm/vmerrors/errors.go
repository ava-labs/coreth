// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmerrors

import "errors"

var (
	ErrGenerateBlockFailed     = errors.New("failed to generate block")
	ErrBlockVerificationFailed = errors.New("failed to verify block")
	ErrMakeNewBlockFailed      = errors.New("failed to make new block")
)
