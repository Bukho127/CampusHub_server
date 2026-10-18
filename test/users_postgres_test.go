package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Bukho127/CampusHub_server/internal/auth"
	"github.com/Bukho127/CampusHub_server/internal/config"
	"github.com/Bukho127/CampusHub_server/internal/database"
	"github.com/Bukho127/CampusHub_server/internal/password"
	"github.com/Bukho127/CampusHub_server/internal/users"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Database tests opt in explicitly and roll back an isolated schema after every case.
func TestUsersPostgres(t *testing.T) {
	connectionURL := os.Getenv("TEST_DATABASE_URL")
	if connectionURL == "" {
		if os.Getenv("RUN_DATABASE_TESTS") != "1" {
			t.Skip("set TEST_DATABASE_URL or RUN_DATABASE_TESTS=1 to run PostgreSQL tests")
		}
		settings, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		connectionURL = settings.DatabaseURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	readMigration := func(name string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join("..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	initialMigration := readMigration("000001_create_users.up.sql")
	passwordMigration := readMigration("000002_add_password_hash.up.sql")
	schemaMigration := readMigration("000003_expand_user_schema.up.sql")
	schemaRollback := readMigration("000003_expand_user_schema.down.sql")

	for _, scenario := range []string{
		"CRUD",
		"duplicate create",
		"duplicate update",
		"existing profile migration",
		"schema rollback",
		"vendor creation",
		"account state",
		"database constraints",
		"JWT authorization",
	} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
					t.Errorf("rollback integration schema: %v", err)
				}
			}()
			schema := pgx.Identifier{fmt.Sprintf("users_api_test_%d_%d", os.Getpid(), time.Now().UnixNano())}.Sanitize()
			for _, statement := range []string{"CREATE SCHEMA " + schema, "SET LOCAL search_path TO " + schema, initialMigration} {
				if _, err := tx.Exec(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			legacy := scenario == "existing profile migration" || scenario == "schema rollback"
			if legacy {
				if _, err := tx.Exec(ctx, "INSERT INTO users (name, email) VALUES ($1, $2)", "Existing", "existing@example.com"); err != nil {
					t.Fatal(err)
				}
			}
			for _, statement := range []string{passwordMigration, schemaMigration} {
				if _, err := tx.Exec(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			router := userRouter(users.NewPostgresRepository(tx))
			request := func(method, path, body string, status int) []byte {
				t.Helper()
				response := userRequest(router, method, path, body, "application/json")
				if response.Code != status {
					t.Fatalf("%s %s: got %d, want %d; body = %s", method, path, response.Code, status, response.Body)
				}
				assertNoCredentials(t, response.Body.Bytes())
				return response.Body.Bytes()
			}
			decodeUser := func(body []byte) users.User {
				t.Helper()
				var profile users.User
				if err := json.Unmarshal(body, &profile); err != nil {
					t.Fatal(err)
				}
				return profile
			}
			if scenario == "schema rollback" {
				if _, err := tx.Exec(ctx, schemaRollback); err != nil {
					t.Fatal(err)
				}
				var name string
				if err := tx.QueryRow(ctx, "SELECT name FROM users WHERE email = $1", "existing@example.com").Scan(&name); err != nil || name != "Existing" {
					t.Fatalf("rollback did not preserve the legacy name: %v", err)
				}
				return
			}
			if legacy {
				var id int64
				var hash *string
				if err := tx.QueryRow(ctx, "SELECT id, password_hash FROM users WHERE email = $1", "existing@example.com").Scan(
					&id,
					&hash,
				); err != nil {
					t.Fatal(err)
				}
				if hash != nil {
					t.Fatal("migration assigned a password to an existing user")
				}
				path := fmt.Sprintf("/api/v1/users/%d", id)
				profile := decodeUser(request("GET", path, "", 200))
				if profile.DisplayName != "Existing" ||
					profile.FirstName != "" ||
					profile.LastName != "" ||
					profile.IdentityType != nil {
					t.Fatal("migration invented or lost legacy profile data")
				}
				request("PUT", path, userProfileBody("Updated", "existing@example.com"), 200)
				return
			}

			createBody := userCreationBody(" Bukho ", " BUKHO@example.com ", testPassword)
			for field, value := range map[string]any{
				"institution_id": 9,
				"student_number": " 12345 ",
				"campus_email":   " CAMPUS@EXAMPLE.COM ",
				"location":       " Cape Town ",
				"avatar":         " /avatars/bukho.png ",
			} {
				createBody = userBodyWith(createBody, field, value)
			}
			identity := users.IdentityStudent
			if scenario == "vendor creation" {
				identity = users.IdentityVendor
				createBody = userBodyWith(createBody, "identity_type", identity)
			}
			created := decodeUser(request("POST", "/api/v1/users", createBody, 201))
			if created.ID <= 0 ||
				created.FirstName != "Bukho" ||
				created.LastName != "Mabhula" ||
				created.DisplayName != "Bukho Mabhula" ||
				created.Email != "bukho@example.com" ||
				created.CreatedAt.IsZero() ||
				created.UpdatedAt.IsZero() {
				t.Fatalf("unexpected stored profile: %+v", created)
			}
			if created.Role != users.RoleUser ||
				created.Status != users.StatusActive ||
				created.IdentityType == nil ||
				*created.IdentityType != identity ||
				created.EmailVerified ||
				created.EmailVerificationStatus != users.VerificationUnverified ||
				created.VendorVerificationStatus != users.VerificationUnverified ||
				created.CampusEmailVerificationStatus != users.VerificationUnverified ||
				created.Rating != nil ||
				created.ReviewCount != 0 ||
				created.InstitutionID == nil ||
				*created.InstitutionID != 9 ||
				created.Location != "Cape Town" ||
				created.Avatar != "/avatars/bukho.png" {
				t.Fatalf("invalid account defaults: %+v", created)
			}
			path := fmt.Sprintf("/api/v1/users/%d", created.ID)
			var storedHash, studentNumber, campusEmail string
			if err := tx.QueryRow(ctx, "SELECT password_hash, student_number, campus_email FROM users WHERE id = $1", created.ID).Scan(
				&storedHash,
				&studentNumber,
				&campusEmail,
			); err != nil {
				t.Fatal(err)
			}
			if matched, err := password.Verify(storedHash, testPassword); err != nil || !matched || storedHash == testPassword {
				t.Fatal("stored password is not a valid hash")
			}
			if studentNumber != "12345" || campusEmail != "campus@example.com" {
				t.Fatal("private identity fields were not normalized and stored")
			}
			switch scenario {
			case "JWT authorization":
				authenticationRepository := auth.NewPostgresRepository(tx)
				secured := authenticationRouter(t, authenticationRepository, users.NewPostgresRepository(tx))
				call := func(method, target, body, token string, status int) []byte {
					t.Helper()
					response := authRequest(secured, method, target, body, token)
					if response.Code != status {
						t.Fatalf("%s %s: got %d, want %d; body=%s", method, target, response.Code, status, response.Body)
					}
					assertNoCredentials(t, response.Body.Bytes())
					return response.Body.Bytes()
				}
				login := func(email string) string {
					t.Helper()
					var response auth.TokenResponse
					if err := json.Unmarshal(call("POST", "/api/v1/auth/login", loginBody(email, testPassword), "", 200), &response); err != nil {
						t.Fatal(err)
					}
					if response.UserID != created.ID || response.AccessToken == "" {
						t.Fatal("login returned unexpected identity or no token")
					}
					return response.AccessToken
				}
				var registered users.RegistrationResponse
				if err := json.Unmarshal(
					call("POST", "/api/v1/auth/register", userCreationBody("New", "new@example.com", testPassword), "", 201),
					&registered,
				); err != nil {
					t.Fatal(err)
				}
				if registered.UserID <= 0 ||
					registered.UserID == created.ID ||
					registered.User.ID != registered.UserID ||
					registered.AccessToken == "" {
					t.Fatal("registration did not return the new account and token")
				}
				registeredPath := fmt.Sprintf("/api/v1/users/%d", registered.UserID)
				call("GET", registeredPath, "", registered.AccessToken, 200)
				call("GET", path, "", registered.AccessToken, 403)
				call("POST", "/api/v1/auth/logout", "", registered.AccessToken, 204)
				call("GET", registeredPath, "", registered.AccessToken, 401)
				call("GET", path, "", "", 401)
				token := login(" BUKHO@example.com ")
				call("GET", path, "", token, 200)
				call("GET", "/api/v1/users", "", token, 403)
				call(
					"PUT",
					fmt.Sprintf("/api/v1/users/%d", created.ID+1000),
					userProfileBody("Other", "other@example.com"),
					token,
					403,
				)
				call("POST", "/api/v1/auth/login", loginBody(created.Email, "wrong password"), "", 401)
				call("POST", "/api/v1/auth/login", loginBody("unknown@example.com", testPassword), "", 401)
				unknown, err := testTokens.Issue(created.ID+1000, 0)
				if err != nil {
					t.Fatal(err)
				}
				call("GET", path, "", unknown.AccessToken, 401)
				if err := authenticationRepository.Revoke(ctx, created.ID+1000); !errors.Is(err, auth.ErrUnauthorized) {
					t.Fatal("logout accepted a missing account")
				}
				if _, err := tx.Exec(ctx, "UPDATE users SET role = 'admin' WHERE id = $1", created.ID); err != nil {
					t.Fatal(err)
				}
				call("GET", "/api/v1/users", "", token, 200)
				if _, err := tx.Exec(ctx, "UPDATE users SET role = 'user' WHERE id = $1", created.ID); err != nil {
					t.Fatal(err)
				}
				call("GET", "/api/v1/users", "", token, 403)
				if _, err := tx.Exec(ctx, "UPDATE users SET status = 'banned' WHERE id = $1", created.ID); err != nil {
					t.Fatal(err)
				}
				call("GET", path, "", token, 401)
				call("POST", "/api/v1/auth/login", loginBody(created.Email, testPassword), "", 401)
				if _, err := tx.Exec(
					ctx,
					"UPDATE users SET status = 'active', token_version = token_version + 1 WHERE id = $1",
					created.ID,
				); err != nil {
					t.Fatal(err)
				}
				call("GET", path, "", token, 401)
				token = login(created.Email)
				call("PUT", path, userProfileBody("Bukho", "changed@example.com"), token, 200)
				call("GET", path, "", token, 401)
				call("POST", "/api/v1/auth/login", loginBody(created.Email, testPassword), "", 401)
				token = login("changed@example.com")
				another := login("changed@example.com")
				call("POST", "/api/v1/auth/logout", "", token, 204)
				call("GET", path, "", token, 401)
				call("GET", path, "", another, 401)
				token = login("changed@example.com")
				call("DELETE", path, "", token, 204)
				call("GET", path, "", token, 401)
				call("POST", "/api/v1/auth/login", loginBody("changed@example.com", testPassword), "", 401)
				return
			case "vendor creation":
				return // A vendor identity still has the default user role and unverified status.
			case "duplicate create":
				request("POST", "/api/v1/users", userCreationBody("Other", "BUKHO@example.com", testPassword), 409)
				return
			case "duplicate update":
				other := decodeUser(request("POST", "/api/v1/users", userCreationBody("Other", "other@example.com", testPassword), 201))
				request("PUT", fmt.Sprintf("/api/v1/users/%d", other.ID), userProfileBody("Other", "bukho@example.com"), 409)
				return
			case "database constraints":
				for _, assignment := range []string{
					"role = 'owner'",
					"status = 'unknown'",
					"identity_type = 'alien'",
					"email_verification_status = 'unknown'",
					"vendor_verification_status = 'unknown'",
					"campus_email_verification_status = 'unknown'",
					"rating = -1",
					"rating = 5.1",
					"review_count = -1",
					"verify_attempts = -1",
					"token_version = -1",
				} {
					savepoint, err := tx.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					_, err = savepoint.Exec(ctx, "UPDATE users SET "+assignment+" WHERE id = $1", created.ID)
					var pgError *pgconn.PgError
					if !errors.As(err, &pgError) || pgError.Code != "23514" {
						t.Fatalf("database accepted invalid state %s: %v", assignment, err)
					}
					if err := savepoint.Rollback(ctx); err != nil {
						t.Fatal(err)
					}
				}
				return
			case "account state":
				if _, err := tx.Exec(ctx, `UPDATE users SET email_verification_status = 'verified',
					verify_code_hash = 'private', verify_code_expires = CURRENT_TIMESTAMP + interval '1 hour',
					verify_attempts = 2, password_reset_token_hash = 'private',
					password_reset_expires_at = CURRENT_TIMESTAMP + interval '1 hour' WHERE id = $1`, created.ID); err != nil {
					t.Fatal(err)
				}
				sameEmail := decodeUser(request("PUT", path, userProfileBody("Updated", created.Email), 200))
				if !sameEmail.EmailVerified || sameEmail.EmailVerificationStatus != users.VerificationVerified {
					t.Fatal("ordinary profile edit reset verified email")
				}
				changedEmail := decodeUser(request("PUT", path, userProfileBody("Updated", "new@example.com"), 200))
				if changedEmail.EmailVerified || changedEmail.EmailVerificationStatus != users.VerificationUnverified {
					t.Fatal("changing email preserved verified state")
				}
				var version, attempts int64
				var code, reset *string
				if err := tx.QueryRow(
					ctx,
					"SELECT token_version, verify_attempts, verify_code_hash, password_reset_token_hash FROM users WHERE id = $1",
					created.ID,
				).Scan(
					&version,
					&attempts,
					&code,
					&reset,
				); err != nil {
					t.Fatal(err)
				}
				if version != 1 || attempts != 0 || code != nil || reset != nil {
					t.Fatal("email change preserved obsolete verification or reset state")
				}
				return
			}

			got := decodeUser(request("GET", path, "", 200))
			if !reflect.DeepEqual(got, created) {
				t.Fatalf("read profile = %+v, want %+v", got, created)
			}
			second := decodeUser(request(
				"POST",
				"/api/v1/users",
				userCreationBody("Robert'); DROP TABLE users;--", "second@example.com", testPassword),
				201,
			))
			var secondHash string
			if err := tx.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", second.ID).Scan(&secondHash); err != nil {
				t.Fatal(err)
			}
			if secondHash == storedHash {
				t.Fatal("identical passwords must have different salts")
			}
			var page users.Page
			if err := json.Unmarshal(request("GET", "/api/v1/users?limit=1&offset=1", "", 200), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Users) != 1 || page.Users[0].ID != second.ID {
				t.Fatalf("unexpected paginated profiles: %+v", page)
			}
			updated := decodeUser(request("PUT", path, userProfileBody("New Name", "new@example.com"), 200))
			if updated.ID != created.ID ||
				updated.FirstName != "New Name" ||
				updated.Email != "new@example.com" ||
				updated.CreatedAt != created.CreatedAt {
				t.Fatalf("unexpected updated profile: %+v", updated)
			}
			request(
				"PUT",
				path,
				userBodyWith(userProfileBody("New Name", "new@example.com"), "password", "a replacement password"),
				400,
			)
			var unchangedHash string
			if err := tx.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", created.ID).Scan(&unchangedHash); err != nil {
				t.Fatal(err)
			}
			if unchangedHash != storedHash {
				t.Fatal("profile update changed the stored password hash")
			}
			request("DELETE", path, "", http.StatusNoContent)
			var status string
			var deletedAt *time.Time
			var tokenVersion int64
			if err := tx.QueryRow(ctx, "SELECT status, deleted_at, token_version FROM users WHERE id = $1", created.ID).Scan(
				&status,
				&deletedAt,
				&tokenVersion,
			); err != nil {
				t.Fatal(err)
			}
			if status != "deleted" || deletedAt == nil || tokenVersion != 2 {
				t.Fatal("soft deletion did not retain the row and revoke token versions")
			}
			request("GET", path, "", 404)
			request("PUT", path, userProfileBody("Missing", "missing@example.com"), 404)
			request("DELETE", path, "", 404)
			request("DELETE", fmt.Sprintf("/api/v1/users/%d", second.ID), "", 204)
			if body := request("GET", "/api/v1/users", "", 200); !strings.Contains(string(body), "\"users\":[]") {
				t.Fatalf("empty page should contain an array: %s", body)
			}
		})
	}
}
