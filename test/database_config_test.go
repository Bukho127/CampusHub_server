package test

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bukho127/CampusHub_server/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func databaseEnvironment(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for key, value := range map[string]string{
		"DB_USER": "postgres", "DB_PASSWORD": "pa@ss:/$?# %", "DB_HOST": "::1",
		"DB_PORT": "5432", "DB_NAME": "campus_hub", "DB_SSLMODE": "disable",
		"HOST": "", "PORT": "", "JWT_SECRET": testJWTSecret,
	} {
		t.Setenv(key, value)
	}
}

func TestDatabaseConfig(t *testing.T) {
	databaseEnvironment(t)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	connection, err := url.Parse(settings.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := connection.User.Password()
	if password != os.Getenv("DB_PASSWORD") || connection.Host != "[::1]:5432" || settings.Address != "127.0.0.1:8080" {
		t.Fatal("credentials, IPv6 host, or HTTP defaults were not preserved")
	}
	parsed, err := pgxpool.ParseConfig(settings.DatabaseURL)
	if err != nil {
		t.Fatal("configuration is not accepted by pgx")
	}
	if parsed.ConnConfig.Password != password || parsed.ConnConfig.Host != "::1" || parsed.ConnConfig.Database != "campus_hub" {
		t.Fatal("pgx connection settings do not match the environment")
	}
}

func TestDatabaseConfigRejectsMissingSettings(t *testing.T) {
	for _, key := range []string{"DB_USER", "DB_PASSWORD", "DB_HOST", "DB_PORT", "DB_NAME", "DB_SSLMODE", "JWT_SECRET"} {
		t.Run(key, func(t *testing.T) {
			databaseEnvironment(t)
			t.Setenv(key, "")
			if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("got %v, want missing %s", err, key)
			}
		})
	}
}

func TestConfigDoesNotExposeDotenvContents(t *testing.T) {
	databaseEnvironment(t)
	if err := os.WriteFile(".env", []byte("// invalid comment\nJWT_SECRET=private-test-sentinel\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load()
	if err == nil || strings.Contains(err.Error(), "private-test-sentinel") || !strings.Contains(err.Error(), "load .env") {
		t.Fatal("configuration error was missing or exposed dotenv contents")
	}
}

func TestDatabaseConfigRejectsInvalidPorts(t *testing.T) {
	for _, key := range []string{"DB_PORT", "PORT"} {
		for _, value := range []string{"0", "-1", "+5432", "65536", "abc"} {
			t.Run(key+"="+value, func(t *testing.T) {
				databaseEnvironment(t)
				t.Setenv(key, value)
				if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("got %v, want invalid %s", err, key)
				}
			})
		}
	}
}
