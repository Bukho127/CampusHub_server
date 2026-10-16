package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Bukho127/CampusHub_server/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Load .env from the working directory or a parent up to the project root.
	err := config.LoadEnv()
	if errors.Is(err, os.ErrNotExist) {
		log.Println("No .env file found, reading from system environment.")
	} else if err != nil {
		log.Fatalf("Unable to load .env: %v", err)
	}

	// Require explicit settings instead of falling back to the Windows username.
	for _, key := range []string{"DB_USER", "DB_PASSWORD", "DB_HOST", "DB_PORT", "DB_NAME", "DB_SSLMODE"} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			log.Fatalf("Missing %s: set it in the project-root .env or system environment.", key)
		}
	}

	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbName := os.Getenv("DB_NAME")
	dbSSL := os.Getenv("DB_SSLMODE")

	// Escape credentials so special characters in passwords are preserved.
	connURL := &url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(dbUser, dbPassword),
		Host:   dbHost + ":" + dbPort,
		Path:   "/" + dbName,
	}
	query := url.Values{}
	query.Set("sslmode", dbSSL)
	connURL.RawQuery = query.Encode()

	// 4. Create a timeout context for network execution safety
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Printf("Attempting to connect to host '%s' on port %s...\n", dbHost, dbPort)

	dbpool, err := pgxpool.New(ctx, connURL.String())
	if err != nil {
		log.Fatalf("Unable to initialize connection pool config: %v\n", err)
	}
	defer dbpool.Close()

	err = dbpool.Ping(ctx)
	if err != nil {
		log.Fatalf("Connection failed for user %q, database %q at %s:%s: %v", dbUser, dbName, dbHost, dbPort, err)
	}

	fmt.Println("Connection successful! Environment configuration and PostgreSQL are linked.")
}
