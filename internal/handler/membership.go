package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

type membershipService interface {
	AddMember(ctx context.Context, companyID, userID uuid.UUID, role string) (*domain.Membership, error)
	RemoveMember(ctx context.Context, companyID, userID uuid.UUID) error
}

type membershipRequest struct {
	Role string `json:"role"`
}

// MembershipHandler associates users with companies.
//
// Adding a member runs under SERIALIZABLE with a retry, because the seat limit
// is decided from a count that a concurrent insert can invalidate.
type MembershipHandler struct {
	service membershipService
	logger  logger.Logger
}

// NewMembershipHandler creates the membership handler.
func NewMembershipHandler(svc membershipService, log logger.Logger) *MembershipHandler {
	return &MembershipHandler{service: svc, logger: log}
}

func (h *MembershipHandler) Add(c *gin.Context) {
	companyID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	userID, ok := uuidParam(c, "userId")
	if !ok {
		return
	}

	// The body is optional: no role means the default one.
	var req membershipRequest
	if c.Request.ContentLength > 0 && !bindJSON(c, &req) {
		return
	}

	membership, err := h.service.AddMember(c.Request.Context(), companyID, userID, req.Role)
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusCreated, membership)
}

func (h *MembershipHandler) Remove(c *gin.Context) {
	companyID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	userID, ok := uuidParam(c, "userId")
	if !ok {
		return
	}

	if err := h.service.RemoveMember(c.Request.Context(), companyID, userID); err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.Status(http.StatusNoContent)
}
