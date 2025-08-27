// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExtractMilliseconds(t *testing.T) {
	tests := []struct {
		name string
		time time.Time
		want uint16
	}{
		{
			name: "0 nsec should return 0",
			time: time.Unix(0, 0),
			want: 0,
		},
		{
			name: "999_999 nsec should return 0",
			time: time.Unix(0, 999_999),
			want: 0,
		},
		{
			name: "1M nsec should return 1",
			time: time.Unix(0, 1_000_000),
			want: 1,
		},
		{
			name: "should return only milliseconds",
			time: time.Unix(0, 123_456_789),
			want: 123,
		},
		{
			name: "sec should not affect milliseconds",
			time: time.Unix(1, 123_456_789),
			want: 123,
		},
		{
			name: "nsec exceeds max should still return correct milliseconds",
			time: time.Unix(1, 999_123_456_789),
			want: 123,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ExtractMilliseconds(test.time)
			require.Equal(t, test.want, got)
		})
	}
}
