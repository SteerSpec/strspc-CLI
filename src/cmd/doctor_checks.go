package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/providers"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// doctorProject is what the config checks parsed, for the checks that follow.
// cfg is nil when there is no config or it doesn't parse.
type doctorProject struct {
	cfg *strspcConfig
	ttl time.Duration
	// claude reports that the evaluator is claude, so it needs an API key.
	claude bool
	// realmDirs are the local sources holding a realm.json.
	realmDirs []doctorRealmDir
}

type doctorRealmDir struct {
	source string // as configured
	dir    string // joined with the project directory
}

func doctorOutcome(group, name, subject, status, message, remedy string) doctorResult {
	return doctorResult{
		ID: group + "." + name, Group: group, Subject: subject,
		Status: status, Message: message, Remedy: remedy,
	}
}

// skipped reports checks that couldn't run because an earlier one failed.
func skipped(group, why string, names ...string) []doctorResult {
	out := make([]doctorResult, 0, len(names))
	for _, n := range names {
		out = append(out, doctorOutcome(group, n, "", doctorSkip, why, ""))
	}
	return out
}

// --- install ---

const doctorInstallRemedy = "install a release: see https://github.com/SteerSpec/strspc-CLI#install"

func doctorInstallChecks(env *doctorEnv) []doctorResult {
	const g = "install"
	var out []doctorResult

	if v := env.version; v == "" || v == "dev" {
		out = append(out, doctorOutcome(g, "version", "", doctorWarn,
			fmt.Sprintf("%q is a development build, not a release", v),
			doctorInstallRemedy))
	} else {
		out = append(out, doctorOutcome(g, "version", "", doctorPass, v, ""))
	}

	return append(out, doctorPathCheck(env), doctorOutcome(g, "platform", "", doctorInfo,
		fmt.Sprintf("%s/%s, %s", env.goos, env.goarch, env.goVersion), ""))
}

// doctorPathCheck compares the strspc found on PATH with the running binary.
func doctorPathCheck(env *doctorEnv) doctorResult {
	const g = "install"
	self, err := env.executable()
	if err != nil {
		return doctorOutcome(g, "path", "", doctorSkip, "can't locate the running binary: "+err.Error(), "")
	}
	self = resolvedPath(self)
	found, err := env.lookPath("strspc")
	if err != nil {
		return doctorOutcome(g, "path", "", doctorWarn,
			"strspc is not on PATH (running "+self+")",
			"add "+filepath.Dir(self)+" to PATH")
	}
	found = resolvedPath(found)
	if !samePath(env.goos, found, self) {
		return doctorOutcome(g, "path", "", doctorWarn,
			found+" comes first on PATH, not this binary ("+self+")",
			"remove "+found+", or put "+filepath.Dir(self)+" before it on PATH")
	}
	return doctorOutcome(g, "path", "", doctorPass, found, "")
}

// resolvedPath is p made absolute with its symlinks evaluated, or p when that
// fails.
func resolvedPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if target, err := filepath.EvalSymlinks(p); err == nil {
		p = target
	}
	return p
}

// samePath compares two resolved paths, ignoring case on Windows.
func samePath(goos, a, b string) bool {
	if goos == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// --- tools ---

func doctorToolChecks(env *doctorEnv) []doctorResult {
	const g = "tools"
	if p, err := env.lookPath("git"); err == nil {
		return []doctorResult{doctorOutcome(g, "git", "", doctorPass, p, "")}
	}
	const remedy = "install git: diff, refs and check --diff need it"
	if inGitRepo(env.cwd) {
		return []doctorResult{doctorOutcome(g, "git", "", doctorFail, "git is not on PATH, and the project is a git repository", remedy)}
	}
	return []doctorResult{doctorOutcome(g, "git", "", doctorWarn, "git is not on PATH", remedy)}
}

// inGitRepo reports whether dir or one of its parents holds a .git entry: a
// directory, or a file in a worktree or submodule. It doesn't need git.
func inGitRepo(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return false
		}
		abs = parent
	}
}

// --- config ---

var doctorConfigNames = []string{"parse", "schema", "ttl", "states", "scopes", "provider"}

