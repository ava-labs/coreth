package fw

import (
	"github.com/ava-labs/coreth/triedb"
	firewood "github.com/ava-labs/firewood/ffi"
)

var _ triedb.KVBackend = &Firewood{}

type Firewood struct {
	*firewood.Database
}

func (f *Firewood) Root() []byte {
	b, err := f.Database.Root()
	if err != nil {
		panic(err)
	}
	return b
}

// PrefixDelete is a no-op as firewood implements deletes as prefix deletes.
// This means when the account is deleted, all related storage is also deleted.
func (f *Firewood) PrefixDelete(prefix []byte) (int, error) {
	return 0, nil
}

// Update updates the trie with the provided key-value pairs.
// Firewood ffi does not accept empty batches, so if the keys are empty, the
// root is returned.
func (f *Firewood) Update(ks, vs [][]byte) ([]byte, error) {
	if len(ks) == 0 {
		return f.Database.Root()
	}
	return f.Database.Update(ks, vs)
}
