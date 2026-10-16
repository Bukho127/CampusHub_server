package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

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
