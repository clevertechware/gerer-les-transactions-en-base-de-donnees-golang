package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction"
)

// CompanyReport is a consistent view of a company and its members.
type CompanyReport struct {
	Company     domain.Company `json:"company"`
	Members     []domain.User  `json:"members"`
	MemberCount int            `json:"member_count"`
	SeatsLeft   int            `json:"seats_left"`
}

// Report builds read-only views.
type Report struct {
	txManager   transaction.Manager
	companies   companyRepository
	users       userRepository
	memberships membershipRepository
	logger      logger.Logger
}

// NewReport creates the report service.
func NewReport(
	txManager transaction.Manager,
	companies companyRepository,
	users userRepository,
	memberships membershipRepository,
	log logger.Logger,
) *Report {
	return &Report{
		txManager:   txManager,
		companies:   companies,
		users:       users,
		memberships: memberships,
		logger:      log,
	}
}

// CompanyReport assembles a company, its members, and its seat count.
//
// This is the one-read path in the demo that deserves a transaction, and it is worth being precise about why.
//
// Not because reading needs protecting, a single SELECT is fine on its own, which is why GetCompany and ListUsers
// open nothing. It is because *three* reads have to agree with each other: without a shared snapshot, a member added
// between the second and third query would make the list and the count contradict, and the report would be visibly wrong.
//
// Note that READ COMMITTED would not fix that. It takes a fresh snapshot per statement, so the two reads could still
// disagree. ExecuteReadOnly runs at REPEATABLE READ for exactly this reason.
//
// Declaring it READ-ONLY buys two more things: PostgreSQL refuses any write that slips in, and a routing layer may
// send the whole block to a replica.
func (s *Report) CompanyReport(ctx context.Context, companyID uuid.UUID) (*CompanyReport, error) {
	var report CompanyReport

	err := s.txManager.ExecuteReadOnly(ctx, func(ctx context.Context) error {
		company, err := s.companies.GetByID(ctx, companyID)
		if err != nil {
			return err
		}

		members, err := s.users.ListByCompany(ctx, companyID)
		if err != nil {
			return err
		}

		count, err := s.memberships.CountByCompany(ctx, companyID)
		if err != nil {
			return err
		}

		report = CompanyReport{
			Company:     *company,
			Members:     members,
			MemberCount: count,
			SeatsLeft:   max(company.SeatLimit-count, 0),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &report, nil
}
