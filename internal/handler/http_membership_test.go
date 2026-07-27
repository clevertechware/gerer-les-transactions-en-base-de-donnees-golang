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
		svc        func(t *testing.T) *mocks.MembershipService
		wantStatus int
	}{
		{
			name: "default role when body is empty", companyID: companyID.String(), userID: userID.String(),
			svc: func(t *testing.T) *mocks.MembershipService {
				s := mocks.NewMembershipService(t)
				s.EXPECT().AddMember(mock.Anything, companyID, userID, "").Return(membership, nil).Once()
				return s
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "explicit role", companyID: companyID.String(), userID: userID.String(),
			body: `{"role": "owner"}`,
			svc: func(t *testing.T) *mocks.MembershipService {
				s := mocks.NewMembershipService(t)
				s.EXPECT().AddMember(mock.Anything, companyID, userID, "owner").Return(membership, nil).Once()
				return s
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "seat limit reached", companyID: companyID.String(), userID: userID.String(),
			svc: func(t *testing.T) *mocks.MembershipService {
				s := mocks.NewMembershipService(t)
				s.EXPECT().AddMember(mock.Anything, companyID, userID, "").
					Return(membership, domain.ErrSeatLimitReached).Once()
				return s
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "already a member", companyID: companyID.String(), userID: userID.String(),
			svc: func(t *testing.T) *mocks.MembershipService {
				s := mocks.NewMembershipService(t)
				s.EXPECT().AddMember(mock.Anything, companyID, userID, "").
					Return(membership, domain.ErrMembershipExists).Once()
				return s
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "malformed company id", companyID: "not-a-uuid", userID: userID.String(),
			svc: func(t *testing.T) *mocks.MembershipService {
				return mocks.NewMembershipService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "malformed user id", companyID: companyID.String(), userID: "not-a-uuid",
			svc: func(t *testing.T) *mocks.MembershipService {
				return mocks.NewMembershipService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "malformed body", companyID: companyID.String(), userID: userID.String(),
			body:       `{`,
			svc: func(t *testing.T) *mocks.MembershipService {
				return mocks.NewMembershipService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

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
		svc        func(t *testing.T) *mocks.MembershipService
		wantStatus int
	}{
		{
			name: "success", companyID: companyID.String(), userID: userID.String(),
			svc: func(t *testing.T) *mocks.MembershipService {
				s := mocks.NewMembershipService(t)
				s.EXPECT().RemoveMember(mock.Anything, companyID, userID).Return(nil).Once()
				return s
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "not found", companyID: companyID.String(), userID: userID.String(),
			svc: func(t *testing.T) *mocks.MembershipService {
				s := mocks.NewMembershipService(t)
				s.EXPECT().RemoveMember(mock.Anything, companyID, userID).
					Return(domain.ErrMembershipNotFound).Once()
				return s
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "malformed company id", companyID: "not-a-uuid", userID: userID.String(),
			svc: func(t *testing.T) *mocks.MembershipService {
				return mocks.NewMembershipService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "malformed user id", companyID: companyID.String(), userID: "not-a-uuid",
			svc: func(t *testing.T) *mocks.MembershipService {
				return mocks.NewMembershipService(t)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

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
