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
)

func TestMembershipHandler_Add(t *testing.T) {
	t.Parallel()

	companyID, userID := uuid.New(), uuid.New()
	membership := &domain.Membership{CompanyID: companyID, UserID: userID, Role: domain.RoleMember}

	tests := []struct {
		name       string
		companyID  string
		userID     string
		body       string
		callsSvc   bool
		wantRole   string
		serviceErr error
		wantStatus int
	}{
		{
			name: "default role when body is empty", companyID: companyID.String(), userID: userID.String(),
			callsSvc: true, wantRole: "", wantStatus: http.StatusCreated,
		},
		{
			name: "explicit role", companyID: companyID.String(), userID: userID.String(),
			body: `{"role": "owner"}`, callsSvc: true, wantRole: "owner", wantStatus: http.StatusCreated,
		},
		{
			name: "seat limit reached", companyID: companyID.String(), userID: userID.String(),
			callsSvc: true, serviceErr: domain.ErrSeatLimitReached, wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "already a member", companyID: companyID.String(), userID: userID.String(),
			callsSvc: true, serviceErr: domain.ErrMembershipExists, wantStatus: http.StatusConflict,
		},
		{name: "malformed company id", companyID: "not-a-uuid", userID: userID.String(), wantStatus: http.StatusBadRequest},
		{name: "malformed user id", companyID: companyID.String(), userID: "not-a-uuid", wantStatus: http.StatusBadRequest},
		{
			name: "malformed body", companyID: companyID.String(), userID: userID.String(),
			body: `{`, wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewMembershipService(t)
			if tt.callsSvc {
				service.EXPECT().AddMember(mock.Anything, companyID, userID, tt.wantRole).
					Return(membership, tt.serviceErr).Once()
			}

			h := NewHTTPMembershipHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPut,
				"/api/companies/"+tt.companyID+"/members/"+tt.userID, tt.body,
				gin.Params{{Key: "id", Value: tt.companyID}, {Key: "userId", Value: tt.userID}})

			h.Add(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestMembershipHandler_Remove(t *testing.T) {
	t.Parallel()

	companyID, userID := uuid.New(), uuid.New()

	tests := []struct {
		name       string
		companyID  string
		userID     string
		callsSvc   bool
		serviceErr error
		wantStatus int
	}{
		{
			name: "success", companyID: companyID.String(), userID: userID.String(),
			callsSvc: true, wantStatus: http.StatusNoContent,
		},
		{
			name: "not found", companyID: companyID.String(), userID: userID.String(),
			callsSvc: true, serviceErr: domain.ErrMembershipNotFound, wantStatus: http.StatusNotFound,
		},
		{name: "malformed company id", companyID: "not-a-uuid", userID: userID.String(), wantStatus: http.StatusBadRequest},
		{name: "malformed user id", companyID: companyID.String(), userID: "not-a-uuid", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewMembershipService(t)
			if tt.callsSvc {
				service.EXPECT().RemoveMember(mock.Anything, companyID, userID).Return(tt.serviceErr).Once()
			}

			h := NewHTTPMembershipHandler(service, logger.NewNoOpLogger())
			c, _ := newTestContext(t, http.MethodDelete,
				"/api/companies/"+tt.companyID+"/members/"+tt.userID, "",
				gin.Params{{Key: "id", Value: tt.companyID}, {Key: "userId", Value: tt.userID}})

			h.Remove(c)

			// A 204 carries no body, so gin never flushes the status to the
			// recorder; c.Writer.Status() reflects it either way.
			assert.Equal(t, tt.wantStatus, c.Writer.Status())
		})
	}
}
