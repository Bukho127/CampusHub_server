package test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Bukho127/CampusHub_server/internal/auth"
	"github.com/Bukho127/CampusHub_server/internal/config"
	"github.com/Bukho127/CampusHub_server/internal/password"
	"github.com/Bukho127/CampusHub_server/internal/server"
	"github.com/Bukho127/CampusHub_server/internal/users"
	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-only-jwt-secret-at-least-32-bytes-long"

var testTokens = func() *auth.Tokens {
	tokens, err := auth.NewTokens(testJWTSecret)
	if err != nil {
		panic(err)
	}
	return tokens
}()

type authRepositoryStub struct {
	credentialsFn func(context.Context, string) (auth.Credentials, error)
	accountFn     func(context.Context, int64) (auth.Account, error)
	revokeFn      func(context.Context, int64) error
}

func (s *authRepositoryStub) CredentialsByEmail(ctx context.Context, email string) (auth.Credentials, error) {
	if s.credentialsFn == nil {
		return auth.Credentials{}, auth.ErrAccountNotFound
	}
	return s.credentialsFn(ctx, email)
}

func (s *authRepositoryStub) AccountByID(ctx context.Context, id int64) (auth.Account, error) {
	if s.accountFn == nil {
		return auth.Account{}, auth.ErrAccountNotFound
	}
	return s.accountFn(ctx, id)
}

func (s *authRepositoryStub) Revoke(ctx context.Context, id int64) error {
	if s.revokeFn == nil {
		return auth.ErrUnauthorized
	}
	return s.revokeFn(ctx, id)
}

func authenticationRouter(t *testing.T, repository auth.Repository, profiles users.Repository) http.Handler {
	t.Helper()
	service, err := auth.NewService(repository, testTokens)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return server.NewRouter(users.NewService(profiles), service, logger)
}

func authRequest(router http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func loginBody(email, plaintext string) string {
	body, _ := json.Marshal(auth.LoginInput{Email: email, Password: plaintext})
	return string(body)
}

func TestJWTSecretValidation(t *testing.T) {
	for _, secret := range []string{"", "short", strings.Repeat(" ", 32), strings.Repeat("x", 31)} {
		if _, err := auth.NewTokens(secret); err == nil {
			t.Fatal("weak or missing JWT secret accepted")
		}
		databaseEnvironment(t)
		t.Setenv("JWT_SECRET", secret)
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
			t.Fatal("configuration accepted weak JWT secret")
		}
	}
}

func TestJWTTokens(t *testing.T) {
	issued, err := testTokens.Issue(42, 3)
	if err != nil {
		t.Fatal(err)
	}
	id, version, err := testTokens.Parse(issued.AccessToken)
	if err != nil || id != 42 || version != 3 || issued.UserID != 42 || issued.TokenType != "Bearer" || issued.ExpiresIn != 900 {
		t.Fatalf("unexpected issued token metadata: id=%d, version=%d, error=%v", id, version, err)
	}
	another, err := testTokens.Issue(42, 3)
	if err != nil || another.AccessToken == issued.AccessToken {
		t.Fatal("tokens should have unique IDs")
	}
	for _, input := range [][2]int64{{0, 0}, {-1, 0}, {1, -1}} {
		if _, err := testTokens.Issue(input[0], input[1]); err == nil {
			t.Fatal("issued a token with invalid account metadata")
		}
	}
}

