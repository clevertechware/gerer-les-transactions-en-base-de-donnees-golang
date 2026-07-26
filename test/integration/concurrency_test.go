package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/service"
)

// errorIs is a small wrapper so the tests read as prose.
func errorIs(err, target error) bool { return errors.Is(err, target) }

// TestSeatLimit_HoldsUnderConcurrency is the invariant the schema cannot express:
// a count on one table compared against a column on another.
//
// Several callers race for a single free seat. Under READ COMMITTED they would
// all read "zero members, one seat free" and all insert. SERIALIZABLE makes
// PostgreSQL abort the losers with 40001, the manager replays them, and on the
// replay they see the seat is gone.
func TestSeatLimit_HoldsUnderConcurrency(t *testing.T) {
	provider := newSlowProvider(t, time.Millisecond)
	s := newStack(t, provider)

	company := s.newCompany(t, "One Seat Only", 1)

	const racers = 6

	users := make([]*domain.User, racers)
	for i := range users {
		users[i] = s.newUser(t, "racer"+string(rune('a'+i)))
	}

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		refused   int
		other     []error
	)

	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			_, err := s.membership.AddMember(context.Background(), company.ID, users[i].ID, domain.RoleMember)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errorIs(err, domain.ErrSeatLimitReached):
				refused++
			default:
				other = append(other, err)
			}
		}()
	}

	close(start)
	wg.Wait()

	t.Logf("serialization retries: %d", s.txManager.SerializationRetries())

	require.Empty(t, other, "no caller should fail for an unexpected reason")
	assert.Equal(t, 1, succeeded, "exactly one caller should take the seat")
	assert.Equal(t, racers-1, refused)

	// The invariant, checked against the database rather than against our counters.
	count, err := s.memberships.CountByCompany(context.Background(), company.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "the seat limit must never be exceeded")
}

// TestExecuteSerializable_ReplaysARealSerializationFailure forces the exact
// interleaving rather than hoping for it, so the retry path runs against a real
// PostgreSQL every time instead of whenever the scheduler cooperates.
//
// The shape required is write skew: two transactions that each read what the
// other is about to write. One transaction reading a range that another later
// writes into is *not* enough — that schedule is still serializable, and
// PostgreSQL rightly lets both commit.
//
// So both transactions read the member count, a barrier holds them until both
// reads are done, and only then do they insert. Neither can be ordered before
// the other, PostgreSQL aborts one with 40001, and the manager replays it.
func TestExecuteSerializable_ReplaysARealSerializationFailure(t *testing.T) {
	provider := newSlowProvider(t, time.Millisecond)
	s := newStack(t, provider)

	company := s.newCompany(t, "Write Skew", 10)
	candidates := []*domain.User{s.newUser(t, "first"), s.newUser(t, "second")}

	ctx := context.Background()

	// bothHaveRead releases once each transaction has taken its snapshot of the
	// count, guaranteeing the reads interleave with the writes.
	var bothHaveRead sync.WaitGroup
	bothHaveRead.Add(len(candidates))

	var (
		mu       sync.Mutex
		attempts int
		wg       sync.WaitGroup
	)

	for i, candidate := range candidates {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Only the first attempt waits at the barrier; a replay must not.
			var once sync.Once

			err := s.txManager.ExecuteSerializable(ctx, func(txCtx context.Context) error {
				mu.Lock()
				attempts++
				mu.Unlock()

				if _, err := s.memberships.CountByCompany(txCtx, company.ID); err != nil {
					return err
				}

				once.Do(func() {
					bothHaveRead.Done()
					bothHaveRead.Wait()
				})

				return s.memberships.Add(txCtx, &domain.Membership{
					UserID: candidate.ID, CompanyID: company.ID, Role: domain.RoleMember,
				})
			})
			assert.NoError(t, err, "transaction %d should succeed, on a replay if need be", i)
		}()
	}

	wg.Wait()

	t.Logf("attempts: %d, serialization retries: %d", attempts, s.txManager.SerializationRetries())

	assert.Positive(t, s.txManager.SerializationRetries(),
		"PostgreSQL should have aborted one of the two transactions with 40001")
	assert.Greater(t, attempts, len(candidates),
		"an aborted transaction should have been replayed")

	// Both memberships exist: the retry did its job, nothing was lost.
	count, err := s.memberships.CountByCompany(ctx, company.ID)
	require.NoError(t, err)
	assert.Equal(t, len(candidates), count)
}