func doctorConfigChecks(env *doctorEnv) ([]doctorResult, *doctorProject) {
	const g = "config"
	project := &doctorProject{}
	path := filepath.Join(env.cwd, ".strspc", "config.yaml")
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return append([]doctorResult{doctorOutcome(g, "present", "", doctorWarn,
			"no .strspc/config.yaml", "strspc init")},
			skipped(g, "no configuration", doctorConfigNames...)...), project
	case err != nil:
		return append([]doctorResult{doctorOutcome(g, "present", "", doctorFail,
			"can't read .strspc/config.yaml: "+err.Error(), "make .strspc/config.yaml a readable file")},
			skipped(g, "no configuration", doctorConfigNames...)...), project
	}
	out := []doctorResult{doctorOutcome(g, "present", "", doctorPass, ".strspc/config.yaml", "")}

	cfg, err := parseStrspcConfig(raw)
	if err != nil {
		out = append(out, doctorOutcome(g, "parse", "", doctorFail, err.Error(), "fix the YAML in .strspc/config.yaml"))
		return append(out, skipped(g, "the configuration doesn't parse", doctorConfigNames[1:]...)...), project
	}
	project.cfg = cfg
	out = append(out,
		doctorOutcome(g, "parse", "", doctorPass, "valid YAML", ""),
		doctorConfigSchemaCheck(env, raw),
		doctorTTLCheck(cfg, project),
		doctorStatesCheck(cfg))
	if _, err := buildSourceEntries(cfg); err != nil {
		out = append(out, doctorOutcome(g, "scopes", "", doctorFail, err.Error(), "set each source's scope to local or global"))
	} else {
		out = append(out, doctorOutcome(g, "scopes", "", doctorPass, fmt.Sprintf("%d source(s)", len(cfg.Rules)), ""))
	}
	out = append(out, doctorProviderCheck(env, cfg, project))
	return out, project
}

// doctorConfigSchemaCheck validates the config against the cached config.v1
// schema, the one `strspc check` caches in .strspc/cache/schemas.
func doctorConfigSchemaCheck(env *doctorEnv, raw []byte) doctorResult {
	const g = "config"
	fetcher := schema.New(
		schema.WithCacheDir(filepath.Join(env.cwd, ".strspc", "cache", "schemas")),
		schema.WithClient(env.httpClient),
	)
	compiled, err := compileConfigSchema(env.ctx, fetcher)
	if err != nil {
		if errors.Is(err, errDoctorOffline) {
			return doctorOutcome(g, "schema", "", doctorSkip, "the config.v1 schema is not cached", "strspc check caches it")
		}
		return doctorOutcome(g, "schema", "", doctorSkip, err.Error(), "")
	}
	violations := configSchemaViolations(compiled, raw)
	switch len(violations) {
	case 0:
		return doctorOutcome(g, "schema", "", doctorPass, "matches config.v1", "")
	case 1:
		return doctorOutcome(g, "schema", "", doctorWarn, violations[0], "fix .strspc/config.yaml")
	default:
		return doctorOutcome(g, "schema", "", doctorWarn,
			fmt.Sprintf("%s (and %d more)", violations[0], len(violations)-1), "fix .strspc/config.yaml")
	}
}

func doctorTTLCheck(cfg *strspcConfig, project *doctorProject) doctorResult {
	const g = "config"
	if cfg.Cache.TTL == "" {
		return doctorOutcome(g, "ttl", "", doctorPass, "default ("+effectiveTTL(0).String()+")", "")
	}
	ttl, err := time.ParseDuration(cfg.Cache.TTL)
	if err != nil {
		return doctorOutcome(g, "ttl", "", doctorFail,
			fmt.Sprintf("cache.ttl %q is not a duration", cfg.Cache.TTL), "set cache.ttl to a duration such as 24h")
	}
	project.ttl = ttl
	return doctorOutcome(g, "ttl", "", doctorPass, effectiveTTL(ttl).String(), "")
}