func signedTestToken(t *testing.T, changes map[string]any, method jwt.SigningMethod, key any) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": auth.TokenIssuer, "aud": auth.TokenAudience, "sub": "42",
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(auth.AccessTokenTTL).Unix(),
		"jti": "test-token-id", "token_version": 0, "token_use": "access",
	}
	for key, value := range changes {
		if value == nil {
			delete(claims, key)
		} else {
			claims[key] = value
		}
	}
	raw, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestJWTRejectsInvalidTokens(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changes map[string]any
		method  jwt.SigningMethod
		key     any
	}{
		{"wrong signature", nil, jwt.SigningMethodHS256, []byte("another-secret-of-at-least-32-bytes")},
		{"wrong algorithm", nil, jwt.SigningMethodHS512, []byte(testJWTSecret)},
		{"unsigned token", nil, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType},
		{"wrong issuer", map[string]any{"iss": "other-app"}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing issuer", map[string]any{"iss": nil}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"wrong audience", map[string]any{"aud": "other-api"}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing audience", map[string]any{"aud": nil}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"expired", map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing expiration", map[string]any{"exp": nil}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing issue time", map[string]any{"iat": nil}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"future issue time", map[string]any{"iat": time.Now().Add(time.Hour).Unix()}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing not before", map[string]any{"nbf": nil}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"future not before", map[string]any{"nbf": time.Now().Add(time.Hour).Unix()}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing subject", map[string]any{"sub": nil}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"zero subject", map[string]any{"sub": "0"}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"noncanonical subject", map[string]any{"sub": "042"}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing token version", map[string]any{"token_version": nil}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"negative token version", map[string]any{"token_version": -1}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"invalid token version type", map[string]any{"token_version": "0"}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"wrong token use", map[string]any{"token_use": "reset"}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing token ID", map[string]any{"jti": nil}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"excessive lifetime", map[string]any{"exp": time.Now().Add(24 * time.Hour).Unix()}, jwt.SigningMethodHS256, []byte(testJWTSecret)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := signedTestToken(t, tc.changes, tc.method, tc.key)
			if _, _, err := testTokens.Parse(raw); !errors.Is(err, auth.ErrUnauthorized) {
				t.Fatalf("invalid JWT accepted: %v", err)
			}
		})
	}
	for _, raw := range []string{"", "not-a-jwt", strings.Repeat("a", 5000)} {
		if _, _, err := testTokens.Parse(raw); !errors.Is(err, auth.ErrUnauthorized) {
			t.Fatal("malformed token accepted")
		}
	}
}

func TestLogin(t *testing.T) {
	hash, err := password.Hash(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, status, hash, plaintext string
		missing                       bool
		want                          int
	}{
		{"valid", "active", hash, testPassword, false, 200},
		{"wrong password", "active", hash, "wrong password", false, 401},
		{"unknown email", "active", "", testPassword, true, 401},
		{"legacy account", "active", "", testPassword, false, 401},
		{"banned", "banned", hash, testPassword, false, 401},
		{"deleted", "deleted", hash, testPassword, false, 401},
		{"malformed stored hash", "active", "$argon2id$invalid", testPassword, false, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &authRepositoryStub{credentialsFn: func(ctx context.Context, email string) (auth.Credentials, error) {
				if email != "bukho@example.com" {
					t.Fatal("login email was not normalized")
				}
				if tc.missing {
					return auth.Credentials{}, auth.ErrAccountNotFound
				}
				return auth.Credentials{Account: auth.Account{ID: 42, Role: "user", Status: tc.status}, PasswordHash: tc.hash}, nil
			}}
			router := authenticationRouter(t, repository, &userRepositoryStub{t: t})
			response := authRequest(router, "POST", "/api/v1/auth/login", loginBody(" BUKHO@example.com ", tc.plaintext), "")
			if response.Code != tc.want {
				t.Fatalf("login status = %d, want %d; body=%s", response.Code, tc.want, response.Body)
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), testPassword) {
				t.Fatal("login response leaked credentials or allowed caching")
			}
			if tc.want == 200 {
				var issued auth.TokenResponse
				if err := json.Unmarshal(response.Body.Bytes(), &issued); err != nil {
					t.Fatal(err)
				}
				if id, _, err := testTokens.Parse(issued.AccessToken); err != nil || id != 42 {
					t.Fatal("login did not issue a usable access token")
				}
			} else if !strings.Contains(response.Body.String(), "invalid email or password") {
				t.Fatal("login failures must use a generic response")
			}
		})
	}
}

