package types

import "time"

// FirstSeen is the time a transaction is first seen.
func (tx *Transaction) FirstSeen() time.Time {
	return tx.time
}

// SetFirstSeen sets overwrites the time a transaction is first seen.
func (tx *Transaction) SetFirstSeen(t time.Time) {
	tx.time = t
}

func (tx *TxWithMinerFee) Tx() *Transaction {
	return tx.tx
}
