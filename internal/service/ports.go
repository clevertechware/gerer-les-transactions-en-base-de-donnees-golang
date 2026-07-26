// Package service holds the application logic, and with it the decision that
// this demo is about: where a transaction starts and where it ends.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
)

// The interfaces below are declared here, where they are consumed, rather than
// next to their PostgreSQL implementations. The service layer states what it
// needs; the adapter satisfies it.

type companyRepository interface {
	Create(ctx context.Context, company *domain.Company) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Company, error)
	List(ctx context.Context) ([]domain.Company, error)
	Update(ctx context.Context, company *domain.Company) error
	Delete(ctx context.Context, id uuid.UUID) error

	// LockForUpdate and SetVerified exist for the broken verification path only.
	LockForUpdate(ctx context.Context, id uuid.UUID) (*domain.Company, error)
	SetVerified(ctx context.Context, id uuid.UUID, reference string) error

	// MarkVerified is the corrected path: conditional, idempotent, lock-free.
	MarkVerified(ctx context.Context, id uuid.UUID, reference string) (*domain.Company, error)
}

type userRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
	ListByCompany(ctx context.Context, companyID uuid.UUID) ([]domain.User, error)
	Update(ctx context.Context, user *domain.User) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type membershipRepository interface {
	Add(ctx context.Context, membership *domain.Membership) error
	Remove(ctx context.Context, companyID, userID uuid.UUID) error
	CountByCompany(ctx context.Context, companyID uuid.UUID) (int, error)
	ListByCompany(ctx context.Context, companyID uuid.UUID) ([]domain.Membership, error)
}

// verificationGateway is the slow third party. Everything this demo warns about
// hangs on one question: is a call to it inside a transaction, or outside?
type verificationGateway interface {
	Verify(ctx context.Context, companyName string) (reference string, err error)
}
