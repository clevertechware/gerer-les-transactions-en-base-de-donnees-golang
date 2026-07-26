package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/logger"
)

func TestStatusFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"invalid input", domain.ErrInvalidInput, http.StatusBadRequest},
		{"company not found", domain.ErrCompanyNotFound, http.StatusNotFound},
		{"user not found", domain.ErrUserNotFound, http.StatusNotFound},
		{"membership not found", domain.ErrMembershipNotFound, http.StatusNotFound},
		{"duplicate email", domain.ErrEmailAlreadyExists, http.StatusConflict},
		{"duplicate username", domain.ErrUsernameAlreadyExists, http.StatusConflict},
		{"duplicate membership", domain.ErrMembershipExists, http.StatusConflict},
		{"already verified", domain.ErrVerificationConflict, http.StatusConflict},
		{"seat limit reached", domain.ErrSeatLimitReached, http.StatusUnprocessableEntity},
		{"serialization failure", domain.ErrSerializationFailure, http.StatusServiceUnavailable},
		{"provider down", domain.ErrVerificationUnavailable, http.StatusBadGateway},
		{"anything else", errors.New("boom"), http.StatusInternalServerError},

		// A transaction was required and missing: a programming mistake, not
		// something the caller can fix, so it must not read as a client error.
		{"transaction required", domain.ErrTransactionRequired, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.wantStatus, statusFor(tt.err))

			// Services wrap errors on the way up; the mapping has to survive that.
			wrapped := fmt.Errorf("adding member: %w", tt.err)
			assert.Equal(t, tt.wantStatus, statusFor(wrapped), "wrapping must not change the mapping")
		})
	}
}

// TestRespondError_DoesNotLeakInternalDetails covers the one response the caller
// must never be able to read: a 500 carrying the wrapped chain would hand out
// table names, column names and query fragments.
func TestRespondError_DoesNotLeakInternalDetails(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	internal := fmt.Errorf("selecting company 42: %w",
		errors.New(`ERROR: relation "companies_secret" does not exist (SQLSTATE 42P01)`))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/companies/42", nil)

	respondError(c, logger.NewNoOpLogger(), internal)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, "internal server error", body["error"])
	assert.NotContains(t, recorder.Body.String(), "companies_secret")
	assert.NotContains(t, recorder.Body.String(), "SQLSTATE")
}

// TestRespondError_KeepsDomainMessages: domain errors are written for the
// caller, so those do get through.
func TestRespondError_KeepsDomainMessages(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/companies/1/members/2", nil)

	respondError(c, logger.NewNoOpLogger(), domain.ErrSeatLimitReached)

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, domain.ErrSeatLimitReached.Error(), body["error"])
}
