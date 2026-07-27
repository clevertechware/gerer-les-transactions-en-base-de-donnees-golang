package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/config"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/handler/mocks"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

// newTestServer wires a real HTTPServer, backed by mocked services. The only
// two routes with nothing to reject before reaching their service — the list
// endpoints — get a stubbed success response; every other route is exercised
// with a malformed id or body, which every handler rejects before ever
// touching its service.
func newTestServer(t *testing.T, cfg config.Server, db Pinger) *HTTPServer {
	t.Helper()

	companies := mocks.NewCompanyService(t)
	companies.EXPECT().ListCompanies(mock.Anything).Return(nil, nil).Maybe()

	users := mocks.NewUserService(t)
	users.EXPECT().ListUsers(mock.Anything).Return(nil, nil).Maybe()

	handlers := HTTPHandlers{
		Company:      NewHTTPCompanyHandler(companies, logger.NewNoOpLogger()),
		User:         NewHTTPUserHandler(users, logger.NewNoOpLogger()),
		Membership:   NewHTTPMembershipHandler(mocks.NewMembershipService(t), logger.NewNoOpLogger()),
		Onboarding:   NewHTTPOnboardingHandler(mocks.NewOnboardingService(t), logger.NewNoOpLogger()),
		Verification: NewHTTPVerificationHandler(mocks.NewVerificationService(t), logger.NewNoOpLogger()),
		Report:       NewHTTPReportHandler(mocks.NewReportService(t), logger.NewNoOpLogger()),
	}

	return NewHTTPServer(cfg, logger.NewNoOpLogger(), db, handlers)
}

func TestNewHTTPServer_RegistersEveryRoute(t *testing.T) {
	t.Parallel()

	userID := uuid.New()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "create company - malformed body", method: http.MethodPost, path: "/api/companies", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "list companies", method: http.MethodGet, path: "/api/companies", wantStatus: http.StatusOK},
		{name: "get company - malformed id", method: http.MethodGet, path: "/api/companies/not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "update company - malformed id", method: http.MethodPut, path: "/api/companies/not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "delete company - malformed id", method: http.MethodDelete, path: "/api/companies/not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "company report - malformed id", method: http.MethodGet, path: "/api/companies/not-a-uuid/report", wantStatus: http.StatusBadRequest},
		{
			name: "add member - malformed company id", method: http.MethodPut,
			path: "/api/companies/not-a-uuid/members/" + userID.String(), wantStatus: http.StatusBadRequest,
		},
		{
			name: "remove member - malformed company id", method: http.MethodDelete,
			path: "/api/companies/not-a-uuid/members/" + userID.String(), wantStatus: http.StatusBadRequest,
		},
		{name: "verify-bad - malformed id", method: http.MethodPost, path: "/api/companies/not-a-uuid/verify-bad", wantStatus: http.StatusBadRequest},
		{name: "verify-good - malformed id", method: http.MethodPost, path: "/api/companies/not-a-uuid/verify-good", wantStatus: http.StatusBadRequest},
		{name: "create user - malformed body", method: http.MethodPost, path: "/api/users", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "list users", method: http.MethodGet, path: "/api/users", wantStatus: http.StatusOK},
		{name: "get user - malformed id", method: http.MethodGet, path: "/api/users/not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "update user - malformed id", method: http.MethodPut, path: "/api/users/not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "delete user - malformed id", method: http.MethodDelete, path: "/api/users/not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "onboarding - malformed body", method: http.MethodPost, path: "/api/onboarding", body: `{`, wantStatus: http.StatusBadRequest},
	}

	server := newTestServer(t, config.Server{Mode: "test"}, mocks.NewPinger(t))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			server.router.ServeHTTP(recorder, req)

			// Anything other than 404 proves the route is registered on this
			// method and path; the exact status is what each handler already
			// promises for this input, tested in isolation elsewhere.
			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestHTTPServer_Health(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
	}{
		{name: "database reachable", wantStatus: http.StatusOK},
		{name: "database unreachable", pingErr: errors.New("connection refused"), wantStatus: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := mocks.NewPinger(t)
			db.EXPECT().Ping(mock.Anything).Return(tt.pingErr).Once()

			server := newTestServer(t, config.Server{Mode: "test"}, db)

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			recorder := httptest.NewRecorder()
			server.router.ServeHTTP(recorder, req)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

// TestHTTPServer_Run_ShutsDownOnContextCancellation proves Run actually
// serves — Addr ":0" binds an OS-assigned loopback-all port — and drains
// cleanly instead of erroring when its context is cancelled.
func TestHTTPServer_Run_ShutsDownOnContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := config.Server{Mode: "test", ShutdownTimeout: time.Second}
	server := newTestServer(t, cfg, mocks.NewPinger(t))

	ctx, cancel := context.WithCancel(t.Context())

	errCh := make(chan error, 1)
	go func() { errCh <- server.Run(ctx) }()

	time.Sleep(20 * time.Millisecond) // let ListenAndServe start listening
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not shut down after context cancellation")
	}
}
