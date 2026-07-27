package postgres

import (
	"context"

	"github.com/jackc/pgerrcode"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
)

// TestReadOnlyTransaction_RejectsWrite backs the article's first claim about
// explicit read-only transactions: they are a safety net PostgreSQL enforces
// itself, not a convention the application has to remember.
func (s *RepositorySuite) TestReadOnlyTransaction_RejectsWrite() {
	t := s.T()
	ctx := t.Context()

	err := s.txManager.ExecuteReadOnly(ctx, func(ctx context.Context) error {
		return s.companies.Create(ctx, &domain.Company{Name: "should never exist", SeatLimit: 1})
	})

	require.Error(t, err, "a write inside a read-only transaction must fail")

	pgErr, ok := pgError(err)
	require.True(t, ok, "expected a PostgreSQL error, got %v", err)
	require.Equal(t, pgerrcode.ReadOnlySQLTransaction, pgErr.Code,
		"PostgreSQL should refuse the write with 25006, got %s", pgErr.Code)

	// And nothing slipped through.
	var count int
	require.NoError(t, s.pg.Pool.QueryRow(ctx,
		`SELECT count(*) FROM companies WHERE name = 'should never exist'`).Scan(&count))
	require.Zero(t, count)
}

// TestReadOnlyTransaction_SeesOneSnapshot is the reason a multi-read endpoint
// deserves a transaction at all: without a frozen snapshot, two reads in the
// same request can disagree.
//
// It also pins down *which* isolation level delivers that. READ COMMITTED — the
// default, and what the first version of ExecuteReadOnly used — takes a new
// snapshot per statement, so it does not. REPEATABLE READ does. Nothing else in
// the codebase would notice the difference; this test is the only thing standing
// between the report endpoint and a silently inconsistent answer.
func (s *RepositorySuite) TestReadOnlyTransaction_SeesOneSnapshot() {
	t := s.T()
	ctx := t.Context()

	original := "snapshot-" + t.Name()
	company := &domain.Company{Name: original, SeatLimit: 5}
	require.NoError(t, s.companies.Create(ctx, company))
	t.Cleanup(func() {
		// Detached: t.Context() is already cancelled by the time cleanup runs, so
		// the DELETE would never reach the server.
		_, _ = s.pg.Pool.Exec(context.WithoutCancel(ctx),
			`DELETE FROM companies WHERE id = $1`, company.ID)
	})

	err := s.txManager.ExecuteReadOnly(ctx, func(txCtx context.Context) error {
		// The snapshot is taken here, on the first query.
		first, err := s.companies.GetByID(txCtx, company.ID)
		require.NoError(t, err)
		require.Equal(t, original, first.Name)

		// Someone else commits a change, on another connection, right now.
		_, err = s.pg.Pool.Exec(ctx,
			`UPDATE companies SET name = 'renamed by someone else' WHERE id = $1`, company.ID)
		require.NoError(t, err)

		second, err := s.companies.GetByID(txCtx, company.ID)
		require.NoError(t, err)

		require.Equal(t, original, second.Name,
			"the second read must still see the snapshot taken by the first")
		return nil
	})
	require.NoError(t, err)

	// Outside the transaction, the committed change is of course visible.
	after, err := s.companies.GetByID(ctx, company.ID)
	require.NoError(t, err)
	require.Equal(t, "renamed by someone else", after.Name)
}

// TestExecute_RollsBackEveryWrite is the atomicity claim, checked end to end
// rather than mocked: three inserts, one failure, nothing left behind.
func (s *RepositorySuite) TestExecute_RollsBackEveryWrite() {
	t := s.T()
	ctx := t.Context()

	name := "rollback-" + t.Name()
	email := "rollback-" + t.Name() + "@example.com"

	err := s.txManager.Execute(ctx, func(txCtx context.Context) error {
		company := &domain.Company{Name: name, SeatLimit: 3}
		if err := s.companies.Create(txCtx, company); err != nil {
			return err
		}

		user := &domain.User{
			FirstName: "Rollback", LastName: "Test",
			Email: email, Username: "rollback-" + t.Name(),
		}
		if err := s.users.Create(txCtx, user); err != nil {
			return err
		}

		// A membership pointing at a company that does not exist 👉 the foreign key rejects it so nothing exists.
		return s.memberships.Add(txCtx, &domain.Membership{
			UserID:    user.ID,
			CompanyID: uuidNotInDatabase,
			Role:      domain.RoleOwner,
		})
	})

	require.ErrorIs(t, err, domain.ErrCompanyNotFound)

	var companies, users int
	err = s.pg.Pool.QueryRow(ctx, `SELECT count(*) FROM companies WHERE name = $1`, name).Scan(&companies)
	require.NoError(t, err)
	require.Zero(t, companies, "the company insert should have been rolled back")
	err = s.pg.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = $1`, email).Scan(&users)
	require.NoError(t, err)
	require.Zero(t, users, "the user insert should have been rolled back")

}
