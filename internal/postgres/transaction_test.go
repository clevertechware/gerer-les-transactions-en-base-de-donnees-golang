package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/postgres/mocks"
	pgxmocks "github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/mocks/github.com/jackc/pgx/v5"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction"
)

var errUnitOfWork = errors.New("unit of work failed")

func serializationFailure() error {
	return &pgconn.PgError{Code: pgerrcode.SerializationFailure, Message: "could not serialize access"}
}

func newTestManager(client Client) *TxManager {
	return NewTxManager(logger.NewNoOpLogger(), client)
}

// expectCommit sets the expectations of a transaction that reaches COMMIT.
//
// The deferred rollback still runs after a successful commit: pgx marks the
// transaction closed and answers ErrTxClosed without sending anything to the
// server. A mock has no such state, so the test spells the no-op out.
func expectCommit(tx *pgxmocks.Tx) {
	tx.EXPECT().Commit(mock.Anything).Return(nil).Once()
	tx.EXPECT().Rollback(mock.Anything).Return(pgx.ErrTxClosed).Once()
}

// TestTxManager_Execute_Commits proves the happy path ends in COMMIT, not just
// "no error".
func TestTxManager_Execute_Commits(t *testing.T) {
	t.Parallel()

	tx := pgxmocks.NewTx(t)
	expectCommit(tx)

	client := mocks.NewClient(t)
	client.EXPECT().BeginTx(mock.Anything, pgx.TxOptions{}).Return(tx, nil).Once()

	var ranInsideTx bool
	err := newTestManager(client).Execute(t.Context(), func(ctx context.Context) error {
		// The unit of work must receive the transaction, otherwise repositories
		// would silently write outside it.
		ranInsideTx = txExists(ctx)
		return nil
	})

	require.NoError(t, err)
	assert.True(t, ranInsideTx, "unit of work should run with the transaction in context")
}

// TestTxManager_Execute_RollsBackAndReturnsCause is the behaviour a demo about
// transactions cannot get wrong: on failure, ROLLBACK is issued and the caller
// gets the error that caused it — not the rollback's own outcome.
func TestTxManager_Execute_RollsBackAndReturnsCause(t *testing.T) {
	t.Parallel()

	tx := pgxmocks.NewTx(t)
	tx.EXPECT().Rollback(mock.Anything).Return(nil).Once()

	client := mocks.NewClient(t)
	client.EXPECT().BeginTx(mock.Anything, pgx.TxOptions{}).Return(tx, nil).Once()

	err := newTestManager(client).Execute(t.Context(), func(context.Context) error {
		return errUnitOfWork
	})

	require.ErrorIs(t, err, errUnitOfWork)
	tx.AssertNotCalled(t, "Commit", mock.Anything)
}

// TestTxManager_Execute_RollbackFailureKeepsCause guards a mistake that is easy
// to make: reporting the rollback error and losing why we rolled back.
func TestTxManager_Execute_RollbackFailureKeepsCause(t *testing.T) {
	t.Parallel()

	tx := pgxmocks.NewTx(t)
	tx.EXPECT().Rollback(mock.Anything).Return(errors.New("connection reset")).Once()

	client := mocks.NewClient(t)
	client.EXPECT().BeginTx(mock.Anything, pgx.TxOptions{}).Return(tx, nil).Once()

	err := newTestManager(client).Execute(t.Context(), func(context.Context) error {
		return errUnitOfWork
	})

	require.ErrorIs(t, err, errUnitOfWork, "the cause must survive a failing rollback")
}

