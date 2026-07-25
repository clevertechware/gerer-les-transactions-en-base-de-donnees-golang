// Package integration holds the tests that demonstrate the article's claims.
//
// Everything here needs a real PostgreSQL and real concurrency: locks, snapshots
// and serialization failures are behaviours of the database, and a mock can only
// ever restate what we already believe about them.
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/config"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/domain"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/gateway"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/logger"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/postgres"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/service"
	"github.com/clevertechware/handling-db-transactions-in-golang/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithPostgres(m))
}

// slowProvider stands in for cmd/remote: it answers after delay, and counts how
// many times it was called.
type slowProvider struct {
	server *httptest.Server
	calls  atomic.Int64
}

func newSlowProvider(t *testing.T, delay time.Duration) *slowProvider {
	t.Helper()

	p := &slowProvider{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.calls.Add(1)

		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"reference": "VRF-" + uuid.NewString(),
			"status":    "verified",
		})
	}))
	t.Cleanup(p.server.Close)

	return p
}

// stack is the application wired for a test, minus the HTTP layer.
type stack struct {
	pg *testutil.Postgres

	txManager    *postgres.TxManager
	companies    *postgres.CompanyRepository
	users        *postgres.UserRepository
	memberships  *postgres.MembershipRepository
	company      *service.Company
	user         *service.User
	onboarding   *service.Onboarding
	membership   *service.Membership
	report       *service.Report
	verification *service.Verification
}

// newStack builds the dependency graph exactly as cmd/server does, pointed at
// the given provider.
func newStack(t *testing.T, provider *slowProvider) *stack {
	t.Helper()

	pg := testutil.Shared(t)
	testutil.Truncate(t, pg)

	log := logger.NewNoOpLogger()
	txManager := postgres.NewTxManager(log, pg.Pool)

	companies := postgres.NewCompanyRepository(txManager, log)
	users := postgres.NewUserRepository(txManager, log)
	memberships := postgres.NewMembershipRepository(txManager, log)

	verificationGateway := gateway.NewVerification(config.Remote{
		BaseURL: provider.server.URL,
		Timeout: 30 * time.Second,
	}, log)

	return &stack{
		pg:           pg,
		txManager:    txManager,
		companies:    companies,
		users:        users,
		memberships:  memberships,
		company:      service.NewCompany(companies, log),
		user:         service.NewUser(users, log),
		onboarding:   service.NewOnboarding(txManager, companies, users, memberships, log),
		membership:   service.NewMembership(txManager, companies, memberships, log),
		report:       service.NewReport(txManager, companies, users, memberships, log),
		verification: service.NewVerification(txManager, companies, verificationGateway, log),
	}
}

// newCompany inserts a pending company and returns it.
func (s *stack) newCompany(t *testing.T, name string, seatLimit int) *domain.Company {
	t.Helper()

	company := &domain.Company{Name: name, SeatLimit: seatLimit}
	require.NoError(t, s.company.CreateCompany(context.Background(), company))
	return company
}

// newUser inserts a user and returns it.
func (s *stack) newUser(t *testing.T, username string) *domain.User {
	t.Helper()

	user := &domain.User{
		FirstName: "Test", LastName: username,
		Email: username + "@example.com", Username: username,
	}
	require.NoError(t, s.user.CreateUser(context.Background(), user))
	return user
}

// timeConcurrentUpdate measures how long an UPDATE on the given company has to
// wait. It runs on its own connection, so what it measures is contention on the
// row, nothing else.
func (s *stack) timeConcurrentUpdate(t *testing.T, companyID uuid.UUID) time.Duration {
	t.Helper()

	start := time.Now()
	_, err := s.pg.Pool.Exec(context.Background(),
		`UPDATE companies SET address = 'touched by someone else' WHERE id = $1`, companyID)
	elapsed := time.Since(start)

	require.NoError(t, err)
	return elapsed
}
