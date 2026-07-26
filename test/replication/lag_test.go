package replication

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/domain"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/logger"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/postgres"
)

// pauseReplay stops the standby from replaying WAL and resumes it on cleanup.
//
// Replication on a laptop settles in single-digit milliseconds, so a test that
// merely raced the standby would pass or fail by luck. Pausing replay turns the
// lag into something the test controls, and every assertion below becomes a
// statement about behaviour rather than about timing.
func pauseReplay(t *testing.T, pg *topology) {
	t.Helper()

	ctx := t.Context()
	_, err := pg.Replica.Exec(ctx, `SELECT pg_wal_replay_pause()`)
	require.NoError(t, err)

	t.Cleanup(func() {
		// Detached: t.Context() is already cancelled here, and leaving the
		// standby paused would poison every later test in the package.
		ctx := context.WithoutCancel(ctx)
		if _, err := pg.Replica.Exec(ctx, `SELECT pg_wal_replay_resume()`); err != nil {
			t.Errorf("resuming WAL replay: %v", err)
		}
		waitForCatchUp(t, pg, ctx)
	})
}

// waitForCatchUp blocks until the standby has replayed everything the primary
// has written.
func waitForCatchUp(t *testing.T, pg *topology, ctx context.Context) {
	t.Helper()

	var target string
	require.NoError(t, pg.Primary.QueryRow(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&target))

	deadline := time.Now().Add(10 * time.Second)
	for {
		var caughtUp bool
		require.NoError(t, pg.Replica.
			QueryRow(ctx, `SELECT pg_last_wal_replay_lsn() >= $1::pg_lsn`, target).
			Scan(&caughtUp))
		if caughtUp {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("standby never replayed up to %s", target)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func insertCompanyOnPrimary(t *testing.T, pg *topology, name string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	require.NoError(t, pg.Primary.
		QueryRow(t.Context(), `INSERT INTO companies (name) VALUES ($1) RETURNING id`, name).
		Scan(&id))
	return id
}

func newRoutingManager(pg *topology) *postgres.TxManager {
	return postgres.NewTxManager(logger.NewNoOpLogger(), pg.Primary, postgres.WithReadReplica(pg.Replica))
}

// TestExecuteReadOnly_ReadsAStandbyThatHasNotCaughtUp is the price of the
// routing, stated as a failing read rather than as a warning in a README.
//
// The row is committed on the primary. ExecuteReadOnly still cannot find it,
// because that transaction was routed to a standby which has not replayed the
// WAL yet. Any endpoint that reads back what the same request just wrote must
// not go through ExecuteReadOnly.
func TestExecuteReadOnly_ReadsAStandbyThatHasNotCaughtUp(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	manager := newRoutingManager(pg)
	companies := postgres.NewCompanyRepository(manager, logger.NewNoOpLogger())

	pauseReplay(t, pg)
	id := insertCompanyOnPrimary(t, pg, "committed before the standby caught up")

	var readOnlyErr error
	require.NoError(t, manager.ExecuteReadOnly(ctx, func(ctx context.Context) error {
		_, readOnlyErr = companies.GetByID(ctx, id)
		return nil
	}))

	assert.ErrorIs(t, readOnlyErr, domain.ErrCompanyNotFound,
		"the standby cannot show a row whose WAL it has not replayed")
}

// TestExecute_SeesItsOwnWritesWhileTheStandbyLags proves the routing is
// selective rather than global. Same manager, same paused standby: the
// read-write path never leaves the primary, so it reads what it wrote.
func TestExecute_SeesItsOwnWritesWhileTheStandbyLags(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	manager := newRoutingManager(pg)
	companies := postgres.NewCompanyRepository(manager, logger.NewNoOpLogger())

	pauseReplay(t, pg)
	id := insertCompanyOnPrimary(t, pg, "read back on the primary")

	var found *domain.Company
	require.NoError(t, manager.Execute(ctx, func(ctx context.Context) error {
		var err error
		found, err = companies.GetByID(ctx, id)
		return err
	}))

	require.NotNil(t, found)
	assert.Equal(t, id, found.ID)
}

// TestAutocommitReadsStayOnThePrimary covers the decision that is invisible in
// the code and would be the easiest one to get wrong.
//
// A repository call outside any transaction cannot be routed: the manager has
// no way to know whether the next statement writes. Sending those reads to the
// standby would break read-your-writes for every plain GET in the API.
func TestAutocommitReadsStayOnThePrimary(t *testing.T) {
	pg := sharedTopology(t)

	manager := newRoutingManager(pg)
	companies := postgres.NewCompanyRepository(manager, logger.NewNoOpLogger())

	pauseReplay(t, pg)
	id := insertCompanyOnPrimary(t, pg, "read back in autocommit")

	found, err := companies.GetByID(t.Context(), id)

	require.NoError(t, err)
	assert.Equal(t, id, found.ID)
}

// TestExecuteReadOnly_FindsTheRowOnceTheStandbyCatchesUp closes the loop: the
// miss above is a delay, not a loss.
func TestExecuteReadOnly_FindsTheRowOnceTheStandbyCatchesUp(t *testing.T) {
	pg := sharedTopology(t)
	ctx := t.Context()

	manager := newRoutingManager(pg)
	companies := postgres.NewCompanyRepository(manager, logger.NewNoOpLogger())

	id := insertCompanyOnPrimary(t, pg, "read after the standby caught up")
	waitForCatchUp(t, pg, ctx)

	var found *domain.Company
	require.NoError(t, manager.ExecuteReadOnly(ctx, func(ctx context.Context) error {
		var err error
		found, err = companies.GetByID(ctx, id)
		return err
	}))

	require.NotNil(t, found)
	assert.Equal(t, id, found.ID)
}
