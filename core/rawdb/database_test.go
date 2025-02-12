package rawdb

import (
	"fmt"

	"github.com/ava-labs/libevm/ethdb"
)

func ExampleInspectDatabase() {
	extendRight := func(base []byte, totalLength int) []byte {
		if len(base) > totalLength {
			return base[:totalLength]
		}
		extended := make([]byte, totalLength)
		copy(extended, base)
		return extended
	}

	db := &stubDatabase{
		iterator: &stubIterator{
			kvs: []keyValue{
				// Extra metadata keys: (17 + 1) + (9 + 1) = 28 bytes
				{key: snapshotBlockHashKey, value: []byte("a")},
				{key: syncRootKey, value: []byte("b")},
				// Trie segments: 77 + 1 = 78 bytes
				{key: extendRight(syncSegmentsPrefix, syncSegmentsKeyLength), value: []byte("c")},
				// Storage tries to fetch: 76 + 1 = 77 bytes
				{key: extendRight(syncStorageTriesPrefix, syncStorageTriesKeyLength), value: []byte("d")},
				// Code to fetch: 34 + 1 = 35 bytes
				{key: extendRight(CodeToFetchPrefix, codeToFetchKeyLength), value: []byte("e")},
				// Block numbers synced to: 22 + 1 = 23 bytes
				{key: extendRight(syncPerformedPrefix, syncPerformedKeyLength), value: []byte("f")},
			},
		},
	}

	keyPrefix := []byte(nil)
	keyStart := []byte(nil)

	err := InspectDatabase(db, keyPrefix, keyStart)
	if err != nil {
		fmt.Println(err)
	}
	// Output:
	// +-----------------+-------------------------+----------+-------+
	// |    DATABASE     |        CATEGORY         |   SIZE   | ITEMS |
	// +-----------------+-------------------------+----------+-------+
	// | Key-Value store | Headers                 | 0.00 B   |     0 |
	// | Key-Value store | Bodies                  | 0.00 B   |     0 |
	// | Key-Value store | Receipt lists           | 0.00 B   |     0 |
	// | Key-Value store | Block number->hash      | 0.00 B   |     0 |
	// | Key-Value store | Block hash->number      | 0.00 B   |     0 |
	// | Key-Value store | Transaction index       | 0.00 B   |     0 |
	// | Key-Value store | Bloombit index          | 0.00 B   |     0 |
	// | Key-Value store | Contract codes          | 0.00 B   |     0 |
	// | Key-Value store | Hash trie nodes         | 0.00 B   |     0 |
	// | Key-Value store | Path trie state lookups | 0.00 B   |     0 |
	// | Key-Value store | Path trie account nodes | 0.00 B   |     0 |
	// | Key-Value store | Path trie storage nodes | 0.00 B   |     0 |
	// | Key-Value store | Trie preimages          | 0.00 B   |     0 |
	// | Key-Value store | Account snapshot        | 0.00 B   |     0 |
	// | Key-Value store | Storage snapshot        | 0.00 B   |     0 |
	// | Key-Value store | Clique snapshots        | 0.00 B   |     0 |
	// | Key-Value store | Singleton metadata      | 28.00 B  |     2 |
	// | Light client    | CHT trie nodes          | 0.00 B   |     0 |
	// | Light client    | Bloom trie nodes        | 0.00 B   |     0 |
	// | State sync      | Trie segments           | 78.00 B  |     1 |
	// | State sync      | Storage tries to fetch  | 77.00 B  |     1 |
	// | State sync      | Code to fetch           | 35.00 B  |     1 |
	// | State sync      | Block numbers synced to | 23.00 B  |     1 |
	// +-----------------+-------------------------+----------+-------+
	// |                            TOTAL          | 241.00 B |       |
	// +-----------------+-------------------------+----------+-------+
}

type stubDatabase struct {
	ethdb.Database
	iterator ethdb.Iterator
}

func (s *stubDatabase) NewIterator(keyPrefix, keyStart []byte) ethdb.Iterator {
	return s.iterator
}

// AncientSize is used in [InspectDatabase] to determine the ancient sizes.
func (s *stubDatabase) AncientSize(kind string) (uint64, error) {
	return 0, nil
}

func (s *stubDatabase) Ancients() (uint64, error) {
	return 0, nil
}

func (s *stubDatabase) Tail() (uint64, error) {
	return 0, nil
}

func (s *stubDatabase) Get(key []byte) ([]byte, error) {
	return nil, nil
}

func (s *stubDatabase) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	return nil
}

type stubIterator struct {
	ethdb.Iterator
	i   int // see [stubIterator.pos]
	kvs []keyValue
}

type keyValue struct {
	key   []byte
	value []byte
}

// pos returns the true iterator position, which is otherwise off by one because
// Next() is called _before_ usage.
func (s *stubIterator) pos() int {
	return s.i - 1
}

func (s *stubIterator) Next() bool {
	s.i++
	return s.pos() < len(s.kvs)
}

func (s *stubIterator) Release() {}

func (s *stubIterator) Key() []byte {
	return s.kvs[s.pos()].key
}

func (s *stubIterator) Value() []byte {
	return s.kvs[s.pos()].value
}
