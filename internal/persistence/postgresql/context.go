package postgresql

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type postgresTxKey struct{}

func NewContextWithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, postgresTxKey{}, tx)
}

func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(postgresTxKey{}).(pgx.Tx)

	return tx, ok
}
