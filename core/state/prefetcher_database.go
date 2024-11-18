// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"sync"

	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type withPrefetcherDB interface {
	PrefetcherDB() Database
}

type withPrefetcher struct {
	Database
	maxConcurrency int
}

func (db *withPrefetcher) PrefetcherDB() Database {
	return newPrefetcherDatabase(db.Database, db.maxConcurrency)
}

func WithPrefetcher(db Database, maxConcurrency int) Database {
	return &withPrefetcher{db, maxConcurrency}
}

type prefetcherDatabase struct {
	Database

	maxConcurrency int
	workers        *utils.BoundedWorkers
}

func newPrefetcherDatabase(db Database, maxConcurrency int) *prefetcherDatabase {
	return &prefetcherDatabase{
		Database:       db,
		maxConcurrency: maxConcurrency,
		workers:        utils.NewBoundedWorkers(maxConcurrency),
	}
}

func (p *prefetcherDatabase) OpenTrie(root common.Hash) (Trie, error) {
	t, err := p.Database.OpenTrie(root)
	return newPrefetcherTrie(p, t), err
}

func (p *prefetcherDatabase) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, trie Trie) (Trie, error) {
	t, err := p.Database.OpenStorageTrie(stateRoot, address, root, trie)
	return newPrefetcherTrie(p, t), err
}

func (p *prefetcherDatabase) CopyTrie(t Trie) Trie {
	switch t := t.(type) {
	case *prefetcherTrie:
		return t.getCopy()
	default:
		return p.Database.CopyTrie(t)
	}
}

func (p *prefetcherDatabase) Close() {
	p.workers.Wait()
}

type prefetcher interface {
	PrefetchAccount(address common.Address)
	PrefetchStorage(address common.Address, key []byte)
}

type withPrefetcherDefaults struct {
	Trie
}

func (t withPrefetcherDefaults) PrefetchAccount(address common.Address) {
	if p, ok := t.Trie.(prefetcher); ok {
		p.PrefetchAccount(address)
		return
	}

	_, _ = t.GetAccount(address)
}

func (t withPrefetcherDefaults) PrefetchStorage(address common.Address, key []byte) {
	if p, ok := t.Trie.(prefetcher); ok {
		p.PrefetchStorage(address, key)
		return
	}

	_, _ = t.GetStorage(address, key)
}

var _ prefetcher = (*prefetcherTrie)(nil)

type prefetcherTrie struct {
	p *prefetcherDatabase

	Trie
	copyLock sync.Mutex

	copies chan Trie
	wg     sync.WaitGroup
}

func newPrefetcherTrie(p *prefetcherDatabase, t Trie) *prefetcherTrie {
	prefetcher := &prefetcherTrie{
		p:      p,
		Trie:   t,
		copies: make(chan Trie, p.maxConcurrency),
	}
	prefetcher.copies <- prefetcher.getCopy()
	return prefetcher
}

func (p *prefetcherTrie) Wait() {
	p.wg.Wait()
}

func (p *prefetcherTrie) getCopy() Trie {
	select {
	case copy := <-p.copies:
		return copy
	default:
		p.copyLock.Lock()
		defer p.copyLock.Unlock()
		return p.p.Database.CopyTrie(p.Trie)
	}
}

func (p *prefetcherTrie) putCopy(copy Trie) {
	select {
	case p.copies <- copy:
	default:
	}
}

func (p *prefetcherTrie) PrefetchAccount(address common.Address) {
	p.wg.Add(1)
	f := func() {
		defer p.wg.Done()

		tr := p.getCopy()
		_, err := tr.GetAccount(address)
		if err != nil {
			log.Error("GetAccount failed in prefetcher", "err", err)
		}
		p.putCopy(tr)
	}
	p.p.workers.Execute(f)
}

func (p *prefetcherTrie) PrefetchStorage(address common.Address, key []byte) {
	p.wg.Add(1)
	f := func() {
		defer p.wg.Done()

		tr := p.getCopy()
		_, err := tr.GetStorage(address, key)
		if err != nil {
			log.Error("GetStorage failed in prefetcher", "err", err)
		}
		p.putCopy(tr)
	}
	p.p.workers.Execute(f)
}
