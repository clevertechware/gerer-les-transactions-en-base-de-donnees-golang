package domain

import "errors"

var (
	// Lookup failures.

	// ErrCompanyNotFound indicates that the specified company could not be found.
	ErrCompanyNotFound = errors.New("company not found")
	// ErrUserNotFound indicates that the specified user could not be found.
	ErrUserNotFound = errors.New("user not found")
	// ErrMembershipNotFound indicates that the specified membership could not be found.
	ErrMembershipNotFound = errors.New("membership not found")

	// Uniqueness failures, translated from the constraints in the schema.

	// ErrEmailAlreadyExists indicates that the given email already exists in the system.
	ErrEmailAlreadyExists = errors.New("email already exists")
	// ErrUsernameAlreadyExists indicates that the given username already exists in the system.
	ErrUsernameAlreadyExists = errors.New("username already exists")
	// ErrMembershipExists indicates that the user is already a member of the specified company.
	ErrMembershipExists = errors.New("user already belongs to this company")

	// Business rules.

	// ErrSeatLimitReached indicates that the company has reached its maximum allowed seat limit.
	ErrSeatLimitReached = errors.New("company has reached its seat limit")
	// ErrInvalidInput indicates that the provided input is invalid.
	ErrInvalidInput = errors.New("invalid input")
	// ErrVerificationConflict indicates that the company is no longer pending verification due to a conflict.
	ErrVerificationConflict = errors.New("company is no longer pending verification")
	// ErrVerificationUnavailable indicates that the verification provider could not be reached or provided unusable data.
	ErrVerificationUnavailable = errors.New("verification provider unavailable")

	// ErrTransactionRequired indicates that the operation requires an open transaction to execute.
	ErrTransactionRequired = errors.New("operation requires an open transaction")
	// ErrSerializationFailure indicates the transaction could not be serialized and all retry attempts were exhausted.
	ErrSerializationFailure = errors.New("could not serialize access")
	// ErrIsolationDowngrade indicates that the open transaction is weaker than the isolation level being asked for,
	// so joining it would silently drop the guarantee the caller requested.
	ErrIsolationDowngrade = errors.New("ambient transaction is weaker than the isolation level requested")
	// ErrAccessModeMismatch indicates that the open transaction does not run in the access mode being asked for:
	// read-only work would lose its safety net, and read-write work would be rejected at the first write.
	ErrAccessModeMismatch = errors.New("ambient transaction does not run in the access mode requested")
	// ErrTransactionAborted indicates that the server had already rolled the transaction back when the commit was
	// issued, so nothing was written.
	ErrTransactionAborted = errors.New("transaction was already aborted when the commit was issued")
	// ErrConflictAbortsTransaction indicates that a nested unit of work hit a concurrency conflict. It marks the one
	// error a caller must never swallow: the conflict invalidates the whole transaction, not only the nested scope.
	ErrConflictAbortsTransaction = errors.New("conflict with a concurrent transaction invalidates the whole transaction")
)
