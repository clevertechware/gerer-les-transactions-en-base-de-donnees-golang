package migrate_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/migrate"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithPostgres(m))
}

// countDemoColumns reports how many of the columns added by the second
// migration are currently present.
func countDemoColumns(t *testing.T, pg *testutil.Postgres) int {
	t.Helper()

	const query = `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'companies'
		  AND column_name IN ('verification_status', 'verification_ref', 'verified_at', 'seat_limit')`

	var count int
	require.NoError(t, pg.Pool.QueryRow(context.Background(), query).Scan(&count))
	return count
}

// TestMigrations_DownThenUp checks that the down files really undo the up files.
//
// A down migration that is never executed is a down migration that does not
// work — it is only ever needed in a rollback, which is the worst moment to find
// out. This test runs the full cycle so that discovery happens here instead.
func TestMigrations_DownThenUp(t *testing.T) {
	pg := testutil.Shared(t)

	// RunWithPostgres already applied everything.
	require.Equal(t, 4, countDemoColumns(t, pg), "migrations should be applied at start")

	dir, err := os.Getwd()
	require.NoError(t, err)
	migrations := dir + "/../../migrations"

	require.NoError(t, migrate.Down(pg.Config, migrations), "reverting migrations")
	require.Equal(t, 0, countDemoColumns(t, pg), "down should have dropped the demo columns")

	require.NoError(t, migrate.Up(pg.Config, migrations), "re-applying migrations")
	require.Equal(t, 4, countDemoColumns(t, pg), "up should have restored the demo columns")

	// The tables must be usable again, not merely present.
	var name string
	err = pg.Pool.QueryRow(context.Background(),
		`INSERT INTO companies (name) VALUES ('after-cycle') RETURNING name`).Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "after-cycle", name)
}
