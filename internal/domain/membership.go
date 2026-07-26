package domain

import "github.com/google/uuid"

// Roles a member can hold within a company.
const (

	// RoleOwner represents the role assigned to a user who owns a company or organization.
	RoleOwner = "owner"

	// RoleMember represents the default role assigned to a user within a company or organization.
	RoleMember = "member"
)

// Membership associates a user with a company.
type Membership struct {
	UserID    uuid.UUID `json:"user_id"`
	CompanyID uuid.UUID `json:"company_id"`
	Role      string    `json:"role"`
}

// Onboarding is the result of creating a company, its first user, and the membership tying them together.
type Onboarding struct {
	Company    Company    `json:"company"`
	Owner      User       `json:"owner"`
	Membership Membership `json:"membership"`
}
