package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/config"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/logger"
)

func TestVerification_Verify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantRef string
		wantErr error
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"reference": "VRF-1", "status": "verified"}`))
			},
			wantRef: "VRF-1",
		},
		{
			name: "empty reference",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"reference": "", "status": "verified"}`))
			},
			wantErr: domain.ErrVerificationUnavailable,
		},
		{
			name: "non-200 status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: domain.ErrVerificationUnavailable,
		},
		{
			name: "undecodable body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`not json`))
			},
			wantErr: domain.ErrVerificationUnavailable,
		},
		{
			name: "slower than the configured timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(50 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			},
			wantErr: domain.ErrVerificationUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)

			gw := NewVerification(config.Remote{BaseURL: server.URL, Timeout: 10 * time.Millisecond}, logger.NewNoOpLogger())

			ref, err := gw.Verify(t.Context(), "Clevertechware")

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRef, ref)
		})
	}
}

// TestVerification_Verify_ContextCancellation is not a table case: it needs a
// context cancelled before the request even starts, which the timeout-driven
// cases above cannot express.
func TestVerification_Verify_ContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	gw := NewVerification(config.Remote{BaseURL: server.URL, Timeout: time.Second}, logger.NewNoOpLogger())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := gw.Verify(ctx, "Clevertechware")
	assert.ErrorIs(t, err, domain.ErrVerificationUnavailable)
	assert.ErrorIs(t, err, context.Canceled)
}
