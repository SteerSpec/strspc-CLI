package cmd

import (
	"fmt"
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

var rootCmd = &cobra.Command{
	Use:   "strspc",
	Short: "SteerSpec CLI",
	Long:  "SteerSpec CLI — manage steering specifications from the command line.",
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.SetHelpFunc(customHelp)
}

func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func customHelp(cmd *cobra.Command, _ []string) {
	fmt.Println(brandStyle.Render(cmd.UseLine()))
	fmt.Println()

	desc := cmd.Long
	if strings.TrimSpace(desc) == "" {
		desc = cmd.Short
	}
	if strings.TrimSpace(desc) != "" {
		fmt.Println(descStyle.Render("  " + desc))
		fmt.Println()
	}

	var visibleCmds []*cobra.Command
	for _, c := range cmd.Commands() {
		if !c.Hidden {
			visibleCmds = append(visibleCmds, c)
		}
	}

	if len(visibleCmds) > 0 {
		fmt.Println(brandStyle.Render("Available Commands:"))
		for _, c := range visibleCmds {
			name := cmdStyle.Render(fmt.Sprintf("  %-12s", c.Name()))
			d := descStyle.Render(c.Short)
			fmt.Println(name + d)
		}
		fmt.Println()
	}

	localFlags := renderFlags(cmd.LocalFlags())
	inheritedFlags := renderFlags(cmd.InheritedFlags())

	if len(localFlags) > 0 {
		fmt.Println(brandStyle.Render("Flags:"))
		fmt.Println(strings.Join(localFlags, "\n"))
		fmt.Println()
	}

	if len(inheritedFlags) > 0 {
		fmt.Println(brandStyle.Render("Global Flags:"))
		fmt.Println(strings.Join(inheritedFlags, "\n"))
		fmt.Println()
	}

	fmt.Println(descStyle.Render(fmt.Sprintf("  Use \"%s [command] --help\" for more information about a command.", cmd.CommandPath())))
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
