package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/service/mocks"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction"
	txmocks "github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction/mocks"
)

// TestVerifyBad_CallsTheProviderInsideTheTransaction states the defect in
// executable form.
//
// The order of calls is the whole problem: the row is locked, *then* the network
// call happens, and only then is the lock released. Locking is not incidental
// here — LockForUpdate is what makes the subsequent unconditional UPDATE safe,
// and it is also what makes the endpoint toxic under load.
func TestVerifyBad_CallsTheProviderInsideTheTransaction(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	company := &domain.Company{ID: companyID, Name: "Clevertechware"}

	var calls []string

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().LockForUpdate(mock.Anything, companyID).
		RunAndReturn(func(context.Context, uuid.UUID) (*domain.Company, error) {
			calls = append(calls, "lock")
			return company, nil
		}).Once()
	companies.EXPECT().SetVerified(mock.Anything, companyID, "VRF-1").
		RunAndReturn(func(context.Context, uuid.UUID, string) error {
			calls = append(calls, "update")
			return nil
		}).Once()
	companies.EXPECT().GetByID(mock.Anything, companyID).Return(company, nil).Once()

	gateway := mocks.NewVerificationGateway(t)
	gateway.EXPECT().Verify(mock.Anything, "Clevertechware").
		RunAndReturn(func(context.Context, string) (string, error) {
			calls = append(calls, "provider")
			return "VRF-1", nil
		}).Once()

	manager := txmocks.NewManager(t)
	manager.EXPECT().Execute(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, unitOfWork transaction.UnitOfWork) error {
			calls = append(calls, "begin")
			err := unitOfWork(ctx)
			calls = append(calls, "commit")
			return err
		}).Once()

	svc := NewVerification(manager, companies, gateway, logger.NewNoOpLogger())

	_, err := svc.VerifyBad(t.Context(), companyID)
	require.NoError(t, err)

	// The provider call sits between BEGIN and COMMIT, after a row lock was
	// taken. Everything the article warns about follows from this sequence.
	assert.Equal(t, []string{"begin", "lock", "provider", "update", "commit"}, calls)
}

