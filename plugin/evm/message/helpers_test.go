// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"crypto/sha256"
	"fmt"
)

func deterministicBytes(label string, n int) []byte {
	buf := make([]byte, n)
	off := 0
	var ctr uint64
	for off < n {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", label, ctr)))
		toCopy := n - off
		if toCopy > len(h) {
			toCopy = len(h)
		}
		copy(buf[off:], h[:toCopy])
		off += toCopy
		ctr++
	}
	return buf
}
