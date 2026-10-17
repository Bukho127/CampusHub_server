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

	"github.com/Bukho127/CampusHub_server/internal/password"
	"github.com/Bukho127/CampusHub_server/internal/server"
	"github.com/Bukho127/CampusHub_server/internal/users"
)

type userRepositoryStub struct {
	t        *testing.T
	createFn func(context.Context, users.CreateParams) (users.User, error)
	getFn    func(context.Context, int64) (users.User, error)
	listFn   func(context.Context, int, int) ([]users.User, error)
	updateFn func(context.Context, int64, users.ProfileInput) (users.User, error)
	deleteFn func(context.Context, int64) error
}

func (s *userRepositoryStub) Create(ctx context.Context, input users.CreateParams) (users.User, error) {
	s.t.Helper()
	if s.createFn == nil {
		s.t.Fatal("unexpected Create call")
	}
	return s.createFn(ctx, input)
}

func (s *userRepositoryStub) Get(ctx context.Context, id int64) (users.User, error) {
	s.t.Helper()
	if s.getFn == nil {
		s.t.Fatal("unexpected Get call")
	}
	return s.getFn(ctx, id)
}

func (s *userRepositoryStub) List(ctx context.Context, limit, offset int) ([]users.User, error) {
	s.t.Helper()
	if s.listFn == nil {
		s.t.Fatal("unexpected List call")
	}
	return s.listFn(ctx, limit, offset)
}

func (s *userRepositoryStub) Update(ctx context.Context, id int64, input users.ProfileInput) (users.User, error) {
	s.t.Helper()
	if s.updateFn == nil {
		s.t.Fatal("unexpected Update call")
	}
	return s.updateFn(ctx, id, input)
}

func (s *userRepositoryStub) Delete(ctx context.Context, id int64) error {
	s.t.Helper()
	if s.deleteFn == nil {
		s.t.Fatal("unexpected Delete call")
	}
	return s.deleteFn(ctx, id)
}

func userRouter(repository users.Repository) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return server.NewRouter(users.NewService(repository), logger)
}

func userRequest(router http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

const testPassword = "a very long test password"

func userCreationBody(name, email, plaintext string) string {
	body, _ := json.Marshal(users.Input{Name: name, Email: email, Password: plaintext})
	return string(body)
}

func assertNoCredentials(t *testing.T, body []byte) {
	t.Helper()
	if strings.Contains(string(body), "\"password\"") || strings.Contains(string(body), "\"password_hash\"") || strings.Contains(string(body), "$argon2id$") {
		t.Fatal("response exposed a password or hash")
	}
}

func TestUserAPI(t *testing.T) {
	profile := users.User{
		ID: 42, Name: "Bukho", Email: "bukho@example.com",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	input := users.ProfileInput{Name: "Bukho", Email: "bukho@example.com"}
	body := "{\"name\":\"  Bukho  \",\"email\":\"  BUKHO@example.com  \"}"
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			repository := &userRepositoryStub{t: t}
			called := false
			check := func(ctx context.Context, id int64) {
				t.Helper()
				called = true
				if id != profile.ID {
					t.Fatalf("id = %d, want %d", id, profile.ID)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("repository context has no deadline")
				}
			}
			repository.createFn = func(ctx context.Context, got users.CreateParams) (users.User, error) {
				check(ctx, profile.ID)
				if got.Name != input.Name || got.Email != input.Email {
					t.Fatal("profile fields were not normalized")
				}
				if matched, err := password.Verify(got.PasswordHash, testPassword); err != nil || !matched {
					t.Fatal("repository did not receive a valid password hash")
				}
				return profile, nil
			}
			repository.getFn = func(ctx context.Context, id int64) (users.User, error) {
				check(ctx, id)
				return profile, nil
			}
			repository.updateFn = func(ctx context.Context, id int64, got users.ProfileInput) (users.User, error) {
				check(ctx, id)
				if got != input {
					t.Fatalf("input = %+v, want %+v", got, input)
				}
				return profile, nil
			}
			repository.deleteFn = func(ctx context.Context, id int64) error {
				check(ctx, id)
				return nil
			}
			path, wantStatus := "/api/v1/users/42", http.StatusOK
			requestBody := body
			if method == http.MethodPost {
				path, wantStatus = "/api/v1/users", http.StatusCreated
				requestBody = userCreationBody("  Bukho  ", "  BUKHO@example.com  ", testPassword)
			}
			if method == http.MethodDelete {
				wantStatus = http.StatusNoContent
			}
			response := userRequest(userRouter(repository), method, path, requestBody, "application/json; charset=utf-8")
			assertNoCredentials(t, response.Body.Bytes())
			if response.Code != wantStatus || !called {
				t.Fatalf("status = %d, want %d; repository called = %v; body = %s", response.Code, wantStatus, called, response.Body)
			}
			if method == http.MethodDelete {
				if response.Body.Len() != 0 {
					t.Fatal("DELETE response should be empty")
				}
				return
			}
			var got users.User
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got != profile {
				t.Fatalf("profile = %+v, want %+v", got, profile)
			}
			if method == http.MethodPost && response.Header().Get("Location") != "/api/v1/users/42" {
				t.Fatal("missing created-user Location header")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("user response must not be cached")
			}
		})
	}
}

