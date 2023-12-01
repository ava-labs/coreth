package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/coreth/plugin/evm"
	evmMessage "github.com/ava-labs/coreth/plugin/evm/message"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("failed %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("termianted successfully\n")
}

type leafClient struct {
	p         peer.Peer
	creator   message.Creator
	chainID   ids.ID
	requestID uint32
	deadline  time.Duration
	responses <-chan *p2p.AppResponse
}

func newLeafClient(
	ctx context.Context,
	peerIP ips.IPPort,
	networkID uint32,
	chainID ids.ID,
) (*leafClient, error) {
	responses := make(chan *p2p.AppResponse, 1000) // This is not correct, but it should work for now
	fmt.Printf("starting test peer...\n")
	p, err := peer.StartTestPeer(
		ctx,
		peerIP,
		networkID,
		router.InboundHandlerFunc(func(_ context.Context, msg message.InboundMessage) {
			// fmt.Printf("received msg \n%s\n", msg)
			if message.AppResponseOp != msg.Op() {
				fmt.Printf("dropping op %s\n", msg.Op())
				return
			}

			res, ok := msg.Message().(*p2p.AppResponse)
			if !ok {
				fmt.Printf("dropping msg type %T\n", msg.Message())
				return
			}

			if !bytes.Equal(res.ChainId, chainID[:]) {
				fmt.Printf("dropping res with chainID %x\n", res.ChainId)
				return
			}

			fmt.Printf("adding response to queue")
			responses <- res
		}),
	)
	if err != nil {
		return nil, err
	}
	fmt.Printf("created test peer\n")

	creator, err := message.NewCreator(logging.NoLog{}, prometheus.NewRegistry(), "", compression.TypeNone, 3*time.Second)
	if err != nil {
		return nil, err
	}

	return &leafClient{
		p:         p,
		creator:   creator,
		chainID:   chainID,
		deadline:  3 * time.Second,
		responses: responses,
	}, nil
}

func (l *leafClient) GetLeafs(ctx context.Context, request evmMessage.LeafsRequest) (evmMessage.LeafsResponse, error) {
	fmt.Printf("sending GetLeafs request %s\n", request)
	msgBytes, err := evmMessage.RequestToBytes(evmMessage.Codec, request)
	if err != nil {
		return evmMessage.LeafsResponse{}, err
	}
	msg, err := l.creator.AppRequest(l.chainID, l.requestID, l.deadline, msgBytes)
	if err != nil {
		return evmMessage.LeafsResponse{}, err
	}
	l.requestID++
	if !l.p.Send(ctx, msg) {
		return evmMessage.LeafsResponse{}, errors.New("failed to send message")
	}

	fmt.Printf("waiting for response...\n")
	select {
	case res := <-l.responses:
		fmt.Printf("received response\n")

		respIntf, numKeys, err := statesyncclient.ParseLeafsResponse(
			evm.Codec, request, res.AppBytes,
		)

		if err != nil {
			fmt.Printf("dropping \n%s\n with error %s\n", msg, err)
			return evmMessage.LeafsResponse{}, err
		}
		leafsResponse := respIntf.(evmMessage.LeafsResponse)
		fmt.Printf("received leafs response %d\n", numKeys)
		return leafsResponse, nil

	case <-time.After(l.deadline + 2*time.Second):
		fmt.Printf("deadline\n")
		return evmMessage.LeafsResponse{}, fmt.Errorf("request %s timed out after %s", request, l.deadline+2*time.Second)
	}
}

func newDB() database.Database {
	folder := os.TempDir()
	db, err := leveldb.New(folder, nil, logging.NoLog{}, "", prometheus.NewRegistry())
	if err != nil {
		panic(err)
	}
	return db
}

func run() error {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	fs := BuildFlagSet()
	v, err := BuildViper(fs, os.Args[1:])
	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}

	peerIP, err := ips.ToIPPort(v.GetString(IPPortKey))
	if err != nil {
		return err
	}

	cChainID, err := ids.FromString("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	if err != nil {
		return err
	}

	fmt.Printf("starting leaf client...\n")
	leafClient, err := newLeafClient(ctx, peerIP, constants.MainnetID, cChainID)
	if err != nil {
		return err
	}

	// var (
	// 	tasks = make(chan syncclient.LeafSyncTask, 1)
	// 	start = make([]byte, 40)
	// 	end   = make([]byte, 40)
	// )

	targetRoot := common.HexToHash(v.GetString(RootKey))
	targetHeight := v.GetUint64(HeightKey)
	db := newDB()
	err = evm.Script(cChainID, evm.Codec, db, nil, leafClient, targetRoot, targetHeight)
	return err

	// fmt.Printf("creating leaf sync task...\n")
	// binary.BigEndian.PutUint64(start[0:8], v.GetUint64(StartKey))
	// binary.BigEndian.PutUint64(end[0:8], v.GetUint64(EndKey))
	// root := common.HexToHash(v.GetString(RootKey))
	// tasks <- &leafSyncTask{
	// 	root:  root,
	// 	start: start,
	// 	end:   end,
	// 	onLeafs: func(keys, vals [][]byte) error {
	// 		// TODO: log keys/vals in human readable format
	// 		return nil
	// 	},
	// 	onFinish: func() error {
	// 		return errors.New("trigger panic")
	// 	},
	// }
	// syncer := syncclient.NewCallbackLeafSyncer(leafClient, tasks, 1024)
	// syncer.Start(ctx, 1, func(err error) error {
	// 	return err
	// })

	// return <-syncer.Done()
}
