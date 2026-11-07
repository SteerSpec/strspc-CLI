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
	cmd.AddCommand(newCheckCmd())
	cmd.AddCommand(newLintCmd())
	cmd.AddCommand(newRefsCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newStandardsCmd())
	cmd.AddCommand(newDiffCmd())
	cmd.AddCommand(newHistoryCmd())
	cmd.AddCommand(newExplainCmd())
	cmd.AddCommand(newDoctorCmd())
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
		for _, c := range visibleCmds {
			name := cmdStyle.Render(fmt.Sprintf("  %-12s", c.Name()))
			d := descStyle.Render(c.Short)
			writeln(w, name+d)
		}
		writeln(w)
	}

	localFlags := renderFlags(cmd.LocalFlags())
	inheritedFlags := renderFlags(cmd.InheritedFlags())

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

	writeln(w, descStyle.Render(fmt.Sprintf("  Use \"%s [command] --help\" for more information about a command.", cmd.CommandPath())))

	// Only the root help carries the licence pointer; subcommand help stays focused.
	if !cmd.HasParent() {
		writeln(w)
		writeln(w, descStyle.Render(fmt.Sprintf("  Licensed under %s · %s", licenseName, websiteURL)))
	}
}

func writeln(w io.Writer, a ...any) {
	_, _ = fmt.Fprintln(w, a...)
}

func renderFlags(flags *pflag.FlagSet) []string {
	var lines []string
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		short := ""
		if f.Shorthand != "" {
			short = "-" + f.Shorthand + ", "
		}
		line := cmdStyle.Render(fmt.Sprintf("  %s--%-12s", short, f.Name)) + descStyle.Render(f.Usage)
		lines = append(lines, line)
	})
	return lines
}
