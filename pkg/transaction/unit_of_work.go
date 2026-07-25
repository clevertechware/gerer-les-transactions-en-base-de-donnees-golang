// Package transaction defines the transaction boundary contract, kept free of
// any database driver so the service layer never sees a *pgx.Tx.
package transaction

import "context"

// UnitOfWork is a piece of work that must succeed or fail as a whole.
// It receives a context carrying the open transaction.
type UnitOfWork func(ctx context.Context) error

// Manager runs a UnitOfWork inside a transaction.
//
// There is deliberately no method to obtain the underlying transaction: a
// service decides *whether* work is transactional, never *how*.
type Manager interface {
	// Execute runs unitOfWork in a read-write transaction at the server's
	// default isolation level. Use it when a business invariant spans several
	// writes — and only then. A single statement is already atomic.
	Execute(ctx context.Context, unitOfWork UnitOfWork) error

	// ExecuteReadOnly runs unitOfWork in a read-only transaction whose reads all
	// see the same snapshot. Worth it when several reads must agree with each
	// other, or to route the work to a replica. A single SELECT does not need it.
	ExecuteReadOnly(ctx context.Context, unitOfWork UnitOfWork) error

	// ExecuteSerializable runs unitOfWork under SERIALIZABLE isolation, replaying
	// it when PostgreSQL aborts with a serialization failure. Use it when a
	// decision is made from a read that a concurrent write could invalidate.
	ExecuteSerializable(ctx context.Context, unitOfWork UnitOfWork) error
}
