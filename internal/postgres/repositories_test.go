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

// TestCompany_LockForUpdate_RequiresTransaction guards the one query that is
// meaningless in autocommit.
func (s *RepositorySuite) TestCompany_LockForUpdate_RequiresTransaction() {
	t := s.T()

	// Note: t.Context() deliberately, not s.txContext(t) — no ambient transaction.
	_, err := s.companies.LockForUpdate(t.Context(), uuidNotInDatabase)
	assert.ErrorIs(t, err, domain.ErrTransactionRequired)
}

func (s *RepositorySuite) TestUser_UniqueConstraintsAreTranslated() {
	tests := []struct {
		name    string
		user    *domain.User
		wantErr error
	}{
		{
			name:    "duplicate email",
			user:    &domain.User{FirstName: "A", LastName: "B", Email: "ada@example.com", Username: "other"},
			wantErr: domain.ErrEmailAlreadyExists,
		},
		{
			name:    "duplicate username",
			user:    &domain.User{FirstName: "A", LastName: "B", Email: "other@example.com", Username: "ada"},
			wantErr: domain.ErrUsernameAlreadyExists,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			// Each subtest needs its own transaction: a failed statement aborts
			// the current one, and every later statement in it comes back as
			// 25P02 instead of the constraint violation being tested.
			ctx := s.txContext(t)

			require.NoError(t, s.users.Create(ctx, &domain.User{
				FirstName: "Ada", LastName: "Lovelace",
				Email: "ada@example.com", Username: "ada",
			}))

			// A single SQLSTATE 23505 has to become two different domain errors,
			// which is only possible by looking at the constraint name.
			assert.ErrorIs(t, s.users.Create(ctx, tt.user), tt.wantErr)
		})
	}
}

// TestUser_SoftDeleteFreesTheEmail is why the unique indexes are partial: a
// deleted user must not keep reserving an address forever.
func (s *RepositorySuite) TestUser_SoftDeleteFreesTheEmail() {
	t := s.T()
	ctx := s.txContext(t)

	user := &domain.User{FirstName: "Grace", LastName: "Hopper", Email: "grace@example.com", Username: "grace"}
	require.NoError(t, s.users.Create(ctx, user))
	require.NoError(t, s.users.Delete(ctx, user.ID))

	reuse := &domain.User{FirstName: "Someone", LastName: "Else", Email: "grace@example.com", Username: "grace"}
	assert.NoError(t, s.users.Create(ctx, reuse), "a soft-deleted user should release its email and username")
}

// seedCompanyAndUser inserts one company and one user in a fresh transaction and
// returns it along with their identifiers.
func (s *RepositorySuite) seedCompanyAndUser(t *testing.T) (ctx context.Context, companyID, userID uuid.UUID) {
	t.Helper()

	ctx = s.txContext(t)

	company := &domain.Company{Name: "Host", SeatLimit: 3}
	require.NoError(t, s.companies.Create(ctx, company))

	user := &domain.User{FirstName: "Alan", LastName: "Turing", Email: "alan@example.com", Username: "alan"}
	require.NoError(t, s.users.Create(ctx, user))

	return ctx, company.ID, user.ID
}

func (s *RepositorySuite) TestMembership_ForeignKeysAreTranslated() {
	tests := []struct {
		name string
		// build picks which side of the association is dangling.
		build   func(companyID, userID uuid.UUID) domain.Membership
		wantErr error
	}{
		{
			name: "unknown user",
			build: func(companyID, _ uuid.UUID) domain.Membership {
				return domain.Membership{UserID: uuidNotInDatabase, CompanyID: companyID, Role: domain.RoleMember}
			},
			wantErr: domain.ErrUserNotFound,
		},
		{
			name: "unknown company",
			build: func(_, userID uuid.UUID) domain.Membership {
				return domain.Membership{UserID: userID, CompanyID: uuidNotInDatabase, Role: domain.RoleMember}
			},
			wantErr: domain.ErrCompanyNotFound,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			// One transaction per subtest: the first foreign-key violation aborts
			// it, and anything run afterwards would fail with 25P02 instead.
			ctx, companyID, userID := s.seedCompanyAndUser(t)

			membership := tt.build(companyID, userID)
			// The schema does the checking; the repository only names the failure.
			assert.ErrorIs(t, s.memberships.Add(ctx, &membership), tt.wantErr)
		})
	}
}

func (s *RepositorySuite) TestMembership_Lifecycle() {
	t := s.T()
	ctx, companyID, userID := s.seedCompanyAndUser(t)

	valid := domain.Membership{UserID: userID, CompanyID: companyID, Role: domain.RoleOwner}
	require.NoError(t, s.memberships.Add(ctx, &valid))

	count, err := s.memberships.CountByCompany(ctx, companyID)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	members, err := s.users.ListByCompany(ctx, companyID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "alan", members[0].Username)

	require.NoError(t, s.memberships.Remove(ctx, companyID, userID))
	assert.ErrorIs(t, s.memberships.Remove(ctx, companyID, userID), domain.ErrMembershipNotFound)
}

// TestMembership_DuplicateIsRejected relies on the composite primary key rather
// than on a prior SELECT, which would be a read-then-write race.
func (s *RepositorySuite) TestMembership_DuplicateIsRejected() {
	t := s.T()
	ctx, companyID, userID := s.seedCompanyAndUser(t)

	valid := domain.Membership{UserID: userID, CompanyID: companyID, Role: domain.RoleOwner}
	require.NoError(t, s.memberships.Add(ctx, &valid))
	assert.ErrorIs(t, s.memberships.Add(ctx, &valid), domain.ErrMembershipExists)
}

// TestRepositories_WorkWithoutATransaction is the point of the Executor
// indirection: the same code that ran inside a transaction above also runs in
// autocommit, so single-statement operations never need a BEGIN.
func (s *RepositorySuite) TestRepositories_WorkWithoutATransaction() {
	t := s.T()
	ctx := t.Context()

	company := &domain.Company{Name: "autocommit-" + t.Name(), SeatLimit: 2}
	require.NoError(t, s.companies.Create(ctx, company))
	t.Cleanup(func() {
		// Detached: t.Context() is cancelled before cleanup runs, so passing it
		// here would make the DELETE a silent no-op.
		_, _ = s.pg.Pool.Exec(context.WithoutCancel(ctx), `DELETE FROM companies WHERE id = $1`, company.ID)
	})

	got, err := s.companies.GetByID(ctx, company.ID)
	require.NoError(t, err)
	assert.Equal(t, company.ID, got.ID)
}
