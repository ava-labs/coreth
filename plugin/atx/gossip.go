package atx

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
)

var (
	_ gossip.Gossipable                  = (*GossipAtomicTx)(nil)
	_ gossip.Marshaller[*GossipAtomicTx] = (*GossipAtomicTxMarshaller)(nil)
)

type GossipAtomicTxMarshaller struct{}

func (g GossipAtomicTxMarshaller) MarshalGossip(tx *GossipAtomicTx) ([]byte, error) {
	return tx.Tx.SignedBytes(), nil
}

func (g GossipAtomicTxMarshaller) UnmarshalGossip(bytes []byte) (*GossipAtomicTx, error) {
	tx, err := ExtractAtomicTx(bytes, Codec)
	return &GossipAtomicTx{
		Tx: tx,
	}, err
}

type GossipAtomicTx struct {
	Tx *Tx
}

func (tx *GossipAtomicTx) GossipID() ids.ID {
	return tx.Tx.ID()
}

// [ExtractAtomicTx] extracts a singular atomic transaction from [atomicTxBytes]
// and returns a slice of atomic transactions for compatibility with the type returned post
// ApricotPhase5.
// Note: this function assumes [atomicTxBytes] is non-empty.
func ExtractAtomicTx(atomicTxBytes []byte, codec codec.Manager) (*Tx, error) {
	atomicTx := new(Tx)
	if _, err := codec.Unmarshal(atomicTxBytes, atomicTx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal atomic transaction (pre-AP5): %w", err)
	}
	if err := atomicTx.Sign(codec, nil); err != nil {
		return nil, fmt.Errorf("failed to initialize singleton atomic tx due to: %w", err)
	}
	return atomicTx, nil
}
