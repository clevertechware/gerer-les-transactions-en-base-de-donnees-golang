package handler

import (
	"context"
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

	h := NewHTTPCompanyHandler(service, logger.NewNoOpLogger())
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

func TestCompanyHandler_Create_RejectsAMalformedBody(t *testing.T) {
	t.Parallel()

	service := mocks.NewCompanyService(t) // must not be called

	h := NewHTTPCompanyHandler(service, logger.NewNoOpLogger())
	c, recorder := newTestContext(t, http.MethodPost, "/api/companies", `{`, nil)

	h.Create(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestCompanyHandler_Get(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	company := &domain.Company{ID: companyID, Name: "Clevertechware"}

	tests := []struct {
		name       string
		id         string
		serviceErr error
		wantStatus int
	}{
		{name: "found", id: companyID.String(), wantStatus: http.StatusOK},
		{name: "not found", id: companyID.String(), serviceErr: domain.ErrCompanyNotFound, wantStatus: http.StatusNotFound},
		{name: "malformed id", id: "not-a-uuid", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewCompanyService(t)
			if tt.id != "not-a-uuid" {
				service.EXPECT().GetCompany(mock.Anything, companyID).Return(company, tt.serviceErr).Once()
			}

			h := NewHTTPCompanyHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodGet, "/api/companies/"+tt.id, "",
				gin.Params{{Key: "id", Value: tt.id}})

			h.Get(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestCompanyHandler_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
	}{
		{name: "success", wantStatus: http.StatusOK},
		{name: "repository failure", serviceErr: assert.AnError, wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewCompanyService(t)
			service.EXPECT().ListCompanies(mock.Anything).
				Return([]domain.Company{{Name: "Clevertechware"}}, tt.serviceErr).Once()

			h := NewHTTPCompanyHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodGet, "/api/companies", "", nil)

			h.List(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestCompanyHandler_Update(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()

	tests := []struct {
		name       string
		id         string
		body       string
		callsSvc   bool
		serviceErr error
		wantStatus int
	}{
		{
			name: "success", id: companyID.String(), body: `{"name": "Clevertechware", "seat_limit": 5}`,
			callsSvc: true, wantStatus: http.StatusOK,
		},
		{
			name: "not found", id: companyID.String(), body: `{"name": "Clevertechware"}`,
			callsSvc: true, serviceErr: domain.ErrCompanyNotFound, wantStatus: http.StatusNotFound,
		},
		{name: "malformed id", id: "not-a-uuid", body: `{"name": "Clevertechware"}`, wantStatus: http.StatusBadRequest},
		{name: "malformed body", id: companyID.String(), body: `{`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewCompanyService(t)
			if tt.callsSvc {
				service.EXPECT().UpdateCompany(mock.Anything, mock.Anything).Return(tt.serviceErr).Once()
			}

			h := NewHTTPCompanyHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPut, "/api/companies/"+tt.id, tt.body,
				gin.Params{{Key: "id", Value: tt.id}})

			h.Update(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestCompanyHandler_Delete(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()

	tests := []struct {
		name       string
		id         string
		callsSvc   bool
		serviceErr error
		wantStatus int
	}{
		{name: "success", id: companyID.String(), callsSvc: true, wantStatus: http.StatusNoContent},
		{
			name: "not found", id: companyID.String(), callsSvc: true,
			serviceErr: domain.ErrCompanyNotFound, wantStatus: http.StatusNotFound,
		},
		{name: "malformed id", id: "not-a-uuid", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewCompanyService(t)
			if tt.callsSvc {
				service.EXPECT().DeleteCompany(mock.Anything, companyID).Return(tt.serviceErr).Once()
			}

			h := NewHTTPCompanyHandler(service, logger.NewNoOpLogger())
			c, _ := newTestContext(t, http.MethodDelete, "/api/companies/"+tt.id, "",
				gin.Params{{Key: "id", Value: tt.id}})

			h.Delete(c)

			// A 204 carries no body, so gin never flushes the header to the
			// recorder; c.Writer.Status() reflects it either way.
			assert.Equal(t, tt.wantStatus, c.Writer.Status())
		})
	}
}
