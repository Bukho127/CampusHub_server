package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	AccessTokenTTL = 15 * time.Minute
	TokenIssuer    = "campus-hub"
	TokenAudience  = "campus-hub-api"
)

type Claims struct {
	jwt.RegisteredClaims
	TokenVersion *int64 `json:"token_version"`
	TokenUse     string `json:"token_use"`
}

// Tokens signs and validates access tokens using only HS256 and the configured key.
type Tokens struct {
	secret []byte
}

func NewTokens(secret string) (*Tokens, error) {
	if len(secret) < 32 || strings.TrimSpace(secret) == "" {
		return nil, errors.New("JWT_SECRET must contain at least 32 bytes; use a randomly generated secret")
	}
	return &Tokens{secret: []byte(secret)}, nil
}

func (t *Tokens) Issue(id, version int64) (TokenResponse, error) {
	if id <= 0 || version < 0 {
		return TokenResponse{}, errors.New("invalid account for token issuance")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return TokenResponse{}, err
	}
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: TokenIssuer, Audience: jwt.ClaimStrings{TokenAudience},
			Subject: strconv.FormatInt(id, 10), ID: hex.EncodeToString(nonce),
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
		TokenVersion: &version, TokenUse: "access",
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return TokenResponse{}, err
	}
	return TokenResponse{AccessToken: token, TokenType: "Bearer", ExpiresIn: int64(AccessTokenTTL / time.Second), UserID: id}, nil
}

func (t *Tokens) Parse(raw string) (int64, int64, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return 0, 0, ErrUnauthorized
	}
	var claims Claims
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrUnauthorized
		}
		return t.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(TokenIssuer),
		jwt.WithAudience(TokenAudience), jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(), jwt.WithLeeway(5*time.Second), jwt.WithStrictDecoding())
	if err != nil || token == nil || !token.Valid ||
		claims.TokenUse != "access" || claims.TokenVersion == nil || *claims.TokenVersion < 0 ||
		claims.IssuedAt == nil || claims.NotBefore == nil || claims.ID == "" ||
		claims.ExpiresAt == nil || !claims.ExpiresAt.After(claims.IssuedAt.Time) ||
		claims.ExpiresAt.Sub(claims.IssuedAt.Time) > AccessTokenTTL {
		return 0, 0, ErrUnauthorized
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != claims.Subject {
		return 0, 0, ErrUnauthorized
	}
	return id, *claims.TokenVersion, nil
}
