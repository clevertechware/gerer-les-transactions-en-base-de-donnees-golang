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

func TestCompanyHandler_Create(t *testing.T) {
	t.Parallel()

	var received domain.Company

	tests := []struct {
		name       string
		body       string
		svc        func(t *testing.T) *mocks.CompanyService
		assertions func(t *testing.T, received *domain.Company)
		wantStatus int
	}{
		{
			name: "ignores client-supplied verification state",
			body: `{
				"name": "Sneaky SAS",
				"verification_status": "verified",
				"verification_ref": "VRF-forged",
				"id": "11111111-1111-4111-8111-111111111111"
			}`,
			svc: func(t *testing.T) *mocks.CompanyService {
				s := mocks.NewCompanyService(t)
				s.EXPECT().CreateCompany(mock.Anything, mock.Anything).
					RunAndReturn(func(_ context.Context, c *domain.Company) error {
						received = *c
						return nil
					}).Once()
				return s
			},
			assertions: func(t *testing.T, received *domain.Company) {
				assert.Equal(t, "Sneaky SAS", received.Name)
				assert.Equal(t, uuid.Nil, received.ID, "the client must not choose the identifier")
				assert.Empty(t, received.VerificationStatus, "the client must not declare itself verified")
				assert.Nil(t, received.VerificationRef)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "rejects malformed body",
			body: `{`,
			svc: func(t *testing.T) *mocks.CompanyService {
				return mocks.NewCompanyService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

			h := NewHTTPCompanyHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPost, "/api/companies", tt.body, nil)

			h.Create(c)

			require.Equal(t, tt.wantStatus, recorder.Code)
			if tt.assertions != nil {
				tt.assertions(t, &received)
			}
		})
	}
}

func TestCompanyHandler_Get(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	company := &domain.Company{ID: companyID, Name: "Clevertechware"}

	tests := []struct {
		name       string
		id         string
		svc        func(t *testing.T) *mocks.CompanyService
		wantStatus int
	}{
		{
			name: "found", id: companyID.String(),
			svc: func(t *testing.T) *mocks.CompanyService {
				s := mocks.NewCompanyService(t)
				s.EXPECT().GetCompany(mock.Anything, companyID).Return(company, nil).Once()
				return s
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found", id: companyID.String(),
			svc: func(t *testing.T) *mocks.CompanyService {
				s := mocks.NewCompanyService(t)
				s.EXPECT().GetCompany(mock.Anything, companyID).Return(company, domain.ErrCompanyNotFound).Once()
				return s
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "malformed id", id: "not-a-uuid",
			svc: func(t *testing.T) *mocks.CompanyService {
				return mocks.NewCompanyService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

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
		svc        func(t *testing.T) *mocks.CompanyService
		wantStatus int
	}{
		{
			name: "success",
			svc: func(t *testing.T) *mocks.CompanyService {
				s := mocks.NewCompanyService(t)
				s.EXPECT().ListCompanies(mock.Anything).
					Return([]domain.Company{{Name: "Clevertechware"}}, nil).Once()
				return s
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "repository failure",
			svc: func(t *testing.T) *mocks.CompanyService {
				s := mocks.NewCompanyService(t)
				s.EXPECT().ListCompanies(mock.Anything).Return(nil, assert.AnError).Once()
				return s
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

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
		svc        func(t *testing.T) *mocks.CompanyService
		wantStatus int
	}{
		{
			name: "success", id: companyID.String(), body: `{"name": "Clevertechware", "seat_limit": 5}`,
			svc: func(t *testing.T) *mocks.CompanyService {
				s := mocks.NewCompanyService(t)
				s.EXPECT().UpdateCompany(mock.Anything, mock.Anything).Return(nil).Once()
				return s
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found", id: companyID.String(), body: `{"name": "Clevertechware"}`,
			svc: func(t *testing.T) *mocks.CompanyService {
				s := mocks.NewCompanyService(t)
				s.EXPECT().UpdateCompany(mock.Anything, mock.Anything).Return(domain.ErrCompanyNotFound).Once()
				return s
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "malformed id", id: "not-a-uuid", body: `{"name": "Clevertechware"}`,
			svc: func(t *testing.T) *mocks.CompanyService {
				return mocks.NewCompanyService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "malformed body", id: companyID.String(), body: `{`,
			svc: func(t *testing.T) *mocks.CompanyService {
				return mocks.NewCompanyService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

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
		svc        func(t *testing.T) *mocks.CompanyService
		wantStatus int
	}{
		{
			name: "success", id: companyID.String(),
			svc: func(t *testing.T) *mocks.CompanyService {
				s := mocks.NewCompanyService(t)
				s.EXPECT().DeleteCompany(mock.Anything, companyID).Return(nil).Once()
				return s
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "not found", id: companyID.String(),
			svc: func(t *testing.T) *mocks.CompanyService {
				s := mocks.NewCompanyService(t)
				s.EXPECT().DeleteCompany(mock.Anything, companyID).Return(domain.ErrCompanyNotFound).Once()
				return s
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "malformed id", id: "not-a-uuid",
			svc: func(t *testing.T) *mocks.CompanyService {
				return mocks.NewCompanyService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

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