// TestOnboarding_LeavesNothingBehindWhenItFails is the atomicity claim end to
// end: two inserts succeed, the third fails, and the database looks as though
// none of them ever happened.
func TestOnboarding_LeavesNothingBehindWhenItFails(t *testing.T) {
	provider := newSlowProvider(t, time.Millisecond)
	s := newStack(t, provider)

	ctx := context.Background()

	// An existing user, so the second onboarding trips the unique index on email.
	existing := s.newUser(t, "taken")

	_, err := s.onboarding.Execute(ctx, service.OnboardingInput{
		Company: domain.Company{Name: "Doomed SARL"},
		Owner: domain.User{
			FirstName: "Another", LastName: "Person",
			Email: existing.Email, Username: "different",
		},
	})
	require.ErrorIs(t, err, domain.ErrEmailAlreadyExists)

	// The company insert had already succeeded inside the transaction.
	companies, err := s.companies.List(ctx)
	require.NoError(t, err)
	for _, c := range companies {
		assert.NotEqual(t, "Doomed SARL", c.Name,
			"the company must have gone away with the rolled-back transaction")
	}

	// And no stray membership either.
	var memberships int
	require.NoError(t, s.pg.Pool.QueryRow(ctx, `SELECT count(*) FROM user_companies`).Scan(&memberships))
	assert.Zero(t, memberships)
}

// TestOnboarding_CommitsAllThreeRows is the other half: when it works, all three
// rows are there.
func TestOnboarding_CommitsAllThreeRows(t *testing.T) {
	provider := newSlowProvider(t, time.Millisecond)
	s := newStack(t, provider)

	ctx := context.Background()

	result, err := s.onboarding.Execute(ctx, service.OnboardingInput{
		Company: domain.Company{Name: "Clevertechware", SeatLimit: 5},
		Owner: domain.User{
			FirstName: "Ada", LastName: "Lovelace",
			Email: "ada@example.com", Username: "ada",
		},
	})
	require.NoError(t, err)

	company, err := s.companies.GetByID(ctx, result.Company.ID)
	require.NoError(t, err)
	assert.Equal(t, "Clevertechware", company.Name)

	owner, err := s.users.GetByID(ctx, result.Owner.ID)
	require.NoError(t, err)
	assert.Equal(t, "ada", owner.Username)

	memberships, err := s.memberships.ListByCompany(ctx, result.Company.ID)
	require.NoError(t, err)
	require.Len(t, memberships, 1)
	assert.Equal(t, domain.RoleOwner, memberships[0].Role)
}

// TestReport_AgreesWithItselfWhileTheDataChanges is the read-side claim: the
// three queries behind a report share one snapshot, so the member list and the
// count cannot contradict each other even while members are being added.
func TestReport_AgreesWithItselfWhileTheDataChanges(t *testing.T) {
	provider := newSlowProvider(t, time.Millisecond)
	s := newStack(t, provider)

	ctx := context.Background()

	company := s.newCompany(t, "Busy", 50)
	for i := range 3 {
		user := s.newUser(t, "member"+string(rune('a'+i)))
		require.NoError(t, s.memberships.Add(ctx, &domain.Membership{
			UserID: user.ID, CompanyID: company.ID, Role: domain.RoleMember,
		}))
	}

	// Keep adding members while the reports are being built.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 20 {
			select {
			case <-stop:
				return
			default:
			}

			user := &domain.User{
				FirstName: "Churn", LastName: "User",
				Email:    "churn" + string(rune('a'+i)) + "@example.com",
				Username: "churn" + string(rune('a'+i)),
			}
			if err := s.user.CreateUser(ctx, user); err != nil {
				return
			}
			_ = s.memberships.Add(ctx, &domain.Membership{
				UserID: user.ID, CompanyID: company.ID, Role: domain.RoleMember,
			})
			time.Sleep(2 * time.Millisecond)
		}
	}()

	for range 15 {
		report, err := s.report.CompanyReport(ctx, company.ID)
		require.NoError(t, err)

		// This is the assertion. Without a shared snapshot the count and the
		// list are read at different instants and drift apart.
		require.Equal(t, report.MemberCount, len(report.Members),
			"the member count and the member list must come from the same snapshot")
	}

	close(stop)
	<-done
}
