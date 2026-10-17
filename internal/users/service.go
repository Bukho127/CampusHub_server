package users

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/Bukho127/CampusHub_server/internal/password"
)

var (
	ErrNotFound    = errors.New("user not found")
	ErrEmailExists = errors.New("email already exists")
)

// ValidationError describes invalid user input.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Repository stores and retrieves user profiles.
type Repository interface {
	Create(context.Context, CreateParams) (User, error)
	Get(context.Context, int64) (User, error)
	List(context.Context, int, int) ([]User, error)
	Update(context.Context, int64, ProfileInput) (User, error)
	Delete(context.Context, int64) error
}

// Service validates user operations before accessing storage.
type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, input Input) (User, error) {
	profile, err := normalize(input.ProfileInput)
	if err != nil {
		return User{}, err
	}
	switch input.IdentityType {
	case IdentityStudent, IdentityFaculty, IdentityResident, IdentityVendor:
	default:
		return User{}, &ValidationError{"identity_type must be student, faculty, resident, or vendor"}
	}
	if input.InstitutionID != nil && *input.InstitutionID <= 0 {
		return User{}, &ValidationError{"institution_id must be a positive integer"}
	}
	input.StudentNumber = strings.TrimSpace(input.StudentNumber)
	if utf8.RuneCountInString(input.StudentNumber) > 128 {
		return User{}, &ValidationError{"student_number must contain at most 128 characters"}
	}
	input.CampusEmail = strings.ToLower(strings.TrimSpace(input.CampusEmail))
	if input.CampusEmail != "" && !validEmail(input.CampusEmail) {
		return User{}, &ValidationError{"campus_email must be a valid address of at most 254 bytes"}
	}
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	hash, err := password.Hash(input.Password)
	if errors.Is(err, password.ErrInvalidPassword) {
		return User{}, &ValidationError{err.Error()}
	}
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	return s.repository.Create(ctx, CreateParams{
		Profile: profile, IdentityType: input.IdentityType, InstitutionID: input.InstitutionID,
		StudentNumber: input.StudentNumber, CampusEmail: input.CampusEmail, PasswordHash: hash,
	})
}

func (s *Service) Get(ctx context.Context, id int64) (User, error) {
	if id <= 0 {
		return User{}, &ValidationError{"id must be a positive integer"}
	}
	return s.repository.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, limit, offset int) (Page, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 2147483647 {
		return Page{}, &ValidationError{"limit must be between 1 and 100; offset must be between 0 and 2147483647"}
	}
	profiles, err := s.repository.List(ctx, limit, offset)
	if err != nil {
		return Page{}, err
	}
	if profiles == nil {
		profiles = []User{}
	}
	return Page{Users: profiles, Limit: limit, Offset: offset}, nil
}

func (s *Service) Update(ctx context.Context, id int64, input ProfileInput) (User, error) {
	if id <= 0 {
		return User{}, &ValidationError{"id must be a positive integer"}
	}
	input, err := normalize(input)
	if err != nil {
		return User{}, err
	}
	return s.repository.Update(ctx, id, input)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return &ValidationError{"id must be a positive integer"}
	}
	return s.repository.Delete(ctx, id)
}

func normalize(input ProfileInput) (ProfileInput, error) {
	input.FirstName = strings.TrimSpace(input.FirstName)
	input.LastName = strings.TrimSpace(input.LastName)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Location = strings.TrimSpace(input.Location)
	input.Avatar = strings.TrimSpace(input.Avatar)
	for _, field := range []struct{ name, value string }{
		{"first_name", input.FirstName}, {"last_name", input.LastName}, {"display_name", input.DisplayName},
	} {
		if length := utf8.RuneCountInString(field.value); length < 1 || length > 100 {
			return ProfileInput{}, &ValidationError{field.name + " must contain between 1 and 100 characters"}
		}
	}
	if !validEmail(input.Email) {
		return ProfileInput{}, &ValidationError{"email must be a valid address of at most 254 bytes"}
	}
	if utf8.RuneCountInString(input.Location) > 255 {
		return ProfileInput{}, &ValidationError{"location must contain at most 255 characters"}
	}
	if utf8.RuneCountInString(input.Avatar) > 2048 {
		return ProfileInput{}, &ValidationError{"avatar must contain at most 2048 characters"}
	}
	return input, nil
}

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && len(email) <= 254
}
