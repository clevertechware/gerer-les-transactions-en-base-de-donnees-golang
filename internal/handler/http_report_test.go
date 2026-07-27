package handler

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/handler/mocks"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/service"
)

func TestReportHandler_Get(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	report := &service.CompanyReport{Company: domain.Company{ID: companyID}, SeatsLeft: 2}

	tests := []struct {
		name       string
		id         string
		svc        func(t *testing.T) *mocks.ReportService
		wantStatus int
	}{
		{
			name: "found",
			id:   companyID.String(),
			svc: func(t *testing.T) *mocks.ReportService {
				s := mocks.NewReportService(t)
				s.EXPECT().CompanyReport(mock.Anything, companyID).Return(report, nil).Once()
				return s
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found",
			id:   companyID.String(),
			svc: func(t *testing.T) *mocks.ReportService {
				s := mocks.NewReportService(t)
				s.EXPECT().CompanyReport(mock.Anything, companyID).Return(report, domain.ErrCompanyNotFound).Once()
				return s
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "malformed id",
			id:   "not-a-uuid",
			svc: func(t *testing.T) *mocks.ReportService {
				return mocks.NewReportService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := tt.svc(t)

			h := NewHTTPReportHandler(svc, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodGet, "/api/companies/"+tt.id+"/report", "",
				gin.Params{{Key: "id", Value: tt.id}})

			h.get(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}
