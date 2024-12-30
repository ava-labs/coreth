package atomic

import (
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

type SharedMemories struct {
	thisChain   atomic.SharedMemory
	peerChain   atomic.SharedMemory
	thisChainID ids.ID
	peerChainID ids.ID
}

func (s *SharedMemories) addItemsToBeRemovedToPeerChain(ops map[ids.ID]*atomic.Requests) error {
	for _, reqs := range ops {
		puts := make(map[ids.ID]*atomic.Requests)
		puts[s.thisChainID] = &atomic.Requests{}
		for _, key := range reqs.RemoveRequests {
			val := []byte{0x1}
			puts[s.thisChainID].PutRequests = append(puts[s.thisChainID].PutRequests, &atomic.Element{Key: key, Value: val})
		}
		if err := s.peerChain.Apply(puts); err != nil {
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
			val, err := s.peerChain.Get(s.thisChainID, [][]byte{elem.Key})
			if err != nil {
				t.Fatalf("error finding puts in peerChainMemory: %s", err)
			}
			assert.Equal(t, elem.Value, val[0])
		}

		// should not be able to get remove requests
		for _, key := range reqs.RemoveRequests {
			_, err := s.thisChain.Get(s.peerChainID, [][]byte{key})
			assert.EqualError(t, err, "not found")
		}
	}
}

func (s *SharedMemories) assertOpsNotApplied(t *testing.T, ops map[ids.ID]*atomic.Requests) {
	t.Helper()
	for _, reqs := range ops {
		// should not be able to get put requests
		for _, elem := range reqs.PutRequests {
			_, err := s.peerChain.Get(s.thisChainID, [][]byte{elem.Key})
			assert.EqualError(t, err, "not found")
		}

		// should be able to get remove requests (these were previously added as puts on peerChain)
		for _, key := range reqs.RemoveRequests {
			val, err := s.thisChain.Get(s.peerChainID, [][]byte{key})
			assert.NoError(t, err)
			assert.Equal(t, []byte{0x1}, val[0])
		}
	}
}

// TODO: once tetsts are moved to atomic package, unexport this function
func NewSharedMemories(atomicMemory *atomic.Memory, thisChainID, peerChainID ids.ID) *SharedMemories {
	return &SharedMemories{
		thisChain:   atomicMemory.NewSharedMemory(thisChainID),
		peerChain:   atomicMemory.NewSharedMemory(peerChainID),
		thisChainID: thisChainID,
		peerChainID: peerChainID,
	}
}
