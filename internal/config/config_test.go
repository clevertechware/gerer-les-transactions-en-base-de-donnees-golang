package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validConfig = `
server:
  host: ""
  port: 8080
  mode: debug
  shutdown_timeout: 10s
postgres:
  host: localhost
  port: 5432
  database: demo
  user: postgres
  password: postgres
  sslmode: disable
  min_conns: 2
  max_conns: 10
logging:
  level: info
  format: text
remote:
  base_url: http://localhost:9090
  timeout: 10s
  port: 9090
  delay: 3s
`

// writeConfig drops a config file into a temporary directory and returns it.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), []byte(content), 0o600))
	return dir
}

func TestLoad(t *testing.T) {
	t.Parallel()

	dir := writeConfig(t, validConfig)

	cfg, err := Load(dir)
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.Server.Addr())
	assert.Equal(t, 10*time.Second, cfg.Server.ShutdownTimeout)
	assert.Equal(t, "demo", cfg.Postgres.Database)
	assert.Equal(t, int32(10), cfg.Postgres.MaxConns)
	assert.Equal(t, 3*time.Second, cfg.Remote.Delay)
	assert.Equal(t, ":9090", cfg.Remote.Addr())
}

// TestLoad_EnvironmentOverridesTheFile is what keeps credentials out of the
// committed application.yaml.
func TestLoad_EnvironmentOverridesTheFile(t *testing.T) {
	dir := writeConfig(t, validConfig)

	t.Setenv("DEMO_POSTGRES__PASSWORD", "from-the-environment")
	t.Setenv("DEMO_POSTGRES__HOST", "db.internal")
	t.Setenv("DEMO_SERVER__PORT", "9999")

	cfg, err := Load(dir)
	require.NoError(t, err)

	assert.Equal(t, "from-the-environment", cfg.Postgres.Password)
	assert.Equal(t, "db.internal", cfg.Postgres.Host)
	assert.Equal(t, 9999, cfg.Server.Port)
	// Untouched keys keep their file value.
	assert.Equal(t, "demo", cfg.Postgres.Database)
}

func TestLoad_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "missing file",
			config:  "",
			wantErr: "loading application.yaml",
		},
		{
			name: "no database name",
			config: `
server: {port: 8080}
postgres: {host: localhost, database: ""}
remote: {base_url: "http://x"}
`,
			wantErr: "postgres.database is required",
		},
		{
			name: "max_conns below min_conns",
			config: `
server: {port: 8080}
postgres: {host: localhost, database: demo, min_conns: 10, max_conns: 2}
remote: {base_url: "http://x"}
`,
			wantErr: "postgres.max_conns",
		},
		{
			name: "no remote base url",
			config: `
server: {port: 8080}
postgres: {host: localhost, database: demo, min_conns: 1, max_conns: 2}
remote: {base_url: ""}
`,
			wantErr: "remote.base_url is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tt.config != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), []byte(tt.config), 0o600))
			}

			_, err := Load(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestPostgres_ConnectionStrings covers both renderings, since one feeds pgxpool
// and the other golang-migrate and they are easy to get subtly wrong.
func TestPostgres_ConnectionStrings(t *testing.T) {
	t.Parallel()

	pg := Postgres{
		Host: "localhost", Port: 55432, Database: "demo",
		User: "postgres", Password: "p@ss word", SSLMode: "disable",
	}

	assert.Equal(t,
		"host=localhost port=55432 user=postgres password=p@ss word dbname=demo sslmode=disable",
		pg.DSN())

	// The URL form must percent-encode the password, or a special character
	// silently truncates the credentials.
	assert.Equal(t,
		"pgx5://postgres:p%40ss%20word@localhost:55432/demo?sslmode=disable",
		pg.MigrateURL())
}

func TestPostgres_SSLModeDefaultsToDisable(t *testing.T) {
	t.Parallel()

	pg := Postgres{Host: "localhost", Port: 5432, Database: "demo", User: "postgres"}

	assert.Contains(t, pg.DSN(), "sslmode=disable")
	assert.Contains(t, pg.MigrateURL(), "sslmode=disable")
}
