package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// cancelDuringValidation cancels when a schema is fetched, which still
// succeeds, as with a cached one.
func cancelDuringValidation(t *testing.T, cancel func()) {
	t.Helper()
	orig := newValidationSchemaFetcher
	newValidationSchemaFetcher = func(opts ...schema.Option) *schema.Fetcher {
		cancel()
		return orig(opts...)
	}
	t.Cleanup(func() { newValidationSchemaFetcher = orig })
}

func TestRewritersStopWhenInterrupted(t *testing.T) {
	for name, tc := range map[string]struct {
		file  string
		setup func(t *testing.T, dir string)
		args  []string
	}{
		"entity rehash": {
			"ENT.json",
			func(t *testing.T, dir string) { setupEntityFile(t, dir, "ENT") },
			[]string{"entity", "rehash", "--all"},
		},
		"realm rehash": {
			"realm.json",
			func(t *testing.T, dir string) {
				writeRealmManifest(t, dir, "dev.app",
					`{"realm_id": "dev.steerspec.core", "version": "`+manifestMinCore.String()+`", "source": "../core/"}`)
			},
			[]string{"realm", "rehash"},
		},
		"realm rewrite-added-by": {
			"APP.json",
			func(t *testing.T, dir string) {
				writeRealmManifest(t, dir, "dev.app")
				writeInheritEntity(t, dir, "APP", "", inheritRule{id: "APP-001", state: "D"})
			},
			[]string{"realm", "rewrite-added-by", "--map", "github:test=github:other"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			p := filepath.Join(dir, tc.file)
			before, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelDuringValidation(t, cancel)

			root := NewRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(append(tc.args, "--dir", dir))
			if err := root.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
				t.Errorf("error = %v, want context.Canceled", err)
			}
			if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
				t.Error("the file was written after the interruption")
			}
		})
	}
}

// add-subrealm writes a realm.json from its parent's.
func TestAddSubRealmRefusesParentThatFailsItsSchema(t *testing.T) {
	parent := t.TempDir()
	setupValidRealm(t, parent)
	p := filepath.Join(parent, "realm.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, bytes.Replace(data, []byte(`"$schema"`), []byte(`"$schemaX"`), 1), 0o600); err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(parent, "child")
	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--parent-dir", parent, "--dir", sub, "--id", "dev.steerspec.test.child", "--title", "Child")
	if err == nil || !strings.Contains(err.Error(), "does not match the realm/v1.json schema") {
		t.Fatalf("error = %v, want a schema refusal", err)
	}
	if _, err := os.Stat(sub); err == nil {
		t.Error("the sub-realm was created")
	}
}

func TestCheckConsumedRefusesDeletedCopy(t *testing.T) {
	for _, deleted := range []string{"lib", "copy"} {
		t.Run(deleted, func(t *testing.T) {
			dir, _ := setupSameIDCopy(t, "the same source")
			r := &checkRun{ctx: context.Background(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, cwd: dir}
			if err := r.loadConfig(); err != nil {
				t.Fatal(err)
			}
			if err := r.resolveRules(); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(dir, deleted, "ACCT.json")); err != nil {
				t.Fatal(err)
			}
			if err := r.validateRuleSchemas(); !errors.Is(err, errDifferingCopies) && !errors.Is(err, errNotValidated) {
				t.Errorf("error = %v, want a refusal: the other copy can't vouch", err)
			}
		})
	}
}
