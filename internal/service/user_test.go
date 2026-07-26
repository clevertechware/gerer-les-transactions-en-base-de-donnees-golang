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
)

func TestUser_GetUser(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	want := &domain.User{ID: userID, Username: "ada"}

	tests := []struct {
		name    string
		repoErr error
	}{
		{name: "found"},
		{name: "not found", repoErr: domain.ErrUserNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users := mocks.NewUserRepository(t)
			if tt.repoErr != nil {
				users.EXPECT().GetByID(mock.Anything, userID).Return(nil, tt.repoErr).Once()
			} else {
				users.EXPECT().GetByID(mock.Anything, userID).Return(want, nil).Once()
			}

			svc := NewUser(users, logger.NewNoOpLogger())
			got, err := svc.GetUser(t.Context(), userID)

			if tt.repoErr != nil {
				assert.ErrorIs(t, err, tt.repoErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestUser_ListUsers(t *testing.T) {
	t.Parallel()

	want := []domain.User{{Username: "ada"}, {Username: "grace"}}

	tests := []struct {
		name    string
		repoErr error
	}{
		{name: "success"},
		{name: "repository failure", repoErr: assert.AnError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users := mocks.NewUserRepository(t)
			if tt.repoErr != nil {
				users.EXPECT().List(mock.Anything).Return(nil, tt.repoErr).Once()
			} else {
				users.EXPECT().List(mock.Anything).Return(want, nil).Once()
			}

			svc := NewUser(users, logger.NewNoOpLogger())
			got, err := svc.ListUsers(t.Context())

			if tt.repoErr != nil {
				assert.ErrorIs(t, err, tt.repoErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestUser_DeleteUser(t *testing.T) {
	t.Parallel()

	userID := uuid.New()

	tests := []struct {
		name    string
		repoErr error
	}{
		{name: "success"},
		{name: "not found", repoErr: domain.ErrUserNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users := mocks.NewUserRepository(t)
			users.EXPECT().Delete(mock.Anything, userID).Return(tt.repoErr).Once()

			svc := NewUser(users, logger.NewNoOpLogger())
			err := svc.DeleteUser(t.Context(), userID)

			assert.ErrorIs(t, err, tt.repoErr)
		})
	}
}

// TestUser_UpdateUser covers the same validation rules as CreateUser, since
// UpdateUser runs them before writing.
func TestUser_UpdateUser(t *testing.T) {
	t.Parallel()

	valid := domain.User{ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Username: "ada"}

	tests := []struct {
		name      string
		mutate    func(*domain.User)
		callsRepo bool
		repoErr   error
		wantErr   error
	}{
		{
			name:    "no first name",
			mutate:  func(u *domain.User) { u.FirstName = " " },
			wantErr: domain.ErrInvalidInput,
		},
		{
			name:    "no last name",
			mutate:  func(u *domain.User) { u.LastName = "" },
			wantErr: domain.ErrInvalidInput,
		},
		{
			name:    "no username",
			mutate:  func(u *domain.User) { u.Username = "" },
			wantErr: domain.ErrInvalidInput,
		},
		{
			name:    "email without an at sign",
			mutate:  func(u *domain.User) { u.Email = "ada.example.com" },
			wantErr: domain.ErrInvalidInput,
		},
		{
			name:      "success",
			callsRepo: true,
		},
		{
			name:      "repository failure propagates",
			callsRepo: true,
			repoErr:   domain.ErrEmailAlreadyExists,
			wantErr:   domain.ErrEmailAlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			user := valid
			if tt.mutate != nil {
				tt.mutate(&user)
			}

			users := mocks.NewUserRepository(t)
			if tt.callsRepo {
				users.EXPECT().Update(mock.Anything, mock.Anything).Return(tt.repoErr).Once()
			}

			svc := NewUser(users, logger.NewNoOpLogger())
			err := svc.UpdateUser(t.Context(), &user)

			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
