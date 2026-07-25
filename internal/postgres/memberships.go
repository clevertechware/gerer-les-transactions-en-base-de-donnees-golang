package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/domain"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/logger"
)

// MembershipRepository reads and writes the association between users and
// companies.
type MembershipRepository struct {
	txManager *TxManager
	logger    logger.Logger
}

// NewMembershipRepository creates a MembershipRepository.
func NewMembershipRepository(txManager *TxManager, log logger.Logger) *MembershipRepository {
	return &MembershipRepository{txManager: txManager, logger: log}
}

// Add associates a user with a company.
//
// The primary key and the two foreign keys do the checking here — the
// application never has to ask "does this user exist?" before inserting.
func (r *MembershipRepository) Add(ctx context.Context, membership *domain.Membership) error {
	const query = `
		INSERT INTO user_companies (user_id, company_id, role)
		VALUES ($1, $2, $3)`

	_, err := r.txManager.Executor(ctx).
		Exec(ctx, query, membership.UserID, membership.CompanyID, membership.Role)
	if err != nil {
		pgErr, ok := pgError(err)
		if !ok {
			return fmt.Errorf("inserting membership: %w", err)
		}

		switch {
		case pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == constraintMembershipPK:
			return domain.ErrMembershipExists
		case pgErr.Code == pgerrcode.ForeignKeyViolation && pgErr.ConstraintName == constraintMembershipUserFK:
			return domain.ErrUserNotFound
		case pgErr.Code == pgerrcode.ForeignKeyViolation && pgErr.ConstraintName == constraintMembershipCoFK:
			return domain.ErrCompanyNotFound
		default:
			return fmt.Errorf("inserting membership: %w", err)
		}
	}

	return nil
}

// Remove dissociates a user from a company.
func (r *MembershipRepository) Remove(ctx context.Context, companyID, userID uuid.UUID) error {
	const query = `DELETE FROM user_companies WHERE company_id = $1 AND user_id = $2`

	tag, err := r.txManager.Executor(ctx).Exec(ctx, query, companyID, userID)
	if err != nil {
		return fmt.Errorf("deleting membership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMembershipNotFound
	}

	return nil
}

// CountByCompany returns how many members a company has.
//
// Read on its own it is harmless. Combined with a subsequent insert to enforce
// the seat limit, it becomes the classic read-then-write anomaly: two concurrent
// callers both read "one seat left" and both take it. That is why the service
// runs the pair under SERIALIZABLE.
func (r *MembershipRepository) CountByCompany(ctx context.Context, companyID uuid.UUID) (int, error) {
	const query = `SELECT count(*) FROM user_companies WHERE company_id = $1`

	var count int
	if err := r.txManager.Executor(ctx).QueryRow(ctx, query, companyID).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting members of company %s: %w", companyID, err)
	}

	return count, nil
}

// ListByCompany returns the memberships of a company.
func (r *MembershipRepository) ListByCompany(ctx context.Context, companyID uuid.UUID) ([]domain.Membership, error) {
	const query = `
		SELECT user_id, company_id, role
		FROM user_companies WHERE company_id = $1 ORDER BY role`

	rows, err := r.txManager.Executor(ctx).Query(ctx, query, companyID)
	if err != nil {
		return nil, fmt.Errorf("selecting memberships of company %s: %w", companyID, err)
	}
	defer rows.Close()

	memberships := make([]domain.Membership, 0)
	for rows.Next() {
		var m domain.Membership
		if err := rows.Scan(&m.UserID, &m.CompanyID, &m.Role); err != nil {
			return nil, fmt.Errorf("scanning membership: %w", err)
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating memberships: %w", err)
	}

	return memberships, nil
}
