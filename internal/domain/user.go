package domain

import (
	"time"

	"github.com/google/uuid"
)

// User is a person who may belong to one or more companies.
type User struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
