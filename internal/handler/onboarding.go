package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/service"
)

type onboardingService interface {
	Execute(ctx context.Context, input service.OnboardingInput) (*domain.Onboarding, error)
}

type onboardingRequest struct {
	Company companyRequest `json:"company"`
	Owner   userRequest    `json:"owner"`
}

// OnboardingHandler registers a company together with its owner.
//
// The one write endpoint in this demo that genuinely needs a transaction:
// three rows, one invariant.
type OnboardingHandler struct {
	service onboardingService
	logger  logger.Logger
}

// NewOnboardingHandler creates the onboarding handler.
func NewOnboardingHandler(svc onboardingService, log logger.Logger) *OnboardingHandler {
	return &OnboardingHandler{service: svc, logger: log}
}

func (h *OnboardingHandler) Execute(c *gin.Context) {
	var req onboardingRequest
	if !bindJSON(c, &req) {
		return
	}

	input := service.OnboardingInput{
		Company: domain.Company{
			Name:      req.Company.Name,
			Address:   req.Company.Address,
			SeatLimit: req.Company.SeatLimit,
		},
		Owner: req.Owner.toDomain(uuid.Nil),
	}

	result, err := h.service.Execute(c.Request.Context(), input)
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusCreated, result)
}
