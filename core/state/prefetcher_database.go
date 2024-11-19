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
	PrefetcherDB() PrefetcherDB
}

type withPrefetcher struct {
	Database
	maxConcurrency int
}

func (db *withPrefetcher) PrefetcherDB() PrefetcherDB {
	return newPrefetcherDatabase(db.Database, db.maxConcurrency)
}

func WithPrefetcher(db Database, maxConcurrency int) Database {
	return &withPrefetcher{db, maxConcurrency}
}

type withPrefetcherDefaults struct {
	Database
}

func (withPrefetcherDefaults) PrefetchAccount(t Trie, address common.Address) {
	_, _ = t.GetAccount(address)
}

func (withPrefetcherDefaults) PrefetchStorage(t Trie, address common.Address, key []byte) {
	_, _ = t.GetStorage(address, key)
}

func (withPrefetcherDefaults) WaitTrie(Trie) {}
func (withPrefetcherDefaults) Close()        {}

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
	trie, err := p.Database.OpenTrie(root)
	return newPrefetcherTrie(p, trie), err
}

func (p *prefetcherDatabase) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, trie Trie) (Trie, error) {
	storageTrie, err := p.Database.OpenStorageTrie(stateRoot, address, root, trie)
	return newPrefetcherTrie(p, storageTrie), err
}

func (p *prefetcherDatabase) CopyTrie(t Trie) Trie {
	switch t := t.(type) {
	case *prefetcherTrie:
		return t.getCopy()
	default:
		return p.Database.CopyTrie(t)
	}
}

func (*prefetcherDatabase) PrefetchAccount(t Trie, address common.Address) {
	t.(*prefetcherTrie).PrefetchAccount(address)
}

func (*prefetcherDatabase) PrefetchStorage(t Trie, address common.Address, key []byte) {
	t.(*prefetcherTrie).PrefetchStorage(address, key)
}

func (*prefetcherDatabase) WaitTrie(t Trie) {
	t.(*prefetcherTrie).Wait()
}

func (p *prefetcherDatabase) Close() {
	p.workers.Wait()
}

type PrefetcherDB interface {
	// From Database
	OpenTrie(root common.Hash) (Trie, error)
	OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, trie Trie) (Trie, error)
	CopyTrie(t Trie) Trie

	// Additional methods
	PrefetchAccount(t Trie, address common.Address)
	PrefetchStorage(t Trie, address common.Address, key []byte)
	WaitTrie(t Trie)
	Close()
}

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
