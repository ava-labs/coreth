package manipulation

import (
	"sync"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/counter"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type Manipulator struct {
	Enabled           bool
	CensoredAddresses map[common.Address]struct{}
	PriorityAddresses map[common.Address]struct{}
	DetectInjection   bool
	logger            log.Logger
	txCounter         *counter.TxCounter
	ChainConfig       *params.ChainConfig
	mu                sync.RWMutex
}

func New(enabled, detectInjection bool, logger log.Logger, txCounter *counter.TxCounter, chainConfig *params.ChainConfig) *Manipulator {
	return &Manipulator{
		Enabled:           enabled,
		CensoredAddresses: make(map[common.Address]struct{}),
		PriorityAddresses: make(map[common.Address]struct{}),
		DetectInjection:   detectInjection,
		logger:            logger,
		txCounter:         txCounter,
		ChainConfig:       chainConfig,
	}
}

func (m *Manipulator) ShouldCensor(tx *types.Transaction) bool {
	if !m.Enabled || tx == nil {
		return false
	}

	signer := types.LatestSigner(m.ChainConfig)
	sender, err := types.Sender(signer, tx)
	if err != nil {
		m.logger.Warn("Failed to recover sender", "tx", tx.Hash().Hex(), "err", err)
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, censored := m.CensoredAddresses[sender]; censored {
		m.logger.Info("Censoring tx by sender", "tx", tx.Hash().Hex(), "sender", sender.Hex())
		return true
	}
	if tx.To() != nil {
		if _, censored := m.CensoredAddresses[*tx.To()]; censored {
			m.logger.Info("Censoring tx by recipient", "tx", tx.Hash().Hex(), "to", tx.To().Hex())
			return true
		}
	}
	return false
}

func (m *Manipulator) ShouldDropAsInjection(tx *types.Transaction) bool {
	if !m.Enabled || !m.DetectInjection || tx == nil {
		return false
	}

	txHash := tx.Hash()
	if !m.txCounter.HasTxNum(txHash) {
		m.logger.Info("Dropping tx as injection", "tx", txHash.Hex())
		return true
	}
	return false
}

func (m *Manipulator) ShouldPrioritize(tx *types.Transaction) bool {
	if !m.Enabled || tx == nil {
		return false
	}

	signer := types.LatestSigner(m.ChainConfig)
	sender, err := types.Sender(signer, tx)
	if err != nil {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, prioritized := m.PriorityAddresses[sender]; prioritized {
		m.logger.Debug("Prioritizing tx", "tx", tx.Hash().Hex(), "sender", sender.Hex())
		return true
	}
	return false
}

func (m *Manipulator) ApplyReordering(txs []*types.Transaction) []*types.Transaction {
	if !m.Enabled || len(txs) <= 1 {
		return txs
	}

	var prioritized, regular []*types.Transaction
	for _, tx := range txs {
		if m.ShouldPrioritize(tx) {
			prioritized = append(prioritized, tx)
		} else {
			regular = append(regular, tx)
		}
	}
	m.logger.Info("Reordered txs", "prioritized", len(prioritized), "regular", len(regular))
	return append(prioritized, regular...)
}

func (m *Manipulator) AddCensoredAddress(addr common.Address) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CensoredAddresses[addr] = struct{}{}
	m.logger.Info("Added censored address", "addr", addr.Hex())
}

func (m *Manipulator) AddPriorityAddress(addr common.Address) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PriorityAddresses[addr] = struct{}{}
	m.logger.Info("Added priority address", "addr", addr.Hex())
}
