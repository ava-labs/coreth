// Copyright (C) 2022, Chain4Travel AG. All rights reserved.

package vmerrs

import (
	"errors"
)

// List evm execution errors
var (
	ErrNotKycVerified = errors.New("not KYC verified")
)
