package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/pkg/transaction"
)

// Membership associates users with companies.
type Membership struct {
	txManager   transaction.Manager
	companies   companyRepository
	memberships membershipRepository
	logger      logger.Logger
}

// NewMembership creates the membership service.
func NewMembership(
	txManager transaction.Manager,
	companies companyRepository,
	memberships membershipRepository,
	log logger.Logger,
) *Membership {
	return &Membership{
		txManager:   txManager,
		companies:   companies,
		memberships: memberships,
		logger:      log,
	}
}

// AddMember adds a user to a company, refusing to exceed its seat limit.
//
// The invariant here is not enforceable by a constraint: it compares a count
// against a column on another table. So the application has to defend it, and
// the shape it takes — read a count, decide, then write — is exactly what breaks
// under concurrency. Two callers on the last free seat both read "one left" and
// both take it.
//
// READ COMMITTED would not catch that: each statement gets a fresh snapshot and
// neither transaction sees the other's uncommitted insert. SERIALIZABLE does,
// and pays for it by aborting one of them with SQLSTATE 40001 — which is not a
// bug but the contract, and why this runs through ExecuteSerializable.
func (s *Membership) AddMember(
	ctx context.Context,
	companyID, userID uuid.UUID,
	role string,
) (*domain.Membership, error) {
	if role == "" {
		role = domain.RoleMember
	}

	membership := domain.Membership{CompanyID: companyID, UserID: userID, Role: role}

	err := s.txManager.ExecuteSerializable(ctx, func(ctx context.Context) error {
		company, err := s.companies.GetByID(ctx, companyID)
		if err != nil {
			return err
		}

		count, err := s.memberships.CountByCompany(ctx, companyID)
		if err != nil {
			return err
		}
		if count >= company.SeatLimit {
			return domain.ErrSeatLimitReached
		}

		return s.memberships.Add(ctx, &membership)
	})
	if err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "member added",
		"company_id", companyID, "user_id", userID, "role", role)
	return &membership, nil
}

// RemoveMember dissociates a user from a company. Single DELETE, no transaction.
func (s *Membership) RemoveMember(ctx context.Context, companyID, userID uuid.UUID) error {
	if err := s.memberships.Remove(ctx, companyID, userID); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "member removed", "company_id", companyID, "user_id", userID)
	return nil
}
