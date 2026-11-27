package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type VersionInfo struct {
	Version   string
	BuildTime string
	GitCommit string
	GitBranch string
}

// Licence and project pointers shown by --version, `version` and the root
// help. Every URL printed here must resolve (rule CFG-002).
const (
	licenseName = "Apache 2.0"
	licenseLine = "Apache License 2.0 · Copyright 2026 Consulare and contributors"
	websiteURL  = "https://steerspec.dev"
)

// helpStep is one command of a path the root help shows, with what it does.
type helpStep struct{ command, purpose string }

// gettingStarted is the short path the root help shows to someone new to strspc.
var gettingStarted = []helpStep{
	{"strspc init", "Set up SteerSpec in this project"},
	{"strspc doctor", "Check your installation and setup"},
	{"strspc check --static-only", "Run structural checks (no API key needed)"},
	{"strspc status", "See your rule sources and how they stand"},
	{"strspc skill --install", "Teach your AI agent the strspc workflow"},
}

// authoringPath is the root help's shortest way to see a rule enforced
// without an API key: author a rule, publish it, commit, and strspc diff
// refuses the next edit to its text.
var authoringPath = []helpStep{
	{"strspc realm init --id <realm-id> --title <title>", "Create a realm in ./rules"},
	{"strspc realm add <EUID> --title <title>", "Add an entity to it"},
	{"strspc rule add <EUID> --body <text> --added-by github:<login>", "Add a Draft rule"},
	{"strspc rule promote <EUID>-001", "Publish the rule, then commit"},
	{"strspc diff rules/ --base HEAD", "See an edit to the Published rule refused"},
}

var versionInfo VersionInfo

func SetVersionInfo(v VersionInfo) {
	versionInfo = v
}

// NewRootCmd creates a fresh root command with all subcommands registered.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "strspc",
		SilenceErrors: true, // Execute prints the error, sanitized
		Short:         "SteerSpec CLI",
		Long:          "SteerSpec CLI — manage steering specifications from the command line.",
		// Enables the --version flag; `strspc version` prints the full details.
		Version: versionInfo.Version,
	}
	// The first line stays `strspc <version>` so scripts that parse it keep working.
	cmd.SetVersionTemplate("strspc {{.Version}}\n" + licenseLine + "\n" + websiteURL + "\n")
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newRenderCmd())
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newCICmd())
	cmd.AddCommand(newRealmCmd())
	cmd.AddCommand(newRuleCmd())
	cmd.AddCommand(newNoteCmd())
	cmd.AddCommand(newEntityCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newSchemaCmd())
	cmd.AddCommand(newCheckCmd())
	cmd.AddCommand(newLintCmd())
	cmd.AddCommand(newRefsCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newStandardsCmd())
	cmd.AddCommand(newDiffCmd())
	cmd.AddCommand(newHistoryCmd())
	cmd.AddCommand(newExplainCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newSkillCmd())
	cmd.SetHelpFunc(customHelp)
	return cmd
}

// Execute runs the root command. Errors are printed once to stderr; the
// process exits with the code carried by an exitError, or 1 otherwise.
// The command context is cancelled on SIGINT or SIGTERM so that network
// work (schema fetches, remote sources, evaluation) stops promptly.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	root := NewRootCmd()
	err := root.ExecuteContext(ctx)
	stop()
	if err != nil {
		_, _ = fmt.Fprintln(root.ErrOrStderr(), printableDiagnostic("Error: "+err.Error()))
		os.Exit(exitCodeOf(err))
	}
}

