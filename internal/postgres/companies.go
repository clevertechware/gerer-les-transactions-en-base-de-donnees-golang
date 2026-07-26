package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/logger"
)

const companyColumns = `id, name, address, verification_status, verification_ref,
	verified_at, seat_limit, created_at, updated_at`

// CompanyRepository reads and writes companies.
//
// Every method goes through txManager.Executor, so the same code runs inside a
// transaction or in autocommit depending on what the caller opened.
type CompanyRepository struct {
	txManager *TxManager
	logger    logger.Logger
}

// NewCompanyRepository creates a CompanyRepository.
func NewCompanyRepository(txManager *TxManager, log logger.Logger) *CompanyRepository {
	return &CompanyRepository{txManager: txManager, logger: log}
}

func scanCompany(s scanner) (domain.Company, error) {
	var c domain.Company
	err := s.Scan(
		&c.ID, &c.Name, &c.Address, &c.VerificationStatus, &c.VerificationRef,
		&c.VerifiedAt, &c.SeatLimit, &c.CreatedAt, &c.UpdatedAt,
	)
	return c, err
}

// Create inserts a company. A single statement: it needs no transaction.
func (r *CompanyRepository) Create(ctx context.Context, company *domain.Company) error {
	const query = `
		INSERT INTO companies (name, address, seat_limit)
		VALUES ($1, $2, $3)
		RETURNING ` + companyColumns

	created, err := scanCompany(r.txManager.Executor(ctx).
		QueryRow(ctx, query, company.Name, company.Address, company.SeatLimit))
	if err != nil {
		return fmt.Errorf("inserting company: %w", err)
	}

	*company = created
	return nil
}

// GetByID returns a company, or ErrCompanyNotFound.
func (r *CompanyRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Company, error) {
	const query = `SELECT ` + companyColumns + `
		FROM companies WHERE id = $1 AND deleted_at IS NULL`

	company, err := scanCompany(r.txManager.Executor(ctx).QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCompanyNotFound
		}
		return nil, fmt.Errorf("selecting company %s: %w", id, err)
	}

	return &company, nil
}

// List returns every live company.
func (r *CompanyRepository) List(ctx context.Context) ([]domain.Company, error) {
	const query = `SELECT ` + companyColumns + `
		FROM companies WHERE deleted_at IS NULL ORDER BY created_at`

	rows, err := r.txManager.Executor(ctx).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("selecting companies: %w", err)
	}
	defer rows.Close()

	companies := make([]domain.Company, 0)
	for rows.Next() {
		company, err := scanCompany(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning company: %w", err)
		}
		companies = append(companies, company)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating companies: %w", err)
	}

	return companies, nil
}

// Update changes the mutable fields of a company.
func (r *CompanyRepository) Update(ctx context.Context, company *domain.Company) error {
	const query = `
		UPDATE companies
		SET name = $2, address = $3, seat_limit = $4, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING ` + companyColumns

	updated, err := scanCompany(r.txManager.Executor(ctx).
		QueryRow(ctx, query, company.ID, company.Name, company.Address, company.SeatLimit))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrCompanyNotFound
		}
		return fmt.Errorf("updating company %s: %w", company.ID, err)
	}

	*company = updated
	return nil
}

// Delete soft-deletes a company.
func (r *CompanyRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `
		UPDATE companies SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.txManager.Executor(ctx).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("deleting company %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCompanyNotFound
	}

	return nil
}

// LockForUpdate reads a company and holds a row lock until the end of the
// transaction.
//
// Used only by the broken verification path, to show what it costs. The lock is
// released at COMMIT, so anything the caller does before then — a network call,
// say — is time every concurrent writer of this row spends waiting.
func (r *CompanyRepository) LockForUpdate(ctx context.Context, id uuid.UUID) (*domain.Company, error) {
	// Outside a transaction the lock would be released immediately and protect
	// nothing, so refuse rather than silently do nothing useful.
	tx, err := r.txManager.RequireTx(ctx)
	if err != nil {
		return nil, err
	}

	const query = `SELECT ` + companyColumns + `
		FROM companies WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE`

	company, err := scanCompany(tx.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCompanyNotFound
		}
		return nil, fmt.Errorf("locking company %s: %w", id, err)
	}

	return &company, nil
}

// SetVerified marks a company verified unconditionally.
//
// The naive write of the broken path: it assumes nothing changed since the row
// was read, which is only true because a lock has been held all along.
func (r *CompanyRepository) SetVerified(ctx context.Context, id uuid.UUID, reference string) error {
	const query = `
		UPDATE companies
		SET verification_status = 'verified',
		    verification_ref = $2,
		    verified_at = now(),
		    updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.txManager.Executor(ctx).Exec(ctx, query, id, reference)
	if err != nil {
		return fmt.Errorf("verifying company %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCompanyNotFound
	}

	return nil
}

// MarkVerified marks a company verified only while it is still pending.
//
// The corrected write. The extra predicate does two jobs at once: it makes a
// replay a no-op, and it detects a concurrent execution — no explicit lock, no
// transaction, one statement. Zero rows back means someone else got there first.
func (r *CompanyRepository) MarkVerified(ctx context.Context, id uuid.UUID, reference string) (*domain.Company, error) {
	const query = `
		UPDATE companies
		SET verification_status = 'verified',
		    verification_ref = $2,
		    verified_at = now(),
		    updated_at = now()
		WHERE id = $1
		  AND deleted_at IS NULL
		  AND verification_status = 'pending'
		RETURNING ` + companyColumns

	company, err := scanCompany(r.txManager.Executor(ctx).QueryRow(ctx, query, id, reference))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Either the company is gone, or it is no longer pending. Tell them
			// apart so the caller can answer 404 rather than 409.
			if _, getErr := r.GetByID(ctx, id); getErr != nil {
				return nil, getErr
			}
			return nil, domain.ErrVerificationConflict
		}
		return nil, fmt.Errorf("verifying company %s: %w", id, err)
	}

	return &company, nil
}
