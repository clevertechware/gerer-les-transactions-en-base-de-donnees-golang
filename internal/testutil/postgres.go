// Package testutil starts the PostgreSQL container the integration tests run
// against. These tests are about what the database actually does under
// concurrency — locks, snapshots, serialization failures — so none of it can be
// faked with a mock.
package testutil

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/config"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/migrate"
)

const (
	image    = "postgres:18-alpine"
	database = "demo_test"
	username = "postgres"
	password = "postgres"
)

// Postgres is a running container with the migrations applied.
type Postgres struct {
	Config config.Postgres
	Pool   *pgxpool.Pool
}

// shared is the single container for the test binary. Go runs each package as
// its own process, so one per package is also one per process.
var shared *Postgres

// RunWithPostgres starts the container, runs the tests, then tears it down.
// Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.RunWithPostgres(m)) }
//
// In short mode it starts nothing, so `go test -short` needs no Docker.
func RunWithPostgres(m *testing.M) int {
	// testing.Short() is only meaningful once the flags are parsed, and m.Run()
	// has not done that yet.
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}

	ctx := context.Background()

	pg, terminate, err := start(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "starting postgres container: %v\n", err)
		return 1
	}
	shared = pg

	code := m.Run()

	if err := terminate(); err != nil {
		fmt.Fprintf(os.Stderr, "terminating postgres container: %v\n", err)
	}
	return code
}

// Shared returns the container started by RunWithPostgres, skipping the test in
// short mode.
func Shared(t *testing.T) *Postgres {
	t.Helper()

	if testing.Short() {
		t.Skip("integration test: needs Docker, skipped in short mode")
	}
	if shared == nil {
		t.Fatal("no container: TestMain must call testutil.RunWithPostgres")
	}
	return shared
}

func start(ctx context.Context) (*Postgres, func() error, error) {
	container, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase(database),
		tcpostgres.WithUsername(username),
		tcpostgres.WithPassword(password),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, nil, err
	}

	terminate := func() error {
		return testcontainers.TerminateContainer(container)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, nil, err
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, nil, err
	}

	cfg := config.Postgres{
		Host:     host,
		Port:     int(port.Num()),
		Database: database,
		User:     username,
		Password: password,
		SSLMode:  "disable",
		MinConns: 2,
		// Small on purpose: the concurrency tests should contend on rows, not
		// wait for a connection, but a tiny pool also surfaces leaks fast.
		MaxConns: 10,
	}

	migrations, err := migrationsDir()
	if err != nil {
		return nil, nil, err
	}
	if err := migrate.Up(cfg, migrations); err != nil {
		return nil, nil, fmt.Errorf("applying migrations: %w", err)
	}

	// Built here rather than through internal/postgres: that package's own tests
	// import this one, and going the other way would be an import cycle.
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, nil, err
	}
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConns = cfg.MaxConns

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, nil, err
	}

	return &Postgres{Config: cfg, Pool: pool}, func() error {
		pool.Close()
		return terminate()
	}, nil
}

// Truncate empties every table. Used by the tests that need real concurrency
// across connections, where the transaction-per-test trick does not apply.
func Truncate(t *testing.T, pg *Postgres) {
	t.Helper()

	_, err := pg.Pool.Exec(context.Background(),
		`TRUNCATE user_companies, companies, users RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncating tables: %v", err)
	}
}

// Port returns the container port as a string, for building DSNs in tests.
func (p *Postgres) Port() string { return strconv.Itoa(p.Config.Port) }

// migrationsDir locates the migrations folder by walking up to the module root.
func migrationsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "migrations"), nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("module root not found above %s", dir)
		}
		dir = parent
	}
}