func TestListUsers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		query  string
		limit  int
		offset int
	}{
		{"defaults", "", 20, 0},
		{"pagination", "?limit=5&offset=10", 5, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &userRepositoryStub{t: t, listFn: func(ctx context.Context, limit, offset int) ([]users.User, error) {
				if limit != tc.limit || offset != tc.offset {
					t.Fatalf("pagination = %d,%d; want %d,%d", limit, offset, tc.limit, tc.offset)
				}
				return nil, nil
			}}
			response := userRequest(userRouter(repository), http.MethodGet, "/api/v1/users"+tc.query, "", "")
			var page users.Page
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || page.Users == nil || len(page.Users) != 0 || page.Limit != tc.limit || page.Offset != tc.offset {
				t.Fatalf("unexpected list response: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestUserAPIRejectsInvalidInput(t *testing.T) {
	valid := userCreationBody("Bukho", "bukho@example.com", testPassword)
	for _, tc := range []struct {
		name, method, path, body, contentType string
		status                                int
	}{
		{"malformed JSON", "POST", "/api/v1/users", "{", "application/json", 400},
		{"empty body", "POST", "/api/v1/users", "", "application/json", 400},
		{"null", "POST", "/api/v1/users", "null", "application/json", 400},
		{"array", "POST", "/api/v1/users", "[]", "application/json", 400},
		{"missing password", "POST", "/api/v1/users", "{\"name\":\"Bukho\",\"email\":\"bukho@example.com\"}", "application/json", 400},
		{"short password", "POST", "/api/v1/users", userCreationBody("Bukho", "bukho@example.com", "too short"), "application/json", 400},
		{"long password", "POST", "/api/v1/users", userCreationBody("Bukho", "bukho@example.com", strings.Repeat("a", 129)), "application/json", 400},
		{"password change on profile route", "PUT", "/api/v1/users/42", valid, "application/json", 400},
		{"client supplied hash", "POST", "/api/v1/users", "{\"name\":\"Bukho\",\"email\":\"bukho@example.com\",\"password_hash\":\"attacker supplied\"}", "application/json", 400},
		{"unknown field", "POST", "/api/v1/users", "{\"name\":\"Bukho\",\"email\":\"bukho@example.com\",\"admin\":true}", "application/json", 400},
		{"extra JSON", "POST", "/api/v1/users", valid + " {}", "application/json", 400},
		{"trailing garbage", "POST", "/api/v1/users", valid + "invalid", "application/json", 400},
		{"wrong field type", "POST", "/api/v1/users", "{\"name\":123,\"email\":\"a@example.com\"}", "application/json", 400},
		{"blank name", "POST", "/api/v1/users", "{\"name\":\"  \",\"email\":\"a@example.com\"}", "application/json", 400},
		{"long name", "POST", "/api/v1/users", "{\"name\":\"" + strings.Repeat("a", 101) + "\",\"email\":\"a@example.com\"}", "application/json", 400},
		{"invalid email", "POST", "/api/v1/users", "{\"name\":\"A\",\"email\":\"bad\"}", "application/json", 400},
		{"display address", "POST", "/api/v1/users", "{\"name\":\"A\",\"email\":\"A <a@example.com>\"}", "application/json", 400},
		{"long email", "POST", "/api/v1/users", "{\"name\":\"A\",\"email\":\"" + strings.Repeat("a", 250) + "@example.com\"}", "application/json", 400},
		{"missing name on replacement", "PUT", "/api/v1/users/42", "{\"email\":\"a@example.com\"}", "application/json", 400},
		{"missing content type", "POST", "/api/v1/users", valid, "", 415},
		{"wrong content type", "POST", "/api/v1/users", valid, "text/plain", 415},
		{"large body", "POST", "/api/v1/users", "{\"name\":\"" + strings.Repeat("a", 17000) + "\",\"email\":\"a@example.com\"}", "application/json", 413},
		{"large trailing whitespace", "POST", "/api/v1/users", valid + strings.Repeat(" ", 17000), "application/json", 413},
		{"bad id", "GET", "/api/v1/users/abc", "", "", 400},
		{"zero id", "DELETE", "/api/v1/users/0", "", "", 400},
		{"negative id", "PUT", "/api/v1/users/-1", valid, "application/json", 400},
		{"id overflow", "GET", "/api/v1/users/999999999999999999999", "", "", 400},
		{"bad limit", "GET", "/api/v1/users?limit=abc", "", "", 400},
		{"empty limit", "GET", "/api/v1/users?limit=", "", "", 400},
		{"zero limit", "GET", "/api/v1/users?limit=0", "", "", 400},
		{"large limit", "GET", "/api/v1/users?limit=101", "", "", 400},
		{"negative offset", "GET", "/api/v1/users?offset=-1", "", "", 400},
		{"large offset", "GET", "/api/v1/users?offset=2147483648", "", "", 400},
		{"bad offset", "GET", "/api/v1/users?offset=abc", "", "", 400},
		{"repeated limit", "GET", "/api/v1/users?limit=1&limit=2", "", "", 400},
		{"unsupported method", "PATCH", "/api/v1/users/42", valid, "application/json", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := userRequest(userRouter(&userRepositoryStub{t: t}), tc.method, tc.path, tc.body, tc.contentType)
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tc.status, response.Body)
			}
		})
	}
}