func doctorStatesCheck(cfg *strspcConfig) doctorResult {
	const g = "config"
	for _, f := range []struct {
		name   string
		states []string
	}{{"fail_on", cfg.FailOn}, {"evaluate_on", cfg.EvaluateOn}} {
		if err := validateStateCodes(f.name, normalizeStateCodes(f.states)); err != nil {
			return doctorOutcome(g, "states", "", doctorFail, err.Error(), "fix "+f.name+" in .strspc/config.yaml")
		}
	}
	return doctorOutcome(g, "states", "", doctorPass, "fail_on: "+orNotSet(strings.Join(normalizeStateCodes(cfg.FailOn), ", ")), "")
}

// doctorProviderCheck configures the provider as `strspc check` would, with a
// placeholder key and no network, to find the errors check exits 2 on.
func doctorProviderCheck(env *doctorEnv, cfg *strspcConfig, project *doctorProject) doctorResult {
	const g = "config"
	const remedy = "set evaluator.provider to claude, or null for static checks"
	name := strings.TrimSpace(cfg.Evaluator.Provider)
	if isStaticProviderName(name) {
		return doctorOutcome(g, "provider", "", doctorPass, "none: static checks only", "")
	}
	if err := rejectControlChars("evaluator.provider", name); err != nil {
		return doctorOutcome(g, "provider", "", doctorFail, err.Error(), remedy)
	}
	if err := rejectControlChars("evaluator.model", cfg.Evaluator.Model); err != nil {
		return doctorOutcome(g, "provider", "", doctorFail, err.Error(), "fix evaluator.model in .strspc/config.yaml")
	}
	endpoint, err := resolveEndpoint(cfg.Evaluator.Endpoint, "", env.getenv(envEvaluatorEndpoint))
	if err != nil {
		return doctorOutcome(g, "provider", "", doctorFail, err.Error(), "remove evaluator.endpoint, or set "+envEvaluatorEndpoint)
	}
	_, err = providers.New(name, providers.Options{
		APIKey:     "doctor-placeholder", // only the configuration is checked
		Model:      cfg.Evaluator.Model,
		Endpoint:   endpoint,
		Effort:     cfg.Evaluator.Effort,
		HTTPClient: env.httpClient,
	})
	switch {
	case err == nil:
		project.claude = true
		return doctorOutcome(g, "provider", "", doctorPass, name, "")
	case errors.Is(err, providers.ErrProviderNotImplemented):
		return doctorOutcome(g, "provider", "", doctorFail, fmt.Sprintf("provider %q is not implemented yet", name), remedy)
	case errors.Is(err, providers.ErrUnknownProvider):
		return doctorOutcome(g, "provider", "", doctorFail, fmt.Sprintf("provider %q is not a known provider", name), remedy)
	default:
		return doctorOutcome(g, "provider", "", doctorFail, err.Error(), "fix the evaluator section of .strspc/config.yaml")
	}
}

// --- credentials ---

func doctorCredentialChecks(env *doctorEnv, project *doctorProject) []doctorResult {
	const g = "credentials"
	if project.cfg == nil {
		return skipped(g, "no usable configuration", "provider", "github")
	}
	var out []doctorResult
	switch {
	case !project.claude:
		out = append(out, doctorOutcome(g, "provider", "", doctorSkip, "no AI provider configured, so no key is needed", ""))
	case env.getenv(claude.EnvAPIKey) == "":
		out = append(out, doctorOutcome(g, "provider", "", doctorWarn,
			claude.EnvAPIKey+" is not set: strspc check exits 3 until it is",
			"export "+claude.EnvAPIKey+", or set evaluator.provider: null"))
	default:
		out = append(out, doctorOutcome(g, "provider", "", doctorPass, claude.EnvAPIKey+" is set", ""))
	}

	n := len(out)
	for _, r := range project.cfg.Rules {
		if !strings.HasPrefix(r.Source, remote.Scheme) {
			continue
		}
		ref, err := remote.ParseGitHub(r.Source)
		if err != nil {
			continue // sources.state reports it
		}
		repo := ref.Owner + "/" + ref.Repo
		switch {
		case remote.TokenFromEnv() == "":
			out = append(out, doctorOutcome(g, "github", r.Source, doctorWarn,
				repo+": no GITHUB_TOKEN or GH_TOKEN, so a private repository can't be fetched",
				"export GITHUB_TOKEN if "+repo+" is private"))
		case remote.TokenForOwner(ref.Owner) == "":
			out = append(out, doctorOutcome(g, "github", r.Source, doctorWarn,
				repo+": the token is not sent to "+ref.Owner,
				"add "+ref.Owner+" to "+remote.TokenOwnersEnv+" if "+repo+" is private"))
		default:
			out = append(out, doctorOutcome(g, "github", r.Source, doctorPass, repo+": the token is sent", ""))
		}
	}
	if len(out) == n {
		out = append(out, doctorOutcome(g, "github", "", doctorSkip, "no github:// source", ""))
	}
	return out
}

