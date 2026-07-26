package replication

import (
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSerializableReadOnlyDeferrable_IsRejectedOnAStandby contradicts the
// received wisdom that DEFERRABLE is the tool for long analytical queries on a
// replica.
//
// DEFERRABLE only means anything for a SERIALIZABLE READ ONLY transaction: it
// lets the transaction wait for a snapshot on which it can never see a
// serialization failure. A hot standby cannot offer SERIALIZABLE at all, so the
// combination is refused outright with SQLSTATE 0A000. DEFERRABLE is a primary
// tool.
func TestSerializableReadOnlyDeferrable_IsRejectedOnAStandby(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	conn, err := pg.Replica.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, `BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE READ ONLY DEFERRABLE`)

	require.Error(t, err)
	assert.Equal(t, pgerrcode.FeatureNotSupported, sqlState(t, err))
}

// TestReadOnlyDeferrable_IsAcceptedOnAStandbyAndDoesNothing is the trap the
// previous test only half explains.
//
// Drop SERIALIZABLE and the standby accepts the statement happily:
// transaction_deferrable reads "on" and nothing complains. But the isolation
// level is READ COMMITTED, and outside SERIALIZABLE READ ONLY the flag has no
// effect whatsoever. A query written this way looks protected and is not.
func TestReadOnlyDeferrable_IsAcceptedOnAStandbyAndDoesNothing(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	conn, err := pg.Replica.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, `BEGIN TRANSACTION READ ONLY DEFERRABLE`)
	require.NoError(t, err)
	defer func() { _, _ = conn.Exec(ctx, `ROLLBACK`) }()

	var deferrable, isolation string
	require.NoError(t, conn.QueryRow(ctx, `SHOW transaction_deferrable`).Scan(&deferrable))
	require.NoError(t, conn.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation))

	assert.Equal(t, "on", deferrable)
	assert.Equal(t, "read committed", isolation,
		"DEFERRABLE is inert here: it only applies to SERIALIZABLE READ ONLY")
}

// TestSerializableReadOnlyDeferrable_BelongsOnThePrimary shows where the mode
// actually works, which is the same server the read replica was supposed to
// take load off.
func TestSerializableReadOnlyDeferrable_BelongsOnThePrimary(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	conn, err := pg.Primary.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, `BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE READ ONLY DEFERRABLE`)
	require.NoError(t, err)
	defer func() { _, _ = conn.Exec(ctx, `ROLLBACK`) }()

	var deferrable, isolation string
	require.NoError(t, conn.QueryRow(ctx, `SHOW transaction_deferrable`).Scan(&deferrable))
	require.NoError(t, conn.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation))

	assert.Equal(t, "on", deferrable)
	assert.Equal(t, "serializable", isolation)
}

// TestStandby_ProtectsLongQueriesWithADelayNotWithDeferrable names what does the
// job instead.
//
// A standby cancels a query whose snapshot the incoming WAL would invalidate.
// max_standby_streaming_delay is how long it waits before doing so, and
// hot_standby_feedback is what stops the primary from vacuuming rows the
// standby still needs. Those two knobs are the analytical-query story on a
// replica — not DEFERRABLE.
func TestStandby_ProtectsLongQueriesWithADelayNotWithDeferrable(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	var delay, feedback string
	require.NoError(t, pg.Replica.QueryRow(ctx, `SELECT current_setting('max_standby_streaming_delay')`).Scan(&delay))
	require.NoError(t, pg.Replica.QueryRow(ctx, `SELECT current_setting('hot_standby_feedback')`).Scan(&feedback))

	assert.Equal(t, "30s", delay, "the default grace period before a conflicting query is cancelled")
	assert.Equal(t, "off", feedback, "off by default: the primary vacuums without waiting for this standby")
}
