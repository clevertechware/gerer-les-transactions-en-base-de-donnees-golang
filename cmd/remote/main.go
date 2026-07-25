// Command remote is a deliberately slow stand-in for a third-party provider —
// a payment gateway, a KYC service, a partner API.
//
// It exists so the demo can hold a database transaction open across a real
// network call, and measure what that costs. Nothing here is clever: it sleeps,
// then answers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/config"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/logger"
)

type verifyRequest struct {
	Company string `json:"company"`
}

type verifyResponse struct {
	Reference string `json:"reference"`
	Status    string `json:"status"`
}

func main() {
	if err := run(); err != nil {
		logger.NewDefault().Error("remote exited with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configDir = flag.String("config", ".", "directory containing application.yaml")
		delay     = flag.Duration("delay", 0, "override how long a verification takes")
	)
	flag.Parse()

	cfg, err := config.Load(*configDir)
	if err != nil {
		return err
	}
	if *delay > 0 {
		cfg.Remote.Delay = *delay
	}

	log := logger.New(cfg.Logging)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /verify", verifyHandler(log, cfg.Remote.Delay))

	server := &http.Server{
		Addr:              cfg.Remote.Addr(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.InfoContext(ctx, "fake provider listening", "addr", server.Addr, "delay", cfg.Remote.Delay)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down fake provider")
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	}
}

// verifyHandler answers after delay, unless the caller gives up first.
func verifyHandler(log logger.Logger, delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var req verifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid payload"}`, http.StatusBadRequest)
			return
		}

		log.InfoContext(ctx, "verification requested", "company", req.Company, "delay", delay)

		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			// The client hung up. In the bad endpoint, this is where a lock has
			// already been held for the full delay for nothing.
			log.WarnContext(ctx, "caller gave up before we answered", "company", req.Company)
			return
		case <-timer.C:
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verifyResponse{
			Reference: "VRF-" + uuid.NewString(),
			Status:    "verified",
		})
	}
}
