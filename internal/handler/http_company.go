package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

type companyService interface {
	CreateCompany(ctx context.Context, company *domain.Company) error
	GetCompany(ctx context.Context, id uuid.UUID) (*domain.Company, error)
	ListCompanies(ctx context.Context) ([]domain.Company, error)
	UpdateCompany(ctx context.Context, company *domain.Company) error
	DeleteCompany(ctx context.Context, id uuid.UUID) error
}

type companyRequest struct {
	Name      string  `json:"name"`
	Address   *string `json:"address"`
	SeatLimit int     `json:"seat_limit"`
}

// HTTPCompanyHandler exposes CRUD on companies. None of these routes opens a
// transaction: each is one statement.
type HTTPCompanyHandler struct {
	service companyService
	logger  logger.Logger
}

// NewHTTPCompanyHandler creates the company handler.
func NewHTTPCompanyHandler(service companyService, log logger.Logger) *HTTPCompanyHandler {
	return &HTTPCompanyHandler{service: service, logger: log}
}

func (h *HTTPCompanyHandler) Create(c *gin.Context) {
	var req companyRequest
	if !bindJSON(c, &req) {
		return
	}

	company := domain.Company{Name: req.Name, Address: req.Address, SeatLimit: req.SeatLimit}
	if err := h.service.CreateCompany(c.Request.Context(), &company); err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusCreated, company)
}

func (h *HTTPCompanyHandler) Get(c *gin.Context) {
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}

	company, err := h.service.GetCompany(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, company)
}

func (h *HTTPCompanyHandler) List(c *gin.Context) {
	companies, err := h.service.ListCompanies(c.Request.Context())
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, companies)
}

func (h *HTTPCompanyHandler) Update(c *gin.Context) {
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}

	var req companyRequest
	if !bindJSON(c, &req) {
		return
	}

	company := domain.Company{ID: id, Name: req.Name, Address: req.Address, SeatLimit: req.SeatLimit}
	if err := h.service.UpdateCompany(c.Request.Context(), &company); err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, company)
}

func (h *HTTPCompanyHandler) Delete(c *gin.Context) {
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}

	if err := h.service.DeleteCompany(c.Request.Context(), id); err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.Status(http.StatusNoContent)
}
