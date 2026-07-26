package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/service/mocks"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction"
)

// The CRUD services take no transaction.Manager at all. A constructor signature
// is a stronger guarantee than any assertion: it makes "wrap this single INSERT
// in a transaction" impossible rather than merely detectable. These declarations
// stop compiling the day someone adds one.
var (
	_ func(companyRepository, logger.Logger) *Company = NewCompany
	_ func(userRepository, logger.Logger) *User       = NewUser

	// The three services that genuinely need a boundary do take one, first.
	_ func(transaction.Manager, companyRepository, userRepository, membershipRepository, logger.Logger) *Onboarding = NewOnboarding
	_ func(transaction.Manager, companyRepository, membershipRepository, logger.Logger) *Membership                 = NewMembership
	_ func(transaction.Manager, companyRepository, verificationGateway, logger.Logger) *Verification                = NewVerification
)

func TestCompany_ValidationRejectsBadInputBeforeAnyQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		company domain.Company
	}{
		{name: "empty name", company: domain.Company{Name: ""}},
		{name: "blank name", company: domain.Company{Name: "   "}},
		{name: "negative seat limit", company: domain.Company{Name: "Ok", SeatLimit: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// No expectations: reaching the repository would fail the test.
			companies := mocks.NewCompanyRepository(t)
			svc := NewCompany(companies, logger.NewNoOpLogger())

			company := tt.company
			assert.ErrorIs(t, svc.CreateCompany(t.Context(), &company), domain.ErrInvalidInput)
		})
	}
}

func TestCompany_CreateAppliesTheDefaultSeatLimit(t *testing.T) {
	t.Parallel()

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

	svc := NewCompany(companies, logger.NewNoOpLogger())

	company := domain.Company{Name: "Clevertechware"}
	require.NoError(t, svc.CreateCompany(t.Context(), &company))
	assert.Equal(t, domain.DefaultSeatLimit, company.SeatLimit)
}

func TestUser_ValidationRejectsBadInputBeforeAnyQuery(t *testing.T) {
	t.Parallel()

	valid := domain.User{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Username: "ada"}

	tests := []struct {
		name   string
		mutate func(*domain.User)
	}{
		{name: "no first name", mutate: func(u *domain.User) { u.FirstName = " " }},
		{name: "no last name", mutate: func(u *domain.User) { u.LastName = "" }},
		{name: "no username", mutate: func(u *domain.User) { u.Username = "" }},
		{name: "email without an at sign", mutate: func(u *domain.User) { u.Email = "ada.example.com" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users := mocks.NewUserRepository(t) // must never be called
			svc := NewUser(users, logger.NewNoOpLogger())

			user := valid
			tt.mutate(&user)
			assert.ErrorIs(t, svc.CreateUser(t.Context(), &user), domain.ErrInvalidInput)
		})
	}
}

// TestUser_CreateDoesNotProbeForDuplicates: checking uniqueness with a SELECT
// first would be a read-then-write race. The unique index decides, and the
// repository translates the violation.
func TestUser_CreateDoesNotProbeForDuplicates(t *testing.T) {
	t.Parallel()

	users := mocks.NewUserRepository(t)
	users.EXPECT().Create(mock.Anything, mock.Anything).Return(domain.ErrEmailAlreadyExists).Once()

	svc := NewUser(users, logger.NewNoOpLogger())

	user := domain.User{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Username: "ada"}
	assert.ErrorIs(t, svc.CreateUser(t.Context(), &user), domain.ErrEmailAlreadyExists)

	users.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything)
	users.AssertNotCalled(t, "List", mock.Anything)
}

