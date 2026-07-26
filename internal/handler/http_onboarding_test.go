package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/handler/mocks"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

func TestOnboardingHandler_Execute(t *testing.T) {
	t.Parallel()

	validBody := `{
		"company": {"name": "Clevertechware", "seat_limit": 3},
		"owner": {"first_name": "Ada", "last_name": "Lovelace", "email": "ada@example.com", "username": "ada"}
	}`

	tests := []struct {
		name       string
		body       string
		callsSvc   bool
		serviceErr error
		wantStatus int
	}{
		{name: "success", body: validBody, callsSvc: true, wantStatus: http.StatusCreated},
		{
			name: "owner email already exists", body: validBody, callsSvc: true,
			serviceErr: domain.ErrEmailAlreadyExists, wantStatus: http.StatusConflict,
		},
		{
			name: "seat limit invalid", body: validBody, callsSvc: true,
			serviceErr: domain.ErrInvalidInput, wantStatus: http.StatusBadRequest,
		},
		{name: "malformed body", body: `{`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewOnboardingService(t)
			if tt.callsSvc {
				service.EXPECT().Execute(mock.Anything, mock.Anything).
					Return(&domain.Onboarding{}, tt.serviceErr).Once()
			}

			h := NewHTTPOnboardingHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPost, "/api/onboarding", tt.body, nil)

			h.Execute(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}
