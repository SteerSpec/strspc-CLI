package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// TestNoteTypesRefuseOldCore refuses to add a `standard` or `structured` note,
// or retype a note into one, under a core pin whose entity schema rejects that
// type, leaving the entity file as it was, and accepts it from the release
// that brings the type.
func TestNoteTypesRefuseOldCore(t *testing.T) {
	for _, tc := range []struct {
		noteType, body, old string
		minCore             *semver.Version
	}{
		{entity.NoteTypeStandard, "iso.27001:A.5", "1.14.0", standardsMinCore},
		{entity.NoteTypeStructured, `json:{"a": 1}`, "1.22.0", structuredMinCore},
	} {
		t.Run(tc.noteType, func(t *testing.T) {
			dir := t.TempDir()
			setupValidRealm(t, dir)
			setupEntityFile(t, dir, "MYE", makeRule("MYE-001", "D", "The system MUST validate.", "github:alice"))
			pinCore(t, dir, tc.old)
			if out, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "MYE-001", "--type", "rationale",
				"--body", "Why.", "--added-by", "github:alice", "--dir", dir); err != nil {
				t.Fatalf("note add rationale: %v\n%s", err, out)
			}
			before, err := os.ReadFile(filepath.Join(dir, "MYE.json"))
			if err != nil {
				t.Fatal(err)
			}

			for _, args := range [][]string{
				{"note", "add", "MYE-001", "--type", tc.noteType, "--body", tc.body, "--added-by", "github:alice"},
				{"note", "update", "MYE-001/01", "--type", tc.noteType, "--body", tc.body},
			} {
				_, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...)
				if err == nil || !strings.Contains(err.Error(), "`type: "+tc.noteType+"`") ||
					!strings.Contains(err.Error(), tc.old) || !strings.Contains(err.Error(), tc.minCore.String()) {
					t.Errorf("%s: error = %v, want a refusal naming the type, the pin and %s", args[1], err, tc.minCore)
				}
			}
			if after, _ := os.ReadFile(filepath.Join(dir, "MYE.json")); !bytes.Equal(before, after) {
				t.Error("the entity file changed on refusal")
			}

			pinCore(t, dir, tc.minCore.String())
			if out, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "MYE-001", "--type", tc.noteType,
				"--body", tc.body, "--added-by", "github:alice", "--dir", dir); err != nil {
				t.Fatalf("note add under %s: %v\n%s", tc.minCore, err, out)
			}

			// A body-only update of that note is refused too once the pin
			// falls below the type's release, as the file would stay invalid.
			pinCore(t, dir, tc.old)
			before, err = os.ReadFile(filepath.Join(dir, "MYE.json"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = testutil.ExecuteCommand(NewRootCmd(), "note", "update", "MYE-001/02", "--body", tc.body+"2", "--dir", dir)
			if err == nil || !strings.Contains(err.Error(), "`type: "+tc.noteType+"`") {
				t.Errorf("body-only update under %s: error = %v, want a refusal naming the type", tc.old, err)
			}
			if after, _ := os.ReadFile(filepath.Join(dir, "MYE.json")); !bytes.Equal(before, after) {
				t.Error("the entity file changed on refusal of a body-only update")
			}
		})
	}
}

// TestNoteTypeOtherTypesUnchecked leaves the types every supported core accepts
// unchecked, also under an old pin, and a body-only update unchecked.
func TestNoteTypeOtherTypesUnchecked(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	setupEntityFile(t, dir, "MYE")
	pinCore(t, dir, "1.14.0")
	path := filepath.Join(dir, "MYE.json")
	for _, noteType := range []string{entity.NoteTypeRationale, ""} {
		if err := checkCoreAcceptsNoteType(dir, path, noteType); err != nil {
			t.Errorf("%q under 1.14.0: %v", noteType, err)
		}
	}
}
