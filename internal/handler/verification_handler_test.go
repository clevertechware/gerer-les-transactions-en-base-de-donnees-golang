package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// newTestContext builds a gin context with the given path parameters.
func newTestContext(t *testing.T, method, target, body string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	c.Request = httptest.NewRequest(method, target, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params

	return c, recorder
}

func TestVerificationHandler_RoutesToTheRightVariant(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	company := &domain.Company{ID: companyID, Name: "Clevertechware"}

	tests := []struct {
		name        string
		call        func(*VerificationHandler, *gin.Context)
		expectSetup func(*mocks.VerificationService)
		wantVariant string
	}{
		{
			name: "verify-bad",
			call: func(h *VerificationHandler, c *gin.Context) { h.Bad(c) },
			expectSetup: func(s *mocks.VerificationService) {
				s.EXPECT().VerifyBad(mock.Anything, companyID).Return(company, nil).Once()
			},
			wantVariant: "bad",
		},
		{
			name: "verify-good",
			call: func(h *VerificationHandler, c *gin.Context) { h.Good(c) },
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

			h := NewVerificationHandler(service, logger.NewNoOpLogger())
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

			h := NewVerificationHandler(service, logger.NewNoOpLogger())
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

	h := NewVerificationHandler(service, logger.NewNoOpLogger())
	c, recorder := newTestContext(t, http.MethodPost, "/verify-good", "",
		gin.Params{{Key: "id", Value: "not-a-uuid"}})

	h.Good(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

// TestCompanyHandler_IgnoresClientSuppliedVerificationState is why the handler
// binds a request struct instead of the domain entity: otherwise a caller could
// declare itself verified without ever talking to the provider.
func TestCompanyHandler_IgnoresClientSuppliedVerificationState(t *testing.T) {
	t.Parallel()

	service := mocks.NewCompanyService(t)

	var received domain.Company
	service.EXPECT().CreateCompany(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, c *domain.Company) error {
			received = *c
			return nil
		}).Once()

	h := NewCompanyHandler(service, logger.NewNoOpLogger())
	c, recorder := newTestContext(t, http.MethodPost, "/api/companies", `{
		"name": "Sneaky SAS",
		"verification_status": "verified",
		"verification_ref": "VRF-forged",
		"id": "11111111-1111-4111-8111-111111111111"
	}`, nil)

	h.Create(c)

	require.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, "Sneaky SAS", received.Name)
	assert.Equal(t, uuid.Nil, received.ID, "the client must not choose the identifier")
	assert.Empty(t, received.VerificationStatus, "the client must not declare itself verified")
	assert.Nil(t, received.VerificationRef)
}
