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
	profile, err := normalize(ProfileInput{Name: input.Name, Email: input.Email})
	if err != nil {
		return User{}, err
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
	return s.repository.Create(ctx, CreateParams{Name: profile.Name, Email: profile.Email, PasswordHash: hash})
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
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if length := utf8.RuneCountInString(input.Name); length < 1 || length > 100 {
		return ProfileInput{}, &ValidationError{"name must contain between 1 and 100 characters"}
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || len(input.Email) > 254 {
		return ProfileInput{}, &ValidationError{"email must be a valid address of at most 254 bytes"}
	}
	return input, nil
}
