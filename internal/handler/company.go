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

// companyRequest is the writable shape of a company. Binding the domain entity
// directly would let a caller set its own id, timestamps, or — worse — declare
// itself verified without ever talking to the provider.
type companyRequest struct {
	Name      string  `json:"name"`
	Address   *string `json:"address"`
	SeatLimit int     `json:"seat_limit"`
}

// CompanyHandler exposes CRUD on companies. None of these routes opens a
// transaction: each is one statement.
type CompanyHandler struct {
	service companyService
	logger  logger.Logger
}

// NewCompanyHandler creates the company handler.
func NewCompanyHandler(service companyService, log logger.Logger) *CompanyHandler {
	return &CompanyHandler{service: service, logger: log}
}

func (h *CompanyHandler) Create(c *gin.Context) {
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

func (h *CompanyHandler) Get(c *gin.Context) {
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

func (h *CompanyHandler) List(c *gin.Context) {
	companies, err := h.service.ListCompanies(c.Request.Context())
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, companies)
}

func (h *CompanyHandler) Update(c *gin.Context) {
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

func (h *CompanyHandler) Delete(c *gin.Context) {
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
