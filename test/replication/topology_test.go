// Package replication tests what routing reads to a hot standby actually buys
// and actually costs.
//
// None of it can be mocked: replication lag and the standby's refusal to write
// are properties of a running PostgreSQL pair, so this package builds one — a
// primary and a standby cloned from it, streaming.
package replication

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/config"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/migrate"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/testutil"
)

const (
	image        = "postgres:18-alpine"
	database     = "demo_test"
	username     = "postgres"
	password     = "postgres"
	primaryAlias = "primary"
	replicaData  = "/var/lib/postgresql/replica"
)

// topology is a primary and the standby streaming from it.
type topology struct {
	PrimaryConfig config.Postgres
	ReplicaConfig config.Postgres
	Primary       *pgxpool.Pool
	Replica       *pgxpool.Pool
}

var shared *topology

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	ctx := context.Background()

	t, terminate, err := startTopology(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "starting the replication topology: %v\n", err)
		os.Exit(1)
	}
	shared = t

	code := m.Run()

	if err := terminate(); err != nil {
		fmt.Fprintf(os.Stderr, "terminating the replication topology: %v\n", err)
	}
	os.Exit(code)
}

func sharedTopology(t *testing.T) *topology {
	t.Helper()

	if testing.Short() {
		t.Skip("integration test: needs Docker, skipped in short mode")
	}
	if shared == nil {
		t.Fatal("no topology: TestMain must call startTopology")
	}
	return shared
}

func startTopology(ctx context.Context) (*topology, func() error, error) {
	root, err := testutil.ModuleRoot()
	if err != nil {
		return nil, nil, err
	}

	network, err := tcnetwork.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("creating the network: %w", err)
	}

	primary, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase(database),
		tcpostgres.WithUsername(username),
		tcpostgres.WithPassword(password),
		// The same script compose mounts: initdb only accepts replication
		// connections from localhost, so without it the standby cannot clone.
		tcpostgres.WithInitScripts(filepath.Join(root, "docker", "primary", "10-allow-replication.sh")),
		tcnetwork.WithNetwork([]string{primaryAlias}, network),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("starting the primary: %w", err)
	}

	primaryConfig, err := connectionConfig(ctx, primary)
	if err != nil {
		return nil, nil, err
	}

	// Migrated before the clone, so the standby is born with the schema instead
	// of racing to replay it.
	if err := migrate.Up(primaryConfig, filepath.Join(root, "migrations")); err != nil {
		return nil, nil, fmt.Errorf("applying migrations: %w", err)
	}

	replica, err := startStandby(ctx, network.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("starting the standby: %w", err)
	}

	replicaConfig, err := connectionConfig(ctx, replica)
	if err != nil {
		return nil, nil, err
	}

	primaryPool, err := openPool(ctx, primaryConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to the primary: %w", err)
	}
	replicaPool, err := openPool(ctx, replicaConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to the standby: %w", err)
	}

	topology := &topology{
		PrimaryConfig: primaryConfig,
		ReplicaConfig: replicaConfig,
		Primary:       primaryPool,
		Replica:       replicaPool,
	}

	return topology, func() error {
		primaryPool.Close()
		replicaPool.Close()
		return errors.Join(
			testcontainers.TerminateContainer(replica),
			testcontainers.TerminateContainer(primary),
			network.Remove(context.Background()),
		)
	}, nil
}

// startStandby clones the primary with pg_basebackup, then serves the copy in
// recovery. default_transaction_read_only is the connection-level guard the
// article points at: it refuses writes without anyone having to say BEGIN.
func startStandby(ctx context.Context, networkName string) (testcontainers.Container, error) {
	return testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image,
			User:         username,
			Env:          map[string]string{"PGDATA": replicaData},
			ExposedPorts: []string{"5432/tcp"},
			Networks:     []string{networkName},
			Entrypoint:   []string{"/bin/sh", "-c"},
			Cmd: []string{fmt.Sprintf(
				`set -e
				 pg_basebackup -h %s -p 5432 -U %s -D "$PGDATA" -Fp -Xs -R -w -P
				 chmod 0700 "$PGDATA"
				 exec postgres -c hot_standby=on -c default_transaction_read_only=on`,
				primaryAlias, username,
			)},
			WaitingFor: wait.
				ForLog("database system is ready to accept read-only connections").
				WithStartupTimeout(2 * time.Minute),
		},
	})
}

func connectionConfig(ctx context.Context, container testcontainers.Container) (config.Postgres, error) {
	host, err := container.Host(ctx)
	if err != nil {
		return config.Postgres{}, err
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return config.Postgres{}, err
	}

	return config.Postgres{
		Host:     host,
		Port:     int(port.Num()),
		Database: database,
		User:     username,
		Password: password,
		SSLMode:  "disable",
		MinConns: 1,
		MaxConns: 5,
	}, nil
}

func openPool(ctx context.Context, cfg config.Postgres) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, err
	}
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConns = cfg.MaxConns

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