func TestLoginRejectsInvalidBodies(t *testing.T) {
	for _, body := range []string{
		"", "{", "null", "[]",
		"{\"email\":\"a@example.com\",\"password\":\"x\",\"role\":\"admin\"}",
		"{\"email\":42,\"password\":\"x\"}",
		loginBody("a@example.com", testPassword) + " {}",
	} {
		router := authenticationRouter(t, &authRepositoryStub{}, &userRepositoryStub{t: t})
		response := authRequest(router, "POST", "/api/v1/auth/login", body, "")
		if response.Code != 400 {
			t.Fatalf("invalid login body returned %d", response.Code)
		}
	}
	router := authenticationRouter(t, &authRepositoryStub{}, &userRepositoryStub{t: t})
	response := authRequest(router, "POST", "/api/v1/auth/login", loginBody("a@example.com", strings.Repeat("x", 17000)), "")
	if response.Code != 413 {
		t.Fatal("oversized login body accepted")
	}
	request := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader("{}"))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 415 {
		t.Fatal("login accepted missing content type")
	}
}

func TestUserAuthorization(t *testing.T) {
	issued, err := testTokens.Issue(42, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, path, role, token string
		status                          int
	}{
		{"missing token", "GET", "/api/v1/users/42", "user", "", 401},
		{"malformed token", "GET", "/api/v1/users/42", "user", "invalid", 401},
		{"owner read", "GET", "/api/v1/users/42", "user", issued.AccessToken, 200},
		{"other user read", "GET", "/api/v1/users/43", "user", issued.AccessToken, 403},
		{"owner update", "PUT", "/api/v1/users/42", "user", issued.AccessToken, 200},
		{"other user update", "PUT", "/api/v1/users/43", "user", issued.AccessToken, 403},
		{"owner delete", "DELETE", "/api/v1/users/42", "user", issued.AccessToken, 204},
		{"other user delete", "DELETE", "/api/v1/users/43", "user", issued.AccessToken, 403},
		{"user list", "GET", "/api/v1/users", "user", issued.AccessToken, 403},
		{"vendor list", "GET", "/api/v1/users", "vendor", issued.AccessToken, 403},
		{"admin list", "GET", "/api/v1/users", "admin", issued.AccessToken, 200},
		{"admin read", "GET", "/api/v1/users/43", "admin", issued.AccessToken, 200},
		{"admin update", "PUT", "/api/v1/users/43", "admin", issued.AccessToken, 200},
		{"admin delete", "DELETE", "/api/v1/users/43", "admin", issued.AccessToken, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			accounts := &authRepositoryStub{accountFn: func(context.Context, int64) (auth.Account, error) {
				return auth.Account{ID: 42, Role: tc.role, Status: "active"}, nil
			}}
			profiles := &userRepositoryStub{
				t:      t,
				getFn:  func(ctx context.Context, id int64) (users.User, error) { called = true; return users.User{ID: id}, nil },
				listFn: func(context.Context, int, int) ([]users.User, error) { called = true; return nil, nil },
				updateFn: func(ctx context.Context, id int64, input users.ProfileInput) (users.User, error) {
					called = true
					return users.User{ID: id}, nil
				},
				deleteFn: func(context.Context, int64) error { called = true; return nil },
			}
			response := authRequest(authenticationRouter(t, accounts, profiles), tc.method, tc.path, userProfileBody("Bukho", "bukho@example.com"), tc.token)
			if response.Code != tc.status || called != (tc.status < 400) {
				t.Fatalf("status=%d, want=%d; profile repository called=%v; body=%s", response.Code, tc.status, called, response.Body)
			}
			if tc.status == 401 && response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("missing authentication challenge")
			}
		})
	}
}