// --- sources ---

func doctorSourceChecks(env *doctorEnv, project *doctorProject) []doctorResult {
	const g = "sources"
	if project.cfg == nil {
		return skipped(g, "no usable configuration", "state")
	}
	if len(project.cfg.Rules) == 0 {
		return []doctorResult{doctorOutcome(g, "state", "", doctorWarn, "no rule source configured", "add one under rules: in .strspc/config.yaml")}
	}
	fetcher := &remote.Fetcher{CacheDir: filepath.Join(env.cwd, ".strspc", "cache"), TTL: project.ttl}
	out := make([]doctorResult, 0, len(project.cfg.Rules))
	for _, r := range project.cfg.Rules {
		s, dir := sourceStatus(env.cwd, fetcher, r.Source)
		s.Scope = r.Scope
		if dir != "" {
			s.Rules = countRules(dir)
		}
		msg := sourceLine(&s, env.now())
		switch s.State {
		case sourceLocal, sourcePinned, sourceFresh:
			out = append(out, doctorOutcome(g, "state", r.Source, doctorPass, msg, ""))
		case sourceStale, sourceNotSynced:
			out = append(out, doctorOutcome(g, "state", r.Source, doctorWarn, msg, "strspc sync"))
		case sourceMissing:
			out = append(out, doctorOutcome(g, "state", r.Source, doctorFail, msg, "create it, or fix the source in .strspc/config.yaml"))
		default:
			out = append(out, doctorOutcome(g, "state", r.Source, doctorFail, msg, "fix the source in .strspc/config.yaml"))
		}
		if s.State == sourceLocal {
			if _, err := os.Stat(filepath.Join(dir, "realm.json")); err == nil {
				project.realmDirs = append(project.realmDirs, doctorRealmDir{source: r.Source, dir: dir})
			}
		}
	}
	return out
}

// --- realm ---

func doctorRealmChecks(env *doctorEnv, project *doctorProject) []doctorResult {
	const g = "realm"
	if len(project.realmDirs) == 0 {
		return skipped(g, "no local source holds a realm.json", "validate", "core")
	}
	var out []doctorResult
	for _, rd := range project.realmDirs {
		out = append(out, doctorRealmValidate(env, rd), doctorRealmCore(rd))
	}
	return out
}

// doctorRealmValidate lints the realm as `strspc realm validate` does, against
// cached schemas and dependencies only, and reports the counts.
func doctorRealmValidate(env *doctorEnv, rd doctorRealmDir) doctorResult {
	const g = "realm"
	remedy := "strspc realm validate " + shellQuote(rd.source)
	schemaOpts := []schema.Option{schema.WithClient(env.httpClient)}
	if missing := uncachedRealmSchemas(env.ctx, rd.dir, schemaOpts); missing != "" {
		return doctorOutcome(g, "validate", rd.source, doctorSkip,
			rd.source+": "+missing+" schemas are not cached", remedy+" caches them")
	}

	vs := buildRealmValidator(rd.dir, false, false, false, schemaOpts, doctorDependencyFetcher(env))
	res := vs.realmLinter.Lint(env.ctx, rd.dir)
	var errs, warns int
	for _, d := range res.Diagnostics {
		switch {
		case d.Severity == result.Error && strings.Contains(d.Message, errDoctorOffline.Error()):
			// A schema the pre-check didn't ask for, such as a sub-realm's.
			return doctorOutcome(g, "validate", rd.source, doctorSkip,
				rd.source+": a schema is not cached", remedy+" caches it")
		case d.Severity == result.Error:
			errs++
		case d.Severity == result.Warning:
			warns++
		}
	}
	msg := fmt.Sprintf("%s: %d error(s), %d warning(s)", rd.source, errs, warns)
	switch {
	case errs > 0:
		return doctorOutcome(g, "validate", rd.source, doctorFail, msg, remedy)
	case warns > 0:
		return doctorOutcome(g, "validate", rd.source, doctorWarn, msg, remedy)
	default:
		return doctorOutcome(g, "validate", rd.source, doctorPass, rd.source+": valid", "")
	}
}

