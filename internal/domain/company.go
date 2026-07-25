package domain

import (
	"time"

	"github.com/google/uuid"
)

// VerificationStatus mirrors the companies_verification_status_check constraint.
type VerificationStatus string

const (
	VerificationPending  VerificationStatus = "pending"
	VerificationVerified VerificationStatus = "verified"
	VerificationRejected VerificationStatus = "rejected"
)

// DefaultSeatLimit matches the column default in the schema.
const DefaultSeatLimit = 3

// Company is a registered organisation.
type Company struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Address *string   `json:"address,omitempty"`

	// VerificationStatus is set by the third party in cmd/remote. The corrected
	// verification path updates it conditionally, which is what makes the write
	// idempotent without taking a lock.
	VerificationStatus VerificationStatus `json:"verification_status"`
	VerificationRef    *string            `json:"verification_ref,omitempty"`
	VerifiedAt         *time.Time         `json:"verified_at,omitempty"`

	// SeatLimit caps how many members the company may have. Enforcing it means
	// reading a count and then writing — the shape that conflicts under
	// concurrency, and the reason AddMember runs SERIALIZABLE.
	SeatLimit int `json:"seat_limit"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
