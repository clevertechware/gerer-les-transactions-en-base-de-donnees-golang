package postgres

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
)

func (s *RepositorySuite) TestUser_GetByID() {
	t := s.T()
	ctx := s.txContext(t)

	user := &domain.User{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Username: "ada"}
	require.NoError(t, s.users.Create(ctx, user))

	got, err := s.users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "ada", got.Username)
}

func (s *RepositorySuite) TestUser_GetByID_NotFound() {
	t := s.T()
	ctx := s.txContext(t)

	_, err := s.users.GetByID(ctx, uuidNotInDatabase)
	assert.ErrorIs(t, err, domain.ErrUserNotFound)
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
	t := s.T()
	ctx := s.txContext(t)

	user := &domain.User{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Username: "ada"}
	require.NoError(t, s.users.Create(ctx, user))

	user.FirstName = "Augusta"
	user.Email = "augusta@example.com"
	require.NoError(t, s.users.Update(ctx, user))

	got, err := s.users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "Augusta", got.FirstName)
	assert.Equal(t, "augusta@example.com", got.Email)
}

func (s *RepositorySuite) TestUser_Update_NotFound() {
	t := s.T()
	ctx := s.txContext(t)

	missing := &domain.User{
		ID: uuidNotInDatabase, FirstName: "Ghost", LastName: "User",
		Email: "ghost@example.com", Username: "ghost",
	}
	assert.ErrorIs(t, s.users.Update(ctx, missing), domain.ErrUserNotFound)
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

func (s *RepositorySuite) TestUser_Delete_NotFound() {
	t := s.T()
	ctx := s.txContext(t)

	assert.ErrorIs(t, s.users.Delete(ctx, uuidNotInDatabase), domain.ErrUserNotFound)
}
