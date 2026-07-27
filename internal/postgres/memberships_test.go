package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
)

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

// TestMembership_Add tests membership creation using the composite primary key
// rather than relying on a prior SELECT, which would be a read-then-write race.
func (s *RepositorySuite) TestMembership_Add() {
	tests := []struct {
		name    string
		setup   func(t *testing.T) (context.Context, *domain.Membership)
		wantErr error
	}{
		{
			name: "successfully adds new membership",
			setup: func(t *testing.T) (context.Context, *domain.Membership) {
				ctx, companyID, userID := s.seedCompanyAndUser(t)
				return ctx, &domain.Membership{UserID: userID, CompanyID: companyID, Role: domain.RoleOwner}
			},
		},
		{
			name: "rejects duplicate membership",
			setup: func(t *testing.T) (context.Context, *domain.Membership) {
				ctx, companyID, userID := s.seedCompanyAndUser(t)
				m := &domain.Membership{UserID: userID, CompanyID: companyID, Role: domain.RoleOwner}
				require.NoError(t, s.memberships.Add(ctx, m))
				return ctx, m
			},
			wantErr: domain.ErrMembershipExists,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			ctx, membership := tt.setup(t)

			err := s.memberships.Add(ctx, membership)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func (s *RepositorySuite) TestMembership_ListByCompany() {
	t := s.T()
	ctx, companyID, userID := s.seedCompanyAndUser(t)

	membership := domain.Membership{UserID: userID, CompanyID: companyID, Role: domain.RoleOwner}
	require.NoError(t, s.memberships.Add(ctx, &membership))

	got, err := s.memberships.ListByCompany(ctx, companyID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, userID, got[0].UserID)
	assert.Equal(t, domain.RoleOwner, got[0].Role)
}

func (s *RepositorySuite) TestMembership_CountByCompany() {
	tests := []struct {
		name      string
		setup     func(t *testing.T) (context.Context, uuid.UUID)
		wantCount int
	}{
		{
			name: "counts zero members for empty company",
			setup: func(t *testing.T) (context.Context, uuid.UUID) {
				ctx := s.txContext(t)
				company := &domain.Company{Name: "Emptyhanded", SeatLimit: 1}
				require.NoError(t, s.companies.Create(ctx, company))
				return ctx, company.ID
			},
			wantCount: 0,
		},
		{
			name: "counts existing members",
			setup: func(t *testing.T) (context.Context, uuid.UUID) {
				ctx, companyID, userID := s.seedCompanyAndUser(t)
				m := &domain.Membership{UserID: userID, CompanyID: companyID, Role: domain.RoleOwner}
				require.NoError(t, s.memberships.Add(ctx, m))
				return ctx, companyID
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			ctx, companyID := tt.setup(t)

			count, err := s.memberships.CountByCompany(ctx, companyID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCount, count)
		})
	}
}
