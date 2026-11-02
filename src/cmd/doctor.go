package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// How a doctor check came out, in the status field of doctorResult.
const (
	doctorPass = "pass"
	doctorWarn = "warn"
	doctorFail = "fail"
	doctorSkip = "skip" // the check couldn't run, or doesn't apply
	doctorInfo = "info" // a fact, never a problem
)

// doctorReport is the JSON form of `strspc doctor`.
type doctorReport struct {
	Version string         `json:"version"`
	Checks  []doctorResult `json:"checks"`
	Summary doctorSummary  `json:"summary"`
}

// doctorResult is the outcome of one check. ID is stable (group.name); a check
// run once per source or realm has one result each, told apart by Subject.
type doctorResult struct {
	ID      string `json:"id"`
	Group   string `json:"group"`
	Subject string `json:"subject,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Remedy  string `json:"remedy,omitempty"`
}

type doctorSummary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
	Info int `json:"info"`
}

// doctorEnv holds what the checks read from the host, so that tests don't
// depend on the real PATH, binary, environment or clock.
type doctorEnv struct {
	ctx        context.Context
	cwd        string
	version    string
	goos       string
	goarch     string
	goVersion  string
	getenv     func(string) string
	lookPath   func(string) (string, error)
	executable func() (string, error)
	now        func() time.Time
	// userCacheDir locates the user cache holding the realm dependencies that
	// `realm validate` fetched.
	userCacheDir func() (string, error)
	// httpClient is given to every schema fetcher. The default one refuses
	// every request, so doctor reads only what is cached.
	httpClient *http.Client
}

// newDoctorEnv returns the environment of a doctor run. Package-level so tests
// can override it.
var newDoctorEnv = func(ctx context.Context, cwd string) *doctorEnv {
	return &doctorEnv{
		ctx:          ctx,
		cwd:          cwd,
		version:      versionInfo.Version,
		goos:         runtime.GOOS,
		goarch:       runtime.GOARCH,
		goVersion:    runtime.Version(),
		getenv:       os.Getenv,
		lookPath:     exec.LookPath,
		executable:   os.Executable,
		now:          time.Now,
		userCacheDir: os.UserCacheDir,
		httpClient:   offlineHTTPClient(),
	}
}

// errDoctorOffline is what the offline HTTP client returns: doctor makes no
// network call, so anything not cached is reported as such.
var errDoctorOffline = errors.New("not cached, and doctor makes no network call")

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errDoctorOffline
}

func offlineHTTPClient() *http.Client {
	return &http.Client{Transport: offlineTransport{}}
}

func newDoctorCmd() *cobra.Command {
	var (
		jsonOutput bool
		strict     bool
	)

	cmd := &cobra.Command{
		Use:   "doctor [path]",
		Short: "Diagnose the installation and the project, and suggest fixes",
		Long: "Check the strspc installation and the project at path (the current directory by default):\n" +
			"the binary, git, the configuration, credentials, rule sources, realms and project files. Each\n" +
			"check passes, warns or fails, and a warning or failure comes with a command that fixes it.\n\n" +
			"doctor only reads: it makes no network call and fills no cache, so what was never fetched\n" +
			"is reported as not cached. The API key is checked for presence only; it is never printed.\n\n" +
			"Exit codes: 0 when no check failed; 1 when one did, or with --strict when one warned;\n" +
			"2 when the flags are invalid or path can't be read.\n\n" +
			"Examples:\n" +
			"  strspc doctor\n" +
			"  strspc doctor ./my-project --strict --json",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd := "."
			if len(args) > 0 {
				cwd = args[0]
			}
			info, err := os.Stat(cwd)
			if err != nil {
				return withExitCode(exitConfig, fmt.Errorf("reading %s: %w", cwd, err))
			}
			if !info.IsDir() {
				return withExitCode(exitConfig, fmt.Errorf("%s is not a directory", cwd))
			}

			report := runDoctor(newDoctorEnv(cmd.Context(), cwd))
			w := cmd.OutOrStdout()
			if jsonOutput {
				if err := writeDoctorJSON(w, report); err != nil {
					return err
				}
			} else {
				writeDoctor(w, report)
			}
			s := report.Summary
			if failed := s.Fail + strictWarnings(strict, s.Warn); failed > 0 {
				return withExitCode(exitViolations, doctorFailure(s, strict))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as failures")
	// A usage error exits 2, so that 1 only ever means a check failed.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return withExitCode(exitConfig, err) })
	cmd.Args = func(c *cobra.Command, args []string) error {
		return withExitCode(exitConfig, cobra.MaximumNArgs(1)(c, args))
	}
	return cmd
}

func strictWarnings(strict bool, warn int) int {
	if strict {
		return warn
	}
	return 0
}

func doctorFailure(s doctorSummary, strict bool) error {
	if strict && s.Warn > 0 {
		return fmt.Errorf("doctor: %d check(s) failed and %d warned (--strict)", s.Fail, s.Warn)
	}
	return fmt.Errorf("doctor: %d check(s) failed", s.Fail)
}

// runDoctor runs every check, in the order they are printed. The config checks
// leave what they parsed in a doctorProject for the checks that follow.
func runDoctor(env *doctorEnv) *doctorReport {
	report := &doctorReport{Version: env.version, Checks: []doctorResult{}}
	add := func(rs ...doctorResult) { report.Checks = append(report.Checks, rs...) }

	add(doctorInstallChecks(env)...)
	add(doctorToolChecks(env)...)
	configChecks, project := doctorConfigChecks(env)
	add(configChecks...)
	add(doctorCredentialChecks(env, project)...)
	add(doctorSourceChecks(env, project)...)
	add(doctorRealmChecks(env, project)...)
	add(doctorProjectChecks(env)...)

	for i := range report.Checks {
		switch report.Checks[i].Status {
		case doctorPass:
			report.Summary.Pass++
		case doctorWarn:
			report.Summary.Warn++
		case doctorFail:
			report.Summary.Fail++
		case doctorSkip:
			report.Summary.Skip++
		case doctorInfo:
			report.Summary.Info++
		}
	}
	return report
}

func writeDoctorJSON(w io.Writer, report *doctorReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling doctor report: %w", err)
	}
	_, err = w.Write(append(printableJSON(data), '\n'))
	return err
}

// doctorMark is the symbol printed before a result.
var doctorMark = map[string]string{
	doctorPass: "✓",
	doctorWarn: "⚠",
	doctorFail: "✗",
	doctorSkip: "-",
	doctorInfo: "·",
}

// writeDoctor prints report grouped as the checks ran. Messages and remedies
// can quote the project's files, so every line goes through printableDiagnostic.
func writeDoctor(w io.Writer, report *doctorReport) {
	line := func(style func(...string) string, format string, args ...any) {
		writeln(w, style(printableDiagnostic(fmt.Sprintf(format, args...))))
	}
	line(brandStyle.Render, "strspc doctor %s", report.Version)

	group := ""
	for i := range report.Checks {
		c := &report.Checks[i]
		if c.Group != group {
			group = c.Group
			writeln(w)
			line(brandStyle.Render, "%s", group)
		}
		name := strings.TrimPrefix(c.ID, c.Group+".")
		line(descStyle.Render, "  %s %-10s %s", doctorMark[c.Status], name, c.Message)
		if c.Remedy != "" && (c.Status == doctorWarn || c.Status == doctorFail || c.Status == doctorSkip) {
			line(descStyle.Render, "  %-12s → %s", "", c.Remedy)
		}
	}

	writeln(w)
	line(brandStyle.Render, "%s", doctorSummaryLine(report.Summary))
}

// doctorSummaryLine counts the results, leaving out the statuses no result has.
func doctorSummaryLine(s doctorSummary) string {
	parts := []string{fmt.Sprintf("%d failed", s.Fail), fmt.Sprintf("%d warning(s)", s.Warn), fmt.Sprintf("%d passed", s.Pass)}
	if s.Skip > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", s.Skip))
	}
	if s.Info > 0 {
		parts = append(parts, fmt.Sprintf("%d info", s.Info))
	}
	return strings.Join(parts, ", ")
}
