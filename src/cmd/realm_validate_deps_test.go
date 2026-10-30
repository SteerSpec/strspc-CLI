package cmd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestFetchRealmDependency(t *testing.T) {
	realmDir := filepath.Join("some", "realm")

	dir, err := fetchRealmDependency(context.Background(), realmDir, entity.RealmDep{RealmID: "dev.example.dep", Source: "../dep"})
	if err != nil {
		t.Fatalf("relative source: %v", err)
	}
	if want := filepath.Join("some", "dep"); dir != want {
		t.Errorf("relative source = %q, want %q", dir, want)
	}

	for _, source := range []string{"", "github://acme/specs", "/etc", "https://example.com/realm", "github://acme/specs@v1.0.0/\x1b[2Jrules", "../dep\x1b[2J", "../dep\u009b2J"} {
		if _, err := fetchRealmDependency(context.Background(), realmDir, entity.RealmDep{RealmID: "dev.example.dep", Source: source}); err == nil {
			t.Errorf("source %q: expected error", source)
		}
	}
}
