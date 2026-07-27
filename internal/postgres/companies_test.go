package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
)

// Every test below runs inside a transaction that is rolled back afterwards, so
// they can write freely without cleaning up or interfering with each other.

func (s *RepositorySuite) TestCompany_CreateAndGet() {
	t := s.T()
	ctx := s.txContext(t)

	address := "Toulouse"
	company := &domain.Company{Name: "Clevertechware", Address: &address, SeatLimit: 4}
	require.NoError(t, s.companies.Create(ctx, company))

	assert.NotEqual(t, uuidNotInDatabase, company.ID, "the insert should return the generated id")
	assert.NotZero(t, company.CreatedAt)
	// The column default must reach the entity, not just the row.
	assert.Equal(t, domain.VerificationPending, company.VerificationStatus)
	assert.Nil(t, company.VerificationRef)

	got, err := s.companies.GetByID(ctx, company.ID)
	require.NoError(t, err)
	assert.Equal(t, "Clevertechware", got.Name)
	assert.Equal(t, &address, got.Address)
	assert.Equal(t, 4, got.SeatLimit)
}

func (s *RepositorySuite) TestCompany_GetByID_NotFound() {
	t := s.T()
	ctx := s.txContext(t)

	_, err := s.companies.GetByID(ctx, uuidNotInDatabase)
	assert.ErrorIs(t, err, domain.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestCompany_List() {
	t := s.T()
	ctx := s.txContext(t)

	first := &domain.Company{Name: "First", SeatLimit: 1}
	require.NoError(t, s.companies.Create(ctx, first))
	second := &domain.Company{Name: "Second", SeatLimit: 1}
	require.NoError(t, s.companies.Create(ctx, second))
	deleted := &domain.Company{Name: "Deleted", SeatLimit: 1}
	require.NoError(t, s.companies.Create(ctx, deleted))
	require.NoError(t, s.companies.Delete(ctx, deleted.ID))

	got, err := s.companies.List(ctx)
	require.NoError(t, err)

	ids := make([]uuid.UUID, 0, len(got))
	for _, company := range got {
		ids = append(ids, company.ID)
	}
	assert.Contains(t, ids, first.ID)
	assert.Contains(t, ids, second.ID)
	assert.NotContains(t, ids, deleted.ID, "a soft-deleted company must not be listed")
}

func (s *RepositorySuite) TestCompany_Update() {
	tests := []struct {
		name      string
		setup     func(t *testing.T, ctx context.Context) *domain.Company
		update    func(c *domain.Company)
		wantErr   error
		wantCheck func(t *testing.T, got *domain.Company)
	}{
		{
			name: "successfully updates existing company",
			setup: func(t *testing.T, ctx context.Context) *domain.Company {
				company := &domain.Company{Name: "Before", SeatLimit: 1}
				require.NoError(t, s.companies.Create(ctx, company))
				return company
			},
			update: func(c *domain.Company) {
				address := "Bordeaux"
				c.Name = "After"
				c.Address = &address
				c.SeatLimit = 9
			},
			wantCheck: func(t *testing.T, got *domain.Company) {
				assert.Equal(t, "After", got.Name)
				assert.Equal(t, "Bordeaux", *got.Address)
				assert.Equal(t, 9, got.SeatLimit)
			},
		},
		{
			name: "returns error for missing company",
			setup: func(_ *testing.T, _ context.Context) *domain.Company {
				return &domain.Company{ID: uuidNotInDatabase, Name: "Ghost", SeatLimit: 1}
			},
			update:  func(_ *domain.Company) {},
			wantErr: domain.ErrCompanyNotFound,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			ctx := s.txContext(t)
			company := tt.setup(t, ctx)
			tt.update(company)

			err := s.companies.Update(ctx, company)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			got, err := s.companies.GetByID(ctx, company.ID)
			require.NoError(t, err)
			tt.wantCheck(t, got)
		})
	}
}

func (s *RepositorySuite) TestCompany_Delete() {
	tests := []struct {
		name       string
		setup      func(t *testing.T, ctx context.Context) uuid.UUID
		wantErr    error
		wantHidden bool
	}{
		{
			name: "soft-delete hides the row",
			setup: func(t *testing.T, ctx context.Context) uuid.UUID {
				company := &domain.Company{Name: "To be deleted", SeatLimit: 1}
				require.NoError(t, s.companies.Create(ctx, company))
				return company.ID
			},
			wantHidden: true,
		},
		{
			name: "second delete returns not found",
			setup: func(t *testing.T, ctx context.Context) uuid.UUID {
				company := &domain.Company{Name: "To be deleted", SeatLimit: 1}
				require.NoError(t, s.companies.Create(ctx, company))
				require.NoError(t, s.companies.Delete(ctx, company.ID))
				return company.ID
			},
			wantErr: domain.ErrCompanyNotFound,
		},
		{
			name: "deleting missing company returns error",
			setup: func(_ *testing.T, _ context.Context) uuid.UUID {
				return uuidNotInDatabase
			},
			wantErr: domain.ErrCompanyNotFound,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			ctx := s.txContext(t)
			id := tt.setup(t, ctx)

			if tt.wantErr != nil {
				assert.ErrorIs(t, s.companies.Delete(ctx, id), tt.wantErr)
				return
			}

			require.NoError(t, s.companies.Delete(ctx, id))
			if tt.wantHidden {
				_, err := s.companies.GetByID(ctx, id)
				assert.ErrorIs(t, err, domain.ErrCompanyNotFound, "a soft-deleted company should read as absent")
			}
		})
	}
}

