package evm

import (
	"testing"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/stretchr/testify/require"
)

func TestCacheEvictionPolicy(t *testing.T) {
	onEvict := func(k uint64, v string) {
		t.Logf("evicting key: %v, value: %v", k, v)
	}
	lru, err := lru.NewWithEvict(1024, onEvict)
	require.NoError(t, err)

	for i := 0; i < 2048; i++ {
		lru.Add(uint64(i%32), "value2")
		lru.Add(uint64(i), "value")
	}
}
