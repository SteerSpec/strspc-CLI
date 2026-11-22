package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// A rule file and a realm manifest are validated against their JSON schemas
// before a command evaluates or changes them (SEC-004): check, rule verify
// and rule promote before evaluating, and every command that writes an
// entity file or a realm.json before applying its change. lint and realm
// validate run the same checks as RL002 and RM002.
//
// The schemas come from the schema cache. One that isn't cached and can't be
// fetched fails the command, so that nothing is used unvalidated: `strspc
// schema fetch` fills the cache for offline use.

// newValidationSchemaFetcher builds the fetcher of the SEC-004 validation,
// on the shared schema cache. It is separate from lint's newSchemaFetcher so
// that a test switching lint's schema check off can't switch this one off.
var newValidationSchemaFetcher = newCachedSchemaFetcher

// maxViolationsShown bounds the violations a schema error lists.
const maxViolationsShown = 3

// errSchemaUnavailable marks a schema that could neither be read from the
// cache nor fetched.
var errSchemaUnavailable = errors.New("schema unavailable")

// schemaValidator validates files against the entity and realm schemas of
// the realm holding them, compiling each schema once.
type schemaValidator struct {
	ctx      context.Context
	compiled map[string]*jsonschema.Schema
	options  map[string][]schema.Option // by realm directory
	labels   map[string]string          // by realm directory
	done     map[string]bool            // validated paths
}

func newSchemaValidator(ctx context.Context) *schemaValidator {
	if ctx == nil {
		ctx = context.Background()
	}
	return &schemaValidator{
		ctx:      ctx,
		compiled: map[string]*jsonschema.Schema{},
		options:  map[string][]schema.Option{},
		labels:   map[string]string{},
		done:     map[string]bool{},
	}
}

// realmSchemaFor returns the schema options and a label for the realm in dir
// (dir == "" for a file outside any realm), as lint selects them: those of
// the core release core itself is, or of the core release the realm pins; for
// a sub-realm that pins none, those of the realm listing it; otherwise the
// latest schemas.
func (v *schemaValidator) realmSchemaFor(dir string) (opts []schema.Option, label string) {
	return v.realmSchemaAt(dir, 0)
}

func (v *schemaValidator) realmSchemaAt(dir string, depth int) (opts []schema.Option, label string) {
	if cached, ok := v.options[dir]; ok {
		return cached, v.labels[dir]
	}
	label = "the latest schemas"
	if dir != "" {
		if rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json")); err == nil {
			switch dep := corePin(rf); {
			case rf.Realm.ID == coreRealmID && schemaVersion.MatchString(rf.Realm.Version):
				opts = []schema.Option{schema.WithCoreVersion(rf.Realm.Version)}
				label = coreRealmID + " " + rf.Realm.Version
			case dep != nil && schemaVersion.MatchString(dep.Version):
				opts = schema.ForRealm(dir)
				label = coreRealmID + " " + dep.Version
			default:
				if parent, ok := listingRealm(dir); ok && depth < maxSubRealmDepth {
					opts, label = v.realmSchemaAt(parent, depth+1)
				}
			}
		}
	}
	v.options[dir], v.labels[dir] = opts, label
	return opts, label
}

// listingRealm returns the directory above dir when the realm.json there
// lists dir as one of its sub_realms.
func listingRealm(dir string) (string, bool) {
	parent := filepath.Dir(dir)
	if parent == dir {
		return "", false
	}
	rf, err := entity.LoadRealm(filepath.Join(parent, "realm.json"))
	if err != nil {
		return "", false
	}
	return parent, slices.Contains(rf.SubRealms, filepath.Base(dir))
}