// TestTxManager_Execute_JoinsAmbientTransaction proves nesting does not open a
// second transaction. Without this, a service calling another service would
// commit half the work early.
func TestTxManager_Execute_JoinsAmbientTransaction(t *testing.T) {
	t.Parallel()

	// No expectation at all: touching the client would fail the test.
	client := mocks.NewClient(t)
	ctx := contextWithTx(t.Context(), pgxmocks.NewTx(t), pgx.TxOptions{})

	var ran bool
	err := newTestManager(client).Execute(ctx, func(context.Context) error {
		ran = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, ran)
}

// TestTxManager_JoiningAnAmbientTransaction covers what joining is allowed to cost.
//
// Joining an open transaction is what lets services compose, but it silently
// replaces the isolation level the caller asked for with the one already in
// force. Nothing fails, nothing is logged, and the guarantee is simply gone —
// so a request for a stronger level than the ambient one has to be refused.
func TestTxManager_JoiningAnAmbientTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ambient pgx.TxOptions
		nested  func(*TxManager, context.Context, transaction.UnitOfWork) error
		wantErr error
	}{
		{
			name:    "refuses SERIALIZABLE inside the default isolation level",
			ambient: pgx.TxOptions{},
			nested:  (*TxManager).ExecuteSerializable,
			wantErr: domain.ErrIsolationDowngrade,
		},
		{
			name:    "refuses REPEATABLE READ inside the default isolation level",
			ambient: pgx.TxOptions{},
			nested:  (*TxManager).ExecuteReadOnly,
			wantErr: domain.ErrIsolationDowngrade,
		},
		{
			name:    "joins an equally strict transaction",
			ambient: pgx.TxOptions{IsoLevel: pgx.Serializable},
			nested:  (*TxManager).ExecuteSerializable,
		},
		{
			name:    "joins a stricter transaction",
			ambient: pgx.TxOptions{IsoLevel: pgx.Serializable},
			nested:  (*TxManager).ExecuteReadOnly,
		},
		{
			name:    "joins a stricter transaction for read-write work",
			ambient: pgx.TxOptions{IsoLevel: pgx.RepeatableRead},
			nested:  (*TxManager).Execute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// No expectation on the client: a nested call must never open a transaction.
			manager := newTestManager(mocks.NewClient(t))
			ctx := contextWithTx(t.Context(), pgxmocks.NewTx(t), tt.ambient)

			var ran bool
			err := tt.nested(manager, ctx, func(context.Context) error {
				ran = true
				return nil
			})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.False(t, ran, "the unit of work must not run under a guarantee it did not ask for")
				return
			}

			require.NoError(t, err)
			assert.True(t, ran)
		})
	}
}

// TestTxManager_Execute_RollsBackOnPanic makes sure a panic cannot leave a
// transaction open, holding its locks until the connection is reaped.
func TestTxManager_Execute_RollsBackOnPanic(t *testing.T) {
	t.Parallel()

	tx := pgxmocks.NewTx(t)
	tx.EXPECT().Rollback(mock.Anything).Return(nil).Once()

	client := mocks.NewClient(t)
	client.EXPECT().BeginTx(mock.Anything, pgx.TxOptions{}).Return(tx, nil).Once()

	assert.PanicsWithValue(t, "boom", func() {
		_ = newTestManager(client).Execute(t.Context(), func(context.Context) error {
			panic("boom")
		})
	}, "the panic must still propagate after the rollback")
}

// TestTxManager_Execute_ReportsCommitFailure covers the exit path the earlier
// explicit rollbacks did not: the unit of work succeeded, the COMMIT did not.
func TestTxManager_Execute_ReportsCommitFailure(t *testing.T) {
	t.Parallel()

	commitFailed := errors.New("connection reset")

	tx := pgxmocks.NewTx(t)
	tx.EXPECT().Commit(mock.Anything).Return(commitFailed).Once()
	tx.EXPECT().Rollback(mock.Anything).Return(pgx.ErrTxClosed).Once()

	client := mocks.NewClient(t)
	client.EXPECT().BeginTx(mock.Anything, pgx.TxOptions{}).Return(tx, nil).Once()

	err := newTestManager(client).Execute(t.Context(), func(context.Context) error {
		return nil
	})

	require.ErrorIs(t, err, commitFailed)
}

