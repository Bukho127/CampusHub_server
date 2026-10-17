package users

import "time"

// User is a stored user profile.
type User struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Input contains user creation fields. Password is never stored directly.
type Input struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ProfileInput contains the fields editable without changing credentials.
type ProfileInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// CreateParams contains validated profile fields and a hashed password for storage.
type CreateParams struct {
	Name         string
	Email        string
	PasswordHash string `json:"-"`
}

// Page is a bounded page of user profiles.
type Page struct {
	Users  []User `json:"users"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}
