package postgres

import (
	"os"
	"testing"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/testutil"
)

// TestMain starts one PostgreSQL container for the whole package.
//
// goleak is deliberately not used: testcontainers and pgxpool both keep
// background goroutines alive that it would report as leaks.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithPostgres(m))
}
