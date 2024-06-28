package evm

import (
	"context"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
)

func InitGossip[T gossip.Gossipable](
	ctx context.Context,
	vm *VM,
	protocol uint64,
	marshaller gossip.Marshaller[T],
	mempool gossip.Set[T],
	metrics gossip.Metrics,
	pushGossiper *gossip.PushGossiper[T],
	pullGossiper gossip.Gossiper,
	handler p2p.Handler,
) (*gossip.PushGossiper[T], gossip.Gossiper, p2p.Handler, error) {
	client := vm.Network.NewClient(protocol, p2p.WithValidatorSampling(vm.validators))
	pushGossipParams := gossip.BranchingFactor{
		StakePercentage: vm.config.PushGossipPercentStake,
		Validators:      vm.config.PushGossipNumValidators,
		Peers:           vm.config.PushGossipNumPeers,
	}
	pushRegossipParams := gossip.BranchingFactor{
		Validators: vm.config.PushRegossipNumValidators,
		Peers:      vm.config.PushRegossipNumPeers,
	}
	if pushGossiper == nil {
		var err error
		pushGossiper, err = gossip.NewPushGossiper[T](
			marshaller,
			mempool,
			vm.validators,
			client,
			metrics,
			pushGossipParams,
			pushRegossipParams,
			pushGossipDiscardedElements,
			txGossipTargetMessageSize,
			vm.config.RegossipFrequency.Duration,
		)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if handler == nil {
		handler = newTxGossipHandler[T](
			vm.ctx.Log,
			marshaller,
			mempool,
			metrics,
			txGossipTargetMessageSize,
			txGossipThrottlingPeriod,
			txGossipThrottlingLimit,
			vm.validators,
		)
	}

	if err := vm.Network.AddHandler(protocol, handler); err != nil {
		return nil, nil, nil, err
	}

	if pullGossiper == nil {
		gossiper := gossip.NewPullGossiper[T](
			vm.ctx.Log,
			marshaller,
			mempool,
			client,
			metrics,
			txGossipPollSize,
		)

		pullGossiper = gossip.ValidatorGossiper{
			Gossiper:   gossiper,
			NodeID:     vm.ctx.NodeID,
			Validators: vm.validators,
		}
	}

	vm.shutdownWg.Add(2)
	go func() {
		gossip.Every(ctx, vm.ctx.Log, pushGossiper, vm.config.PushGossipFrequency.Duration)
		vm.shutdownWg.Done()
	}()
	go func() {
		gossip.Every(ctx, vm.ctx.Log, pullGossiper, vm.config.PullGossipFrequency.Duration)
		vm.shutdownWg.Done()
	}()

	return pushGossiper, pullGossiper, handler, nil
}