// compile fetches and compiles schemaPath for the realm in dir.
func (v *schemaValidator) compile(dir, schemaPath string) (*jsonschema.Schema, string, error) {
	opts, label := v.realmSchemaFor(dir)
	key := label + "\x00" + schemaPath
	if c, ok := v.compiled[key]; ok {
		return c, label, nil
	}
	fetcher := newValidationSchemaFetcher(opts...)
	if fetcher == nil {
		return nil, label, fmt.Errorf("%w: no schema fetcher", errSchemaUnavailable)
	}
	data, err := fetcher.Fetch(v.ctx, schemaPath)
	if err != nil {
		return nil, label, fmt.Errorf("%w: the %s schema of %s is not cached and can't be fetched; "+
			"run `strspc schema fetch` while online: %w", errSchemaUnavailable, schemaPath, label, err)
	}
	sch, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, label, fmt.Errorf("parsing the %s schema of %s: %w", schemaPath, label, err)
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(jsonschema.SchemeURLLoader{}) // the schema must not reach beyond itself
	if err := c.AddResource("schema.json", sch); err != nil {
		return nil, label, fmt.Errorf("compiling the %s schema of %s: %w", schemaPath, label, err)
	}
	compiled, err := c.Compile("schema.json")
	if err != nil {
		return nil, label, fmt.Errorf("compiling the %s schema of %s: %w", schemaPath, label, err)
	}
	v.compiled[key] = compiled
	return compiled, label, nil
}

// validate checks data, the content of path, against schemaPath of the realm
// in dir.
func (v *schemaValidator) validate(path string, data []byte, dir, schemaPath string) error {
	compiled, label, err := v.compile(dir, schemaPath)
	if err != nil {
		return fmt.Errorf("can't validate %s: %w", path, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if err := compiled.Validate(doc); err != nil {
		msgs := schemaViolations(err)
		shown := msgs
		if len(shown) > maxViolationsShown {
			shown = shown[:maxViolationsShown]
		}
		more := ""
		if n := len(msgs) - len(shown); n > 0 {
			more = fmt.Sprintf(" (and %d more; strspc lint lists them)", n)
		}
		return fmt.Errorf("%s does not match the %s schema of %s: %s%s", path, schemaPath, label, strings.Join(shown, "; "), more)
	}
	return nil
}

// validateEntityFile checks an entity file against the entity schema of the
// realm holding it.
func (v *schemaValidator) validateEntityFile(path string, data []byte) error {
	if v.done[path] {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("can't validate %s: %w", path, err)
	}
	dir, err := realmDirOf(abs, filepath.VolumeName(abs)+string(filepath.Separator))
	if err != nil {
		return fmt.Errorf("can't validate %s: %w", path, err)
	}
	if err := v.validate(path, data, dir, schema.EntityV1Path); err != nil {
		return err
	}
	v.done[path] = true
	return nil
}

// validateRealmManifest checks the realm.json at path against the realm
// schema of that realm.
func (v *schemaValidator) validateRealmManifest(path string, data []byte) error {
	if v.done[path] {
		return nil
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("can't validate %s: %w", path, err)
	}
	if err := v.validate(path, data, dir, schema.RealmV1Path); err != nil {
		return err
	}
	v.done[path] = true
	return nil
}

// validateRealmTree checks the realm manifest in dir, every entity file of the
// realm, and its sub-realms in turn, each against its own realm's schemas.
func (v *schemaValidator) validateRealmTree(dir string) error {
	return v.validateRealmTreeAt(dir, 0)
}

func (v *schemaValidator) validateRealmTreeAt(dir string, depth int) error {
	manifest := filepath.Join(dir, "realm.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return fmt.Errorf("reading %s: %w", manifest, err)
	}
	if err := v.validateRealmManifest(manifest, data); err != nil {
		return err
	}
	rf, err := entity.ParseRealm(data)
	if err != nil {
		return fmt.Errorf("reading %s: %w", manifest, err)
	}
	err = entity.WalkEntityFiles(dir, func(path string, data []byte, _ *entity.File, parseErr error) error {
		if data == nil {
			return fmt.Errorf("reading %s: %w", path, parseErr)
		}
		return v.validateEntityFile(path, data)
	}, entity.WithExcludeDirs(rf.SubRealms))
	if err != nil {
		return err
	}
	if depth >= maxSubRealmDepth {
		return nil
	}
	for _, sub := range subRealmsOf(dir, rf) {
		if err := v.validateRealmTreeAt(sub.dir, depth+1); err != nil {
			return err
		}
	}
	return nil
}
