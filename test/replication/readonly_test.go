package replication

import (
	"errors"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sqlState returns the SQLSTATE carried by err, failing the test if it carries
// none — an error without one means something other than PostgreSQL went wrong.
func sqlState(t *testing.T, err error) string {
	t.Helper()

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "expected a PostgreSQL error, got %v", err)
	return pgErr.Code
}

// TestReadOnlyConnection_RefusesAWriteWithNoTransactionEverOpened isolates the
// claim the article makes in passing: the write protection comes from the
// connection, not from BEGIN.
//
// This runs on the primary, in autocommit, with nothing replicated and no
// explicit transaction anywhere — only default_transaction_read_only set on the
// session. The write is still refused. Anything a routing layer forgets to wrap
// is covered.
func TestReadOnlyConnection_RefusesAWriteWithNoTransactionEverOpened(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	conn, err := pgx.Connect(ctx, pg.PrimaryConfig.DSN()+" options='-c default_transaction_read_only=on'")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(ctx) })

	_, err = conn.Exec(ctx, `INSERT INTO companies (name) VALUES ('written in autocommit')`)

	require.Error(t, err)
	assert.Equal(t, pgerrcode.ReadOnlySQLTransaction, sqlState(t, err))
}

// TestStandby_RefusesAWriteEvenWithTheSettingTurnedOff separates the two guards
// that are easy to conflate.
//
// default_transaction_read_only is a session setting, so a session can turn it
// off — and PostgreSQL accepts that on a standby. The write is refused anyway,
// because a server in recovery has no way to write at all. The setting is the
// polite refusal; recovery is the one that cannot be argued with.
func TestStandby_RefusesAWriteEvenWithTheSettingTurnedOff(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	conn, err := pg.Replica.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, `SET default_transaction_read_only = off`)
	require.NoError(t, err, "a standby session is allowed to unset the flag")

	_, err = conn.Exec(ctx, `INSERT INTO companies (name) VALUES ('written on the standby')`)

	require.Error(t, err)
	assert.Equal(t, pgerrcode.ReadOnlySQLTransaction, sqlState(t, err))
}
