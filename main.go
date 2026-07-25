package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/config"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/postgres"
)

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dbConfig := config.DefaultDatabase()
	dbPool, err := dbConfig.NewPGPool(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "Unable to create connection pool", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	err = postgres.RunMigrations(dbConfig, "migrations")
	if err != nil {
		logger.ErrorContext(ctx, "Unable to run migrations", "error", err)
		os.Exit(1)
	}
	logger.InfoContext(ctx, "Migrations ran successfully")

}
