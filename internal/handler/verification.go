package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/domain"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/logger"
)

type verificationService interface {
	VerifyBad(ctx context.Context, companyID uuid.UUID) (*domain.Company, error)
	VerifyGood(ctx context.Context, companyID uuid.UUID) (*domain.Company, error)
}

// VerificationHandler exposes the same operation written two ways.
//
// Both call the same slow provider and produce the same row. They differ only in
// where the network call sits relative to BEGIN — and that is the entire lesson.
// Point a load generator at each and watch what happens to the other requests.
type VerificationHandler struct {
	service verificationService
	logger  logger.Logger
}

// NewVerificationHandler creates the verification handler.
func NewVerificationHandler(svc verificationService, log logger.Logger) *VerificationHandler {
	return &VerificationHandler{service: svc, logger: log}
}

// Bad holds a row lock across the provider call. ❌
func (h *VerificationHandler) Bad(c *gin.Context) {
	h.verify(c, "bad", h.service.VerifyBad)
}

// Good calls the provider first, then writes with one conditional statement. ✅
func (h *VerificationHandler) Good(c *gin.Context) {
	h.verify(c, "good", h.service.VerifyGood)
}

func (h *VerificationHandler) verify(
	c *gin.Context,
	variant string,
	verify func(context.Context, uuid.UUID) (*domain.Company, error),
) {
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}

	start := time.Now()
	company, err := verify(c.Request.Context(), id)
	elapsed := time.Since(start)

	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	// Reported so the two variants can be compared without reading the logs.
	// The wall time is nearly identical — the provider dominates both. What
	// differs is how much of it was spent holding a lock.
	c.JSON(http.StatusOK, gin.H{
		"variant":     variant,
		"company":     company,
		"duration_ms": elapsed.Milliseconds(),
	})
}
