package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
)

// How a source stands, in the state field of statusSource.
const (
	sourceLocal       = "local"       // a directory in the project
	sourceMissing     = "missing"     // a local directory that doesn't exist
	sourcePinned      = "pinned"      // an immutable ref, cached
	sourceFresh       = "fresh"       // latest, cached, resolved within the TTL
	sourceStale       = "stale"       // latest, cached, resolved at least a TTL ago
	sourceNotSynced   = "not_synced"  // remote, not in the cache
	sourceInvalid     = "invalid"     // a github:// source that doesn't parse
	sourceUnsupported = "unsupported" // a scheme strspc can't fetch
)

// statusReport is the JSON form of `strspc status`.
type statusReport struct {
	Version     string         `json:"version"`
	Initialized bool           `json:"initialized"`
	Config      *statusConfig  `json:"config,omitempty"`
	Sources     []statusSource `json:"sources,omitempty"`
	Rules       ruleCounts     `json:"rules"`
	Realm       *statusRealm   `json:"realm,omitempty"`
}

type statusConfig struct {
	Path       string   `json:"path"`
	Evaluator  string   `json:"evaluator,omitempty"`
	Model      string   `json:"model,omitempty"`
	FailOn     []string `json:"fail_on,omitempty"`
	EvaluateOn []string `json:"evaluate_on,omitempty"`
	CacheTTL   string   `json:"cache_ttl"`
}

