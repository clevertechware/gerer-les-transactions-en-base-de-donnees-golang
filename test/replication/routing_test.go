package replication

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopology_TheStandbyIsInRecoveryAndThePrimaryIsNot(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	var primaryInRecovery, replicaInRecovery bool
	require.NoError(t, pg.Primary.QueryRow(ctx, `SELECT pg_is_in_recovery()`).Scan(&primaryInRecovery))
	require.NoError(t, pg.Replica.QueryRow(ctx, `SELECT pg_is_in_recovery()`).Scan(&replicaInRecovery))

	assert.False(t, primaryInRecovery)
	assert.True(t, replicaInRecovery, "the standby must be replaying WAL, not serving its own copy")
}
