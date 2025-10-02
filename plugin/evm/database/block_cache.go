// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"sync"

	"github.com/ava-labs/libevm/common"
)

// blockCache is a cache for block header and body.
// It allows saving body and header separately via save while
// fetching both body and header at once via get.
type blockCache struct {
	bodyCache   map[common.Hash][]byte
	headerCache map[common.Hash][]byte
	mu          sync.RWMutex
}

func newBlockCache() *blockCache {
	return &blockCache{
		bodyCache:   make(map[common.Hash][]byte),
		headerCache: make(map[common.Hash][]byte),
	}
}

func (b *blockCache) get(blockHash common.Hash) ([]byte, []byte, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	bodyData, hasBody := b.bodyCache[blockHash]
	headerData, hasHeader := b.headerCache[blockHash]
	return bodyData, headerData, hasBody && hasHeader
}

func (b *blockCache) clear(blockHash common.Hash) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.bodyCache, blockHash)
	delete(b.headerCache, blockHash)
}

func (b *blockCache) save(key []byte, blockHash common.Hash, value []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if isBodyKey(key) {
		b.bodyCache[blockHash] = value
	} else if isHeaderKey(key) {
		b.headerCache[blockHash] = value
	}
}
