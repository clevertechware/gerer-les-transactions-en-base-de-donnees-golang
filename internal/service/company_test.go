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

func TestCompany_GetCompany(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()
	want := &domain.Company{ID: companyID, Name: "Clevertechware"}

	tests := []struct {
		name    string
		repoErr error
	}{
		{name: "found"},
		{name: "not found", repoErr: domain.ErrCompanyNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			companies := mocks.NewCompanyRepository(t)
			if tt.repoErr != nil {
				companies.EXPECT().GetByID(mock.Anything, companyID).Return(nil, tt.repoErr).Once()
			} else {
				companies.EXPECT().GetByID(mock.Anything, companyID).Return(want, nil).Once()
			}

			svc := NewCompany(companies, logger.NewNoOpLogger())
			got, err := svc.GetCompany(t.Context(), companyID)

			if tt.repoErr != nil {
				assert.ErrorIs(t, err, tt.repoErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestCompany_ListCompanies(t *testing.T) {
	t.Parallel()

	want := []domain.Company{{Name: "Clevertechware"}, {Name: "Acme"}}

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

			companies := mocks.NewCompanyRepository(t)
			if tt.repoErr != nil {
				companies.EXPECT().List(mock.Anything).Return(nil, tt.repoErr).Once()
			} else {
				companies.EXPECT().List(mock.Anything).Return(want, nil).Once()
			}

			svc := NewCompany(companies, logger.NewNoOpLogger())
			got, err := svc.ListCompanies(t.Context())

			if tt.repoErr != nil {
				assert.ErrorIs(t, err, tt.repoErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestCompany_DeleteCompany(t *testing.T) {
	t.Parallel()

	companyID := uuid.New()

	tests := []struct {
		name    string
		repoErr error
	}{
		{name: "success"},
		{name: "not found", repoErr: domain.ErrCompanyNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			companies := mocks.NewCompanyRepository(t)
			companies.EXPECT().Delete(mock.Anything, companyID).Return(tt.repoErr).Once()

			svc := NewCompany(companies, logger.NewNoOpLogger())
			err := svc.DeleteCompany(t.Context(), companyID)

			assert.ErrorIs(t, err, tt.repoErr)
		})
	}
}

// TestCompany_UpdateCompany covers the same validation and default-seat-limit
// rules as CreateCompany, since UpdateCompany runs both before writing.
func TestCompany_UpdateCompany(t *testing.T) {
	t.Parallel()

	valid := domain.Company{ID: uuid.New(), Name: "Clevertechware", SeatLimit: 10}

	tests := []struct {
		name          string
		mutate        func(*domain.Company)
		callsRepo     bool
		repoErr       error
		wantErr       error
		wantSeatLimit int
	}{
		{
			name:    "empty name",
			mutate:  func(c *domain.Company) { c.Name = "" },
			wantErr: domain.ErrInvalidInput,
		},
		{
			name:    "blank name",
			mutate:  func(c *domain.Company) { c.Name = "   " },
			wantErr: domain.ErrInvalidInput,
		},
		{
			name:    "negative seat limit",
			mutate:  func(c *domain.Company) { c.SeatLimit = -1 },
			wantErr: domain.ErrInvalidInput,
		},
		{
			name:          "zero seat limit falls back to the default",
			mutate:        func(c *domain.Company) { c.SeatLimit = 0 },
			callsRepo:     true,
			wantSeatLimit: domain.DefaultSeatLimit,
		},
		{
			name:          "repository failure propagates",
			callsRepo:     true,
			repoErr:       domain.ErrCompanyNotFound,
			wantErr:       domain.ErrCompanyNotFound,
			wantSeatLimit: valid.SeatLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			company := valid
			if tt.mutate != nil {
				tt.mutate(&company)
			}

			companies := mocks.NewCompanyRepository(t)
			if tt.callsRepo {
				companies.EXPECT().Update(mock.Anything, mock.Anything).Return(tt.repoErr).Once()
			}

			svc := NewCompany(companies, logger.NewNoOpLogger())
			err := svc.UpdateCompany(t.Context(), &company)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSeatLimit, company.SeatLimit)
		})
	}
}
