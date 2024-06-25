package atx

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Codec does serialization and deserialization
var Codec codec.Manager

func init() {
	Codec = codec.NewDefaultManager()

	var (
		lc   = linearcodec.NewDefault()
		errs = wrappers.Errs{}
	)
	errs.Add(
		lc.RegisterType(&UnsignedImportTx{}),
		lc.RegisterType(&UnsignedExportTx{}),
	)
	lc.SkipRegistrations(3)
	errs.Add(
		lc.RegisterType(&secp256k1fx.TransferInput{}),
		lc.RegisterType(&secp256k1fx.MintOutput{}),
		lc.RegisterType(&secp256k1fx.TransferOutput{}),
		lc.RegisterType(&secp256k1fx.MintOperation{}),
		lc.RegisterType(&secp256k1fx.Credential{}),
		lc.RegisterType(&secp256k1fx.Input{}),
		lc.RegisterType(&secp256k1fx.OutputOwners{}),
		Codec.RegisterCodec(codecVersion, lc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