func TestUserAPIErrors(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, message string
		err                         error
		status                      int
	}{
		{"get missing", "GET", "/api/v1/users/42", "user not found", users.ErrNotFound, 404},
		{"update missing", "PUT", "/api/v1/users/42", "user not found", users.ErrNotFound, 404},
		{"delete missing", "DELETE", "/api/v1/users/42", "user not found", users.ErrNotFound, 404},
		{"create conflict", "POST", "/api/v1/users", "email already exists", users.ErrEmailExists, 409},
		{"update conflict", "PUT", "/api/v1/users/42", "email already exists", users.ErrEmailExists, 409},
		{"timeout", "GET", "/api/v1/users/42", "request timed out", context.DeadlineExceeded, 504},
		{"storage failure", "GET", "/api/v1/users/42", "internal server error", errors.New("private database details"), 500},
		{"list failure", "GET", "/api/v1/users", "internal server error", errors.New("private database details"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			repository := &userRepositoryStub{t: t}
			result := func() (users.User, error) { called = true; return users.User{}, tc.err }
			repository.createFn = func(context.Context, users.CreateParams) (users.User, error) { return result() }
			repository.getFn = func(context.Context, int64) (users.User, error) { return result() }
			repository.updateFn = func(context.Context, int64, users.ProfileInput) (users.User, error) { return result() }
			repository.deleteFn = func(context.Context, int64) error { called = true; return tc.err }
			repository.listFn = func(context.Context, int, int) ([]users.User, error) { called = true; return nil, tc.err }
			requestBody := "{\"name\":\"A\",\"email\":\"a@example.com\"}"
			if tc.method == http.MethodPost {
				requestBody = userCreationBody("A", "a@example.com", testPassword)
			}
			response := userRequest(userRouter(repository), tc.method, tc.path, requestBody, "application/json")
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if response.Code != tc.status || body["error"] != tc.message || !called {
				t.Fatalf("unexpected error response: %d %s; called = %v", response.Code, response.Body, called)
			}
		})
	}
}

func TestUserServiceRejectsInvalidID(t *testing.T) {
	service := users.NewService(&userRepositoryStub{t: t})
	_, getError := service.Get(context.Background(), 0)
	_, updateError := service.Update(context.Background(), -1, users.ProfileInput{})
	deleteError := service.Delete(context.Background(), 0)
	for _, err := range []error{getError, updateError, deleteError} {
		var validation *users.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("got %v, want ValidationError", err)
		}
	}
}

func TestHealth(t *testing.T) {
	response := userRequest(userRouter(&userRepositoryStub{t: t}), "GET", "/healthz", "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "\"status\":\"ok\"") {
		t.Fatalf("unexpected health response: %d %s", response.Code, response.Body)
	}
}
