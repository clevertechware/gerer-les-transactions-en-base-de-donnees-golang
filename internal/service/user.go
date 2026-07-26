package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

// User handles plain CRUD on users. Like Company, it opens no transaction: every method is a single statement.
type User struct {
	users  userRepository
	logger logger.Logger
}

// NewUser creates the user service.
func NewUser(users userRepository, log logger.Logger) *User {
	return &User{users: users, logger: log}
}

// CreateUser inserts a user. Single INSERT, no transaction.
//
// The uniqueness of the email and the username are not checked here with a SELECT first:
// that would be a read-then-write race, and the unique indexes already decide.
//
// The repository turns the violation into a domain error.
func (s *User) CreateUser(ctx context.Context, user *domain.User) error {
	if err := validateUser(user); err != nil {
		return err
	}

	if err := s.users.Create(ctx, user); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "user created", "user_id", user.ID, "email", user.Email)
	return nil
}

// GetUser returns a user. Single SELECT, no transaction.
func (s *User) GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.users.GetByID(ctx, id)
}

// ListUsers returns every user. Single SELECT, no transaction.
func (s *User) ListUsers(ctx context.Context) ([]domain.User, error) {
	return s.users.List(ctx)
}

// UpdateUser changes a user. Single UPDATE, no transaction.
func (s *User) UpdateUser(ctx context.Context, user *domain.User) error {
	if err := validateUser(user); err != nil {
		return err
	}

	if err := s.users.Update(ctx, user); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "user updated", "user_id", user.ID)
	return nil
}

// DeleteUser soft-deletes a user. Single UPDATE, no transaction.
func (s *User) DeleteUser(ctx context.Context, id uuid.UUID) error {
	if err := s.users.Delete(ctx, id); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "user deleted", "user_id", id)
	return nil
}

func validateUser(user *domain.User) error {
	switch {
	case strings.TrimSpace(user.FirstName) == "",
		strings.TrimSpace(user.LastName) == "",
		strings.TrimSpace(user.Username) == "",
		!strings.Contains(user.Email, "@"):
		return domain.ErrInvalidInput
	}
	return nil
}
