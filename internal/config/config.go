package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config contains database, HTTP server, and media settings.
type Config struct {
	DatabaseURL string
	Address     string
	JWTSecret   string `json:"-"`
	Media       MediaConfig
}

type MediaConfig struct {
	Enabled              bool
	AccountName          string
	AccountKey           string `json:"-"`
	ContainerName        string
	BlobURL              string
	FFmpegPath           string
	CompressionThreshold int64
}

// Load reads .env when present and validates the process environment.
func Load() (Config, error) {
	if err := LoadEnv(); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Dotenv parse errors can include raw lines containing credentials.
		return Config{}, errors.New("unable to load .env: check file syntax and permissions; use # for comments")
	}
	for _, key := range []string{
		"DB_USER",
		"DB_PASSWORD",
		"DB_HOST",
		"DB_PORT",
		"DB_NAME",
		"DB_SSLMODE",
	} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return Config{}, fmt.Errorf("missing %s: set it in .env or the process environment", key)
		}
	}
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 || strings.TrimSpace(secret) == "" {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 bytes; use a randomly generated secret")
	}
	dbPort := strings.TrimSpace(os.Getenv("DB_PORT"))
	if !validPort(dbPort) {
		return Config{}, errors.New("DB_PORT must be between 1 and 65535")
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	if !validPort(port) {
		return Config{}, errors.New("PORT must be between 1 and 65535")
	}
	host := strings.TrimSpace(os.Getenv("HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	connection := &url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(strings.TrimSpace(os.Getenv("DB_USER")), os.Getenv("DB_PASSWORD")),
		Host:   net.JoinHostPort(strings.TrimSpace(os.Getenv("DB_HOST")), dbPort),
		Path:   "/" + strings.TrimSpace(os.Getenv("DB_NAME")),
	}
	query := url.Values{}
	query.Set("sslmode", strings.TrimSpace(os.Getenv("DB_SSLMODE")))
	connection.RawQuery = query.Encode()
	media, err := loadMedia()
	if err != nil {
		return Config{}, err
	}
	return Config{
		DatabaseURL: connection.String(),
		Address:     net.JoinHostPort(host, port),
		JWTSecret:   secret,
		Media:       media,
	}, nil
}

func loadMedia() (MediaConfig, error) {
	enabled := false
	if value := strings.TrimSpace(os.Getenv("MEDIA_ENABLED")); value != "" {
		var err error
		enabled, err = strconv.ParseBool(value)
		if err != nil {
			return MediaConfig{}, errors.New("MEDIA_ENABLED must be true or false")
		}
	}
	threshold := int64(1 << 20)
	if value := strings.TrimSpace(os.Getenv("MEDIA_COMPRESSION_THRESHOLD_BYTES")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 1 || parsed > 20<<20 {
			return MediaConfig{}, errors.New("MEDIA_COMPRESSION_THRESHOLD_BYTES must be between 1 and 20971520")
		}
		threshold = parsed
	}
	return MediaConfig{
		Enabled:              enabled,
		AccountName:          strings.TrimSpace(os.Getenv("AZURE_STORAGE_ACCOUNT_NAME")),
		AccountKey:           os.Getenv("AZURE_STORAGE_ACCOUNT_KEY"),
		ContainerName:        strings.TrimSpace(os.Getenv("AZURE_STORAGE_CONTAINER_NAME")),
		BlobURL:              strings.TrimSpace(os.Getenv("AZURE_STORAGE_BLOB_URL")),
		FFmpegPath:           strings.TrimSpace(os.Getenv("FFMPEG_PATH")),
		CompressionThreshold: threshold,
	}, nil
}

func validPort(value string) bool {
	port, err := strconv.ParseUint(value, 10, 16)
	return err == nil && port >= 1
}

// LoadEnv loads the nearest .env up to the project root, preserving existing environment variables.
func LoadEnv() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		err := godotenv.Load(filepath.Join(dir, ".env"))
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return os.ErrNotExist
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return os.ErrNotExist
		}
		dir = parent
	}
}