type statusSource struct {
	Source     string     `json:"source"`
	Scope      string     `json:"scope"`
	State      string     `json:"state"`
	Ref        string     `json:"ref,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	Error      string     `json:"error,omitempty"`
	Rules      ruleCounts `json:"rules"`
}

// ruleCounts counts rules by lifecycle state. Unreadable counts the files that
// couldn't be parsed, which hold rules nobody can count.
type ruleCounts struct {
	Total       int `json:"total"`
	Draft       int `json:"draft"`
	Published   int `json:"published"`
	Implemented int `json:"implemented"`
	Abandoned   int `json:"abandoned"`
	Retired     int `json:"retired"`
	Terminated  int `json:"terminated"`
	Unknown     int `json:"unknown,omitempty"`
	Unreadable  int `json:"unreadable_files,omitempty"`
}

func (c *ruleCounts) add(other ruleCounts) {
	c.Total += other.Total
	c.Draft += other.Draft
	c.Published += other.Published
	c.Implemented += other.Implemented
	c.Abandoned += other.Abandoned
	c.Retired += other.Retired
	c.Terminated += other.Terminated
	c.Unknown += other.Unknown
	c.Unreadable += other.Unreadable
}

type statusRealm struct {
	ID        string   `json:"id"`
	Version   string   `json:"version"`
	Entities  []string `json:"entities"`
	SubRealms []string `json:"sub_realms"`
}

func newStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status [path]",
		Short: "Show the project's rule sources and how they stand",
		Long: "Show the configuration, rule sources, rule counts by state and realm of a project. Defaults to\n" +
			"the current directory.\n\n" +
			"status only reads: it makes no network call and fills no cache, so a source that was never\n" +
			"synced shows as such. Run strspc sync to fetch it. A pinned source can't go stale; only @latest\n" +
			"has a TTL.\n\n" +
			"Exit codes: 0 when the report is printed, also for a project with no configuration; 2 when\n" +
			"the configuration can't be parsed.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd := "."
			if len(args) > 0 {
				cwd = args[0]
			}
			report, err := collectStatus(cwd)
			if err != nil {
				return withExitCode(exitConfig, err)
			}
			w := cmd.OutOrStdout()
			if jsonOutput {
				return writeStatusJSON(w, report)
			}
			writeStatus(w, report, time.Now())
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// collectStatus reads the project at cwd. It makes no network call and writes
// nothing.
func collectStatus(cwd string) (*statusReport, error) {
	report := &statusReport{Version: versionInfo.Version}
	configPath := filepath.Join(cwd, ".strspc", "config.yaml")
	if _, err := os.Stat(configPath); errors.Is(err, fs.ErrNotExist) {
		return report, nil
	}
	cfg, err := loadStrspcConfig(cwd)
	if err != nil {
		return nil, err
	}
	var ttl time.Duration
	if cfg.Cache.TTL != "" {
		if ttl, err = time.ParseDuration(cfg.Cache.TTL); err != nil {
			return nil, fmt.Errorf("parsing cache.ttl %q: %w", cfg.Cache.TTL, err)
		}
	}

	report.Initialized = true
	report.Config = &statusConfig{
		Path:       filepath.ToSlash(filepath.Join(".strspc", "config.yaml")),
		Evaluator:  strings.TrimSpace(cfg.Evaluator.Provider),
		Model:      cfg.Evaluator.Model,
		FailOn:     normalizeStateCodes(cfg.FailOn),
		EvaluateOn: normalizeStateCodes(cfg.EvaluateOn),
		CacheTTL:   effectiveTTL(ttl).String(),
	}

	fetcher := &remote.Fetcher{CacheDir: filepath.Join(cwd, ".strspc", "cache"), TTL: ttl}
	for _, r := range cfg.Rules {
		s, dir := sourceStatus(cwd, fetcher, r.Source)
		s.Scope = r.Scope
		if dir != "" {
			s.Rules = countRules(dir)
			report.Rules.add(s.Rules)
			if report.Realm == nil && s.State == sourceLocal {
				report.Realm = realmStatus(dir)
			}
		}
		report.Sources = append(report.Sources, s)
	}
	return report, nil
}

// effectiveTTL is the TTL a fetcher applies: an unset one selects the default.
func effectiveTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return remote.DefaultTTL
	}
	return ttl
}

// sourceStatus reports how source stands, and the directory holding its rules
// if it has one.
func sourceStatus(cwd string, fetcher *remote.Fetcher, source string) (s statusSource, dir string) {
	s = statusSource{Source: source}
	switch {
	case strings.HasPrefix(source, remote.Scheme):
		return remoteSourceStatus(source, fetcher)
	case strings.Contains(source, "://"):
		s.State = sourceUnsupported
		return s, ""
	}
	dir = source
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		s.State = sourceMissing
		return s, ""
	}
	s.State = sourceLocal
	return s, dir
}

func remoteSourceStatus(source string, fetcher *remote.Fetcher) (s statusSource, dir string) {
	s = statusSource{Source: source}
	ref, err := remote.ParseGitHub(source)
	if err != nil {
		s.State, s.Error = sourceInvalid, err.Error()
		return s, ""
	}
	entry, err := fetcher.Cached(ref)
	if err != nil {
		s.State, s.Error = sourceInvalid, err.Error()
		return s, ""
	}
	s.Ref = entry.Ref
	if !entry.ResolvedAt.IsZero() {
		s.ResolvedAt = &entry.ResolvedAt
	}
	switch {
	case entry.Dir == "":
		s.State = sourceNotSynced
	case ref.IsImmutable():
		s.State = sourcePinned
	case entry.Stale:
		s.State = sourceStale
	default:
		s.State = sourceFresh
	}
	return s, entry.Dir
}

// countRules counts the rules of the entity files under dir, sub-entities
// included. A file that can't be read or parsed is counted as unreadable.
func countRules(dir string) ruleCounts {
	var counts ruleCounts
	err := entity.WalkEntityFiles(dir, func(_ string, _ []byte, ef *entity.File, parseErr error) error {
		switch {
		case parseErr != nil:
			counts.Unreadable++
		case ef != nil:
			countEntityRules(ef, &counts)
		}
		return nil
	})
	if err != nil {
		counts.Unreadable++
	}
	return counts
}

func countEntityRules(ef *entity.File, counts *ruleCounts) {
	for i := range ef.Rules {
		counts.Total++
		switch ef.Rules[i].State {
		case entity.StateDraft:
			counts.Draft++
		case entity.StatePublished:
			counts.Published++
		case entity.StateImplemented:
			counts.Implemented++
		case entity.StateAbandoned:
			counts.Abandoned++
		case entity.StateRetired:
			counts.Retired++
		case entity.StateTerminated:
			counts.Terminated++
		default:
			counts.Unknown++
		}
	}
	for i := range ef.SubEntities {
		countEntityRules(&ef.SubEntities[i], counts)
	}
}

// realmStatus describes the realm in dir, or returns nil when it holds none.
func realmStatus(dir string) *statusRealm {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		return nil
	}
	realm := &statusRealm{
		ID:        rf.Realm.ID,
		Version:   rf.Realm.Version,
		Entities:  []string{},
		SubRealms: slices.Clone(rf.SubRealms),
	}
	if realm.SubRealms == nil {
		realm.SubRealms = []string{}
	}
	// A file that can't be parsed is already counted by countRules.
	_ = entity.WalkEntityFiles(dir, func(_ string, _ []byte, ef *entity.File, _ error) error {
		if ef != nil && ef.Entity.ID != "" {
			realm.Entities = append(realm.Entities, ef.Entity.ID)
		}
		return nil
	}, entity.WithExcludeDirs(rf.SubRealms))
	slices.Sort(realm.Entities)
	return realm
}

func writeStatusJSON(w io.Writer, report *statusReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling status: %w", err)
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// writeStatus prints report. Values come from the project's files, so every line
// starts with fixed text and goes through printableDiagnostic.
func writeStatus(w io.Writer, report *statusReport, now time.Time) {
	title := func(format string, args ...any) {
		writeln(w, brandStyle.Render(printableDiagnostic(fmt.Sprintf(format, args...))))
	}
	detail := func(format string, args ...any) {
		writeln(w, descStyle.Render(printableDiagnostic(fmt.Sprintf(format, args...))))
	}

	title("SteerSpec CLI %s", report.Version)
	if !report.Initialized {
		writeln(w)
		detail("Not initialized: no .strspc/config.yaml here. Run strspc init.")
		return
	}

	cfg := report.Config
	writeln(w)
	title("Config: %s", cfg.Path)
	detail("  Evaluator: %s", orNotSet(evaluatorLabel(cfg)))
	detail("  Fail on: %s", orNotSet(strings.Join(cfg.FailOn, ", ")))
	detail("  Cache TTL: %s", cfg.CacheTTL)

	writeln(w)
	title("Sources:")
	if len(report.Sources) == 0 {
		detail("  none")
	}
	for i := range report.Sources {
		detail("  - %s", sourceLine(&report.Sources[i], now))
	}

	r := report.Rules
	writeln(w)
	title("Rules: %d total", r.Total)
	detail("  %d Draft  %d Published  %d Implemented  %d Abandoned  %d Retired  %d Terminated",
		r.Draft, r.Published, r.Implemented, r.Abandoned, r.Retired, r.Terminated)
	if r.Unknown > 0 {
		detail("  %d in an unknown state", r.Unknown)
	}
	if r.Unreadable > 0 {
		detail("  %d unreadable file(s), not counted", r.Unreadable)
	}

	if realm := report.Realm; realm != nil {
		writeln(w)
		title("Realm: %s %s", realm.ID, realm.Version)
		detail("  Entities: %s", orNone(realm.Entities))
		detail("  Sub-realms: %s", orNone(realm.SubRealms))
	}
}

func evaluatorLabel(cfg *statusConfig) string {
	if cfg.Model == "" {
		return cfg.Evaluator
	}
	return cfg.Evaluator + " (" + cfg.Model + ")"
}

func orNotSet(s string) string {
	if s == "" {
		return "not set"
	}
	return s
}

func orNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

// sourceLine describes one source.
func sourceLine(s *statusSource, now time.Time) string {
	head := s.Source
	if s.Scope != "" {
		head += " (" + s.Scope + ")"
	}
	rules := fmt.Sprintf(", %d rule(s)", s.Rules.Total)
	switch s.State {
	case sourceLocal:
		return head + ": local" + rules
	case sourcePinned:
		return head + ": pinned, cached" + rules
	case sourceFresh, sourceStale:
		state := fmt.Sprintf(": latest is %s, resolved %s", s.Ref, formatAge(now.Sub(*s.ResolvedAt)))
		if s.State == sourceStale {
			state += " (stale)"
		}
		return head + state + rules
	case sourceNotSynced:
		return head + ": not synced, run strspc sync"
	case sourceMissing:
		return head + ": directory not found"
	case sourceUnsupported:
		return head + ": unsupported scheme"
	default:
		return head + ": invalid: " + s.Error
	}
}

// formatAge renders how long ago something happened, in its largest unit.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
