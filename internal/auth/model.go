package auth

import (
	"context"
	"errors"
)

var (
	ErrUnauthorized       = errors.New("invalid or expired access token")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountNotFound    = errors.New("account not found")
)

// Account contains current database state, not permissions supplied by the client.
type Account struct {
	ID           int64
	Role         string
	Status       string
	TokenVersion int64
}

type Credentials struct {
	Account
	PasswordHash string `json:"-"`
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	UserID      int64  `json:"user_id"`
}

type accountKey struct{}

func WithAccount(ctx context.Context, account Account) context.Context {
	return context.WithValue(ctx, accountKey{}, account)
}

func AccountFromContext(ctx context.Context) (Account, bool) {
	account, ok := ctx.Value(accountKey{}).(Account)
	return account, ok
}

func active(account Account) bool {
	return account.ID > 0 && account.Status == "active" && account.TokenVersion >= 0 &&
		(account.Role == "user" || account.Role == "vendor" || account.Role == "admin")
}
