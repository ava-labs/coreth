package atx

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ethereum/go-ethereum/common"
)

type Client interface {
	IssueTx(ctx context.Context, txBytes []byte, options ...rpc.Option) (ids.ID, error)
	GetAtomicTxStatus(ctx context.Context, txID ids.ID, options ...rpc.Option) (Status, error)
	GetAtomicTx(ctx context.Context, txID ids.ID, options ...rpc.Option) ([]byte, error)
	GetAtomicUTXOs(ctx context.Context, addrs []ids.ShortID, sourceChain string, limit uint32, startAddress ids.ShortID, startUTXOID ids.ID, options ...rpc.Option) ([][]byte, ids.ShortID, ids.ID, error)
	ExportKey(ctx context.Context, userPass api.UserPass, addr common.Address, options ...rpc.Option) (*secp256k1.PrivateKey, string, error)
	ImportKey(ctx context.Context, userPass api.UserPass, privateKey *secp256k1.PrivateKey, options ...rpc.Option) (common.Address, error)
	Import(ctx context.Context, userPass api.UserPass, to common.Address, sourceChain string, options ...rpc.Option) (ids.ID, error)
	ExportAVAX(ctx context.Context, userPass api.UserPass, amount uint64, to ids.ShortID, targetChain string, options ...rpc.Option) (ids.ID, error)
	Export(ctx context.Context, userPass api.UserPass, amount uint64, to ids.ShortID, targetChain string, assetID string, options ...rpc.Option) (ids.ID, error)
}

type client struct {
	requester rpc.EndpointRequester
}

func NewClient(uri, chain string) *client {
	return &client{
		requester: rpc.NewEndpointRequester(fmt.Sprintf("%s/ext/bc/%s/avax", uri, chain)),
	}
}

// GetAtomicTxStatus returns the status of [txID]
func (c *client) GetAtomicTxStatus(ctx context.Context, txID ids.ID, options ...rpc.Option) (Status, error) {
	res := &GetAtomicTxStatusReply{}
	err := c.requester.SendRequest(ctx, "avax.getAtomicTxStatus", &api.JSONTxID{
		TxID: txID,
	}, res, options...)
	return res.Status, err
}

// GetAtomicTx returns the byte representation of [txID]
func (c *client) GetAtomicTx(ctx context.Context, txID ids.ID, options ...rpc.Option) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.requester.SendRequest(ctx, "avax.getAtomicTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: formatting.Hex,
	}, res, options...)
	if err != nil {
		return nil, err
	}

	return formatting.Decode(formatting.Hex, res.Tx)
}

// GetAtomicUTXOs returns the byte representation of the atomic UTXOs controlled by [addresses]
// from [sourceChain]
func (c *client) GetAtomicUTXOs(ctx context.Context, addrs []ids.ShortID, sourceChain string, limit uint32, startAddress ids.ShortID, startUTXOID ids.ID, options ...rpc.Option) ([][]byte, ids.ShortID, ids.ID, error) {
	res := &api.GetUTXOsReply{}
	err := c.requester.SendRequest(ctx, "avax.getUTXOs", &api.GetUTXOsArgs{
		Addresses:   ids.ShortIDsToStrings(addrs),
		SourceChain: sourceChain,
		Limit:       json.Uint32(limit),
		StartIndex: api.Index{
			Address: startAddress.String(),
			UTXO:    startUTXOID.String(),
		},
		Encoding: formatting.Hex,
	}, res, options...)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, err
	}

	utxos := make([][]byte, len(res.UTXOs))
	for i, utxo := range res.UTXOs {
		utxoBytes, err := formatting.Decode(res.Encoding, utxo)
		if err != nil {
			return nil, ids.ShortID{}, ids.Empty, err
		}
		utxos[i] = utxoBytes
	}
	endAddr, err := address.ParseToID(res.EndIndex.Address)
	if err != nil {
		return nil, ids.ShortID{}, ids.Empty, err
	}
	endUTXOID, err := ids.FromString(res.EndIndex.UTXO)
	return utxos, endAddr, endUTXOID, err
}

// ExportKey returns the private key corresponding to [addr] controlled by [user]
// in both Avalanche standard format and hex format
func (c *client) ExportKey(ctx context.Context, user api.UserPass, addr common.Address, options ...rpc.Option) (*secp256k1.PrivateKey, string, error) {
	res := &ExportKeyReply{}
	err := c.requester.SendRequest(ctx, "avax.exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  addr.Hex(),
	}, res, options...)
	return res.PrivateKey, res.PrivateKeyHex, err
}

// ImportKey imports [privateKey] to [user]
func (c *client) ImportKey(ctx context.Context, user api.UserPass, privateKey *secp256k1.PrivateKey, options ...rpc.Option) (common.Address, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "avax.importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res, options...)
	if err != nil {
		return common.Address{}, err
	}
	return ParseEthAddress(res.Address)
}

// Import sends an import transaction to import funds from [sourceChain] and
// returns the ID of the newly created transaction
func (c *client) Import(ctx context.Context, user api.UserPass, to common.Address, sourceChain string, options ...rpc.Option) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "avax.import", &ImportArgs{
		UserPass:    user,
		To:          to,
		SourceChain: sourceChain,
	}, res, options...)
	return res.TxID, err
}

// ExportAVAX sends AVAX from this chain to the address specified by [to].
// Returns the ID of the newly created atomic transaction
func (c *client) ExportAVAX(
	ctx context.Context,
	user api.UserPass,
	amount uint64,
	to ids.ShortID,
	targetChain string,
	options ...rpc.Option,
) (ids.ID, error) {
	return c.Export(ctx, user, amount, to, targetChain, "AVAX", options...)
}

// Export sends an asset from this chain to the P/C-Chain.
// After this tx is accepted, the AVAX must be imported to the P/C-chain with an importTx.
// Returns the ID of the newly created atomic transaction
func (c *client) Export(
	ctx context.Context,
	user api.UserPass,
	amount uint64,
	to ids.ShortID,
	targetChain string,
	assetID string,
	options ...rpc.Option,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "avax.export", &ExportArgs{
		ExportAVAXArgs: ExportAVAXArgs{
			UserPass:    user,
			Amount:      json.Uint64(amount),
			TargetChain: targetChain,
			To:          to.String(),
		},
		AssetID: assetID,
	}, res, options...)
	return res.TxID, err
}

// IssueTx issues a transaction to a node and returns the TxID
func (c *client) IssueTx(ctx context.Context, txBytes []byte, options ...rpc.Option) (ids.ID, error) {
	res := &api.JSONTxID{}
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
	if err != nil {
		return res.TxID, fmt.Errorf("problem hex encoding bytes: %w", err)
	}
	err = c.requester.SendRequest(ctx, "avax.issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res, options...)
	return res.TxID, err
}
