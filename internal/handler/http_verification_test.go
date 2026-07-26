package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/handler/mocks"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

func TestVerificationHandler_RoutesToTheRightVariant(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	company := &domain.Company{ID: companyID, Name: "Clevertechware"}

	tests := []struct {
		name        string
		call        func(*HTTPVerificationHandler, *gin.Context)
		expectSetup func(*mocks.VerificationService)
		wantVariant string
	}{
		{
			name: "verify-bad",
			call: func(h *HTTPVerificationHandler, c *gin.Context) { h.Bad(c) },
			expectSetup: func(s *mocks.VerificationService) {
				s.EXPECT().VerifyBad(mock.Anything, companyID).Return(company, nil).Once()
			},
			wantVariant: "bad",
		},
		{
			name: "verify-good",
			call: func(h *HTTPVerificationHandler, c *gin.Context) { h.Good(c) },
			expectSetup: func(s *mocks.VerificationService) {
				s.EXPECT().VerifyGood(mock.Anything, companyID).Return(company, nil).Once()
			},
			wantVariant: "good",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewVerificationService(t)
			tt.expectSetup(service)

			h := NewHTTPVerificationHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPost,
				"/api/companies/"+companyID.String()+"/"+tt.name, "",
				gin.Params{{Key: "id", Value: companyID.String()}})

			tt.call(h, c)

			require.Equal(t, http.StatusOK, recorder.Code)

			var body map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Equal(t, tt.wantVariant, body["variant"])
			assert.Contains(t, body, "duration_ms")
		})
	}
}

func TestVerificationHandler_MapsErrors(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()

	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
	}{
		{"already verified", domain.ErrVerificationConflict, http.StatusConflict},
		{"unknown company", domain.ErrCompanyNotFound, http.StatusNotFound},
		{"provider unreachable", domain.ErrVerificationUnavailable, http.StatusBadGateway},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewVerificationService(t)
			service.EXPECT().VerifyGood(mock.Anything, companyID).Return(nil, tt.serviceErr).Once()

			h := NewHTTPVerificationHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPost, "/verify-good", "",
				gin.Params{{Key: "id", Value: companyID.String()}})

			h.Good(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestVerificationHandler_RejectsAMalformedID(t *testing.T) {
	t.Parallel()

	// No expectations: a bad path parameter must never reach the service.
	service := mocks.NewVerificationService(t)

	h := NewHTTPVerificationHandler(service, logger.NewNoOpLogger())
	c, recorder := newTestContext(t, http.MethodPost, "/verify-good", "",
		gin.Params{{Key: "id", Value: "not-a-uuid"}})

	h.Good(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}