// uncachedRealmSchemas names the core version whose realm and entity schemas
// the realm in dir needs and the cache lacks ("latest" without a pin), or
// returns "" when both are cached.
func uncachedRealmSchemas(ctx context.Context, dir string, extra []schema.Option) string {
	opts := realmSchemaOptions(dir, false)
	fetcher := newSchemaFetcher(append(opts, extra...)...)
	if _, err := fetcher.RealmV1(ctx); err != nil {
		return realmSchemaLabel(dir)
	}
	if _, err := fetcher.EntityV1(ctx); err != nil {
		return realmSchemaLabel(dir)
	}
	return ""
}

func realmSchemaLabel(dir string) string {
	if rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json")); err == nil {
		if dep := corePin(rf); dep != nil {
			return coreRealmID + " " + dep.Version
		}
	}
	return "the latest"
}

// doctorDependencyFetcher finds a realm's dependency the way
// fetchRealmDependency does, but only in the cache: a dependency that was
// never fetched is reported (RM016, a warning) instead of downloaded.
func doctorDependencyFetcher(env *doctorEnv) func(context.Context, string, entity.RealmDep) (string, error) {
	return func(ctx context.Context, realmDir string, dep entity.RealmDep) (string, error) {
		if !strings.HasPrefix(dep.Source, remote.Scheme) {
			return fetchRealmDependency(ctx, realmDir, dep) // a local path: no network
		}
		ref, err := remote.ParseGitHub(dep.Source)
		if err != nil {
			return "", err
		}
		cacheDir, err := env.userCacheDir()
		if err != nil {
			return "", fmt.Errorf("locating user cache: %w", err)
		}
		f := &remote.Fetcher{CacheDir: filepath.Join(cacheDir, "strspc", "realms")}
		entry, err := f.Cached(ref)
		if err != nil {
			return "", err
		}
		if entry.Dir == "" {
			return "", errDoctorOffline
		}
		return entry.Dir, nil
	}
}

// doctorRealmCore checks that the core pin accepts the standards the realm, its
// sub-realms and their entities declare.
func doctorRealmCore(rd doctorRealmDir) doctorResult {
	const g = "realm"
	paths := standardsDeclarations(rd.dir)
	if len(paths) == 0 {
		return doctorOutcome(g, "core", rd.source, doctorSkip, rd.source+": no standards declared", "")
	}
	for _, path := range paths {
		if checkCoreAcceptsStandards(rd.dir, path) == nil {
			continue
		}
		p := corePinFor(rd.dir, path)
		rel, err := filepath.Rel(rd.dir, path)
		if err != nil {
			rel = path
		}
		return doctorOutcome(g, "core", rd.source, doctorFail,
			fmt.Sprintf("%s: %s declares standards, which %s %s rejects (it needs %s or later)",
				rd.source, filepath.ToSlash(rel), coreRealmID, p.version, standardsMinCore),
			corePinRemedy(*p))
	}
	return doctorOutcome(g, "core", rd.source, doctorPass,
		fmt.Sprintf("%s: the core pin accepts the standards of %d file(s)", rd.source, len(paths)), "")
}

// standardsDeclarations lists the files under dir that declare standards: the
// realm manifests (dir's and its sub-realms') and the entity files.
func standardsDeclarations(dir string) []string {
	var paths []string
	manifests := []string{filepath.Join(dir, "realm.json")}
	if rf, err := entity.LoadRealm(manifests[0]); err == nil {
		for _, sub := range rf.SubRealms {
			manifests = append(manifests, filepath.Join(dir, sub, "realm.json"))
		}
	}
	for _, m := range manifests {
		if rf, err := entity.LoadRealm(m); err == nil && len(rf.Realm.Standards) > 0 {
			paths = append(paths, m)
		}
	}
	// A file that doesn't parse is realm.validate's to report.
	_ = entity.WalkEntityFiles(dir, func(path string, _ []byte, ef *entity.File, _ error) error {
		if ef != nil && declaresStandards(ef) {
			paths = append(paths, path)
		}
		return nil
	})
	return paths
}

