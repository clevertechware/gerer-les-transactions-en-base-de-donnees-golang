package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

// respondError maps a domain error to a status code and answers.
//
// Anything unrecognised becomes a bare 500: the wrapped error chain is written
// to the log, never to the response body, where it would leak table names and
// query fragments to the caller.
func respondError(c *gin.Context, log logger.Logger, err error) {
	ctx := c.Request.Context()
	status := statusFor(err)

	if status == http.StatusInternalServerError {
		log.ErrorContext(ctx, "request failed", "error", err)
		c.JSON(status, gin.H{"error": "internal server error"})
		return
	}

	log.WarnContext(ctx, "request rejected", "status", status, "error", err)
	c.JSON(status, gin.H{"error": err.Error()})
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		return http.StatusBadRequest

	case errors.Is(err, domain.ErrCompanyNotFound),
		errors.Is(err, domain.ErrUserNotFound),
		errors.Is(err, domain.ErrMembershipNotFound):
		return http.StatusNotFound

	case errors.Is(err, domain.ErrEmailAlreadyExists),
		errors.Is(err, domain.ErrUsernameAlreadyExists),
		errors.Is(err, domain.ErrMembershipExists),
		errors.Is(err, domain.ErrVerificationConflict):
		return http.StatusConflict

	case errors.Is(err, domain.ErrSeatLimitReached):
		return http.StatusUnprocessableEntity

	// The transaction was aborted and replaying it did not help. Transient by
	// nature, so tell the caller to come back rather than claim a hard failure.
	case errors.Is(err, domain.ErrSerializationFailure):
		return http.StatusServiceUnavailable

	case errors.Is(err, domain.ErrVerificationUnavailable):
		return http.StatusBadGateway

	default:
		return http.StatusInternalServerError
	}
}

// uuidParam reads a UUID path parameter, answering 400 when it is malformed.
func uuidParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return uuid.Nil, false
	}
	return id, true
}

// bindJSON decodes the request body, answering 400 when it is unusable.
func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return false
	}
	return true
}
