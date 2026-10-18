package auth

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/Bukho127/CampusHub_server/internal/password"
)

type Repository interface {
	CredentialsByEmail(context.Context, string) (Credentials, error)
	AccountByID(context.Context, int64) (Account, error)
	Revoke(context.Context, int64) error
}

type Service struct {
	repository Repository
	tokens     *Tokens
	dummyHash  string
}

// Unknown emails and legacy accounts still incur the password verification cost.
var dummyPassword = sync.OnceValues(func() (string, error) {
	return password.Hash("dummy password for unknown accounts")
})

func NewService(repository Repository, tokens *Tokens) (*Service, error) {
	hash, err := dummyPassword()
	if err != nil {
		return nil, err
	}
	return &Service{
		repository: repository,
		tokens:     tokens,
		dummyHash:  hash,
	}, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (TokenResponse, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" || len(email) > 254 || input.Password == "" || len(input.Password) > password.MaxLength*4 {
		return TokenResponse{}, ErrInvalidCredentials
	}
	credentials, err := s.repository.CredentialsByEmail(ctx, email)
	missing := errors.Is(err, ErrAccountNotFound)
	if err != nil && !missing {
		return TokenResponse{}, err
	}
	hash := credentials.PasswordHash
	if missing || hash == "" {
		hash = s.dummyHash
	}
	matched, err := password.Verify(hash, input.Password)
	if err != nil {
		_, _ = password.Verify(s.dummyHash, input.Password)
		return TokenResponse{}, ErrInvalidCredentials
	}
	if err := ctx.Err(); err != nil {
		return TokenResponse{}, err
	}
	if missing || !matched || credentials.PasswordHash == "" || !active(credentials.Account) {
		return TokenResponse{}, ErrInvalidCredentials
	}
	return s.tokens.Issue(credentials.ID, credentials.TokenVersion)
}

// StartSession issues an access token for a server-identified account after
// registration. The caller must establish ownership before calling this method.
func (s *Service) StartSession(ctx context.Context, id int64) (TokenResponse, error) {
	account, err := s.repository.AccountByID(ctx, id)
	if errors.Is(err, ErrAccountNotFound) {
		return TokenResponse{}, ErrUnauthorized
	}
	if err != nil {
		return TokenResponse{}, err
	}
	if !active(account) || account.ID != id {
		return TokenResponse{}, ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return TokenResponse{}, err
	}
	return s.tokens.Issue(account.ID, account.TokenVersion)
}

func (s *Service) Authenticate(ctx context.Context, raw string) (Account, error) {
	id, version, err := s.tokens.Parse(raw)
	if err != nil {
		return Account{}, ErrUnauthorized
	}
	account, err := s.repository.AccountByID(ctx, id)
	if errors.Is(err, ErrAccountNotFound) {
		return Account{}, ErrUnauthorized
	}
	if err != nil {
		return Account{}, err
	}
	if !active(account) || account.ID != id || account.TokenVersion != version {
		return Account{}, ErrUnauthorized
	}
	return account, nil
}

// Logout revokes all currently issued tokens for this account.
func (s *Service) Logout(ctx context.Context, id int64) error {
	return s.repository.Revoke(ctx, id)
}