func declaresStandards(ef *entity.File) bool {
	if len(ef.Entity.Standards) > 0 {
		return true
	}
	for i := range ef.SubEntities {
		if declaresStandards(&ef.SubEntities[i]) {
			return true
		}
	}
	return false
}

// --- project ---

func doctorProjectChecks(env *doctorEnv) []doctorResult {
	return []doctorResult{doctorGitignoreCheck(env), doctorCICheck(env)}
}

// doctorCachePaths are the paths strspc writes caches to, which `strspc init`
// git-ignores in .strspc/.gitignore.
var doctorCachePaths = []string{".strspc/cache/", ".strspc/cache.db"}

func doctorGitignoreCheck(env *doctorEnv) doctorResult {
	const g = "project"
	git, err := env.lookPath("git")
	if err != nil {
		return doctorOutcome(g, "gitignore", "", doctorSkip, "git is not on PATH", "")
	}
	if !inGitRepo(env.cwd) {
		return doctorOutcome(g, "gitignore", "", doctorSkip, "not a git repository", "")
	}
	var notIgnored []string
	for _, p := range doctorCachePaths {
		// Exit 0: ignored. 1: not ignored, or tracked. Anything else: git failed.
		err := exec.CommandContext(env.ctx, git, "-C", env.cwd, "check-ignore", "-q", p).Run()
		var exitErr *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
			notIgnored = append(notIgnored, p)
		default:
			return doctorOutcome(g, "gitignore", "", doctorSkip, "git check-ignore failed: "+err.Error(), "")
		}
	}
	if len(notIgnored) > 0 {
		return doctorOutcome(g, "gitignore", "", doctorWarn,
			strings.Join(notIgnored, " and ")+" not ignored by git, or tracked",
			"add cache/ and cache.db to .strspc/.gitignore, and git rm -r --cached what is tracked")
	}
	return doctorOutcome(g, "gitignore", "", doctorPass, "the caches are ignored by git", "")
}

// strspcCheckRun matches a `run:` script that invokes strspc check as a command:
// at the start of a line or after a shell separator, so that an echo or a
// quoted mention doesn't count.
var strspcCheckRun = regexp.MustCompile(`(?m)(^|[;&|(]|&&|\|\|)\s*(\S*/)?strspc\s+check\b`)

// strspcCheckUses matches a `uses:` of the org's reusable check workflow, the one
// `strspc ci init` writes. GitHub matches owner and repository names in any case.
var strspcCheckUses = regexp.MustCompile(`^\s*(?i:steerspec/\.github)/\.github/workflows/strspc-check\.ya?ml@\S+\s*$`)

// workflowRunsCheck reports whether a workflow file has a step running strspc
// check, or a job calling the reusable workflow. Only what GitHub Actions
// executes is read: jobs.<id>.uses and jobs.<id>.steps[*].run. Comments, names
// and action inputs under with: don't count.
func workflowRunsCheck(data []byte) bool {
	var wf struct {
		Jobs map[string]struct {
			Uses  string `yaml:"uses"`
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return false
	}
	for _, job := range wf.Jobs {
		if strspcCheckUses.MatchString(job.Uses) {
			return true
		}
		for _, step := range job.Steps {
			if strspcCheckRun.MatchString(step.Run) {
				return true
			}
		}
	}
	return false
}

func doctorCICheck(env *doctorEnv) doctorResult {
	const g = "project"
	var files []string
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		m, _ := filepath.Glob(filepath.Join(env.cwd, ".github", "workflows", pattern))
		files = append(files, m...)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err == nil && workflowRunsCheck(data) {
			return doctorOutcome(g, "ci", "", doctorPass, filepath.ToSlash(filepath.Join(".github", "workflows", filepath.Base(f))), "")
		}
	}
	return doctorOutcome(g, "ci", "", doctorInfo, "no workflow runs strspc check (strspc ci init writes one)", "")
}
