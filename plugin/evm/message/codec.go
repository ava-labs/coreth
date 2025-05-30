// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	Version        = uint16(0)
	maxMessageSize = 2*units.MiB - 64*units.KiB // Subtract 64 KiB from p2p network cap to leave room for encoding overhead from AvalancheGo
)

// NewCodec returns a codec manager that can be used to marshal and unmarshal
// messages, including the provided syncSummaryType. syncSummaryType can be used
// to register a type for sync summaries.
func NewCodec(syncSummaryType interface{}) (codec.Manager, error) {
	codec := codec.NewManager(maxMessageSize)
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	// Gossip types removed from codec
	c.SkipRegistrations(2)
	errs.Add(
		// Types for state sync frontier consensus
		c.RegisterType(syncSummaryType),

		// state sync types
		c.RegisterType(BlockRequest{}),
		c.RegisterType(BlockResponse{}),
		c.RegisterType(LeafsRequest{}),
		c.RegisterType(LeafsResponse{}),
		c.RegisterType(CodeRequest{}),
		c.RegisterType(CodeResponse{}),

		// Warp request types
		c.RegisterType(MessageSignatureRequest{}),
		c.RegisterType(BlockSignatureRequest{}),
		c.RegisterType(SignatureResponse{}),

		codec.RegisterCodec(Version, c),
	)

	if errs.Errored() {
		return nil, errs.Err
	}

	return codec, nil
}
