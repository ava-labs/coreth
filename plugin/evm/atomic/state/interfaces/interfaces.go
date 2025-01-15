package interfaces

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/common"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

// AtomicTrie maintains an index of atomic operations by blockchainIDs for every block
// height containing atomic transactions. The backing data structure for this index is
// a Trie. The keys of the trie are block heights and the values (leaf nodes)
// are the atomic operations applied to shared memory while processing the block accepted
// at the corresponding height.
type AtomicTrie interface {
	// OpenTrie returns a modifiable instance of the atomic trie backed by trieDB
	// opened at hash.
	OpenTrie(hash common.Hash) (*trie.Trie, error)

	// UpdateTrie updates [tr] to inlude atomicOps for height.
	UpdateTrie(tr *trie.Trie, height uint64, atomicOps map[ids.ID]*avalancheatomic.Requests) error

	// Iterator returns an AtomicTrieIterator to iterate the trie at the given
	// root hash starting at [cursor].
	Iterator(hash common.Hash, cursor []byte) (AtomicTrieIterator, error)

	// LastCommitted returns the last committed hash and corresponding block height
	LastCommitted() (common.Hash, uint64)

	// TrieDB returns the underlying trie database
	TrieDB() *triedb.Database

	// Root returns hash if it exists at specified height
	// if trie was not committed at provided height, it returns
	// common.Hash{} instead
	Root(height uint64) (common.Hash, error)

	// LastAcceptedRoot returns the most recent accepted root of the atomic trie,
	// or the root it was initialized to if no new tries were accepted yet.
	LastAcceptedRoot() common.Hash

	// InsertTrie updates the trieDB with the provided node set and adds a reference
	// to root in the trieDB. Once InsertTrie is called, it is expected either
	// AcceptTrie or RejectTrie be called for the same root.
	InsertTrie(nodes *trienode.NodeSet, root common.Hash) error

	// AcceptTrie marks root as the last accepted atomic trie root, and
	// commits the trie to persistent storage if height is divisible by
	// the commit interval. Returns true if the trie was committed.
	AcceptTrie(height uint64, root common.Hash) (bool, error)

	// RejectTrie dereferences root from the trieDB, freeing memory.
	RejectTrie(root common.Hash) error
}

// AtomicBackend abstracts the verification and processing
// of atomic transactions
type AtomicBackend interface {
	// InsertTxs calculates the root of the atomic trie that would
	// result from applying [txs] to the atomic trie, starting at the state
	// corresponding to previously verified block [parentHash].
	// If [blockHash] is provided, the modified atomic trie is pinned in memory
	// and it's the caller's responsibility to call either Accept or Reject on
	// the AtomicState which can be retreived from GetVerifiedAtomicState to commit the
	// changes or abort them and free memory.
	InsertTxs(blockHash common.Hash, blockHeight uint64, parentHash common.Hash, txs []*atomic.Tx) (common.Hash, error)

	// Returns an AtomicState corresponding to a block hash that has been inserted
	// but not Accepted or Rejected yet.
	GetVerifiedAtomicState(blockHash common.Hash) (AtomicState, error)

	// AtomicTrie returns the atomic trie managed by this backend.
	AtomicTrie() AtomicTrie

	// ApplyToSharedMemory applies the atomic operations that have been indexed into the trie
	// but not yet applied to shared memory for heights less than or equal to [lastAcceptedBlock].
	// This executes operations in the range [cursorHeight+1, lastAcceptedBlock].
	// The cursor is initially set by  MarkApplyToSharedMemoryCursor to signal to the atomic trie
	// the range of operations that were added to the trie without being executed on shared memory.
	ApplyToSharedMemory(lastAcceptedBlock uint64) error

	// MarkApplyToSharedMemoryCursor marks the atomic trie as containing atomic ops that
	// have not been executed on shared memory starting at [previousLastAcceptedHeight+1].
	// This is used when state sync syncs the atomic trie, such that the atomic operations
	// from [previousLastAcceptedHeight+1] to the [lastAcceptedHeight] set by state sync
	// will not have been executed on shared memory.
	MarkApplyToSharedMemoryCursor(previousLastAcceptedHeight uint64) error

	// SetLastAccepted is used after state-sync to reset the last accepted block.
	SetLastAccepted(lastAcceptedHash common.Hash)

	// IsBonus returns true if the block for atomicState is a bonus block
	IsBonus(blockHeight uint64, blockHash common.Hash) bool
}

// AtomicTxRepository defines an entity that manages storage and indexing of
// atomic transactions
type AtomicTxRepository interface {
	GetIndexHeight() (uint64, error)
	GetByTxID(txID ids.ID) (*atomic.Tx, uint64, error)
	GetByHeight(height uint64) ([]*atomic.Tx, error)
	Write(height uint64, txs []*atomic.Tx) error
	WriteBonus(height uint64, txs []*atomic.Tx) error

	IterateByHeight(start uint64) database.Iterator
	Codec() codec.Manager
}

// AtomicState is an abstraction created through AtomicBackend
// and can be used to apply the VM's state change for atomic txs
// or reject them to free memory.
// The root of the atomic trie after applying the state change
// is accessible through this interface as well.
type AtomicState interface {
	// Root of the atomic trie after applying the state change.
	Root() common.Hash
	// Accept applies the state change to VM's persistent storage
	// Changes are persisted atomically along with the provided [commitBatch].
	Accept(commitBatch database.Batch, requests map[ids.ID]*avalancheatomic.Requests) error
	// Reject frees memory associated with the state change.
	Reject() error
}

// AtomicTrieIterator is a stateful iterator that iterates the leafs of an AtomicTrie
type AtomicTrieIterator interface {
	// Next advances the iterator to the next node in the atomic trie and
	// returns true if there are more leaves to iterate
	Next() bool

	// Key returns the current database key that the iterator is iterating
	// returned []byte can be freely modified
	Key() []byte

	// Value returns the current database value that the iterator is iterating
	Value() []byte

	// BlockNumber returns the current block number
	BlockNumber() uint64

	// BlockchainID returns the current blockchain ID at the current block number
	BlockchainID() ids.ID

	// AtomicOps returns a map of blockchainIDs to the set of atomic requests
	// for that blockchainID at the current block number
	AtomicOps() *avalancheatomic.Requests

	// Error returns error, if any encountered during this iteration
	Error() error
}