func TestMembership_AddMember(t *testing.T) {
	t.Parallel()

	companyID, userID := uuid.New(), uuid.New()

	tests := []struct {
		name      string
		seatLimit int
		current   int
		wantErr   error
		wantAdd   bool
		wantRole  string
	}{
		{name: "seats available", seatLimit: 3, current: 1, wantAdd: true, wantRole: domain.RoleMember},
		{name: "last seat taken", seatLimit: 3, current: 2, wantAdd: true, wantRole: domain.RoleMember},
		{name: "no seat left", seatLimit: 3, current: 3, wantErr: domain.ErrSeatLimitReached},
		{name: "over the limit already", seatLimit: 1, current: 5, wantErr: domain.ErrSeatLimitReached},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			companies := mocks.NewCompanyRepository(t)
			companies.EXPECT().GetByID(mock.Anything, companyID).
				Return(&domain.Company{ID: companyID, SeatLimit: tt.seatLimit}, nil).Once()

			memberships := mocks.NewMembershipRepository(t)
			memberships.EXPECT().CountByCompany(mock.Anything, companyID).Return(tt.current, nil).Once()
			if tt.wantAdd {
				memberships.EXPECT().Add(mock.Anything, mock.Anything).Return(nil).Once()
			}

			svc := NewMembership(passThroughManager(t), companies, memberships, logger.NewNoOpLogger())

			got, err := svc.AddMember(t.Context(), companyID, userID, "")

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRole, got.Role, "an empty role should fall back to the default")
		})
	}
}

// TestMembership_AddMemberRunsUnderSerializable pins the isolation choice down.
// The count-then-insert pair is only safe because the transaction is
// serializable and replayed on conflict; running it through Execute would
// silently reintroduce the race.
func TestMembership_AddMemberRunsUnderSerializable(t *testing.T) {
	t.Parallel()

	companyID, userID := uuid.New(), uuid.New()

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().GetByID(mock.Anything, companyID).
		Return(&domain.Company{ID: companyID, SeatLimit: 2}, nil).Once()

	memberships := mocks.NewMembershipRepository(t)
	memberships.EXPECT().CountByCompany(mock.Anything, companyID).Return(0, nil).Once()
	memberships.EXPECT().Add(mock.Anything, mock.Anything).Return(nil).Once()

	manager := newManagerExpecting(t, serializableOnly)

	svc := NewMembership(manager, companies, memberships, logger.NewNoOpLogger())

	_, err := svc.AddMember(t.Context(), companyID, userID, domain.RoleMember)
	require.NoError(t, err)
}

// TestReport_RunsUnderReadOnly makes the same point for the read path.
func TestReport_RunsUnderReadOnly(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().GetByID(mock.Anything, companyID).
		Return(&domain.Company{ID: companyID, SeatLimit: 5}, nil).Once()

	users := mocks.NewUserRepository(t)
	users.EXPECT().ListByCompany(mock.Anything, companyID).
		Return([]domain.User{{Username: "ada"}, {Username: "grace"}}, nil).Once()

	memberships := mocks.NewMembershipRepository(t)
	memberships.EXPECT().CountByCompany(mock.Anything, companyID).Return(2, nil).Once()

	manager := newManagerExpecting(t, readOnlyOnly)

	svc := NewReport(manager, companies, users, memberships, logger.NewNoOpLogger())

	report, err := svc.CompanyReport(t.Context(), companyID)
	require.NoError(t, err)
	assert.Equal(t, 2, report.MemberCount)
	assert.Equal(t, 3, report.SeatsLeft)
	assert.Len(t, report.Members, 2)
}

// TestReport_SeatsLeftNeverGoesNegative covers a company whose seat limit was
// lowered below its current membership count.
func TestReport_SeatsLeftNeverGoesNegative(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()

	companies := mocks.NewCompanyRepository(t)
	companies.EXPECT().GetByID(mock.Anything, companyID).
		Return(&domain.Company{ID: companyID, SeatLimit: 1}, nil).Once()

	users := mocks.NewUserRepository(t)
	users.EXPECT().ListByCompany(mock.Anything, companyID).Return(nil, nil).Once()

	memberships := mocks.NewMembershipRepository(t)
	memberships.EXPECT().CountByCompany(mock.Anything, companyID).Return(4, nil).Once()

	svc := NewReport(passThroughManager(t), companies, users, memberships, logger.NewNoOpLogger())

	report, err := svc.CompanyReport(t.Context(), companyID)
	require.NoError(t, err)
	assert.Zero(t, report.SeatsLeft)
}
