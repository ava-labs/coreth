// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package miner

type configHooks struct {
	// allowDuplicateBlocks is a test only boolean
	// to allow mining of the same block multiple times.
	allowDuplicateBlocks bool
}

func (c *configHooks) SetTestOnlyAllowDuplicateBlocks() {
	c.allowDuplicateBlocks = true
}

func (c *configHooks) TestOnlyAllowDuplicateBlocks() bool {
	return c.allowDuplicateBlocks
}
