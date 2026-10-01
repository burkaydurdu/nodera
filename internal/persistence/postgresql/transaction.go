package postgresql

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:generate mockgen -destination=../../../mock/persistence/transaction_manager.go -package=mockpersistence github.com/piggystats/nodera/internal/persistence/postgresql ITransactionManager
type ITransactionManager interface {
	Begin(ctx context.Context) (context.Context, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// TransactionManager carries a pgx transaction on the context, so repositories
// join it through Queries.Executor without a tx parameter.
type TransactionManager struct {
	pool *pgxpool.Pool
}

func NewTransactionManager(pool *pgxpool.Pool) *TransactionManager {
	return &TransactionManager{pool: pool}
}

func (tm *TransactionManager) Begin(ctx context.Context) (context.Context, error) {
	tx, err := tm.pool.Begin(ctx)
	if err != nil {
		return nil, errors.Join(ErrTransactionStartFailed, err)
	}

	return NewContextWithTx(ctx, tx), nil
}

func (tm *TransactionManager) Commit(ctx context.Context) error {
	tx, ok := TxFromContext(ctx)
	if !ok {
		return ErrNoActiveTransaction
	}

	if err := tx.Commit(context.Background()); err != nil {
		return errors.Join(ErrTransactionCommitFailed, err)
	}

	return nil
}

func (tm *TransactionManager) Rollback(ctx context.Context) error {
	tx, ok := TxFromContext(ctx)
	if !ok {
		return ErrNoActiveTransaction
	}

	if err := tx.Rollback(context.Background()); err != nil {
		return errors.Join(ErrTransactionRollbackFailed, err)
	}

	return nil
}
