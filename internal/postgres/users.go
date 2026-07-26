package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/logger"
)

const userColumns = `id, first_name, last_name, email, username, created_at, updated_at`

// UserRepository reads and writes users.
type UserRepository struct {
	txManager *TxManager
	logger    logger.Logger
}

// NewUserRepository creates a UserRepository.
func NewUserRepository(txManager *TxManager, log logger.Logger) *UserRepository {
	return &UserRepository{txManager: txManager, logger: log}
}

func scanUser(s scanner) (domain.User, error) {
	var u domain.User
	err := s.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Username, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// translateUserConflict turns a unique violation into the matching domain error.
func translateUserConflict(err error) error {
	pgErr, ok := pgError(err)
	if !ok || pgErr.Code != pgerrcode.UniqueViolation {
		return nil
	}

	switch pgErr.ConstraintName {
	case constraintUsersEmail:
		return domain.ErrEmailAlreadyExists
	case constraintUsersUsername:
		return domain.ErrUsernameAlreadyExists
	default:
		return nil
	}
}

// Create inserts a user. A single statement: it needs no transaction.
func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	const query = `
		INSERT INTO users (first_name, last_name, email, username)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + userColumns

	created, err := scanUser(r.txManager.Executor(ctx).
		QueryRow(ctx, query, user.FirstName, user.LastName, user.Email, user.Username))
	if err != nil {
		if conflict := translateUserConflict(err); conflict != nil {
			return conflict
		}
		return fmt.Errorf("inserting user: %w", err)
	}

	*user = created
	return nil
}

// GetByID returns a user, or ErrUserNotFound.
func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	const query = `SELECT ` + userColumns + `
		FROM users WHERE id = $1 AND deleted_at IS NULL`

	user, err := scanUser(r.txManager.Executor(ctx).QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("selecting user %s: %w", id, err)
	}

	return &user, nil
}

// List returns every live user.
func (r *UserRepository) List(ctx context.Context) ([]domain.User, error) {
	const query = `SELECT ` + userColumns + `
		FROM users WHERE deleted_at IS NULL ORDER BY created_at`

	rows, err := r.txManager.Executor(ctx).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("selecting users: %w", err)
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating users: %w", err)
	}

	return users, nil
}

// ListByCompany returns the members of a company, in role then name order.
func (r *UserRepository) ListByCompany(ctx context.Context, companyID uuid.UUID) ([]domain.User, error) {
	const query = `
		SELECT u.id, u.first_name, u.last_name, u.email, u.username, u.created_at, u.updated_at
		FROM users u
		JOIN user_companies uc ON uc.user_id = u.id
		WHERE uc.company_id = $1 AND u.deleted_at IS NULL
		ORDER BY uc.role, u.last_name`

	rows, err := r.txManager.Executor(ctx).Query(ctx, query, companyID)
	if err != nil {
		return nil, fmt.Errorf("selecting members of company %s: %w", companyID, err)
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning member: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating members: %w", err)
	}

	return users, nil
}

// Update changes the mutable fields of a user.
func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	const query = `
		UPDATE users
		SET first_name = $2, last_name = $3, email = $4, username = $5, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING ` + userColumns

	updated, err := scanUser(r.txManager.Executor(ctx).
		QueryRow(ctx, query, user.ID, user.FirstName, user.LastName, user.Email, user.Username))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrUserNotFound
		}
		if conflict := translateUserConflict(err); conflict != nil {
			return conflict
		}
		return fmt.Errorf("updating user %s: %w", user.ID, err)
	}

	*user = updated
	return nil
}

// Delete soft-deletes a user.
func (r *UserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `
		UPDATE users SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.txManager.Executor(ctx).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("deleting user %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}
