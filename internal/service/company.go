package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/domain"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/logger"
)

// Company handles plain CRUD on companies.
//
// Not one method here opens a transaction, and that is the point. Each is a
// single statement, and a single statement is already atomic — PostgreSQL runs
// it in an implicit transaction that commits on its own. Wrapping it in an
// explicit BEGIN/COMMIT would buy nothing and cost two extra round trips, plus a
// connection held for longer.
type Company struct {
	companies companyRepository
	logger    logger.Logger
}

// NewCompany creates the company service.
func NewCompany(companies companyRepository, log logger.Logger) *Company {
	return &Company{companies: companies, logger: log}
}

// CreateCompany inserts a company. Single INSERT, no transaction.
func (s *Company) CreateCompany(ctx context.Context, company *domain.Company) error {
	if err := validateCompany(company); err != nil {
		return err
	}
	if company.SeatLimit == 0 {
		company.SeatLimit = domain.DefaultSeatLimit
	}

	if err := s.companies.Create(ctx, company); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "company created", "company_id", company.ID, "name", company.Name)
	return nil
}

// GetCompany returns a company. Single SELECT, no transaction.
func (s *Company) GetCompany(ctx context.Context, id uuid.UUID) (*domain.Company, error) {
	return s.companies.GetByID(ctx, id)
}

// ListCompanies returns every company. Single SELECT, no transaction.
func (s *Company) ListCompanies(ctx context.Context) ([]domain.Company, error) {
	return s.companies.List(ctx)
}

// UpdateCompany changes a company. Single UPDATE, no transaction.
func (s *Company) UpdateCompany(ctx context.Context, company *domain.Company) error {
	if err := validateCompany(company); err != nil {
		return err
	}
	if company.SeatLimit == 0 {
		company.SeatLimit = domain.DefaultSeatLimit
	}

	if err := s.companies.Update(ctx, company); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "company updated", "company_id", company.ID)
	return nil
}

// DeleteCompany soft-deletes a company. Single UPDATE, no transaction.
func (s *Company) DeleteCompany(ctx context.Context, id uuid.UUID) error {
	if err := s.companies.Delete(ctx, id); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "company deleted", "company_id", id)
	return nil
}

// validateCompany runs before any database call, so an invalid payload never
// reaches the database in the first place.
func validateCompany(company *domain.Company) error {
	if strings.TrimSpace(company.Name) == "" {
		return domain.ErrInvalidInput
	}
	if company.SeatLimit < 0 {
		return domain.ErrInvalidInput
	}
	return nil
}
