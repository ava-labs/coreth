// (c) 2024 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/libevm/common"
	ethparams "github.com/ava-labs/libevm/params"
)

// XXX: Must fix this
type ChainConfig = ethparams.ChainConfig

// XXX: Must fix this
var GetExtra func(c *ChainConfig) *ChainConfigExtra

// UpgradeConfig includes the following configs that may be specified in upgradeBytes:
// - Timestamps that enable avalanche network upgrades,
// - Enabling or disabling precompiles as network upgrades.
type UpgradeConfig struct {
	// Config for enabling and disabling precompiles as network upgrades.
	PrecompileUpgrades []PrecompileUpgrade `json:"precompileUpgrades,omitempty"`
}

// AvalancheContext provides Avalanche specific context directly into the EVM.
type AvalancheContext struct {
	SnowCtx *snow.Context
}

type ChainConfigExtra struct {
	NetworkUpgrades // Config for timestamps that enable network upgrades. Skip encoding/decoding directly into ChainConfig.

	AvalancheContext `json:"-"` // Avalanche specific context set during VM initialization. Not serialized.

	UpgradeConfig `json:"-"` // Config specified in upgradeBytes (avalanche network upgrades or enable/disabling precompiles). Skip encoding/decoding directly into ChainConfig.
}

func (c *ChainConfigExtra) CheckConfigCompatible(newcfg_ *ChainConfig, headNumber *big.Int, headTimestamp uint64) *ConfigCompatError {
	if c == nil {
		return nil
	}
	newcfg := GetExtra(newcfg_)

	// Check avalanche network upgrades
	if err := c.checkNetworkUpgradesCompatible(&newcfg.NetworkUpgrades, headTimestamp); err != nil {
		return err
	}

	// Check that the precompiles on the new config are compatible with the existing precompile config.
	// XXX: This is missing in master?
	// if err := c.checkPrecompilesCompatible(newcfg.PrecompileUpgrades, headTimestamp); err != nil {
	// 	return err
	// }

	return nil
}

func (c *ChainConfigExtra) Description() string {
	if c == nil {
		return ""
	}
	var banner string

	banner += "Avalanche Upgrades (timestamp based):\n"
	banner += c.NetworkUpgrades.Description()
	banner += "\n"

	upgradeConfigBytes, err := json.Marshal(c.UpgradeConfig)
	if err != nil {
		upgradeConfigBytes = []byte("cannot marshal UpgradeConfig")
	}
	banner += fmt.Sprintf("Upgrade Config: %s", string(upgradeConfigBytes))
	banner += "\n"
	return banner
}

// isForkTimestampIncompatible returns true if a fork scheduled at timestamp s1
// cannot be rescheduled to timestamp s2 because head is already past the fork.
func isForkTimestampIncompatible(s1, s2 *uint64, head uint64) bool {
	return (isTimestampForked(s1, head) || isTimestampForked(s2, head)) && !configTimestampEqual(s1, s2)
}

// isTimestampForked returns whether a fork scheduled at timestamp s is active
// at the given head timestamp. Whilst this method is the same as isBlockForked,
// they are explicitly separate for clearer reading.
func isTimestampForked(s *uint64, head uint64) bool {
	if s == nil {
		return false
	}
	return *s <= head
}

func configTimestampEqual(x, y *uint64) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return *x == *y
}

// ConfigCompatError is raised if the locally-stored blockchain is initialised with a
// ChainConfig that would alter the past.
type ConfigCompatError = ethparams.ConfigCompatError

func newTimestampCompatError(what string, storedtime, newtime *uint64) *ConfigCompatError {
	var rew *uint64
	switch {
	case storedtime == nil:
		rew = newtime
	case newtime == nil || *storedtime < *newtime:
		rew = storedtime
	default:
		rew = newtime
	}
	err := &ConfigCompatError{
		What:         what,
		StoredTime:   storedtime,
		NewTime:      newtime,
		RewindToTime: 0,
	}
	if rew != nil && *rew > 0 {
		err.RewindToTime = *rew - 1
	}
	return err
}

// UnmarshalJSON parses the JSON-encoded data and stores the result in the
// object pointed to by c.
// This is a custom unmarshaler to handle the Precompiles field.
// Precompiles was presented as an inline object in the JSON.
// This custom unmarshaler ensures backwards compatibility with the old format.
func (c *ChainConfigExtra) UnmarshalJSON(data []byte) error {
	// Alias ChainConfigExtra to avoid recursion
	type _ChainConfigExtra ChainConfigExtra
	tmp := _ChainConfigExtra{}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	// At this point we have populated all fields except PrecompileUpgrade
	*c = ChainConfigExtra(tmp)

	return nil
}