func TestAuthenticationUsesCurrentAccountState(t *testing.T) {
	issued, err := testTokens.Issue(42, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		account auth.Account
		err     error
		status  int
	}{
		{"banned", auth.Account{ID: 42, Role: "user", Status: "banned"}, nil, 401},
		{"deleted", auth.Account{ID: 42, Role: "user", Status: "deleted"}, nil, 401},
		{"revoked", auth.Account{ID: 42, Role: "user", Status: "active", TokenVersion: 1}, nil, 401},
		{"unknown role", auth.Account{ID: 42, Role: "owner", Status: "active"}, nil, 401},
		{"missing", auth.Account{}, auth.ErrAccountNotFound, 401},
		{"database unavailable", auth.Account{}, errors.New("private database details"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts := &authRepositoryStub{accountFn: func(context.Context, int64) (auth.Account, error) { return tc.account, tc.err }}
			response := authRequest(authenticationRouter(t, accounts, &userRepositoryStub{t: t}), "GET", "/api/v1/users/42", "", issued.AccessToken)
			if response.Code != tc.status || strings.Contains(response.Body.String(), "private database") {
				t.Fatalf("unexpected account validation response: %d %s", response.Code, response.Body)
			}
		})
	}
	// Even a correctly signed token carrying an admin claim cannot override the DB role.
	forgedRole := signedTestToken(t, map[string]any{"role": "admin"}, jwt.SigningMethodHS256, []byte(testJWTSecret))
	accounts := &authRepositoryStub{accountFn: func(context.Context, int64) (auth.Account, error) {
		return auth.Account{ID: 42, Role: "user", Status: "active"}, nil
	}}
	response := authRequest(authenticationRouter(t, accounts, &userRepositoryStub{t: t}), "GET", "/api/v1/users", "", forgedRole)
	if response.Code != 403 {
		t.Fatal("token role overrode the current database role")
	}
}

func TestLogoutRevokesTokens(t *testing.T) {
	account := auth.Account{ID: 42, Role: "user", Status: "active"}
	accounts := &authRepositoryStub{
		accountFn: func(context.Context, int64) (auth.Account, error) { return account, nil },
		revokeFn: func(ctx context.Context, id int64) error {
			if id != account.ID {
				t.Fatal("logout targeted another account")
			}
			account.TokenVersion++
			return nil
		},
	}
	router := authenticationRouter(t, accounts, &userRepositoryStub{t: t})
	issued, err := testTokens.Issue(account.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if response := authRequest(router, "POST", "/api/v1/auth/logout", "", issued.AccessToken); response.Code != 204 {
		t.Fatalf("logout failed: %d %s", response.Code, response.Body)
	}
	if response := authRequest(router, "GET", "/api/v1/users/42", "", issued.AccessToken); response.Code != 401 {
		t.Fatal("logged-out token remained usable")
	}
}

func TestAuthorizationHeaderAndPublicRoutes(t *testing.T) {
	router := authenticationRouter(t, &authRepositoryStub{}, &userRepositoryStub{t: t})
	for _, header := range [][]string{nil, {"Basic anything"}, {"Bearer"}, {"Bearer one two"}, {"Bearer one", "Bearer two"}} {
		request := httptest.NewRequest("GET", "/api/v1/users/42?access_token=not-accepted", nil)
		for _, value := range header {
			request.Header.Add("Authorization", value)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatal("invalid Authorization header accepted")
		}
	}
	if response := authRequest(router, "GET", "/healthz", "", ""); response.Code != 200 {
		t.Fatal("health endpoint unexpectedly requires authentication")
	}
	if response := authRequest(router, "POST", "/api/v1/users", "{}", ""); response.Code != 400 {
		t.Fatal("registration should reach public input validation")
	}
}

func TestLoginBackendFailures(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{errors.New("private database failure"), 500},
		{context.DeadlineExceeded, 504},
	} {
		accounts := &authRepositoryStub{credentialsFn: func(context.Context, string) (auth.Credentials, error) {
			return auth.Credentials{}, tc.err
		}}
		router := authenticationRouter(t, accounts, &userRepositoryStub{t: t})
		response := authRequest(router, "POST", "/api/v1/auth/login", loginBody("bukho@example.com", testPassword), "")
		if response.Code != tc.status || strings.Contains(response.Body.String(), "private database") {
			t.Fatalf("login backend error response: %d %s", response.Code, response.Body)
		}
	}
}
