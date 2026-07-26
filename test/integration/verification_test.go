package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/internal/domain"
)

const (
	// providerDelay stands in for a real third party answering in a couple of
	// seconds. Long enough that a lock held across it is unmistakable, short
	// enough not to slow the suite down.
	providerDelay = 1500 * time.Millisecond

	// settleDelay lets the verification reach its network call before the
	// concurrent writer starts timing.
	settleDelay = 300 * time.Millisecond

	// blockedThreshold is the line between "waited for a lock" and "did not".
	// Deliberately far from both outcomes so the test is not timing-sensitive.
	blockedThreshold = time.Second
)

// TestVerifyBad_HoldsTheLockAcrossTheProviderCall is the central measurement of
// this repository.
//
// The article says a transaction that spans a network call holds its locks for
// the whole round trip, and that every other writer of that row waits. This test
// puts a number on it: a concurrent UPDATE on the same company is blocked for
// essentially the entire provider delay.
func TestVerifyBad_HoldsTheLockAcrossTheProviderCall(t *testing.T) {
	provider := newSlowProvider(t, providerDelay)
	s := newStack(t, provider)

	company := s.newCompany(t, "Held Hostage", 3)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := s.verification.VerifyBad(context.Background(), company.ID)
		assert.NoError(t, err)
	}()

	// Let the verification take its lock and start talking to the provider.
	time.Sleep(settleDelay)

	blocked := s.timeConcurrentUpdate(t, company.ID)
	wg.Wait()

	t.Logf("concurrent UPDATE waited %v while the provider took %v", blocked, providerDelay)

	assert.Greater(t, blocked, blockedThreshold,
		"the UPDATE should have waited for the lock held across the provider call")
}

// TestVerifyGood_HoldsNothingAcrossTheProviderCall is the same measurement on
// the corrected path. Same provider, same delay, same concurrent writer — and no
// waiting, because the transaction never spans the network call.
func TestVerifyGood_HoldsNothingAcrossTheProviderCall(t *testing.T) {
	provider := newSlowProvider(t, providerDelay)
	s := newStack(t, provider)

	company := s.newCompany(t, "Free To Write", 3)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := s.verification.VerifyGood(context.Background(), company.ID)
		assert.NoError(t, err)
	}()

	time.Sleep(settleDelay)

	blocked := s.timeConcurrentUpdate(t, company.ID)
	wg.Wait()

	t.Logf("concurrent UPDATE waited %v while the provider took %v", blocked, providerDelay)

	assert.Less(t, blocked, blockedThreshold,
		"nothing should be held while the provider is being called")

	// Both endpoints do the same work and take the same wall time. Only one of
	// them makes everyone else wait.
	verified, err := s.companies.GetByID(context.Background(), company.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.VerificationVerified, verified.VerificationStatus)
}

// TestVerifyGood_IsIdempotentUnderConcurrency covers what replaces the lock on
// the corrected path.
//
// Several callers race. They all read "pending", they all call the provider, and
// they all attempt the write — yet exactly one succeeds, because the predicate on
// the UPDATE matches only once. No lock, no lost update, no double verification.
func TestVerifyGood_IsIdempotentUnderConcurrency(t *testing.T) {
	provider := newSlowProvider(t, 50*time.Millisecond)
	s := newStack(t, provider)

	company := s.newCompany(t, "Contended", 3)

	const racers = 6

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		conflicts int
		other     []error
	)

	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release everyone at once

			_, err := s.verification.VerifyGood(context.Background(), company.ID)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errorIs(err, domain.ErrVerificationConflict):
				conflicts++
			default:
				other = append(other, err)
			}
		}()
	}

	close(start)
	wg.Wait()

	require.Empty(t, other, "no caller should fail for any other reason")
	assert.Equal(t, 1, succeeded, "exactly one caller should verify the company")
	assert.Equal(t, racers-1, conflicts, "every other caller should see a conflict")

	// And the row carries exactly one reference — the winner's.
	verified, err := s.companies.GetByID(context.Background(), company.ID)
	require.NoError(t, err)
	require.NotNil(t, verified.VerificationRef)
	assert.Equal(t, domain.VerificationVerified, verified.VerificationStatus)
}

// TestVerifyBad_ProviderFailureRollsBack: when the third party fails mid
// transaction, the lock is released and nothing is written.
func TestVerifyBad_ProviderFailureRollsBack(t *testing.T) {
	provider := newSlowProvider(t, 10*time.Millisecond)
	s := newStack(t, provider)
	provider.server.Close() // the provider is down before we even start

	company := s.newCompany(t, "Unreachable Provider", 3)

	_, err := s.verification.VerifyBad(context.Background(), company.ID)
	require.ErrorIs(t, err, domain.ErrVerificationUnavailable)

	// The row is untouched, and — more importantly — writable again straight
	// away, which is only true if the transaction was properly rolled back.
	blocked := s.timeConcurrentUpdate(t, company.ID)
	assert.Less(t, blocked, blockedThreshold, "the lock should have been released")

	after, err := s.companies.GetByID(context.Background(), company.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.VerificationPending, after.VerificationStatus)
}
