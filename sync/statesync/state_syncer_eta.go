// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const updateInterval = 1 * time.Minute

type syncETA struct {
	startTime  time.Time
	lastUpdate time.Time

	// use first 16 bits of trie key to estimate progress
	startPos uint16

	// counters to display progress
	triesSynced   uint32
	triesFromDisk uint32
}

func (s *syncETA) start(startKey []byte) {
	s.startTime = time.Now()
	s.lastUpdate = s.startTime
	if len(startKey) > 0 {
		s.startPos = binary.BigEndian.Uint16(startKey)
	}
}

func (s *syncETA) notifyProgress(key []byte) {
	if time.Since(s.lastUpdate) < updateInterval {
		return
	}
	currentPos := binary.BigEndian.Uint16(key)
	if currentPos == s.startPos {
		// have not made enough progress, avoid division by zero
		return
	}

	progress := float32(currentPos) / math.MaxUint16
	timeSpent := time.Since(s.startTime)
	estimatedTotalDuration := float64(timeSpent) * float64(math.MaxUint16-s.startPos) / float64(currentPos-s.startPos)
	eta := time.Duration(estimatedTotalDuration) - timeSpent

	s.lastUpdate = time.Now()
	log.Info(
		"state sync in progress",
		"key", common.BytesToHash(key),
		"progress", fmt.Sprintf("%.1f%%", progress*100),
		"eta", roundETA(eta),
		"triesSynced", atomic.LoadUint32(&s.triesSynced),
		"triesFromDisk", atomic.LoadUint32(&s.triesFromDisk),
	)
}

func (s *syncETA) notifyTrieSynced(skipped bool) {
	if skipped {
		atomic.AddUint32(&s.triesFromDisk, 1)
	} else {
		atomic.AddUint32(&s.triesSynced, 1)
	}
}

func (s *syncETA) notifyLastStorageTries(numTriesLeft int) {
	log.Info("main trie sync completed, waiting for storage tries", "count", numTriesLeft)
}

// roundETA rounds [d] to a minute and chops off the "0s" suffix
func roundETA(d time.Duration) string {
	str := d.Round(time.Minute).String()
	if strings.HasSuffix(str, "m0s") {
		return str[:len(str)-len("0s")]
	}
	return str
}
