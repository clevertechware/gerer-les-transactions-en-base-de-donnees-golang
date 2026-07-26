package service

import (
	"context"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/pkg/transaction"
)

// OnboardingInput is what the caller supplies to register a company together
// with its first user.
type OnboardingInput struct {
	Company domain.Company
	Owner   domain.User
}

// Onboarding creates a company, its owner and the membership joining them.
//
// This is the case where a transaction is not optional. Three writes across
// three tables carry one business invariant: a company always has an owner. A
// company with no owner, or an owner with no company, is a state nobody should
// ever observe — so the three rows appear together or not at all.
type Onboarding struct {
	txManager   transaction.Manager
	companies   companyRepository
	users       userRepository
	memberships membershipRepository
	logger      logger.Logger
}

// NewOnboarding creates the onboarding service.
func NewOnboarding(
	txManager transaction.Manager,
	companies companyRepository,
	users userRepository,
	memberships membershipRepository,
	log logger.Logger,
) *Onboarding {
	return &Onboarding{
		txManager:   txManager,
		companies:   companies,
		users:       users,
		memberships: memberships,
		logger:      log,
	}
}

// Execute registers the company, the owner and their membership atomically.
func (s *Onboarding) Execute(ctx context.Context, input OnboardingInput) (*domain.Onboarding, error) {
	// Validation happens before BEGIN. Rejecting a bad payload from inside a
	// transaction would open one only to roll it back.
	company := input.Company
	if err := validateCompany(&company); err != nil {
		return nil, err
	}
	if company.SeatLimit == 0 {
		company.SeatLimit = domain.DefaultSeatLimit
	}

	owner := input.Owner
	if err := validateUser(&owner); err != nil {
		return nil, err
	}

	var result domain.Onboarding
	err := s.txManager.Execute(ctx, func(ctx context.Context) error {
		// Each repository call picks the ambient transaction out of ctx, so the
		// three writes land in the same one.
		if err := s.companies.Create(ctx, &company); err != nil {
			return err
		}
		if err := s.users.Create(ctx, &owner); err != nil {
			return err
		}

		membership := domain.Membership{
			UserID:    owner.ID,
			CompanyID: company.ID,
			Role:      domain.RoleOwner,
		}
		if err := s.memberships.Add(ctx, &membership); err != nil {
			return err
		}

		result = domain.Onboarding{Company: company, Owner: owner, Membership: membership}
		return nil
	})
	if err != nil {
		// Nothing was persisted: the rollback took the company and the user with
		// it, even though each INSERT succeeded on its own.
		s.logger.WarnContext(ctx, "onboarding rolled back", "company", company.Name, "error", err)
		return nil, err
	}

	s.logger.InfoContext(ctx, "company onboarded",
		"company_id", result.Company.ID, "owner_id", result.Owner.ID)
	return &result, nil
}
