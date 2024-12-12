package clienttypes

import "encoding/json"

type ChainConfigExtra struct {
	NetworkUpgrades
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
func (c ChainConfigExtra) MarshalJSON() ([]byte, error) {
	// Alias ChainConfigExtra to avoid recursion
	type _ChainConfigExtra ChainConfigExtra
	return json.Marshal(_ChainConfigExtra(c))
}
