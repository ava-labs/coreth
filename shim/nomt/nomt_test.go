package nomt

import (
	"fmt"
	"net"
	"testing"

	"github.com/ava-labs/coreth/shim/nomt/nomt"
	"github.com/stretchr/testify/require"
)

//go:generate protoc --go_out=. --go_opt=Mmessage.proto=./nomt message.proto

const socketPath = "/tmp/rust_socket"

func makeKey(key []byte) []byte {
	return key
}

func BenchmarkSimple(b *testing.B) {
	conn, err := net.Dial("unix", socketPath)
	require.NoError(b, err)
	defer conn.Close()

	{
		req := &nomt.Request{
			Request: &nomt.Request_Update{
				Update: &nomt.UpdateRequest{
					Items: []*nomt.UpdateRequestItem{
						{
							Key:   makeKey([]byte("key")),
							Value: []byte("value1"),
						},
					},
				},
			},
		}

		resp, err := response(conn, req)
		require.NoError(b, err)
		b.Logf("Response: %x", resp.GetUpdate().Root)
	}

	batchSize := 20
	batch := make([]*nomt.UpdateRequestItem, batchSize)
	lastKey := 0
	b.Logf("Starting benchmark")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for j := 0; j < batchSize; j++ {
			batch[j] = &nomt.UpdateRequestItem{
				Key:   makeKey([]byte(fmt.Sprintf("key%08d", lastKey))),
				Value: []byte(fmt.Sprintf("value%d", lastKey)),
			}
			lastKey++
		}
		req := &nomt.Request{Request: &nomt.Request_Update{Update: &nomt.UpdateRequest{Items: batch}}}
		b.StartTimer()

		resp, err := response(conn, req)
		require.NoError(b, err)
		require.NotNil(b, resp.GetUpdate().Root)
	}
}
