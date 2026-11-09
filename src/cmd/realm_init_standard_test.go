package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

var standardInitArgs = []string{
	"--kind", "standard", "--edition", "5.0.0", "--profiles", "L1,L2,L3", "--license", "CC-BY-SA-4.0",
	"--attribution-text", "OWASP ASVS 5.0.0, by the OWASP Foundation", "--attribution-url", "https://owasp.org/",
}

func runRealmInit(t *testing.T, target string, args ...string) (string, error) {
	t.Helper()
	return testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "init", "--dir", target, "--id", "dev.test.asvs", "--title", "ASVS"}, args...)...)
}

// TestRealmInitStandard scaffolds a Standard Realm that rule add can then
// fill and realm validate accepts.
func TestRealmInitStandard(t *testing.T) {
	setupSchemaServer(t)
	target := filepath.Join(t.TempDir(), "rules")
	out, err := runRealmInit(t, target, standardInitArgs...)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "Transcribe each clause")
	testutil.AssertContains(t, out, "--clause <clause> --profiles L1")

	rf, err := entity.LoadRealm(filepath.Join(target, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := rf.Realm
	if m.Kind != entity.KindStandard || m.Edition != "5.0.0" || m.License != "CC-BY-SA-4.0" ||
		!slices.Equal(m.Profiles, []string{"L1", "L2", "L3"}) || m.Attribution == nil ||
		m.Attribution.Text != "OWASP ASVS 5.0.0, by the OWASP Foundation" || m.Attribution.URL != "https://owasp.org/" {
		t.Errorf("manifest = %+v", m)
	}

	// With real schemas in place of the stubs, the Realm validates once a rule
	// transcribes a clause.
	copyRepoSchemas(t, target)
	setupEntityFile(t, target, "V01")
	if out, err := runRule(t, "add", "V01", "--body", "Verify that X MUST hold.", "--clause", "V1.1.1", "--profiles", "L1,L2,L3",
		"--added-by", "github:alice", "--dir", target); err != nil {
		t.Fatalf("rule add: %v\n%s", err, out)
	}
	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", target, "--strict"); err != nil {
		t.Errorf("realm validate: %v\n%s", err, out)
	}
}

// TestRealmInitCodeRealmFields keeps a code Realm's manifest as it was, and
// lets it have a license and an attribution.
func TestRealmInitCodeRealmFields(t *testing.T) {
	setupSchemaServer(t)
	realmKeys := func(t *testing.T, target string) []string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(target, "realm.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Realm map[string]any `json:"realm"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		var keys []string
		for k := range doc.Realm {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return keys
	}

	target := filepath.Join(t.TempDir(), "rules")
	if _, err := runRealmInit(t, target); err != nil {
		t.Fatal(err)
	}
	if got := realmKeys(t, target); !slices.Equal(got, []string{"id", "title", "version"}) {
		t.Errorf("code realm keys = %q", got)
	}

	target = filepath.Join(t.TempDir(), "rules")
	if _, err := runRealmInit(t, target, "--license", "Apache-2.0", "--attribution-text", "Acme", "--attribution-url", "https://acme.test/"); err != nil {
		t.Fatal(err)
	}
	if got := realmKeys(t, target); !slices.Equal(got, []string{"attribution", "id", "license", "title", "version"}) {
		t.Errorf("licensed code realm keys = %q", got)
	}
}

// TestRealmInitStandardRefusals refuses each incomplete or invalid Standard
// Realm before writing anything.
func TestRealmInitStandardRefusals(t *testing.T) {
	without := func(flag string) []string {
		var out []string
		for i := 0; i < len(standardInitArgs); i += 2 {
			if standardInitArgs[i] != flag {
				out = append(out, standardInitArgs[i], standardInitArgs[i+1])
			}
		}
		return out
	}
	with := func(flag, value string) []string {
		return append(without(flag), flag, value)
	}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "bad kind", args: with("--kind", "code"), wantErr: `invalid --kind "code"`},
		{name: "edition without kind", args: []string{"--edition", "5.0.0"}, wantErr: "--edition and --profiles describe a Standard Realm"},
		{name: "profiles without kind", args: []string{"--profiles", "L1"}, wantErr: "pass --kind standard"},
		{name: "no edition", args: without("--edition"), wantErr: "--edition (STDRLM-002)"},
		{name: "no license", args: without("--license"), wantErr: "--license (STDRLM-003)"},
		{name: "no attribution", args: without("--attribution-text")[:len(without("--attribution-text"))-2], wantErr: "STDRLM-015"},
		{name: "attribution text alone", args: without("--attribution-url"), wantErr: "go together"},
		{name: "attribution url alone", args: without("--attribution-text"), wantErr: "go together"},
		{name: "relative url", args: with("--attribution-url", "owasp.org"), wantErr: "invalid --attribution-url"},
		{name: "spaced license", args: with("--license", " MIT"), wantErr: "invalid --license"},
		{name: "repeated profile", args: with("--profiles", "L1,L1"), wantErr: `lists "L1" twice`},
		{name: "blank profile", args: with("--profiles", "L1,"), wantErr: "invalid --profiles entry"},
		{name: "empty profiles", args: append(without("--profiles"), "--profiles="), wantErr: "--profiles: want one or more profile names"},
		{name: "empty profiles without kind", args: []string{"--profiles="}, wantErr: "pass --kind standard"},
		{name: "empty edition without kind", args: []string{"--edition="}, wantErr: "pass --kind standard"},
		{name: "control character", args: with("--edition", "5\x1b0"), wantErr: "--edition"},
		{name: "old core", args: append(slices.Clone(standardInitArgs), "--core-version", "1.20.0"), wantErr: "a Standard Realm needs dev.steerspec.core " + manifestMinCore.String() + " or later"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupSchemaServer(t)
			target := filepath.Join(t.TempDir(), "rules")
			_, err := runRealmInit(t, target, tt.args...)
			if err == nil {
				t.Fatal("realm init succeeded")
			}
			testutil.AssertContains(t, err.Error(), tt.wantErr)
			if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
				t.Errorf("realm init left %s behind", target)
			}
		})
	}
}
