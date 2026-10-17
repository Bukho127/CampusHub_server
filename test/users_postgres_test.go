package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bukho127/CampusHub_server/internal/config"
	"github.com/Bukho127/CampusHub_server/internal/database"
	"github.com/Bukho127/CampusHub_server/internal/password"
	"github.com/Bukho127/CampusHub_server/internal/users"
	"github.com/jackc/pgx/v5"
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
	migration, err := os.ReadFile(filepath.Join("..", "migrations", "000001_create_users.up.sql"))
	if err != nil {
		t.Fatal(err)
	}

	passwordMigration, err := os.ReadFile(filepath.Join("..", "migrations", "000002_add_password_hash.up.sql"))
	if err != nil {
		t.Fatal(err)
	}

	for _, scenario := range []string{"CRUD", "duplicate create", "duplicate update", "existing profile migration"} {
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
			for _, statement := range []string{
				"CREATE SCHEMA " + schema,
				"SET LOCAL search_path TO " + schema,
				string(migration),
			} {
				if _, err := tx.Exec(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "existing profile migration" {
				if _, err := tx.Exec(ctx, "INSERT INTO users (name, email) VALUES ($1, $2)", "Existing", "existing@example.com"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(ctx, string(passwordMigration)); err != nil {
				t.Fatal(err)
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
			if scenario == "existing profile migration" {
				var id int64
				var hash *string
				if err := tx.QueryRow(ctx, "SELECT id, password_hash FROM users WHERE email = $1", "existing@example.com").Scan(&id, &hash); err != nil {
					t.Fatal(err)
				}
				if hash != nil {
					t.Fatal("migration assigned a password to an existing user")
				}
				path := fmt.Sprintf("/api/v1/users/%d", id)
				request("GET", path, "", 200)
				request("PUT", path, "{\"name\":\"Updated\",\"email\":\"existing@example.com\"}", 200)
				return
			}
			created := decodeUser(request("POST", "/api/v1/users", userCreationBody(" Bukho ", " BUKHO@example.com ", testPassword), 201))
			if created.ID <= 0 || created.Name != "Bukho" || created.Email != "bukho@example.com" || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
				t.Fatalf("unexpected stored profile: %+v", created)
			}
			path := fmt.Sprintf("/api/v1/users/%d", created.ID)
			var storedHash string
			if err := tx.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", created.ID).Scan(&storedHash); err != nil {
				t.Fatal(err)
			}
			if matched, err := password.Verify(storedHash, testPassword); err != nil || !matched || storedHash == testPassword {
				t.Fatal("stored password is not a valid hash")
			}
			switch scenario {
			case "duplicate create":
				request("POST", "/api/v1/users", userCreationBody("Other", "BUKHO@example.com", testPassword), 409)
				return
			case "duplicate update":
				other := decodeUser(request("POST", "/api/v1/users", userCreationBody("Other", "other@example.com", testPassword), 201))
				request("PUT", fmt.Sprintf("/api/v1/users/%d", other.ID), "{\"name\":\"Other\",\"email\":\"bukho@example.com\"}", 409)
				return
			}
			got := decodeUser(request("GET", path, "", 200))
			if got != created {
				t.Fatalf("read profile = %+v, want %+v", got, created)
			}
			second := decodeUser(request("POST", "/api/v1/users", userCreationBody("Robert'); DROP TABLE users;--", "second@example.com", testPassword), 201))
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
			updated := decodeUser(request("PUT", path, "{\"name\":\"New Name\",\"email\":\"new@example.com\"}", 200))
			if updated.ID != created.ID || updated.Name != "New Name" || updated.Email != "new@example.com" || updated.CreatedAt != created.CreatedAt {
				t.Fatalf("unexpected updated profile: %+v", updated)
			}
			request("PUT", path, userCreationBody("New Name", "new@example.com", "a replacement password"), 400)
			var unchangedHash string
			if err := tx.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", created.ID).Scan(&unchangedHash); err != nil {
				t.Fatal(err)
			}
			if unchangedHash != storedHash {
				t.Fatal("profile update changed the stored password hash")
			}
			request("DELETE", path, "", http.StatusNoContent)
			request("GET", path, "", 404)
			request("PUT", path, "{\"name\":\"Missing\",\"email\":\"missing@example.com\"}", 404)
			request("DELETE", path, "", 404)
			request("DELETE", fmt.Sprintf("/api/v1/users/%d", second.ID), "", 204)
			if body := request("GET", "/api/v1/users", "", 200); !strings.Contains(string(body), "\"users\":[]") {
				t.Fatalf("empty page should contain an array: %s", body)
			}
		})
	}
}
