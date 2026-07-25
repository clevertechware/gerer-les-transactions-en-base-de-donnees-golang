package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Constraint names from the migrations. Translating on the name rather than on
// the SQLSTATE alone is what lets a single 23505 become either
// ErrEmailAlreadyExists or ErrUsernameAlreadyExists.
const (
	constraintUsersEmail       = "users_email_key"
	constraintUsersUsername    = "users_username_key"
	constraintMembershipPK     = "company_user_pk"
	constraintMembershipUserFK = "user_companies_user_id_fkey"
	constraintMembershipCoFK   = "user_companies_company_id_fkey"
)

// scanner is satisfied by both pgx.Row and pgx.Rows, so a single scan helper
// serves single-row and multi-row queries.
type scanner interface {
	Scan(dest ...any) error
}

// pgError extracts the driver error, when there is one.
func pgError(err error) (*pgconn.PgError, bool) {
	return errors.AsType[*pgconn.PgError](err)
}