// TestCompany_MarkVerified_IsIdempotent is the corrected verification write from
// the article: conditional, so a replay changes nothing and a concurrent
// execution is detected without any explicit lock.
func (s *RepositorySuite) TestCompany_MarkVerified_IsIdempotent() {
	t := s.T()
	ctx := s.txContext(t)

	company := &domain.Company{Name: "Verifiable", SeatLimit: 1}
	require.NoError(t, s.companies.Create(ctx, company))

	verified, err := s.companies.MarkVerified(ctx, company.ID, "VRF-first")
	require.NoError(t, err)
	assert.Equal(t, domain.VerificationVerified, verified.VerificationStatus)
	require.NotNil(t, verified.VerificationRef)
	assert.Equal(t, "VRF-first", *verified.VerificationRef)
	assert.NotNil(t, verified.VerifiedAt)

	// Second call: the WHERE clause no longer matches.
	_, err = s.companies.MarkVerified(ctx, company.ID, "VRF-second")
	assert.ErrorIs(t, err, domain.ErrVerificationConflict)

	// And the first reference is intact — no silent overwrite.
	got, err := s.companies.GetByID(ctx, company.ID)
	require.NoError(t, err)
	assert.Equal(t, "VRF-first", *got.VerificationRef)
}

// TestCompany_MarkVerified_MissingCompany checks the caller can still tell "gone"
// from "already verified", which decides 404 versus 409.
func (s *RepositorySuite) TestCompany_MarkVerified_MissingCompany() {
	t := s.T()
	ctx := s.txContext(t)

	_, err := s.companies.MarkVerified(ctx, uuidNotInDatabase, "VRF-x")
	assert.ErrorIs(t, err, domain.ErrCompanyNotFound)
}

// TestCompany_SetVerified_Unconditional tests the unconditional write of the broken path:
// it only ever runs while a row lock is held, but the statement itself has no opinion on
// that — it just needs a transaction to run in, same as any other write.
func (s *RepositorySuite) TestCompany_SetVerified_Unconditional() {
	tests := []struct {
		name      string
		setup     func(t *testing.T, ctx context.Context) uuid.UUID
		wantErr   error
		wantRef   *string
		wantState domain.VerificationStatus
	}{
		{
			name: "existing company becomes verified",
			setup: func(t *testing.T, ctx context.Context) uuid.UUID {
				company := &domain.Company{Name: "Verifiable", SeatLimit: 1}
				require.NoError(t, s.companies.Create(ctx, company))
				return company.ID
			},
			wantRef:   stringPtr("VRF-1"),
			wantState: domain.VerificationVerified,
		},
		{
			name: "missing company returns error",
			setup: func(_ *testing.T, _ context.Context) uuid.UUID {
				return uuidNotInDatabase
			},
			wantErr: domain.ErrCompanyNotFound,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			ctx := s.txContext(t)
			id := tt.setup(t, ctx)

			err := s.companies.SetVerified(ctx, id, "VRF-1")
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			got, err := s.companies.GetByID(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, tt.wantState, got.VerificationStatus)
			assert.Equal(t, tt.wantRef, got.VerificationRef)
		})
	}
}

// TestCompany_LockForUpdate_WithTransactionControl tests row-level locking scenarios.
// The FOR UPDATE query is meaningless in autocommit mode, requiring a transaction.
func (s *RepositorySuite) TestCompany_LockForUpdate_WithTransactionControl() {
	tests := []struct {
		name              string
		useTransaction    bool
		setup             func(t *testing.T, ctx context.Context) uuid.UUID
		wantErr           error
		wantVerifyLocked  bool
	}{
		{
			name:           "requires transaction",
			useTransaction: false,
			setup: func(_ *testing.T, _ context.Context) uuid.UUID {
				return uuidNotInDatabase
			},
			wantErr: domain.ErrTransactionRequired,
		},
		{
			name:           "existing company can be locked",
			useTransaction: true,
			setup: func(t *testing.T, ctx context.Context) uuid.UUID {
				company := &domain.Company{Name: "Lockable", SeatLimit: 1}
				require.NoError(t, s.companies.Create(ctx, company))
				return company.ID
			},
			wantVerifyLocked: true,
		},
		{
			name:           "missing company returns not found",
			useTransaction: true,
			setup: func(_ *testing.T, _ context.Context) uuid.UUID {
				return uuidNotInDatabase
			},
			wantErr: domain.ErrCompanyNotFound,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			if tt.useTransaction {
				ctx = s.txContext(t)
			} else {
				ctx = t.Context()
			}

			id := tt.setup(t, ctx)

			locked, err := s.companies.LockForUpdate(ctx, id)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			if tt.wantVerifyLocked {
				assert.Equal(t, id, locked.ID)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}
