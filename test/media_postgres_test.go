package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Bukho127/CampusHub_server/internal/auth"
	"github.com/Bukho127/CampusHub_server/internal/config"
	"github.com/Bukho127/CampusHub_server/internal/database"
	"github.com/Bukho127/CampusHub_server/internal/media"
	"github.com/Bukho127/CampusHub_server/internal/server"
	"github.com/Bukho127/CampusHub_server/internal/users"
	"github.com/jackc/pgx/v5"
)

func TestMediaPostgres(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error(err)
		}
	}()
	schema := pgx.Identifier{fmt.Sprintf("media_api_test_%d_%d", os.Getpid(), time.Now().UnixNano())}.Sanitize()
	for _, statement := range []string{"CREATE SCHEMA " + schema, "SET LOCAL search_path TO " + schema} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"000001_create_users.up.sql", "000002_add_password_hash.up.sql", "000003_expand_user_schema.up.sql", "000004_create_media.up.sql"} {
		sql, err := os.ReadFile(filepath.Join("..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	var owner int64
	if err := tx.QueryRow(
		ctx,
		"INSERT INTO users (display_name, email) VALUES ('Media owner', 'media@example.com') RETURNING id",
	).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	_, _, storage := mediaFixture(t, 1)
	processor := mediaProcessor(func(ctx context.Context, input, output, kind string) error {
		return os.WriteFile(output, imageFixture(t, true), 0600)
	})
	uploads := media.NewService(media.NewPostgresRepository(tx), storage, processor, 1)
	authentication, err := auth.NewService(auth.NewPostgresRepository(tx), testTokens)
	if err != nil {
		t.Fatal(err)
	}
	router := server.NewRouter(
		users.NewService(users.NewPostgresRepository(tx)),
		authentication,
		uploads,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	token := mediaToken(t, owner)
	response := mediaUpload(t, router, []uploadFile{{"file", "photo.png", imageFixture(t, false)}}, nil, token)
	if response.Code != 201 {
		t.Fatalf("PostgreSQL upload: %d %s", response.Code, response.Body)
	}
	var f media.File
	if err := json.Unmarshal(response.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if f.OwnerID != owner || !f.Optimized || f.CreatedAt.IsZero() {
		t.Fatal("persisted media metadata is incorrect")
	}
	path := "/api/v1/media/" + f.ID
	for _, target := range []string{path, f.ContentURL, f.OriginalURL, "/api/v1/media"} {
		if got := authRequest(router, "GET", target, "", token); got.Code != 200 {
			t.Fatalf("PostgreSQL media get: %d %s", got.Code, got.Body)
		}
	}
	for range 2 {
		if got := authRequest(router, "DELETE", path, "", token); got.Code != 204 {
			t.Fatalf("PostgreSQL delete: %d %s", got.Code, got.Body)
		}
	}
	if got := authRequest(router, "GET", path, "", token); got.Code != 404 {
		t.Fatal("tombstone remained visible")
	}
	var page media.Page
	if err := json.Unmarshal(authRequest(router, "GET", "/api/v1/media", "", token).Body.Bytes(), &page); err != nil || len(page.Files) != 0 {
		t.Fatal("deleted image remained in listing")
	}
	if len(storage.objects) != 0 {
		t.Fatal("delete left blobs behind")
	}
	rollback, err := os.ReadFile(filepath.Join("..", "migrations", "000004_create_media.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(rollback)); err != nil {
		t.Fatal(err)
	}
	var absent bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('media') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatal("media migration rollback failed")
	}
}
