package evm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrimPrefixes(t *testing.T) {
	tests := []struct {
		before string
		after  string
	}{
		{"", ""},
		{"/path/to/coreth/path/file.go", "path/file.go"},
		{"/path/to/coreth@version/path/file.go", "path/file.go"},
	}
	for _, test := range tests {
		require.Equal(t, test.after, trimPrefixes(test.before))
	}
}
