package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/testutil"
)

// uuidNotInDatabase is a well-formed identifier that no row will ever carry,
// for exercising not-found and foreign-key paths.
var uuidNotInDatabase = uuid.MustParse("00000000-0000-4000-8000-000000000000")

// RepositorySuite exercises the repositories against a real PostgreSQL.
//
// Isolation comes from a transaction per test that is always rolled back: fast,
// and it leaves nothing behind for the next test to trip over.
type RepositorySuite struct {
	suite.Suite

	pg          *testutil.Postgres
	txManager   *TxManager
	companies   *CompanyRepository
	users       *UserRepository
	memberships *MembershipRepository
}

func TestRepositorySuite(t *testing.T) {
	suite.Run(t, new(RepositorySuite))
}

func (s *RepositorySuite) SetupSuite() {
	s.pg = testutil.Shared(s.T())

	log := logger.NewNoOpLogger()
	s.txManager = NewTxManager(log, s.pg.Pool)
	s.companies = NewCompanyRepository(s.txManager, log)
	s.users = NewUserRepository(s.txManager, log)
	s.memberships = NewMembershipRepository(s.txManager, log)
}

// txContext returns a context carrying an open transaction, rolled back when the test ends.
func (s *RepositorySuite) txContext(t *testing.T) context.Context {
	t.Helper()

	ctx := t.Context()
	tx, err := s.pg.Pool.Begin(ctx)
	s.Require().NoError(err, "beginning transaction")

	t.Cleanup(func() {
		// Detached from ctx: the test context is cancelled before cleanup runs.
		s.Require().NoError(tx.Rollback(context.WithoutCancel(ctx)), "rolling back")
	})

	return contextWithTx(ctx, tx, pgx.TxOptions{})
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

// TestRepositories_WorkWithoutATransaction is the point of the Executor
// indirection: the same code that runs inside a transaction elsewhere also runs
// in autocommit, so single-statement operations never need a BEGIN. It spans all
// three repositories because that guarantee is about the suite as a whole, not
// any one of them.
func (s *RepositorySuite) TestRepositories_WorkWithoutATransaction() {
	t := s.T()
	ctx := t.Context()

	company := &domain.Company{Name: "autocommit-" + t.Name(), SeatLimit: 2}
	require.NoError(t, s.companies.Create(ctx, company))
	user := &domain.User{
		FirstName: "Autocommit", LastName: "User",
		Email: "autocommit-" + t.Name() + "@example.com", Username: "autocommit-" + t.Name(),
	}
	require.NoError(t, s.users.Create(ctx, user))

	t.Cleanup(func() {
		// Detached: t.Context() is cancelled before cleanup runs, so passing it
		// here would make the DELETE a silent no-op. Deleting the company and the
		// user cascades to any membership between them.
		cleanupCtx := context.WithoutCancel(ctx)
		_, _ = s.pg.Pool.Exec(cleanupCtx, `DELETE FROM companies WHERE id = $1`, company.ID)
		_, _ = s.pg.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, user.ID)
	})

	gotCompany, err := s.companies.GetByID(ctx, company.ID)
	require.NoError(t, err)
	assert.Equal(t, company.ID, gotCompany.ID)

	gotUser, err := s.users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, gotUser.ID)

	membership := domain.Membership{CompanyID: company.ID, UserID: user.ID, Role: domain.RoleMember}
	require.NoError(t, s.memberships.Add(ctx, &membership))
	require.NoError(t, s.memberships.Remove(ctx, company.ID, user.ID))
}
