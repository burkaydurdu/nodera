package postgresql

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 12

const uniqueViolation = "23505"

var (
	ErrNoActiveTransaction       = rerror.Error{Code: 10001, Domain: Domain, Message: "no active transaction found in context"}
	ErrTransactionStartFailed    = rerror.Error{Code: 10002, Domain: Domain, Message: "failed to start transaction"}
	ErrTransactionCommitFailed   = rerror.Error{Code: 10003, Domain: Domain, Message: "failed to commit transaction"}
	ErrTransactionRollbackFailed = rerror.Error{Code: 10004, Domain: Domain, Message: "failed to rollback transaction"}
)

// UniqueViolationConstraint returns the violated constraint name and true when
// err is a Postgres unique violation, so a table with several unique indexes
// can report each collision as its own domain error.
func UniqueViolationConstraint(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return pgErr.ConstraintName, true
	}

	return "", false
}
