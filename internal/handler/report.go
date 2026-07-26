package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/service"
)

type reportService interface {
	CompanyReport(ctx context.Context, companyID uuid.UUID) (*service.CompanyReport, error)
}

// ReportHandler serves the read-only view of a company.
//
// The one read endpoint that opens a transaction, because it runs three queries
// that have to agree with each other.
type ReportHandler struct {
	service reportService
	logger  logger.Logger
}

// NewReportHandler creates the report handler.
func NewReportHandler(svc reportService, log logger.Logger) *ReportHandler {
	return &ReportHandler{service: svc, logger: log}
}

func (h *ReportHandler) Get(c *gin.Context) {
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}

	report, err := h.service.CompanyReport(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, report)
}
