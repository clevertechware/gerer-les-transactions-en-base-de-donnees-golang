package domain

import "github.com/google/uuid"

// Roles a member can hold within a company.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

// Membership associates a user with a company.
type Membership struct {
	UserID    uuid.UUID `json:"user_id"`
	CompanyID uuid.UUID `json:"company_id"`
	Role      string    `json:"role"`
}

// Onboarding is the result of creating a company, its first user and the
// membership tying them together. The three rows must appear together or not at
// all — a company with no owner is not a state worth persisting.
type Onboarding struct {
	Company    Company    `json:"company"`
	Owner      User       `json:"owner"`
	Membership Membership `json:"membership"`
}