// TestTxManager_ExecuteReadOnly_UsesReadOnlyOptions pins both options down.
//
// AccessMode must stay ReadOnly or the safety net disappears; IsoLevel must stay
// RepeatableRead or the reads stop sharing a snapshot — under READ COMMITTED
// each statement takes a fresh one, and the only reason to open this transaction
// evaporates. Neither failure would show up as an error anywhere.
func TestTxManager_ExecuteReadOnly_UsesReadOnlyOptions(t *testing.T) {
	t.Parallel()

	tx := pgxmocks.NewTx(t)
	expectCommit(tx)

	client := mocks.NewClient(t)
	client.EXPECT().
		BeginTx(mock.Anything, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}).
		Return(tx, nil).
		Once()

	require.NoError(t, newTestManager(client).ExecuteReadOnly(t.Context(), func(context.Context) error {
		return nil
	}))
}

func TestTxManager_ExecuteSerializable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// outcomes drives what the unit of work returns on each attempt.
		outcomes        []error
		wantAttempts    int
		wantRetries     int64
		assertErr       func(t *testing.T, err error)
		expectIsolation pgx.TxOptions
	}{
		{
			name:         "succeeds on the first attempt",
			outcomes:     []error{nil},
			wantAttempts: 1,
			wantRetries:  0,
			assertErr:    func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "replays a serialization failure and then succeeds",
			// This is the contract from the article: 40001 is not a bug, it is
			// PostgreSQL telling us to run the transaction again.
			outcomes:     []error{serializationFailure(), nil},
			wantAttempts: 2,
			wantRetries:  1,
			assertErr:    func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:         "gives up after the maximum number of attempts",
			outcomes:     []error{serializationFailure(), serializationFailure(), serializationFailure()},
			wantAttempts: serializableMaxAttempts,
			wantRetries:  serializableMaxAttempts,
			assertErr: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, domain.ErrSerializationFailure)
			},
		},
		{
			name:         "does not replay an ordinary error",
			outcomes:     []error{errUnitOfWork},
			wantAttempts: 1,
			wantRetries:  0,
			assertErr: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, errUnitOfWork)
				assert.NotErrorIs(t, err, domain.ErrSerializationFailure)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := mocks.NewClient(t)
			for _, outcome := range tt.outcomes {
				tx := pgxmocks.NewTx(t)
				if outcome == nil {
					expectCommit(tx)
				} else {
					tx.EXPECT().Rollback(mock.Anything).Return(nil).Once()
				}
				client.EXPECT().
					BeginTx(mock.Anything, pgx.TxOptions{IsoLevel: pgx.Serializable}).
					Return(tx, nil).
					Once()
			}

			var attempts int
			manager := newTestManager(client)
			err := manager.ExecuteSerializable(t.Context(), func(context.Context) error {
				outcome := tt.outcomes[attempts]
				attempts++
				return outcome
			})

			tt.assertErr(t, err)
			assert.Equal(t, tt.wantAttempts, attempts, "number of attempts")
			assert.Equal(t, tt.wantRetries, manager.SerializationRetries(), "retry counter")
		})
	}
}

// TestTxManager_Executor is the behaviour that lets a repository be written once
// and used both inside and outside a transaction.
func TestTxManager_Executor(t *testing.T) {
	t.Parallel()

	client := mocks.NewClient(t)
	manager := newTestManager(client)

	t.Run("falls back to the pool outside a transaction", func(t *testing.T) {
		assert.Same(t, client, manager.Executor(t.Context()),
			"a single statement should run in autocommit, not in a transaction")
	})

	t.Run("returns the ambient transaction", func(t *testing.T) {
		tx := pgxmocks.NewTx(t)
		assert.Same(t, tx, manager.Executor(contextWithTx(t.Context(), tx, pgx.TxOptions{})))
	})
}

// TestTxManager_RequireTx covers the guard on SELECT ... FOR UPDATE: outside a
// transaction the lock would be released immediately, so we refuse instead of
// pretending to protect anything.
func TestTxManager_RequireTx(t *testing.T) {
	t.Parallel()

	manager := newTestManager(mocks.NewClient(t))

	_, err := manager.RequireTx(t.Context())
	require.ErrorIs(t, err, domain.ErrTransactionRequired)

	tx := pgxmocks.NewTx(t)
	got, err := manager.RequireTx(contextWithTx(t.Context(), tx, pgx.TxOptions{}))
	require.NoError(t, err)
	assert.Same(t, tx, got)
}
