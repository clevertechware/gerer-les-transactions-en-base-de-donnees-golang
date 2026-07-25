package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/domain"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/logger"
)

type userService interface {
	CreateUser(ctx context.Context, user *domain.User) error
	GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error)
	ListUsers(ctx context.Context) ([]domain.User, error)
	UpdateUser(ctx context.Context, user *domain.User) error
	DeleteUser(ctx context.Context, id uuid.UUID) error
}

// userRequest is the writable shape of a user.
type userRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Username  string `json:"username"`
}

func (r userRequest) toDomain(id uuid.UUID) domain.User {
	return domain.User{
		ID:        id,
		FirstName: r.FirstName,
		LastName:  r.LastName,
		Email:     r.Email,
		Username:  r.Username,
	}
}

// UserHandler exposes CRUD on users. Like companies, no transaction anywhere.
type UserHandler struct {
	service userService
	logger  logger.Logger
}

// NewUserHandler creates the user handler.
func NewUserHandler(service userService, log logger.Logger) *UserHandler {
	return &UserHandler{service: service, logger: log}
}

func (h *UserHandler) Create(c *gin.Context) {
	var req userRequest
	if !bindJSON(c, &req) {
		return
	}

	user := req.toDomain(uuid.Nil)
	if err := h.service.CreateUser(c.Request.Context(), &user); err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusCreated, user)
}

func (h *UserHandler) Get(c *gin.Context) {
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, user)
}

func (h *UserHandler) List(c *gin.Context) {
	users, err := h.service.ListUsers(c.Request.Context())
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, users)
}

func (h *UserHandler) Update(c *gin.Context) {
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}

	var req userRequest
	if !bindJSON(c, &req) {
		return
	}

	user := req.toDomain(id)
	if err := h.service.UpdateUser(c.Request.Context(), &user); err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, user)
}

func (h *UserHandler) Delete(c *gin.Context) {
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}

	if err := h.service.DeleteUser(c.Request.Context(), id); err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.Status(http.StatusNoContent)
}