func customHelp(cmd *cobra.Command, _ []string) {
	w := cmd.OutOrStdout()

	writeln(w, brandStyle.Render(cmd.UseLine()))
	writeln(w)

	desc := cmd.Long
	if strings.TrimSpace(desc) == "" {
		desc = cmd.Short
	}
	if strings.TrimSpace(desc) != "" {
		writeln(w, descStyle.Render("  "+desc))
		writeln(w)
	}

	var visibleCmds []*cobra.Command
	for _, c := range cmd.Commands() {
		if !c.Hidden {
			visibleCmds = append(visibleCmds, c)
		}
	}

	if len(visibleCmds) > 0 {
		writeln(w, brandStyle.Render("Available Commands:"))
		width := minCommandColumn
		for _, c := range visibleCmds {
			width = max(width, len(c.Name())+helpGutter)
		}
		for _, c := range visibleCmds {
			writeln(w, cmdStyle.Render("  "+padRight(c.Name(), width))+descStyle.Render(c.Short))
		}
		writeln(w)
	}

	// Only the root help guides newcomers; subcommand help stays focused.
	if !cmd.HasParent() {
		writeHelpSteps(w, "Getting started:", gettingStarted)
		writeHelpSteps(w, "Author a rule and see it enforced (no API key needed):", authoringPath)
	}

	// Flags and Global Flags share one column so their descriptions line up.
	width := max(flagColumn(cmd.LocalFlags()), flagColumn(cmd.InheritedFlags()))
	localFlags := renderFlags(cmd.LocalFlags(), width)
	inheritedFlags := renderFlags(cmd.InheritedFlags(), width)

	if len(localFlags) > 0 {
		writeln(w, brandStyle.Render("Flags:"))
		writeln(w, strings.Join(localFlags, "\n"))
		writeln(w)
	}

	if len(inheritedFlags) > 0 {
		writeln(w, brandStyle.Render("Global Flags:"))
		writeln(w, strings.Join(inheritedFlags, "\n"))
		writeln(w)
	}

	// The hint names a [command], so only a command that has some prints it.
	if len(visibleCmds) > 0 {
		writeln(w, descStyle.Render(fmt.Sprintf("  Use \"%s [command] --help\" for more information about a command.", cmd.CommandPath())))
	}

	// Only the root help carries the licence pointer; subcommand help stays focused.
	if !cmd.HasParent() {
		writeln(w)
		writeln(w, descStyle.Render(fmt.Sprintf("  Licensed under %s · %s", licenseName, websiteURL)))
	}
}

func writeln(w io.Writer, a ...any) {
	_, _ = fmt.Fprintln(w, a...)
}

// Help layout. Widths are counted on the plain text, before styling adds
// escape codes.
const (
	// helpGutter is the least space between a name and its description.
	helpGutter = 2
	// minCommandColumn keeps short command lists in the column they always had.
	minCommandColumn = 12
	// shortSlot is the width of a shorthand prefix such as "-h, ". Every flag
	// gets it, blank when it has no shorthand, so the long names line up.
	shortSlot = len("-x, ")
)

// writeHelpSteps prints a titled list of steps, the purposes in one column.
func writeHelpSteps(w io.Writer, title string, steps []helpStep) {
	width := 0
	for _, s := range steps {
		width = max(width, len(s.command)+helpGutter)
	}
	writeln(w, brandStyle.Render(title))
	for _, s := range steps {
		writeln(w, cmdStyle.Render("  "+padRight(s.command, width))+descStyle.Render(s.purpose))
	}
	writeln(w)
}

// padRight pads s with spaces to width; s is returned as is when it is wider.
func padRight(s string, width int) string {
	if n := width - len(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// flagColumn is the width of the name column for the visible flags of flags:
// the shorthand slot, the longest "--name" and the gutter. It is 0 when no
// flag is visible.
func flagColumn(flags *pflag.FlagSet) int {
	width := 0
	flags.VisitAll(func(f *pflag.Flag) {
		if !f.Hidden {
			width = max(width, shortSlot+len("--"+f.Name)+helpGutter)
		}
	})
	return width
}

// renderFlags renders the visible flags of flags, one per line, with the
// names padded to width.
func renderFlags(flags *pflag.FlagSet, width int) []string {
	var lines []string
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		short := strings.Repeat(" ", shortSlot)
		if f.Shorthand != "" {
			short = "-" + f.Shorthand + ", "
		}
		name := padRight(short+"--"+f.Name, width)
		lines = append(lines, cmdStyle.Render("  "+name)+descStyle.Render(f.Usage))
	})
	return lines
}
