package store

import "context"

type txKey struct{}

// WithTx records the transaction a caller holds, so code it calls back into (access limitations, hooks) queries
// through it instead of taking a second connection while the first is held (a pool can then wait forever).
func WithTx(ctx context.Context, tx Tx) context.Context { return context.WithValue(ctx, txKey{}, tx) }

// TxFrom returns the transaction recorded by WithTx, if any.
func TxFrom(ctx context.Context) (Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(Tx)
	return tx, ok
}