// MarshalJSON returns the JSON encoding of c.
// This is a custom marshaler to handle the Precompiles field.
func (c *ChainConfigExtra) MarshalJSON() ([]byte, error) {
	// Alias ChainConfigExtra to avoid recursion
	type _ChainConfigExtra ChainConfigExtra
	return json.Marshal(_ChainConfigExtra(*c))
}

type fork struct {
	name      string
	block     *big.Int // some go-ethereum forks use block numbers
	timestamp *uint64  // Avalanche forks use timestamps
	optional  bool     // if true, the fork may be nil and next fork is still allowed
}

func (c *ChainConfigExtra) CheckConfigForkOrder() error {
	if c == nil {
		return nil
	}
	// Note: In Avalanche, hard forks must take place via block timestamps instead
	// of block numbers since blocks are produced asynchronously. Therefore, we do not
	// check that the block timestamps in the same way as for
	// the block number forks since it would not be a meaningful comparison.
	// Instead, we check only that Phases are enabled in order.
	// Note: we do not add the optional stateful precompile configs in here because they are optional
	// and independent, such that the ordering they are enabled does not impact the correctness of the
	// chain config.
	if err := checkForks(c.forkOrder(), false); err != nil {
		return err
	}

	return nil
}

// checkForks checks that forks are enabled in order and returns an error if not
// [blockFork] is true if the fork is a block number fork, false if it is a timestamp fork
func checkForks(forks []fork, blockFork bool) error {
	lastFork := fork{}
	for _, cur := range forks {
		if lastFork.name != "" {
			switch {
			// Non-optional forks must all be present in the chain config up to the last defined fork
			case lastFork.block == nil && lastFork.timestamp == nil && (cur.block != nil || cur.timestamp != nil):
				if cur.block != nil {
					return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at block %v",
						lastFork.name, cur.name, cur.block)
				} else {
					return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at timestamp %v",
						lastFork.name, cur.name, cur.timestamp)
				}

			// Fork (whether defined by block or timestamp) must follow the fork definition sequence
			case (lastFork.block != nil && cur.block != nil) || (lastFork.timestamp != nil && cur.timestamp != nil):
				if lastFork.block != nil && lastFork.block.Cmp(cur.block) > 0 {
					return fmt.Errorf("unsupported fork ordering: %v enabled at block %v, but %v enabled at block %v",
						lastFork.name, lastFork.block, cur.name, cur.block)
				} else if lastFork.timestamp != nil && *lastFork.timestamp > *cur.timestamp {
					return fmt.Errorf("unsupported fork ordering: %v enabled at timestamp %v, but %v enabled at timestamp %v",
						lastFork.name, lastFork.timestamp, cur.name, cur.timestamp)
				}

				// Timestamp based forks can follow block based ones, but not the other way around
				if lastFork.timestamp != nil && cur.block != nil {
					return fmt.Errorf("unsupported fork ordering: %v used timestamp ordering, but %v reverted to block ordering",
						lastFork.name, cur.name)
				}
			}
		}
		// If it was optional and not set, then ignore it
		if !cur.optional || (cur.block != nil || cur.timestamp != nil) {
			lastFork = cur
		}
	}
	return nil
}

// Verify verifies chain config and returns error
func (c *ChainConfigExtra) Verify() error {
	// Verify the precompile upgrades are internally consistent given the existing chainConfig.
	if err := c.verifyPrecompileUpgrades(); err != nil {
		return fmt.Errorf("invalid precompile upgrades: %w", err)
	}

	return nil
}

// IsPrecompileEnabled returns whether precompile with [address] is enabled at [timestamp].
func (c *ChainConfigExtra) IsPrecompileEnabled(address common.Address, timestamp uint64) bool {
	config := c.GetActivePrecompileConfig(address, timestamp)
	return config != nil && !config.IsDisabled()
}

// IsForkTransition returns true if [fork] activates during the transition from
// [parent] to [current].
// Taking [parent] as a pointer allows for us to pass nil when checking forks
// that activate during genesis.
// Note: this works for both block number and timestamp activated forks.
func IsForkTransition(fork *uint64, parent *uint64, current uint64) bool {
	var parentForked bool
	if parent != nil {
		parentForked = isTimestampForked(fork, *parent)
	}
	currentForked := isTimestampForked(fork, current)
	return !parentForked && currentForked
}

func ptrToString(val *uint64) string {
	if val == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *val)
}