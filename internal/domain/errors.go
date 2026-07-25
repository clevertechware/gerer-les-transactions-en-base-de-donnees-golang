package domain

import "errors"

var (
	// Lookup failures.
	ErrCompanyNotFound    = errors.New("company not found")
	ErrUserNotFound       = errors.New("user not found")
	ErrMembershipNotFound = errors.New("membership not found")

	// Uniqueness failures, translated from the constraints in the schema.
	ErrEmailAlreadyExists    = errors.New("email already exists")
	ErrUsernameAlreadyExists = errors.New("username already exists")
	ErrMembershipExists      = errors.New("user already belongs to this company")

	// Business rules.
	ErrSeatLimitReached = errors.New("company has reached its seat limit")
	ErrInvalidInput     = errors.New("invalid input")

	// ErrVerificationConflict means the conditional UPDATE matched no row:
	// another execution already verified this company. It is what replaces an
	// explicit lock on the corrected verification path.
	ErrVerificationConflict = errors.New("company is no longer pending verification")

	// ErrVerificationUnavailable means the external provider could not be
	// reached, or answered with something unusable.
	ErrVerificationUnavailable = errors.New("verification provider unavailable")

	// ErrTransactionRequired is returned by the few queries that are meaningless
	// outside a transaction, such as SELECT ... FOR UPDATE.
	ErrTransactionRequired = errors.New("operation requires an open transaction")

	// ErrSerializationFailure means PostgreSQL aborted the transaction and we
	// exhausted our retries. Under SERIALIZABLE this is the contract, not a bug.
	ErrSerializationFailure = errors.New("could not serialize access")
)
