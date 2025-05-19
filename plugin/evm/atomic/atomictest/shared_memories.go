package atomictest

import (
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/stretchr/testify/assert"
)

type SharedMemories struct {
	ThisChain   atomic.SharedMemory
	PeerChain   atomic.SharedMemory
	thisChainID ids.ID
	peerChainID ids.ID
}

func (s *SharedMemories) AddItemsToBeRemovedToPeerChain(ops map[ids.ID]*atomic.Requests) error {
	for _, reqs := range ops {
		puts := make(map[ids.ID]*atomic.Requests)
		puts[s.thisChainID] = &atomic.Requests{}
		for _, key := range reqs.RemoveRequests {
			val := []byte{0x1}
			puts[s.thisChainID].PutRequests = append(puts[s.thisChainID].PutRequests, &atomic.Element{Key: key, Value: val})
		}
		if err := s.PeerChain.Apply(puts); err != nil {
			return err
		}
	}
	return nil
}

func (s *SharedMemories) AssertOpsApplied(t *testing.T, ops map[ids.ID]*atomic.Requests) {
	t.Helper()
	for _, reqs := range ops {
		// should be able to get put requests
		for _, elem := range reqs.PutRequests {
			val, err := s.PeerChain.Get(s.thisChainID, [][]byte{elem.Key})
			require.NoError(t, err)
			assert.Equal(t, [][]byte{elem.Value}, val)
		}

		// should not be able to get remove requests
		for _, key := range reqs.RemoveRequests {
			_, err := s.ThisChain.Get(s.peerChainID, [][]byte{key})
			assert.EqualError(t, err, "not found")
		}
	}
}

func (s *SharedMemories) AssertOpsNotApplied(t *testing.T, ops map[ids.ID]*atomic.Requests) {
	t.Helper()
	for _, reqs := range ops {
		// should not be able to get put requests
		for _, elem := range reqs.PutRequests {
			_, err := s.PeerChain.Get(s.thisChainID, [][]byte{elem.Key})
			assert.EqualError(t, err, "not found")
		}

		// should be able to get remove requests (these were previously added as puts on peerChain)
		for _, key := range reqs.RemoveRequests {
			val, err := s.ThisChain.Get(s.peerChainID, [][]byte{key})
			assert.NoError(t, err)
			assert.Equal(t, []byte{0x1}, val[0])
		}
	}
}

func NewSharedMemories(atomicMemory *atomic.Memory, thisChainID, peerChainID ids.ID) *SharedMemories {
	return &SharedMemories{
		ThisChain:   atomicMemory.NewSharedMemory(thisChainID),
		PeerChain:   atomicMemory.NewSharedMemory(peerChainID),
		thisChainID: thisChainID,
		peerChainID: peerChainID,
	}
}

func TestSharedMemory() atomic.SharedMemory {
	m := atomic.NewMemory(memdb.New())
	return m.NewSharedMemory(snowtest.CChainID)
}
