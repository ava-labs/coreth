package fw

import (
	"github.com/ava-labs/coreth/triedb"
	firewood "github.com/ava-labs/firewood/ffi/v2"
)

var _ triedb.KVBackend = &Firewood{}

type Firewood struct {
	firewood.Firewood
}

// PrefixDelete is a no-op as firewood implements deletes as prefix deletes.
// This means when the account is deleted, all related storage is also deleted.
func (f *Firewood) PrefixDelete(prefix []byte) (int, error) {
	return 0, nil
}
