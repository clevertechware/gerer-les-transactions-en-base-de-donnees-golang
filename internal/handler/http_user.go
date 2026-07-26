package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
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

// HTTPUserHandler exposes CRUD on users. Like companies, no transaction anywhere.
type HTTPUserHandler struct {
	service userService
	logger  logger.Logger
}

// NewHTTPUserHandler creates the user handler.
func NewHTTPUserHandler(service userService, log logger.Logger) *HTTPUserHandler {
	return &HTTPUserHandler{service: service, logger: log}
}

func (h *HTTPUserHandler) Create(c *gin.Context) {
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

func (h *HTTPUserHandler) Get(c *gin.Context) {
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

func (h *HTTPUserHandler) List(c *gin.Context) {
	users, err := h.service.ListUsers(c.Request.Context())
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, users)
}

func (h *HTTPUserHandler) Update(c *gin.Context) {
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

func (h *HTTPUserHandler) Delete(c *gin.Context) {
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
