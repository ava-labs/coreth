// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// These tests are run in a different package because the primary `evm` tests
// leak goroutines that result in race conditions with the temporary
// registration of extras, which is intended to be done separately.
package evm_test

import (
	"testing"

	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	cparams "github.com/ava-labs/coreth/params"
)

func TestWithTempRegisteredLibEVMExtras(t *testing.T) {
	params.TestOnlyClearRegisteredExtras()
	state.TestOnlyClearRegisteredExtras()
	types.TestOnlyClearRegisteredExtras()
	vm.TestOnlyClearRegisteredHooks()

	var reRegistered bool
	t.Cleanup(func() {
		if !reRegistered {
			evm.RegisterAllLibEVMExtras()
		}
	})

	payloadTests := map[string]func(t *testing.T){
		"customtypes": func(t *testing.T) {
			t.Helper()
			require.False(t, customtypes.IsMultiCoin(&types.StateAccount{}))
		},
		"params": func(t *testing.T) {
			t.Helper()
			require.False(t, cparams.GetRulesExtra(params.Rules{}).IsEtna)
		},
	}

	t.Run("with_temp_registration", func(t *testing.T) {
		err := libevm.WithTemporaryExtrasLock(func(lock libevm.ExtrasLock) error {
			return evm.WithTempRegisteredLibEVMExtras(lock, func() error {
				//nolint:whitespace // Avoid visual crowding due to nested calls
				t.Run("payloads", func(t *testing.T) {
					for pkg, fn := range payloadTests {
						t.Run(pkg, fn)
					}
				})
				return nil
			})
		})
		require.NoError(t, err)
	})

	// These are deliberately placed after the tests of temporary registration,
	// to demonstrate that (a) they are indeed temporary, and (b) they would
	// otherwise panic.
	t.Run("without_registration", func(t *testing.T) {
		t.Run("payloads", func(t *testing.T) {
			for pkg, fn := range payloadTests {
				t.Run(pkg, func(t *testing.T) {
					require.Panics(t, func() { fn(t) })
				})
			}
		})
	})

	evm.RegisterAllLibEVMExtras()
	reRegistered = true

	t.Run("with_permanent_registration", func(t *testing.T) {
		t.Run("payloads", func(t *testing.T) {
			for pkg, fn := range payloadTests {
				t.Run(pkg, fn)
			}
		})
	})
}
