// Command server exposes the demo API: CRUD on companies and users, plus the
// endpoints that contrast a correct transaction boundary with a broken one.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/config"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/gateway"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/handler"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/migrate"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/postgres"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/service"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet when configuration fails.
		logger.NewDefault().Error("server exited with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configDir := flag.String("config", ".", "directory containing application.yaml")
	migrationsPath := flag.String("migrations", "migrations", "directory containing the SQL migrations")
	flag.Parse()

	cfg, err := config.Load(*configDir)
	if err != nil {
		return err
	}

	log := logger.New(cfg.Logging)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err = migrate.Up(cfg.Postgres, *migrationsPath); err != nil {
		return err
	}
	log.InfoContext(ctx, "migrations applied")

	pool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()
	log.InfoContext(ctx, "connected to database", "database", cfg.Postgres.Database)
	txManager := postgres.NewTxManager(log, pool)

	companyRepo := postgres.NewCompanyRepository(txManager, log)
	userRepo := postgres.NewUserRepository(txManager, log)
	membershipRepo := postgres.NewMembershipRepository(txManager, log)

	verificationGateway := gateway.NewVerification(cfg.Remote, log)

	companyService := service.NewCompany(companyRepo, log)
	userService := service.NewUser(userRepo, log)
	onboardingService := service.NewOnboarding(txManager, companyRepo, userRepo, membershipRepo, log)
	membershipService := service.NewMembership(txManager, companyRepo, membershipRepo, log)
	reportService := service.NewReport(txManager, companyRepo, userRepo, membershipRepo, log)
	verificationService := service.NewVerification(txManager, companyRepo, verificationGateway, log)

	handlers := handler.HTTPHandlers{
		Company:      handler.NewHTTPCompanyHandler(companyService, log),
		User:         handler.NewHTTPUserHandler(userService, log),
		Membership:   handler.NewHTTPMembershipHandler(membershipService, log),
		Onboarding:   handler.NewHTTPOnboardingHandler(onboardingService, log),
		Verification: handler.NewHTTPVerificationHandler(verificationService, log),
		Report:       handler.NewHTTPReportHandler(reportService, log),
	}

	server := handler.NewHTTPServer(cfg.Server, log, pool, handlers)
	return server.Run(ctx)
}