// TestVerifyBad_PropagatesFailuresOutOfTheTransaction covers every step that
// can fail inside VerifyBad's unit of work: whichever one does, its error must
// come back out of Execute unchanged, and nothing past that step must run.
func TestVerifyBad_PropagatesFailuresOutOfTheTransaction(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	company := &domain.Company{ID: companyID, Name: "Clevertechware"}

	tests := []struct {
		name    string
		setup   func(*mocks.CompanyRepository, *mocks.VerificationGateway)
		wantErr error
	}{
		{
			name: "lock fails",
			setup: func(companies *mocks.CompanyRepository, _ *mocks.VerificationGateway) {
				companies.EXPECT().LockForUpdate(mock.Anything, companyID).Return(nil, domain.ErrCompanyNotFound).Once()
			},
			wantErr: domain.ErrCompanyNotFound,
		},
		{
			name: "provider fails",
			setup: func(companies *mocks.CompanyRepository, gateway *mocks.VerificationGateway) {
				companies.EXPECT().LockForUpdate(mock.Anything, companyID).Return(company, nil).Once()
				gateway.EXPECT().Verify(mock.Anything, company.Name).Return("", domain.ErrVerificationUnavailable).Once()
			},
			wantErr: domain.ErrVerificationUnavailable,
		},
		{
			name: "set verified fails",
			setup: func(companies *mocks.CompanyRepository, gateway *mocks.VerificationGateway) {
				companies.EXPECT().LockForUpdate(mock.Anything, companyID).Return(company, nil).Once()
				gateway.EXPECT().Verify(mock.Anything, company.Name).Return("VRF-1", nil).Once()
				companies.EXPECT().SetVerified(mock.Anything, companyID, "VRF-1").Return(domain.ErrVerificationConflict).Once()
			},
			wantErr: domain.ErrVerificationConflict,
		},
		{
			name: "trailing read fails",
			setup: func(companies *mocks.CompanyRepository, gateway *mocks.VerificationGateway) {
				companies.EXPECT().LockForUpdate(mock.Anything, companyID).Return(company, nil).Once()
				gateway.EXPECT().Verify(mock.Anything, company.Name).Return("VRF-1", nil).Once()
				companies.EXPECT().SetVerified(mock.Anything, companyID, "VRF-1").Return(nil).Once()
				companies.EXPECT().GetByID(mock.Anything, companyID).Return(nil, domain.ErrCompanyNotFound).Once()
			},
			wantErr: domain.ErrCompanyNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			companies := mocks.NewCompanyRepository(t)
			gateway := mocks.NewVerificationGateway(t)
			tt.setup(companies, gateway)

			svc := NewVerification(passThroughManager(t), companies, gateway, logger.NewNoOpLogger())

			_, err := svc.VerifyBad(t.Context(), companyID)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestVerifyGood_CallsTheProviderOutsideAnyTransaction is the corrected
// sequence, and the assertion that matters most in this file: the transaction
// manager is never touched at all.
func TestVerifyGood_CallsTheProviderOutsideAnyTransaction(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	pending := &domain.Company{
		ID: companyID, Name: "Clevertechware",
		VerificationStatus: domain.VerificationPending,
	}
	verified := &domain.Company{
		ID: companyID, Name: "Clevertechware",
		VerificationStatus: domain.VerificationVerified,
	}

	var calls []string

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().GetByID(mock.Anything, companyID).
		RunAndReturn(func(context.Context, uuid.UUID) (*domain.Company, error) {
			calls = append(calls, "read")
			return pending, nil
		}).Once()
	companies.EXPECT().MarkVerified(mock.Anything, companyID, "VRF-1").
		RunAndReturn(func(context.Context, uuid.UUID, string) (*domain.Company, error) {
			calls = append(calls, "conditional update")
			return verified, nil
		}).Once()

	gateway := mocks.NewVerificationGateway(t)
	gateway.EXPECT().Verify(mock.Anything, "Clevertechware").
		RunAndReturn(func(context.Context, string) (string, error) {
			calls = append(calls, "provider")
			return "VRF-1", nil
		}).Once()

	// No expectations whatsoever: opening a transaction would fail this test.
	// The corrected path is a single conditional statement, so it needs none.
	manager := txmocks.NewManager(t)

	svc := NewVerification(manager, companies, gateway, logger.NewNoOpLogger())

	got, err := svc.VerifyGood(t.Context(), companyID)
	require.NoError(t, err)
	assert.Equal(t, domain.VerificationVerified, got.VerificationStatus)

	assert.Equal(t, []string{"read", "provider", "conditional update"}, calls,
		"the slow call must happen before the write, never between BEGIN and COMMIT")

	// LockForUpdate has no place here: the predicate on the UPDATE replaces it.
	companies.AssertNotCalled(t, "LockForUpdate", mock.Anything, mock.Anything)
	companies.AssertNotCalled(t, "SetVerified", mock.Anything, mock.Anything, mock.Anything)
}

// TestVerifyGood_DoesNotCallTheProviderWhenAlreadyVerified covers the cheap
// early exit: no point paying for a network round trip we will not use.
func TestVerifyGood_DoesNotCallTheProviderWhenAlreadyVerified(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().GetByID(mock.Anything, companyID).Return(&domain.Company{
		ID: companyID, Name: "Clevertechware", VerificationStatus: domain.VerificationVerified,
	}, nil).Once()

	gateway := mocks.NewVerificationGateway(t) // must not be called

	svc := NewVerification(txmocks.NewManager(t), companies, gateway, logger.NewNoOpLogger())

	_, err := svc.VerifyGood(t.Context(), companyID)
	assert.ErrorIs(t, err, domain.ErrVerificationConflict)
}

// TestVerifyGood_ReportsAConflictWhenTheUpdateMatchesNothing is the race the
// early exit above cannot catch: another caller verified the company between the
// read and the write. The predicate on the UPDATE is what notices.
func TestVerifyGood_ReportsAConflictWhenTheUpdateMatchesNothing(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().GetByID(mock.Anything, companyID).Return(&domain.Company{
		ID: companyID, Name: "Clevertechware", VerificationStatus: domain.VerificationPending,
	}, nil).Once()
	companies.EXPECT().MarkVerified(mock.Anything, companyID, "VRF-1").
		Return(nil, domain.ErrVerificationConflict).Once()

	gateway := mocks.NewVerificationGateway(t)
	gateway.EXPECT().Verify(mock.Anything, "Clevertechware").Return("VRF-1", nil).Once()

	svc := NewVerification(txmocks.NewManager(t), companies, gateway, logger.NewNoOpLogger())

	_, err := svc.VerifyGood(t.Context(), companyID)
	assert.ErrorIs(t, err, domain.ErrVerificationConflict)
}

// TestVerifyGood_ProviderFailureWritesNothing: when the third party is down,
// nothing has been locked and nothing is written.
func TestVerifyGood_ProviderFailureWritesNothing(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().GetByID(mock.Anything, companyID).Return(&domain.Company{
		ID: companyID, Name: "Clevertechware", VerificationStatus: domain.VerificationPending,
	}, nil).Once()

	gateway := mocks.NewVerificationGateway(t)
	gateway.EXPECT().Verify(mock.Anything, "Clevertechware").
		Return("", domain.ErrVerificationUnavailable).Once()

	svc := NewVerification(txmocks.NewManager(t), companies, gateway, logger.NewNoOpLogger())

	_, err := svc.VerifyGood(t.Context(), companyID)
	assert.ErrorIs(t, err, domain.ErrVerificationUnavailable)
	companies.AssertNotCalled(t, "MarkVerified", mock.Anything, mock.Anything, mock.Anything)
}
