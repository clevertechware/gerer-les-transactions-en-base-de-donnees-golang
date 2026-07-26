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

			svc := mocks.NewReportService(t)
			if tt.id != "not-a-uuid" {
				svc.EXPECT().CompanyReport(mock.Anything, companyID).Return(report, tt.serviceErr).Once()
			}

			h := NewHTTPReportHandler(svc, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodGet, "/api/companies/"+tt.id+"/report", "",
				gin.Params{{Key: "id", Value: tt.id}})

			h.Get(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}
