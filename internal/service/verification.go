package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction"
)

// Verification has a company verified by an external provider.
//
// It exposes the same operation twice: the way almost everyone writes it the
// first time, and the way it should be written. The two differ by where the
// network call sits relative to BEGIN — and that single detail is the difference
// between a healthy database and an incident.
type Verification struct {
	txManager transaction.Manager
	companies companyRepository
	gateway   verificationGateway
	logger    logger.Logger
}

// NewVerification creates the verification service.
func NewVerification(
	txManager transaction.Manager,
	companies companyRepository,
	gateway verificationGateway,
	log logger.Logger,
) *Verification {
	return &Verification{
		txManager: txManager,
		companies: companies,
		gateway:   gateway,
		logger:    log,
	}
}

// VerifyBad is the trap. Do not copy this.
//
//	BEGIN
//	SELECT ... FOR UPDATE          🔒 lock taken on the row
//	  -> HTTP call to the provider  ⏳ 2 to 5 seconds, lock still held
//	UPDATE ...
//	COMMIT                         🔒 lock finally released
//
// It looks reasonable: lock the row, do the work, write the result. It passes
// review, and it works perfectly in development where the provider answers in
// milliseconds and nobody else touches that row.
//
// In production it holds a row lock for the entire round trip. Every other
// transaction wanting that company waits. The connection stays pinned as "idle
// in transaction", so the pool drains. VACUUM cannot reclaim dead tuples that
// this transaction might still see — across the whole database, not just this
// table. A latency spike at the provider becomes contention everywhere.
func (s *Verification) VerifyBad(ctx context.Context, companyID uuid.UUID) (*domain.Company, error) {
	var verified *domain.Company

	err := s.txManager.Execute(ctx, func(ctx context.Context) error {
		company, err := s.companies.LockForUpdate(ctx, companyID)
		if err != nil {
			return err
		}

		// 💥 Network I/O inside the transaction. This is the whole mistake.
		reference, err := s.gateway.Verify(ctx, company.Name)
		if err != nil {
			return err
		}

		if err := s.companies.SetVerified(ctx, companyID, reference); err != nil {
			return err
		}

		verified, err = s.companies.GetByID(ctx, companyID)
		return err
	})
	if err != nil {
		return nil, err
	}

	return verified, nil
}

// VerifyGood is the same operation, written so the transaction stays short.
//
//	-> HTTP call to the provider    (outside any transaction)
//	BEGIN
//	UPDATE ... WHERE verification_status = 'pending'
//	COMMIT                          ~2 ms
//
// Two changes carry all the benefit. The slow work happens before the write, so
// no lock is held across it. And the UPDATE is conditional, which replaces the
// explicit lock entirely: the predicate makes a replay a no-op and detects a
// concurrent execution. Zero rows back means someone else already verified this
// company, which the caller learns as a conflict rather than as a silent
// double-write.
//
// Note that the write is a single statement, so it needs no explicit
// transaction at all — PostgreSQL commits it on its own.
func (s *Verification) VerifyGood(ctx context.Context, companyID uuid.UUID) (*domain.Company, error) {
	// Read outside any transaction: one SELECT, nothing to keep consistent.
	company, err := s.companies.GetByID(ctx, companyID)
	if err != nil {
		return nil, err
	}
	if company.VerificationStatus != domain.VerificationPending {
		// Cheap early exit. Not a guarantee — a concurrent call can still slip in
		// between here and the UPDATE, which is precisely what the predicate on
		// the UPDATE is there to catch.
		return nil, domain.ErrVerificationConflict
	}

	// Slow, outside the transaction, holding nothing.
	reference, err := s.gateway.Verify(ctx, company.Name)
	if err != nil {
		return nil, err
	}

	verified, err := s.companies.MarkVerified(ctx, companyID, reference)
	if err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "company verified", "company_id", companyID, "reference", reference)
	return verified, nil
}
