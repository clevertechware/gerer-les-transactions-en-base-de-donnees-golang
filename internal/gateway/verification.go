// Package gateway holds the clients for the external systems this service talks
// to. There is exactly one, and it is deliberately slow.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/config"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/logger"
)

type verifyRequest struct {
	Company string `json:"company"`
}

type verifyResponse struct {
	Reference string `json:"reference"`
	Status    string `json:"status"`
}

// Verification calls the company verification provider served by cmd/remote.
type Verification struct {
	baseURL string
	client  *http.Client
	logger  logger.Logger
}

// NewVerification creates the gateway.
//
// The timeout is not decoration. Whatever the provider does, this client gives
// up after it — which bounds how long a caller can hold anything, including, on
// the broken path, a row lock.
func NewVerification(cfg config.Remote, log logger.Logger) *Verification {
	return &Verification{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		client:  &http.Client{Timeout: cfg.Timeout},
		logger:  log,
	}
}

// Verify asks the provider to verify a company and returns its reference.
func (g *Verification) Verify(ctx context.Context, companyName string) (string, error) {
	body, err := json.Marshal(verifyRequest{Company: companyName})
	if err != nil {
		return "", fmt.Errorf("encoding verification request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/verify", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("building verification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrVerificationUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Logged on purpose: this duration is exactly how long the broken path holds
	// its lock.
	g.logger.InfoContext(ctx, "provider answered",
		"company", companyName, "status", resp.StatusCode, "duration", time.Since(start))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: provider returned %s", domain.ErrVerificationUnavailable, resp.Status)
	}

	var verified verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&verified); err != nil {
		return "", fmt.Errorf("%w: decoding response: %w", domain.ErrVerificationUnavailable, err)
	}
	if verified.Reference == "" {
		return "", fmt.Errorf("%w: provider returned an empty reference", domain.ErrVerificationUnavailable)
	}

	return verified.Reference, nil
}
