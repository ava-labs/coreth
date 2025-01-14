package nomt

import (
	"encoding/binary"
	"net"
	"sync"

	"github.com/ava-labs/coreth/shim/nomt/nomt"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ethereum/go-ethereum/log"
	"google.golang.org/protobuf/proto"
)

var _ triedb.KVBackend = &Nomt{}

const maxReponseSize = 1024

func response(conn net.Conn, req *nomt.Request) (*nomt.Response, error) {
	data, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(binary.BigEndian.AppendUint32(nil, uint32(len(data)))); err != nil {
		return nil, err
	}
	if _, err := conn.Write(data); err != nil {
		return nil, err
	}

	respData := make([]byte, maxReponseSize)
	n, err := conn.Read(respData)
	if err != nil {
		return nil, err
	}
	var resp nomt.Response
	if err := proto.Unmarshal(respData[:n], &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type Nomt struct {
	lock sync.RWMutex
	conn net.Conn
}

func New(conn net.Conn) *Nomt {
	return &Nomt{
		conn: conn,
	}
}

func (n *Nomt) response(req *nomt.Request) (*nomt.Response, error) {
	n.lock.Lock()
	defer n.lock.Unlock()

	return response(n.conn, req)
}

func (n *Nomt) Root() []byte {
	req := &nomt.Request{
		Request: &nomt.Request_Root{
			Root: &nomt.RootRequest{},
		},
	}
	resp, err := n.response(req)
	if err != nil {
		log.Error("Failed to get root", "err", err)
		return nil
	}
	return resp.GetRoot().Root
}

func (n *Nomt) Get(key []byte) ([]byte, error) {
	req := &nomt.Request{
		Request: &nomt.Request_Get{
			Get: &nomt.GetRequest{
				Key: key,
			},
		},
	}

	resp, err := n.response(req)
	if err != nil {
		return nil, err
	}
	if resp.GetErrCode() == 1 { // Not found
		return nil, nil
	}
	return resp.GetGet().Value, nil
}

func (n *Nomt) Prefetch(key []byte) ([]byte, error) {
	req := &nomt.Request{
		Request: &nomt.Request_Prefetch{
			Prefetch: &nomt.PrefetchRequest{
				Key: key,
			},
		},
	}

	_, err := n.response(req)
	return nil, err
}

func (n *Nomt) Update(ks, vs [][]byte) ([]byte, error) {
	req := &nomt.Request{
		Request: &nomt.Request_Update{
			Update: &nomt.UpdateRequest{
				Items: make([]*nomt.UpdateRequestItem, len(ks)),
			},
		},
	}

	for i, k := range ks {
		req.GetUpdate().Items[i] = &nomt.UpdateRequestItem{Key: k, Value: vs[i]}
	}

	resp, err := n.response(req)
	if err != nil {
		return nil, err
	}
	return resp.GetUpdate().Root, nil
}

func (n *Nomt) Commit(root []byte) error {
	return nil
}

func (n *Nomt) Close() error {
	close := &nomt.Request{
		Request: &nomt.Request_Close{
			Close: &nomt.CloseRequest{},
		},
	}
	_, err := n.response(close)
	if err != nil {
		return err
	}

	return n.conn.Close()
}

func (n *Nomt) PrefixDelete(prefix []byte) (int, error) {
	return 0, nil
}
