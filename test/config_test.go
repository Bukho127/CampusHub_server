package test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bukho127/CampusHub_server/internal/config"
)

func TestLoadEnvFromAPIDirectory(t *testing.T) {
	const key = "CAMPUSHUB_ENV_TEST"
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "loads root env", true: "preserves system env"}[existing], func(t *testing.T) {
			root := t.TempDir()
			apiDir := filepath.Join(root, "cmd", "api")
			if err := os.MkdirAll(apiDir, 0755); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{"go.mod": "module example.com/test\n", ".env": key + "=from-file\n"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv(key, "from-system")
			want := "from-system"
			if !existing {
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
				want = "from-file"
			}
			t.Chdir(apiDir)
			if err := config.LoadEnv(); err != nil {
				t.Fatal(err)
			}
			if got := os.Getenv(key); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestLoadEnvStopsAtProjectRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(root, "go.mod"): "module example.com/test\n",
		filepath.Join(parent, ".env"): "INVALID ENV CONTENT\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	if err := config.LoadEnv(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want a missing .env error", err)
	}
}
