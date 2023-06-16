// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package legacypool

import (
	"math/big"
	"time"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// Has returns an indicator whether txpool has a local transaction cached with
// the given hash.
func (pool *LegacyPool) HasLocal(hash common.Hash) bool {
	return pool.all.GetLocal(hash) != nil
}

func (pool *LegacyPool) RemoveTx(hash common.Hash) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.removeTx(hash, true)
}

// GasTip returns the current gas tip enforced by the transaction pool.
func (pool *LegacyPool) GasTip() *big.Int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return new(big.Int).Set(pool.gasTip.Load())
}

func (pool *LegacyPool) SetMinFee(minFee *big.Int) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.minimumFee = minFee
}

func (pool *LegacyPool) startPeriodicFeeUpdate() {
	if pool.chainconfig.ApricotPhase3BlockTimestamp == nil {
		return
	}

	// Call updateBaseFee here to ensure that there is not a [baseFeeUpdateInterval] delay
	// when starting up in ApricotPhase3 before the base fee is updated.
	if time.Now().After(utils.Uint64ToTime(pool.chainconfig.ApricotPhase3BlockTimestamp)) {
		pool.updateBaseFee()
	}

	pool.wg.Add(1)
	go pool.periodicBaseFeeUpdate()
}

func (pool *LegacyPool) periodicBaseFeeUpdate() {
	defer pool.wg.Done()

	// Sleep until its time to start the periodic base fee update or the tx pool is shutting down
	select {
	case <-time.After(time.Until(utils.Uint64ToTime(pool.chainconfig.ApricotPhase3BlockTimestamp))):
	case <-pool.generalShutdownChan:
		return // Return early if shutting down
	}

	// Update the base fee every [baseFeeUpdateInterval]
	// and shutdown when [generalShutdownChan] is closed by Stop()
	for {
		select {
		case <-time.After(baseFeeUpdateInterval):
			pool.updateBaseFee()
		case <-pool.generalShutdownChan:
			return
		}
	}
}

func (pool *LegacyPool) updateBaseFee() {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	_, baseFeeEstimate, err := dummy.EstimateNextBaseFee(pool.chainconfig, pool.currentHead.Load(), uint64(time.Now().Unix()))
	if err == nil {
		pool.priced.SetBaseFee(baseFeeEstimate)
	} else {
		log.Error("failed to update base fee", "currentHead", pool.currentHead.Load().Hash(), "err", err)
	}
}
