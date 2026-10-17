package users

import "time"

// IdentityType describes a user's declared campus identity.
type IdentityType string

const (
	IdentityStudent  IdentityType = "student"
	IdentityFaculty  IdentityType = "faculty"
	IdentityResident IdentityType = "resident"
	IdentityVendor   IdentityType = "vendor"
)

type Role string

const (
	RoleUser   Role = "user"
	RoleVendor Role = "vendor"
	RoleAdmin  Role = "admin"
)

type Status string

const (
	StatusActive  Status = "active"
	StatusBanned  Status = "banned"
	StatusDeleted Status = "deleted"
)

type VerificationStatus string

const (
	VerificationUnverified VerificationStatus = "unverified"
	VerificationPending    VerificationStatus = "pending"
	VerificationVerified   VerificationStatus = "verified"
)

// User contains a profile and its server-managed account state.
// Credential and private identity fields are excluded from JSON.
type User struct {
	ID                            int64              `json:"id"`
	FirstName                     string             `json:"first_name"`
	LastName                      string             `json:"last_name"`
	DisplayName                   string             `json:"display_name"`
	Email                         string             `json:"email"`
	EmailVerified                 bool               `json:"email_verified"`
	InstitutionID                 *int64             `json:"institution_id,omitempty"`
	IdentityType                  *IdentityType      `json:"identity_type,omitempty"`
	Role                          Role               `json:"role"`
	Status                        Status             `json:"status"`
	BanReason                     string             `json:"ban_reason,omitempty"`
	BannedUntil                   *time.Time         `json:"banned_until,omitempty"`
	DeletedAt                     *time.Time         `json:"deleted_at,omitempty"`
	EmailVerificationStatus       VerificationStatus `json:"email_verification_status"`
	VendorVerificationStatus      VerificationStatus `json:"vendor_verification_status"`
	CampusEmailVerificationStatus VerificationStatus `json:"campus_email_verification_status"`
	CampusEmailVerifiedAt         *time.Time         `json:"campus_email_verified_at,omitempty"`
	Rating                        *float64           `json:"rating,omitempty"`
	ReviewCount                   int64              `json:"review_count"`
	Location                      string             `json:"location,omitempty"`
	Avatar                        string             `json:"avatar,omitempty"`
	CreatedAt                     time.Time          `json:"created_at"`
	UpdatedAt                     time.Time          `json:"updated_at"`

	PasswordHash                     string     `json:"-"`
	StudentNumber                    string     `json:"-"`
	VerifyCodeHash                   string     `json:"-"`
	VerifyCodeExpires                *time.Time `json:"-"`
	VerifyAttempts                   int64      `json:"-"`
	PasswordResetTokenHash           string     `json:"-"`
	PasswordResetExpiresAt           *time.Time `json:"-"`
	TokenVersion                     int64      `json:"-"`
	CampusEmail                      string     `json:"-"`
	CampusEmailVerificationTokenHash string     `json:"-"`
	CampusEmailVerificationExpiresAt *time.Time `json:"-"`
}

// ProfileInput contains fields editable through the profile endpoint.
// Verification, account permissions, and identity claims use separate workflows.
type ProfileInput struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Location    string `json:"location,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
}

// Input contains creation fields; identity declarations do not grant permissions.
type Input struct {
	ProfileInput
	Password      string       `json:"password"`
	IdentityType  IdentityType `json:"identity_type"`
	InstitutionID *int64       `json:"institution_id,omitempty"`
	StudentNumber string       `json:"student_number,omitempty"`
	CampusEmail   string       `json:"campus_email,omitempty"`
}

// CreateParams holds validated fields and a hash for persistence.
type CreateParams struct {
	Profile       ProfileInput
	IdentityType  IdentityType
	InstitutionID *int64
	StudentNumber string `json:"-"`
	CampusEmail   string `json:"-"`
	PasswordHash  string `json:"-"`
}

type Page struct {
	Users  []User `json:"users"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}
