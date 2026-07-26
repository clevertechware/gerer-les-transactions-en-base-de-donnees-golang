package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

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

// txContext returns a context carrying an open transaction, rolled back when the
// test ends.
func (s *RepositorySuite) txContext(t *testing.T) context.Context {
	t.Helper()

	ctx := t.Context()
	tx, err := s.pg.Pool.Begin(ctx)
	s.Require().NoError(err, "beginning transaction")

	t.Cleanup(func() {
		// Detached from ctx: the test context is cancelled before cleanup runs.
		s.Require().NoError(tx.Rollback(context.WithoutCancel(ctx)), "rolling back")
	})

	return contextWithTx(ctx, tx)
}
