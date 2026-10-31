package cmd

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
)

func TestNewRemoteFetcherLimitsToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")
	t.Setenv("GITHUB_REPOSITORY_OWNER", "acme")
	t.Setenv(remote.TokenOwnersEnv, "")

	f := newRemoteFetcher(t.TempDir(), time.Hour)
	if f.Token != "secret" || f.TTL != time.Hour {
		t.Errorf("fetcher = %+v", f)
	}
	if want := []string{"SteerSpec", "acme"}; !slices.Equal(f.TokenOwners, want) {
		t.Errorf("TokenOwners = %q, want %q", f.TokenOwners, want)
	}
}

func TestFetchRealmDependency(t *testing.T) {
	realmDir := filepath.Join("some", "realm")

	dir, err := fetchRealmDependency(context.Background(), realmDir, entity.RealmDep{RealmID: "dev.example.dep", Source: "../dep"})
	if err != nil {
		t.Fatalf("relative source: %v", err)
	}
	if want := filepath.Join("some", "dep"); dir != want {
		t.Errorf("relative source = %q, want %q", dir, want)
	}

	for _, source := range []string{"", "github://acme/specs", "/etc", `\etc`, "https://example.com/realm"} {
		if _, err := fetchRealmDependency(context.Background(), realmDir, entity.RealmDep{RealmID: "dev.example.dep", Source: source}); err == nil {
			t.Errorf("source %q: expected error", source)
		}
	}

	for _, source := range []string{"github://acme/specs@v1.0.0/\x1b[2Jrules", "github://acme/specs@v1.0.0/\u009b2Jrules", "../dep\x1b[2J", "../dep\u009b2J"} {
		_, err := fetchRealmDependency(context.Background(), realmDir, entity.RealmDep{RealmID: "dev.example.dep", Source: source})
		if err == nil {
			t.Errorf("source %q: expected error", source)
			continue
		}
		testutil.AssertContains(t, err.Error(), "control character")
	}
}
