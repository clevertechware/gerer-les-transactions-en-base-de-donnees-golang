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
		svc         func(t *testing.T) *mocks.VerificationService
		wantVariant string
	}{
		{
			name: "verify-bad",
			call: func(h *HTTPVerificationHandler, c *gin.Context) { h.bad(c) },
			svc: func(t *testing.T) *mocks.VerificationService {
				s := mocks.NewVerificationService(t)
				s.EXPECT().VerifyBad(mock.Anything, companyID).Return(company, nil).Once()
				return s
			},
			wantVariant: "bad",
		},
		{
			name: "verify-good",
			call: func(h *HTTPVerificationHandler, c *gin.Context) { h.good(c) },
			svc: func(t *testing.T) *mocks.VerificationService {
				s := mocks.NewVerificationService(t)
				s.EXPECT().VerifyGood(mock.Anything, companyID).Return(company, nil).Once()
				return s
			},
			wantVariant: "good",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

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
		svc        func(t *testing.T) *mocks.VerificationService
		wantStatus int
	}{
		{
			name: "already verified",
			svc: func(t *testing.T) *mocks.VerificationService {
				s := mocks.NewVerificationService(t)
				s.EXPECT().VerifyGood(mock.Anything, companyID).Return(nil, domain.ErrVerificationConflict).Once()
				return s
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "unknown company",
			svc: func(t *testing.T) *mocks.VerificationService {
				s := mocks.NewVerificationService(t)
				s.EXPECT().VerifyGood(mock.Anything, companyID).Return(nil, domain.ErrCompanyNotFound).Once()
				return s
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "provider unreachable",
			svc: func(t *testing.T) *mocks.VerificationService {
				s := mocks.NewVerificationService(t)
				s.EXPECT().VerifyGood(mock.Anything, companyID).Return(nil, domain.ErrVerificationUnavailable).Once()
				return s
			},
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

			h := NewHTTPVerificationHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPost, "/verify-good", "",
				gin.Params{{Key: "id", Value: companyID.String()}})

			h.good(c)

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

	h.good(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}
