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
		svc        func(t *testing.T) *mocks.OnboardingService
		wantStatus int
	}{
		{
			name: "success", body: validBody,
			svc: func(t *testing.T) *mocks.OnboardingService {
				s := mocks.NewOnboardingService(t)
				s.EXPECT().Execute(mock.Anything, mock.Anything).
					Return(&domain.Onboarding{}, nil).Once()
				return s
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "owner email already exists", body: validBody,
			svc: func(t *testing.T) *mocks.OnboardingService {
				s := mocks.NewOnboardingService(t)
				s.EXPECT().Execute(mock.Anything, mock.Anything).
					Return(&domain.Onboarding{}, domain.ErrEmailAlreadyExists).Once()
				return s
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "seat limit invalid", body: validBody,
			svc: func(t *testing.T) *mocks.OnboardingService {
				s := mocks.NewOnboardingService(t)
				s.EXPECT().Execute(mock.Anything, mock.Anything).
					Return(&domain.Onboarding{}, domain.ErrInvalidInput).Once()
				return s
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "malformed body", body: `{`,
			svc: func(t *testing.T) *mocks.OnboardingService {
				return mocks.NewOnboardingService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

			h := NewHTTPOnboardingHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPost, "/api/onboarding", tt.body, nil)

			h.execute(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}
