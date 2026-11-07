package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadStrspcConfigErrors(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := readStrspcConfig(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "run strspc init first") {
			t.Fatalf("error = %v, want a prompt to run strspc init", err)
		}
	})

	t.Run("config.yaml is a directory", func(t *testing.T) {
		cwd := t.TempDir()
		if err := os.MkdirAll(filepath.Join(cwd, ".strspc", "config.yaml"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := readStrspcConfig(cwd)
		if err == nil || !strings.Contains(err.Error(), "reading config.yaml") {
			t.Fatalf("error = %v, want reading config.yaml", err)
		}
	})

	// .strspc is a file: Unix reports the path as not a directory, Windows as
	// not found.
	t.Run(".strspc is a file", func(t *testing.T) {
		cwd := t.TempDir()
		if err := os.WriteFile(filepath.Join(cwd, ".strspc"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := readStrspcConfig(cwd)
		if err == nil || !strings.Contains(err.Error(), "config.yaml") {
			t.Fatalf("error = %v, want one naming config.yaml", err)
		}
	})
}
