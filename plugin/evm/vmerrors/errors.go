package vmerrors

import "errors"

var (
	ErrGenerateBlockFailed     = errors.New("failed to generate block")
	ErrBlockVerificationFailed = errors.New("failed to verify block")
	ErrMakeNewBlockFailed      = errors.New("failed to make new block")
)
