package postgres

import (
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
	t := s.T()
	ctx := s.txContext(t)

	company := &domain.Company{Name: "Before", SeatLimit: 1}
	require.NoError(t, s.companies.Create(ctx, company))

	address := "Bordeaux"
	company.Name = "After"
	company.Address = &address
	company.SeatLimit = 9
	require.NoError(t, s.companies.Update(ctx, company))

	got, err := s.companies.GetByID(ctx, company.ID)
	require.NoError(t, err)
	assert.Equal(t, "After", got.Name)
	assert.Equal(t, &address, got.Address)
	assert.Equal(t, 9, got.SeatLimit)
}

func (s *RepositorySuite) TestCompany_Update_NotFound() {
	t := s.T()
	ctx := s.txContext(t)

	missing := &domain.Company{ID: uuidNotInDatabase, Name: "Ghost", SeatLimit: 1}
	assert.ErrorIs(t, s.companies.Update(ctx, missing), domain.ErrCompanyNotFound)
}

func (s *RepositorySuite) TestCompany_Delete_IsSoftAndHidesTheRow() {
	t := s.T()
	ctx := s.txContext(t)

	company := &domain.Company{Name: "To be deleted", SeatLimit: 1}
	require.NoError(t, s.companies.Create(ctx, company))
	require.NoError(t, s.companies.Delete(ctx, company.ID))

	_, err := s.companies.GetByID(ctx, company.ID)
	assert.ErrorIs(t, err, domain.ErrCompanyNotFound, "a soft-deleted company should read as absent")

	// Deleting twice is not an error the caller can act on differently.
	assert.ErrorIs(t, s.companies.Delete(ctx, company.ID), domain.ErrCompanyNotFound)
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

// TestCompany_SetVerified is the unconditional write of the broken path: it only
// ever runs while a row lock is held, but the statement itself has no opinion on
// that — it just needs a transaction to run in, same as any other write.
func (s *RepositorySuite) TestCompany_SetVerified() {
	t := s.T()
	ctx := s.txContext(t)

	company := &domain.Company{Name: "Verifiable", SeatLimit: 1}
	require.NoError(t, s.companies.Create(ctx, company))

	require.NoError(t, s.companies.SetVerified(ctx, company.ID, "VRF-1"))

	got, err := s.companies.GetByID(ctx, company.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.VerificationVerified, got.VerificationStatus)
	require.NotNil(t, got.VerificationRef)
	assert.Equal(t, "VRF-1", *got.VerificationRef)
}

func (s *RepositorySuite) TestCompany_SetVerified_NotFound() {
	t := s.T()
	ctx := s.txContext(t)

	assert.ErrorIs(t, s.companies.SetVerified(ctx, uuidNotInDatabase, "VRF-1"), domain.ErrCompanyNotFound)
}

// TestCompany_LockForUpdate_RequiresTransaction guards the one query that is
// meaningless in autocommit.
func (s *RepositorySuite) TestCompany_LockForUpdate_RequiresTransaction() {
	t := s.T()

	// Note: t.Context() deliberately, not s.txContext(t) — no ambient transaction.
	_, err := s.companies.LockForUpdate(t.Context(), uuidNotInDatabase)
	assert.ErrorIs(t, err, domain.ErrTransactionRequired)
}

func (s *RepositorySuite) TestCompany_LockForUpdate() {
	t := s.T()
	ctx := s.txContext(t)

	company := &domain.Company{Name: "Lockable", SeatLimit: 1}
	require.NoError(t, s.companies.Create(ctx, company))

	locked, err := s.companies.LockForUpdate(ctx, company.ID)
	require.NoError(t, err)
	assert.Equal(t, company.ID, locked.ID)
}

func (s *RepositorySuite) TestCompany_LockForUpdate_NotFound() {
	t := s.T()
	ctx := s.txContext(t)

	_, err := s.companies.LockForUpdate(ctx, uuidNotInDatabase)
	assert.ErrorIs(t, err, domain.ErrCompanyNotFound)
}
