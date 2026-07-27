package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
)

func (s *RepositorySuite) TestUser_GetByID() {
	tests := []struct {
		name    string
		setup   func(t *testing.T, ctx context.Context) uuid.UUID
		wantErr error
		wantCheck func(t *testing.T, got *domain.User)
	}{
		{
			name: "retrieves existing user",
			setup: func(t *testing.T, ctx context.Context) uuid.UUID {
				user := &domain.User{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Username: "ada"}
				require.NoError(t, s.users.Create(ctx, user))
				return user.ID
			},
			wantCheck: func(t *testing.T, got *domain.User) {
				assert.Equal(t, "ada", got.Username)
			},
		},
		{
			name: "returns error for missing user",
			setup: func(_ *testing.T, _ context.Context) uuid.UUID {
				return uuidNotInDatabase
			},
			wantErr: domain.ErrUserNotFound,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			ctx := s.txContext(t)
			id := tt.setup(t, ctx)

			got, err := s.users.GetByID(ctx, id)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			if tt.wantCheck != nil {
				tt.wantCheck(t, got)
			}
		})
	}
}

func (s *RepositorySuite) TestUser_List() {
	t := s.T()
	ctx := s.txContext(t)

	first := &domain.User{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Username: "ada"}
	require.NoError(t, s.users.Create(ctx, first))
	deleted := &domain.User{FirstName: "Gone", LastName: "User", Email: "gone@example.com", Username: "gone"}
	require.NoError(t, s.users.Create(ctx, deleted))
	require.NoError(t, s.users.Delete(ctx, deleted.ID))

	got, err := s.users.List(ctx)
	require.NoError(t, err)

	ids := make([]uuid.UUID, 0, len(got))
	for _, user := range got {
		ids = append(ids, user.ID)
	}
	assert.Contains(t, ids, first.ID)
	assert.NotContains(t, ids, deleted.ID, "a soft-deleted user must not be listed")
}

func (s *RepositorySuite) TestUser_Update() {
	tests := []struct {
		name      string
		setup     func(t *testing.T, ctx context.Context) *domain.User
		update    func(u *domain.User)
		wantErr   error
		wantCheck func(t *testing.T, got *domain.User)
	}{
		{
			name: "successfully updates existing user",
			setup: func(t *testing.T, ctx context.Context) *domain.User {
				user := &domain.User{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Username: "ada"}
				require.NoError(t, s.users.Create(ctx, user))
				return user
			},
			update: func(u *domain.User) {
				u.FirstName = "Augusta"
				u.Email = "augusta@example.com"
			},
			wantCheck: func(t *testing.T, got *domain.User) {
				assert.Equal(t, "Augusta", got.FirstName)
				assert.Equal(t, "augusta@example.com", got.Email)
			},
		},
		{
			name: "returns error for missing user",
			setup: func(_ *testing.T, _ context.Context) *domain.User {
				return &domain.User{
					ID: uuidNotInDatabase, FirstName: "Ghost", LastName: "User",
					Email: "ghost@example.com", Username: "ghost",
				}
			},
			update:  func(_ *domain.User) {},
			wantErr: domain.ErrUserNotFound,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			ctx := s.txContext(t)
			user := tt.setup(t, ctx)
			tt.update(user)

			err := s.users.Update(ctx, user)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			got, err := s.users.GetByID(ctx, user.ID)
			require.NoError(t, err)
			if tt.wantCheck != nil {
				tt.wantCheck(t, got)
			}
		})
	}
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

func (s *RepositorySuite) TestUser_Delete() {
	tests := []struct {
		name    string
		setup   func(t *testing.T, ctx context.Context) uuid.UUID
		wantErr error
	}{
		{
			name: "soft-deletes existing user",
			setup: func(t *testing.T, ctx context.Context) uuid.UUID {
				user := &domain.User{FirstName: "Grace", LastName: "Hopper", Email: "grace@example.com", Username: "grace"}
				require.NoError(t, s.users.Create(ctx, user))
				return user.ID
			},
		},
		{
			name: "returns error for missing user",
			setup: func(_ *testing.T, _ context.Context) uuid.UUID {
				return uuidNotInDatabase
			},
			wantErr: domain.ErrUserNotFound,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			ctx := s.txContext(t)
			id := tt.setup(t, ctx)

			err := s.users.Delete(ctx, id)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}
