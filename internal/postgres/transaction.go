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

// ExecuteNested runs unitOfWork in a savepoint inside the ambient transaction, so a failure undoes only what
// unitOfWork did and leaves the outer transaction usable. Outside a transaction it is an ordinary Execute.
//
// This is the one boundary whose error the caller is allowed to swallow, and that is its entire purpose: propagating
// the failure aborts the outer transaction anyway, which is what joining already does, for free. Use it for work the
// outcome does not depend on — and only there.
//
// The savepoint inherits the isolation level and the access mode of the transaction it opens in. PostgreSQL only
// accepts SET TRANSACTION before the first statement, so there is nothing to negotiate here and no options to take.
//
// A savepoint is a subtransaction, and each one that writes consumes an XID. Past 64 live subtransactions in a
// session the visibility checks fall off the subtrans cache and every read starts paying for it, so this belongs
// around a handful of scopes, never inside a loop over rows.
func (t *TxManager) ExecuteNested(ctx context.Context, unitOfWork transaction.UnitOfWork) error {
	state, ok := txFromContext(ctx)
	if !ok {
		return t.Execute(ctx, unitOfWork)
	}

	savepoint, err := state.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("opening savepoint: %w", err)
	}

	// Load-bearing beyond cleanup: a failed statement leaves the subtransaction aborted, and PostgreSQL answers 25P02
	// to everything until someone rolls back to the savepoint. This is what hands the outer transaction back usable.
	defer t.rollback(ctx, savepoint)

	if err = unitOfWork(contextWithTx(ctx, savepoint, state.opts)); err != nil {
		// A serialization failure or a deadlock is not contained by the savepoint: the snapshot does not move when we
		// roll back to it, and the server has already doomed the whole transaction. Only the outermost boundary can
		// answer it, by replaying everything, so this is marked as the error that must reach it.
		if isRetryable(err) {
			return fmt.Errorf("%w: %w", domain.ErrConflictAbortsTransaction, err)
		}
		return err
	}

	if err = savepoint.Commit(ctx); err != nil {
		return fmt.Errorf("releasing savepoint: %w", err)
	}

	return nil
}

// ConflictRetries reports how many times a transaction was replayed after a serialization failure or a deadlock.
func (t *TxManager) ConflictRetries() int64 {
	return t.conflictRetries.Load()
}

// run opens a transaction with opts, executes unitOfWork against it, and commits or rolls back.
//
// pgx.BeginTxFunc owns the exit paths: it commits when unitOfWork returns nil, rolls back when it does not, and its
// deferred rollback covers the panic. What it does not own is the vocabulary — it hands back the driver's error as
// it is, so the one distinction the caller cannot afford to lose is restored here.
func (t *TxManager) run(ctx context.Context, opts pgx.TxOptions, unitOfWork transaction.UnitOfWork) error {
	// Nested calls join the ambient transaction instead of opening a second one.
	if state, ok := txFromContext(ctx); ok {
		if err := state.canSatisfy(opts); err != nil {
			return err
		}
		return unitOfWork(ctx)
	}

	err := pgx.BeginTxFunc(ctx, t.client, opts, func(tx pgx.Tx) error {
		return unitOfWork(contextWithTx(ctx, tx, opts))
	})

	// PostgreSQL answered ROLLBACK to our COMMIT: the transaction was already aborted server-side, so the commit
	// reached the server but wrote nothing. Worth its own error, the caller must not read it as a lost connection.
	//
	// Asking the returned error rather than the commit is a downgrade: a unit of work that returns something wrapping
	// ErrTxCommitRollback would be reported as an aborted transaction. Nothing here does, and the single error that
	// pgx.BeginTxFunc returns leaves no way to tell BEGIN, the unit of work and COMMIT apart.
	if errors.Is(err, pgx.ErrTxCommitRollback) {
		return fmt.Errorf("%w: %w", domain.ErrTransactionAborted, err)
	}

	return err
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
