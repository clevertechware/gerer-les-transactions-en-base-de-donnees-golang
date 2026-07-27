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
		svc        func(t *testing.T) *mocks.UserService
		wantStatus int
	}{
		{
			name: "success", body: validBody,
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().CreateUser(mock.Anything, mock.Anything).Return(nil).Once()
				return service
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "duplicate email", body: validBody,
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().CreateUser(mock.Anything, mock.Anything).
					Return(domain.ErrEmailAlreadyExists).Once()
				return service
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "malformed body", body: `{`,
			svc:        func(t *testing.T) *mocks.UserService { return mocks.NewUserService(t) },
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPost, "/api/users", tt.body, nil)

			h.create(c)

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
		svc        func(t *testing.T) *mocks.UserService
		wantStatus int
	}{
		{
			name: "found", id: userID.String(),
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().GetUser(mock.Anything, userID).Return(user, nil).Once()
				return service
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found", id: userID.String(),
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().GetUser(mock.Anything, userID).Return(user, domain.ErrUserNotFound).Once()
				return service
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "malformed id", id: "not-a-uuid",
			svc:        func(t *testing.T) *mocks.UserService { return mocks.NewUserService(t) },
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodGet, "/api/users/"+tt.id, "",
				gin.Params{{Key: "id", Value: tt.id}})

			h.get(c)

			assert.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestUserHandler_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		svc        func(t *testing.T) *mocks.UserService
		wantStatus int
	}{
		{
			name: "success",
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().ListUsers(mock.Anything).Return([]domain.User{{Username: "ada"}}, nil).Once()
				return service
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "repository failure",
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().ListUsers(mock.Anything).
					Return([]domain.User{{Username: "ada"}}, assert.AnError).Once()
				return service
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodGet, "/api/users", "", nil)

			h.list(c)

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
		svc        func(t *testing.T) *mocks.UserService
		wantStatus int
	}{
		{
			name: "success", id: userID.String(), body: validBody,
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().UpdateUser(mock.Anything, mock.Anything).Return(nil).Once()
				return service
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found", id: userID.String(), body: validBody,
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().UpdateUser(mock.Anything, mock.Anything).Return(domain.ErrUserNotFound).Once()
				return service
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "malformed id", id: "not-a-uuid", body: validBody,
			svc:        func(t *testing.T) *mocks.UserService { return mocks.NewUserService(t) },
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "malformed body", id: userID.String(), body: `{`,
			svc:        func(t *testing.T) *mocks.UserService { return mocks.NewUserService(t) },
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, recorder := newTestContext(t, http.MethodPut, "/api/users/"+tt.id, tt.body,
				gin.Params{{Key: "id", Value: tt.id}})

			h.update(c)

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
		svc        func(t *testing.T) *mocks.UserService
		wantStatus int
	}{
		{
			name: "success", id: userID.String(),
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().DeleteUser(mock.Anything, userID).Return(nil).Once()
				return service
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "not found", id: userID.String(),
			svc: func(t *testing.T) *mocks.UserService {
				service := mocks.NewUserService(t)
				service.EXPECT().DeleteUser(mock.Anything, userID).Return(domain.ErrUserNotFound).Once()
				return service
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "malformed id", id: "not-a-uuid",
			svc:        func(t *testing.T) *mocks.UserService { return mocks.NewUserService(t) },
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := tt.svc(t)

			h := NewHTTPUserHandler(service, logger.NewNoOpLogger())
			c, _ := newTestContext(t, http.MethodDelete, "/api/users/"+tt.id, "",
				gin.Params{{Key: "id", Value: tt.id}})

			h.delete(c)

			// A 204 carries no body, so gin never flushes the status to the
			// recorder; c.Writer.Status() reflects it either way.
			assert.Equal(t, tt.wantStatus, c.Writer.Status())
		})
	}
}
