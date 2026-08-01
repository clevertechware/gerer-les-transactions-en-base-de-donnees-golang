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

// txState is what the context carries: the open transaction and the options it was opened with.
//
// Keeping the options alongside is what lets a nested call tell whether joining still honors what it asked for.
type txState struct {
	tx   pgx.Tx
	opts pgx.TxOptions
}

// canSatisfy reports whether joining this transaction still honors opts.
//
// Joining is what lets services compose without opening a second transaction, but it is only safe when the open one
// is at least as strict as the caller asks for. An ExecuteSerializable nested in an Execute would otherwise run at
// READ COMMITTED, without a retry, and nothing would say so.
//
// The isolation level is ranked because a stronger level still honors a weaker request.
//
// The access mode is not:
//
// - Read-only nested in read-write silently loses the net that rejects writes,
//
// - Read-write nested in read-only would die on SQLSTATE 25006 at the first write.
//
// Neither direction composes, so both are refused.
func (s txState) canSatisfy(opts pgx.TxOptions) error {
	if isolationRank(opts.IsoLevel) > isolationRank(s.opts.IsoLevel) {
		return fmt.Errorf("%w: %s requested inside %s",
			domain.ErrIsolationDowngrade, effectiveIsolation(opts.IsoLevel), effectiveIsolation(s.opts.IsoLevel))
	}

	if effectiveAccessMode(opts.AccessMode) != effectiveAccessMode(s.opts.AccessMode) {
		return fmt.Errorf("%w: %s requested inside %s",
			domain.ErrAccessModeMismatch, effectiveAccessMode(opts.AccessMode), effectiveAccessMode(s.opts.AccessMode))
	}

	return nil
}

// isolationRank orders isolation levels from the weakest to the strongest.
func isolationRank(level pgx.TxIsoLevel) int {
	switch effectiveIsolation(level) {
	case pgx.Serializable:
		return 3
	case pgx.RepeatableRead:
		return 2
	default:
		return 1
	}
}

// effectiveIsolation resolves the empty option to READ COMMITTED, the level a stock PostgreSQL applies when the client
// does not ask for one.
func effectiveIsolation(level pgx.TxIsoLevel) pgx.TxIsoLevel {
	if level == "" {
		return pgx.ReadCommitted
	}
	return level
}

// effectiveAccessMode resolves the empty option to READ WRITE, the mode a stock PostgreSQL applies when the client
// does not ask for one.
func effectiveAccessMode(mode pgx.TxAccessMode) pgx.TxAccessMode {
	if mode == "" {
		return pgx.ReadWrite
	}
	return mode
}

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

	// conflictRetries counts replays caused by a serialization failure or a deadlock.
	// Exposed so tests can prove a retry actually happened rather than infer it.
	conflictRetries atomic.Int64
}

// NewTxManager creates a TxManager over the given client.
func NewTxManager(log logger.Logger, client Client) *TxManager {
	return &TxManager{logger: log, client: client}
}

// Executor returns the transaction carried by ctx if there is one, and the connection pool otherwise.
//
// This is the hinge of the whole demo. A repository written against Executor
// runs unchanged inside a transaction or in autocommit, so the decision to open
// one stays where it belongs: in the service, next to the business invariant
// that justifies it. Without this, every single-statement read and write would
// be wrapped in a BEGIN it does not need.
func (t *TxManager) Executor(ctx context.Context) Executor {
	if state, ok := txFromContext(ctx); ok {
		return state.tx
	}
	return t.client
}

// RequireTx returns an Executor bound to the ambient transaction, or ErrTransactionRequired.
//
// Only for statements that are meaningless without one: SELECT ... FOR UPDATE
// takes a row lock that is released at the end of the transaction, so in
// autocommit it is released immediately and protects nothing.
//
// It hands back an Executor rather than the pgx.Tx: a repository needs the
// guarantee that a transaction is open, never the ability to end it.
func (t *TxManager) RequireTx(ctx context.Context) (Executor, error) {
	if state, ok := txFromContext(ctx); ok {
		return state.tx, nil
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
	opts := pgx.TxOptions{IsoLevel: pgx.Serializable}

	// Nested calls join the ambient transaction instead of opening a second one, and
	// there is nothing to replay: the transaction that would be retried is not ours.
	if state, ok := txFromContext(ctx); ok {
		if err := state.canSatisfy(opts); err != nil {
			return err
		}
		return unitOfWork(ctx)
	}

	var err error
	for attempt := 1; attempt <= serializableMaxAttempts; attempt++ {
		err = t.run(ctx, opts, unitOfWork)
		if !isRetryable(err) {
			return err
		}

		t.conflictRetries.Add(1)
		t.logger.WarnContext(ctx, "conflict with a concurrent transaction, replaying",
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

// ConflictRetries reports how many times a transaction was replayed after a serialization failure or a deadlock.
func (t *TxManager) ConflictRetries() int64 {
	return t.conflictRetries.Load()
}

// run opens a transaction with opts, executes unitOfWork against it, and commits or rolls back.
func (t *TxManager) run(ctx context.Context, opts pgx.TxOptions, unitOfWork transaction.UnitOfWork) error {
	// Nested calls join the ambient transaction instead of opening a second one.
	if state, ok := txFromContext(ctx); ok {
		if err := state.canSatisfy(opts); err != nil {
			return err
		}
		return unitOfWork(ctx)
	}

	tx, err := t.client.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	// Scheduled before any work: it makes "the transaction is never left open" a property of this function rather
	// than of its exit paths, and it covers the panic as well. After a successful commit, it is a no-op as pgx marks
	// the transaction closed and answers ErrTxClosed without reaching the server.
	defer t.rollback(ctx, tx)

	if err = unitOfWork(contextWithTx(ctx, tx, opts)); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		// PostgreSQL answered ROLLBACK to our COMMIT: the transaction was already aborted server-side, so the commit
		// reached the server but wrote nothing. Worth its own error, the caller must not read it as a lost connection.
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return fmt.Errorf("%w: %w", domain.ErrTransactionAborted, err)
		}
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

// contextWithTx embeds tx and the options it was opened with in ctx, for downstream
// repositories and for nested transaction boundaries.
func contextWithTx(ctx context.Context, tx pgx.Tx, opts pgx.TxOptions) context.Context {
	return context.WithValue(ctx, txKey{}, txState{tx: tx, opts: opts})
}

// txFromContext returns the transaction carried by ctx, if there is one.
func txFromContext(ctx context.Context) (txState, bool) {
	state, ok := ctx.Value(txKey{}).(txState)
	return state, ok
}

// txExists reports whether ctx already carries an open transaction.
func txExists(ctx context.Context) bool {
	_, ok := txFromContext(ctx)
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

// backoff waits before the next attempt, with jitter, so concurrent losers do not collide again in lockstep.
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
