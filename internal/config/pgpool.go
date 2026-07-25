package config

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func (d Database) NewPGPool(ctx context.Context) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(d.connStr())
	if err != nil {
		return nil, err
	}
	config.MinConns = d.min
	config.MaxConns = d.max

	return pgxpool.NewWithConfig(ctx, config)
}
