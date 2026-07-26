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

func TestUserHandler_Create(t *testing.T) {
	t.Parallel()

	validBody := `{"first_name": "Ada", "last_name": "Lovelace", "email": "ada@example.com", "username": "ada"}`

	tests := []struct {
		name       string
		body       string
		callsSvc   bool
		serviceErr error
		wantStatus int
	}{
		{name: "success", body: validBody, callsSvc: true, wantStatus: http.StatusCreated},
		{
			name: "duplicate email", body: validBody, callsSvc: true,
			serviceErr: domain.ErrEmailAlreadyExists, wantStatus: http.StatusConflict,
		},
		{name: "malformed body", body: `{`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewUserService(t)
			if tt.callsSvc {
				service.EXPECT().CreateUser(mock.Anything, mock.Anything).Return(tt.serviceErr).Once()
			}

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPost, "/api/users", tt.body, nil)

			h.Create(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestUserHandler_Get(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	user := &domain.User{ID: userID, Username: "ada"}

	tests := []struct {
		name       string
		id         string
		serviceErr error
		wantStatus int
	}{
		{name: "found", id: userID.String(), wantStatus: http.StatusOK},
		{name: "not found", id: userID.String(), serviceErr: domain.ErrUserNotFound, wantStatus: http.StatusNotFound},
		{name: "malformed id", id: "not-a-uuid", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewUserService(t)
			if tt.id != "not-a-uuid" {
				service.EXPECT().GetUser(mock.Anything, userID).Return(user, tt.serviceErr).Once()
			}

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodGet, "/api/users/"+tt.id, "",
				gin.Params{{Key: "id", Value: tt.id}})

			h.Get(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestUserHandler_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
	}{
		{name: "success", wantStatus: http.StatusOK},
		{name: "repository failure", serviceErr: assert.AnError, wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewUserService(t)
			service.EXPECT().ListUsers(mock.Anything).Return([]domain.User{{Username: "ada"}}, tt.serviceErr).Once()

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodGet, "/api/users", "", nil)

			h.List(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestUserHandler_Update(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	validBody := `{"first_name": "Ada", "last_name": "Lovelace", "email": "ada@example.com", "username": "ada"}`

	tests := []struct {
		name       string
		id         string
		body       string
		callsSvc   bool
		serviceErr error
		wantStatus int
	}{
		{name: "success", id: userID.String(), body: validBody, callsSvc: true, wantStatus: http.StatusOK},
		{
			name: "not found", id: userID.String(), body: validBody, callsSvc: true,
			serviceErr: domain.ErrUserNotFound, wantStatus: http.StatusNotFound,
		},
		{name: "malformed id", id: "not-a-uuid", body: validBody, wantStatus: http.StatusBadRequest},
		{name: "malformed body", id: userID.String(), body: `{`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewUserService(t)
			if tt.callsSvc {
				service.EXPECT().UpdateUser(mock.Anything, mock.Anything).Return(tt.serviceErr).Once()
			}

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPut, "/api/users/"+tt.id, tt.body,
				gin.Params{{Key: "id", Value: tt.id}})

			h.Update(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestUserHandler_Delete(t *testing.T) {
	t.Parallel()

	userID := uuid.New()

	tests := []struct {
		name       string
		id         string
		callsSvc   bool
		serviceErr error
		wantStatus int
	}{
		{name: "success", id: userID.String(), callsSvc: true, wantStatus: http.StatusNoContent},
		{
			name: "not found", id: userID.String(), callsSvc: true,
			serviceErr: domain.ErrUserNotFound, wantStatus: http.StatusNotFound,
		},
		{name: "malformed id", id: "not-a-uuid", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := mocks.NewUserService(t)
			if tt.callsSvc {
				service.EXPECT().DeleteUser(mock.Anything, userID).Return(tt.serviceErr).Once()
			}

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, _ := newTestContext(t, http.MethodDelete, "/api/users/"+tt.id, "",
				gin.Params{{Key: "id", Value: tt.id}})

			h.Delete(c)

			// A 204 carries no body, so gin never flushes the status to the
			// recorder; c.Writer.Status() reflects it either way.
			assert.Equal(t, tt.wantStatus, c.Writer.Status())
		})
	}
}
