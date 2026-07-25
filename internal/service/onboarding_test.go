package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/domain"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/logger"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/service/mocks"
	"github.com/clevertechware/handling-db-transactions-in-golang/pkg/transaction"
	txmocks "github.com/clevertechware/handling-db-transactions-in-golang/pkg/transaction/mocks"
)

func validOnboarding() OnboardingInput {
	return OnboardingInput{
		Company: domain.Company{Name: "Clevertechware"},
		Owner: domain.User{
			FirstName: "Ada", LastName: "Lovelace",
			Email: "ada@example.com", Username: "ada",
		},
	}
}

// TestOnboarding_WrapsEveryWriteInOneTransaction is the structural claim: the
// three writes belong to a single Execute, because the invariant "a company
// always has an owner" spans all three.
func TestOnboarding_WrapsEveryWriteInOneTransaction(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	ownerID := uuid.New()

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, c *domain.Company) error {
			c.ID = companyID
			return nil
		}).Once()

	users := mocks.NewUserRepository(t)
	users.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, u *domain.User) error {
			u.ID = ownerID
			return nil
		}).Once()

	memberships := mocks.NewMembershipRepository(t)
	memberships.EXPECT().Add(mock.Anything, mock.Anything).Return(nil).Once()

	manager := txmocks.NewManager(t)
	manager.EXPECT().Execute(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, unitOfWork transaction.UnitOfWork) error {
			return unitOfWork(ctx)
		}).Once() // exactly one transaction, not three

	svc := NewOnboarding(manager, companies, users, memberships, logger.NewNoOpLogger())

	result, err := svc.Execute(t.Context(), validOnboarding())

	require.NoError(t, err)
	assert.Equal(t, companyID, result.Company.ID)
	assert.Equal(t, ownerID, result.Owner.ID)
	// The membership must join the two rows created in the same transaction.
	assert.Equal(t, companyID, result.Membership.CompanyID)
	assert.Equal(t, ownerID, result.Membership.UserID)
	assert.Equal(t, domain.RoleOwner, result.Membership.Role)
}

// TestOnboarding_PropagatesFailureOutOfTheTransaction checks the error reaches
// the manager, which is what triggers the rollback. Swallowing it here would
// leave a half-created company committed.
func TestOnboarding_PropagatesFailureOutOfTheTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*mocks.CompanyRepository, *mocks.UserRepository, *mocks.MembershipRepository)
		wantErr error
	}{
		{
			name: "the company insert fails",
			setup: func(c *mocks.CompanyRepository, _ *mocks.UserRepository, _ *mocks.MembershipRepository) {
				c.EXPECT().Create(mock.Anything, mock.Anything).Return(errors.New("boom")).Once()
			},
			wantErr: nil,
		},
		{
			name: "the owner's email is taken",
			setup: func(c *mocks.CompanyRepository, u *mocks.UserRepository, _ *mocks.MembershipRepository) {
				c.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
				u.EXPECT().Create(mock.Anything, mock.Anything).Return(domain.ErrEmailAlreadyExists).Once()
			},
			wantErr: domain.ErrEmailAlreadyExists,
		},
		{
			name: "the membership insert fails last",
			setup: func(c *mocks.CompanyRepository, u *mocks.UserRepository, m *mocks.MembershipRepository) {
				c.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
				u.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
				m.EXPECT().Add(mock.Anything, mock.Anything).Return(domain.ErrMembershipExists).Once()
			},
			wantErr: domain.ErrMembershipExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			companies := mocks.NewCompanyRepository(t)
			users := mocks.NewUserRepository(t)
			memberships := mocks.NewMembershipRepository(t)
			tt.setup(companies, users, memberships)

			svc := NewOnboarding(passThroughManager(t), companies, users, memberships, logger.NewNoOpLogger())

			result, err := svc.Execute(t.Context(), validOnboarding())

			require.Error(t, err)
			assert.Nil(t, result)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

// TestOnboarding_ValidatesBeforeOpeningATransaction: rejecting a bad payload
// should not cost a BEGIN and a ROLLBACK. The mock manager has no expectations,
// so any call to it fails the test.
func TestOnboarding_ValidatesBeforeOpeningATransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input OnboardingInput
	}{
		{
			name:  "company without a name",
			input: OnboardingInput{Owner: validOnboarding().Owner},
		},
		{
			name: "owner without an email",
			input: OnboardingInput{
				Company: domain.Company{Name: "Clevertechware"},
				Owner:   domain.User{FirstName: "Ada", LastName: "Lovelace", Username: "ada"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := NewOnboarding(
				txmocks.NewManager(t), // no expectations: must never be called
				mocks.NewCompanyRepository(t),
				mocks.NewUserRepository(t),
				mocks.NewMembershipRepository(t),
				logger.NewNoOpLogger(),
			)

			_, err := svc.Execute(t.Context(), tt.input)
			assert.ErrorIs(t, err, domain.ErrInvalidInput)
		})
	}
}

// TestOnboarding_AppliesTheDefaultSeatLimit keeps the entity and the column
// default in agreement.
func TestOnboarding_AppliesTheDefaultSeatLimit(t *testing.T) {
	t.Parallel()

	companies := mocks.NewCompanyRepository(t)
	var seen int
	companies.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, c *domain.Company) error {
			seen = c.SeatLimit
			return nil
		}).Once()

	users := mocks.NewUserRepository(t)
	users.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

	memberships := mocks.NewMembershipRepository(t)
	memberships.EXPECT().Add(mock.Anything, mock.Anything).Return(nil).Once()

	svc := NewOnboarding(passThroughManager(t), companies, users, memberships, logger.NewNoOpLogger())

	_, err := svc.Execute(t.Context(), validOnboarding())
	require.NoError(t, err)
	assert.Equal(t, domain.DefaultSeatLimit, seen)
}
