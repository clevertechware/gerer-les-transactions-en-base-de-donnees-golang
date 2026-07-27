package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction"
)

// txKey carries the open transaction through the context.
type txKey struct{}

const (
	// serializableMaxAttempts bounds how many times a serialization failure is replayed.
	// Under SERIALIZABLE, error 40001 is the contract, not a bug.
	// Retrying forever would turn a hot row into a livelock.
	serializableMaxAttempts = 3
	serializableBaseBackoff = 5 * time.Millisecond
)

// TxManager draws transaction boundaries on top of a pgx client.
type TxManager struct {
	logger logger.Logger
	client Client

	// serializationRetries counts replays caused by a serialization failure.
	// Exposed so tests can prove a retry actually happened rather than infer it.
	serializationRetries atomic.Int64
}

// NewTxManager creates a TxManager over the given client.
func NewTxManager(log logger.Logger, client Client) *TxManager {
	return &TxManager{logger: log, client: client}
}

// Executor returns the transaction carried by ctx if there is one, and the
// connection pool otherwise.
//
// This is the hinge of the whole demo. A repository written against Executor
// runs unchanged inside a transaction or in autocommit, so the decision to open
// one stays where it belongs: in the service, next to the business invariant
// that justifies it. Without this, every single-statement read and write would
// be wrapped in a BEGIN it does not need.
func (t *TxManager) Executor(ctx context.Context) Executor {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return t.client
}

// RequireTx returns the ambient transaction, or ErrTransactionRequired.
//
// Only for statements that are meaningless without one: SELECT ... FOR UPDATE
// takes a row lock that is released at the end of the transaction, so in
// autocommit it is released immediately and protects nothing.
func (t *TxManager) RequireTx(ctx context.Context) (pgx.Tx, error) {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx, nil
	}
	return nil, domain.ErrTransactionRequired
}

// Execute runs unitOfWork in a read-write transaction at the default isolation level.
func (t *TxManager) Execute(ctx context.Context, unitOfWork transaction.UnitOfWork) error {
	return t.run(ctx, pgx.TxOptions{}, unitOfWork)
}

// ExecuteReadOnly runs unitOfWork in an explicitly read-only transaction whose reads all observe the same snapshot.
//
// REPEATABLE READ, not READ COMMITTED, and the distinction is the entire point.
// Under READ COMMITTED every statement takes a *fresh* snapshot, so two SELECTs in one transaction can legitimately
// disagree, which would defeat the only reason to open a transaction for reading.
// REPEATABLE READ freezes the snapshot at the first query.
//
// AccessMode ReadOnly adds the safety net: PostgreSQL rejects any write with
// SQLSTATE 25006, and a routing layer may send the whole block to a replica.
func (t *TxManager) ExecuteReadOnly(ctx context.Context, unitOfWork transaction.UnitOfWork) error {
	return t.run(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, unitOfWork)
}

// ExecuteSerializable runs unitOfWork under SERIALIZABLE isolation and replays it on a serialization failure.
func (t *TxManager) ExecuteSerializable(ctx context.Context, unitOfWork transaction.UnitOfWork) error {
	// Nested calls join the ambient transaction instead of opening a second one.
	if txExists(ctx) {
		return unitOfWork(ctx)
	}

	opts := pgx.TxOptions{IsoLevel: pgx.Serializable}

	var err error
	for attempt := 1; attempt <= serializableMaxAttempts; attempt++ {
		err = t.run(ctx, opts, unitOfWork)
		if !isRetryable(err) {
			return err
		}

		t.serializationRetries.Add(1)
		t.logger.WarnContext(ctx, "serialization failure, replaying transaction",
			"attempt", attempt, "max_attempts", serializableMaxAttempts, "error", err)

		if attempt == serializableMaxAttempts {
			break
		}
		if waitErr := backoff(ctx, attempt); waitErr != nil {
			return waitErr
		}
	}

	return fmt.Errorf("%w after %d attempts: %w", domain.ErrSerializationFailure, serializableMaxAttempts, err)
}

// SerializationRetries reports how many times a transaction was replayed after a
// serialization failure.
func (t *TxManager) SerializationRetries() int64 {
	return t.serializationRetries.Load()
}

// run opens a transaction with opts, executes unitOfWork against it, and commits
// or rolls back.
func (t *TxManager) run(ctx context.Context, opts pgx.TxOptions, unitOfWork transaction.UnitOfWork) error {
	// Nested calls join the ambient transaction instead of opening a second one.
	if txExists(ctx) {
		return unitOfWork(ctx)
	}

	tx, err := t.client.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			t.rollback(ctx, tx)
			panic(p)
		}
	}()

	if err = unitOfWork(contextWithTx(ctx, tx)); err != nil {
		t.rollback(ctx, tx)
		// Return the cause, not the rollback outcome — that would hide why we
		// are rolling back in the first place.
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// rollback aborts tx, logging any failure.
// The error is not returned: the caller already has the one that caused the rollback.
//
// You should be aware that we are detaching cancellation from ctx because rollback still has to reach the server
// otherwise, on parent cancellation the transaction could stay open until the connection is reaped.
func (t *TxManager) rollback(ctx context.Context, tx pgx.Tx) {
	if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.logger.ErrorContext(ctx, "rolling back transaction", "error", err)
	}
}

// contextWithTx embeds tx in ctx for downstream repositories.
func contextWithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// txExists reports whether ctx already carries an open transaction.
func txExists(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(pgx.Tx)
	return ok
}

// isRetryable reports whether err is a conflict PostgreSQL expects us to replay.
func isRetryable(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return false
	}
	// 40001 is the documented SERIALIZABLE contract; 40P01 is a deadlock, which is equally safe to replay since
	// nothing was committed.
	return pgErr.Code == pgerrcode.SerializationFailure || pgErr.Code == pgerrcode.DeadlockDetected
}

// backoff waits before the next attempt, with jitter so concurrent losers do not collide again in lockstep.
func backoff(ctx context.Context, attempt int) error {
	base := serializableBaseBackoff * time.Duration(1<<(attempt-1))
	wait := base + rand.N(base)

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
