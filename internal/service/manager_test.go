package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction"
	txmocks "github.com/clevertechware/gerer-les-transactions-en-base-de-donnees-golang/pkg/transaction/mocks"
)

// boundary says which transaction boundaries a mocked manager will accept.
// Restricting it turns "the service opened a transaction" into "the service
// opened *this* kind of transaction", which is the part that actually matters.
type boundary int

const (
	anyBoundary boundary = iota
	readOnlyOnly
	serializableOnly
)

// runInline executes the unit of work directly, so the code inside a transaction
// is still exercised without a database behind it.
func runInline(ctx context.Context, unitOfWork transaction.UnitOfWork) error {
	return unitOfWork(ctx)
}

// newManagerExpecting builds a transaction manager mock that only accepts the
// given boundary. Calling any other method fails the test.
func newManagerExpecting(t *testing.T, want boundary) *txmocks.Manager {
	t.Helper()

	manager := txmocks.NewManager(t)

	switch want {
	case readOnlyOnly:
		manager.EXPECT().ExecuteReadOnly(mock.Anything, mock.Anything).RunAndReturn(runInline).Once()
	case serializableOnly:
		manager.EXPECT().ExecuteSerializable(mock.Anything, mock.Anything).RunAndReturn(runInline).Once()
	case anyBoundary:
		manager.EXPECT().Execute(mock.Anything, mock.Anything).RunAndReturn(runInline).Maybe()
		manager.EXPECT().ExecuteReadOnly(mock.Anything, mock.Anything).RunAndReturn(runInline).Maybe()
		manager.EXPECT().ExecuteSerializable(mock.Anything, mock.Anything).RunAndReturn(runInline).Maybe()
	}

	return manager
}

// passThroughManager accepts any boundary and runs the work inline. Use it when
// the test is about what happens inside the transaction rather than about which
// one was opened.
func passThroughManager(t *testing.T) *txmocks.Manager {
	t.Helper()
	return newManagerExpecting(t, anyBoundary)
}
