package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/service/mocks"
	txmocks "github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction/mocks"
)

// TestMembership_RemoveMember uses a bare transaction manager mock with no
// expectations set: unlike AddMember, removing a member is a single DELETE, so
// any call into the manager would fail the test.
func TestMembership_RemoveMember(t *testing.T) {
	t.Parallel()

	companyID, userID := uuid.New(), uuid.New()

	tests := []struct {
		name    string
		repoErr error
	}{
		{name: "success"},
		{name: "not found", repoErr: domain.ErrMembershipNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			memberships := mocks.NewMembershipRepository(t)
			memberships.EXPECT().Remove(mock.Anything, companyID, userID).Return(tt.repoErr).Once()

			svc := NewMembership(txmocks.NewManager(t), mocks.NewCompanyRepository(t), memberships, logger.NewNoOpLogger())
			err := svc.RemoveMember(t.Context(), companyID, userID)

			assert.ErrorIs(t, err, tt.repoErr)
		})
	}
}
