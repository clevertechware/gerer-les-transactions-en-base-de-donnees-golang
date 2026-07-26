// Package config loads the application configuration from application.yaml,
// then lets environment variables override it so that credentials never have to
// live in a committed file.
package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

const (
	configFileName = "application.yaml"

	// envPrefix scopes the environment variables we consider. Nested keys use a
	// double underscore: DEMO_POSTGRES__PASSWORD overrides postgres.password.
	envPrefix = "DEMO_"
	envNested = "__"
)

// Server holds the HTTP server settings.
type Server struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
	// Mode is the gin mode: debug, release or test.
	Mode string `koanf:"mode"`
	// ShutdownTimeout bounds how long in-flight requests get to finish.
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout"`
}

// Addr returns the listen address for the HTTP server.
func (s Server) Addr() string {
	return net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
}

// Postgres holds the database connection settings.
type Postgres struct {
	Host     string `koanf:"host"`
	Port     int    `koanf:"port"`
	Database string `koanf:"database"`
	User     string `koanf:"user"`
	Password string `koanf:"password"`
	SSLMode  string `koanf:"sslmode"`
	MinConns int32  `koanf:"min_conns"`
	MaxConns int32  `koanf:"max_conns"`

	// Replica points at a hot standby that read-only transactions can be sent
	// to. Leave it out and everything runs on this connection, which is the
	// single-database setup the rest of the demo assumes.
	Replica *Postgres `koanf:"replica"`
}

// ReadReplica returns the standby settings, or nil when none is configured.
//
// A standby usually differs from its primary by a host and a port and nothing
// else, so every field left empty is inherited. That way the replica section
// stays as short as what actually changes.
func (p Postgres) ReadReplica() *Postgres {
	if p.Replica == nil {
		return nil
	}

	replica := *p.Replica
	replica.Replica = nil

	if replica.Host == "" {
		replica.Host = p.Host
	}
	if replica.Port == 0 {
		replica.Port = p.Port
	}
	if replica.Database == "" {
		replica.Database = p.Database
	}
	if replica.User == "" {
		replica.User = p.User
	}
	if replica.Password == "" {
		replica.Password = p.Password
	}
	if replica.SSLMode == "" {
		replica.SSLMode = p.SSLMode
	}
	if replica.MinConns == 0 {
		replica.MinConns = p.MinConns
	}
	if replica.MaxConns == 0 {
		replica.MaxConns = p.MaxConns
	}

	return &replica
}

// DSN returns the keyword/value connection string consumed by pgxpool.
func (p Postgres) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		p.Host, p.Port, p.User, p.Password, p.Database, p.sslMode(),
	)
}

// MigrateURL returns the URL golang-migrate expects for its pgx/v5 driver.
func (p Postgres) MigrateURL() string {
	u := url.URL{
		Scheme:   "pgx5",
		User:     url.UserPassword(p.User, p.Password),
		Host:     net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:     "/" + p.Database,
		RawQuery: url.Values{"sslmode": {p.sslMode()}}.Encode(),
	}
	return u.String()
}

func (p Postgres) sslMode() string {
	if p.SSLMode == "" {
		return "disable"
	}
	return p.SSLMode
}

// Remote describes the slow third party the verification endpoints call.
// One section configures both sides of the exchange: the client lives in
// cmd/server, the service it talks to lives in cmd/remote.
type Remote struct {
	// Client side, read by cmd/server.
	BaseURL string        `koanf:"base_url"`
	Timeout time.Duration `koanf:"timeout"`

	// Service side, read by cmd/remote.
	Port int `koanf:"port"`
	// Delay is how long the fake provider takes to answer. It is the whole
	// point of this binary: it makes the cost of an in-transaction network
	// call impossible to miss.
	Delay time.Duration `koanf:"delay"`
}

// Addr returns the listen address for the fake provider.
func (r Remote) Addr() string {
	return net.JoinHostPort("", strconv.Itoa(r.Port))
}

// Application is the root configuration.
type Application struct {
	Server   Server               `koanf:"server"`
	Postgres Postgres             `koanf:"postgres"`
	Logging  logger.LoggingConfig `koanf:"logging"`
	Remote   Remote               `koanf:"remote"`
}

// Load reads application.yaml from dir, then applies DEMO_-prefixed environment
// variables on top so deployment always wins over the committed file.
func Load(dir string) (*Application, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider(filepath.Join(dir, configFileName)), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("loading %s: %w", configFileName, err)
	}

	envProvider := env.Provider(".", env.Opt{
		Prefix: envPrefix,
		TransformFunc: func(key, value string) (string, any) {
			key = strings.ToLower(strings.TrimPrefix(key, envPrefix))
			return strings.ReplaceAll(key, envNested, "."), value
		},
	})
	if err := k.Load(envProvider, nil); err != nil {
		return nil, fmt.Errorf("loading environment variables: %w", err)
	}

	var app Application
	if err := k.UnmarshalWithConf("", &app, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, fmt.Errorf("unmarshalling configuration: %w", err)
	}

	if err := app.validate(); err != nil {
		return nil, err
	}

	return &app, nil
}

// validate rejects the settings that would otherwise fail much later, with a
// far less obvious error.
func (a Application) validate() error {
	switch {
	case a.Postgres.Host == "":
		return fmt.Errorf("postgres.host is required")
	case a.Postgres.Database == "":
		return fmt.Errorf("postgres.database is required")
	case a.Postgres.MaxConns < a.Postgres.MinConns:
		return fmt.Errorf("postgres.max_conns (%d) is lower than postgres.min_conns (%d)",
			a.Postgres.MaxConns, a.Postgres.MinConns)
	case a.replicaPointsAtThePrimary():
		return fmt.Errorf("postgres.replica resolves to the primary (%s), which would route reads back to it",
			a.Postgres.Replica.Host)
	case a.Server.Port <= 0:
		return fmt.Errorf("server.port must be positive, got %d", a.Server.Port)
	case a.Remote.BaseURL == "":
		return fmt.Errorf("remote.base_url is required")
	}
	return nil
}

// replicaPointsAtThePrimary catches the misconfiguration that is invisible at
// runtime: a replica section that inherits so much it ends up describing the
// primary. Reads would silently keep landing on it and the routing would look
// like it works.
func (a Application) replicaPointsAtThePrimary() bool {
	replica := a.Postgres.ReadReplica()
	if replica == nil {
		return false
	}
	return replica.Host == a.Postgres.Host && replica.Port == a.Postgres.Port
}
