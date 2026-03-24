package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type VersionInfo struct {
	Version   string
	BuildTime string
	GitCommit string
	GitBranch string
}

var versionInfo VersionInfo

func SetVersionInfo(v VersionInfo) {
	versionInfo = v
}

// NewRootCmd creates a fresh root command with all subcommands registered.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "strspc",
		Short: "SteerSpec CLI",
		Long:  "SteerSpec CLI — manage steering specifications from the command line.",
	}
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newRenderCmd())
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newRealmCmd())
	cmd.AddCommand(newRuleCmd())
	cmd.AddCommand(newLintCmd())
	cmd.AddCommand(newDiffCmd())
	cmd.SetHelpFunc(customHelp)
	return cmd
}

func Execute() {
	cobra.CheckErr(NewRootCmd().Execute())
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
}

func writeln(w io.Writer, a ...any) {
	_, _ = fmt.Fprintln(w, a...)
}

func renderFlags(flags *pflag.FlagSet) []string {
	var lines []string
	flags.VisitAll(func(f *pflag.Flag) {
		short := ""
		if f.Shorthand != "" {
			short = "-" + f.Shorthand + ", "
		}
		line := cmdStyle.Render(fmt.Sprintf("  %s--%-12s", short, f.Name)) + descStyle.Render(f.Usage)
		lines = append(lines, line)
	})
	return lines
}
