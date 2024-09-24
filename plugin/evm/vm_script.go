package evm

import (
	"encoding/binary"
	"time"
)

func (vm *VM) script() error {
	iter := vm.atomicTxRepository.IterateByHeight(0)
	defer iter.Release()

	var (
		last             time.Time
		height           uint64
		imports, exports = 0, 0
		nonAvaxOutputs   = 0
	)
	for iter.Next() {
		if time.Since(last) > 10*time.Second {
			last = time.Now()
			vm.logger.Info("Processing atomic transactions", "height", height, "imports", imports, "exports", exports, "nonAvaxOutputs", nonAvaxOutputs)
		}

		height = binary.BigEndian.Uint64(iter.Key())
		txs, err := ExtractAtomicTxs(iter.Value(), true, vm.codec)
		if err != nil {
			return err
		}

		for _, tx := range txs {
			switch tx.UnsignedAtomicTx.(type) {
			case *UnsignedImportTx:
				imports++
				t := tx.UnsignedAtomicTx.(*UnsignedImportTx)
				for _, out := range t.Outs {
					if out.AssetID != vm.ctx.AVAXAssetID {
						nonAvaxOutputs++
					}
				}
			case *UnsignedExportTx:
				exports++
			default:
				panic("Unknown atomic transaction type")
			}
		}
	}
	vm.logger.Info("Processed atomic transactions", "height", height, "imports", imports, "exports", exports, "nonAvaxOutputs", nonAvaxOutputs)
	return nil
}
