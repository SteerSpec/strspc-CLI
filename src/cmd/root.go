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

func customHelp(cmd *cobra.Command, args []string) {
	fmt.Println(brandStyle.Render("SteerSpec CLI"))
	fmt.Println()
	fmt.Println(descStyle.Render("  Manage steering specifications from the command line."))
	fmt.Println()
	fmt.Println(brandStyle.Render("Available Commands:"))

	for _, c := range cmd.Commands() {
		if c.Hidden {
			continue
		}
		name := cmdStyle.Render(fmt.Sprintf("  %-12s", c.Name()))
		desc := descStyle.Render(c.Short)
		fmt.Println(name + desc)
	}

	fmt.Println()
	fmt.Println(brandStyle.Render("Flags:"))

	var flagLines []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		short := ""
		if f.Shorthand != "" {
			short = "-" + f.Shorthand + ", "
		}
		line := cmdStyle.Render(fmt.Sprintf("  %s--%-12s", short, f.Name)) + descStyle.Render(f.Usage)
		flagLines = append(flagLines, line)
	})

	if len(flagLines) > 0 {
		fmt.Println(strings.Join(flagLines, "\n"))
	}

	fmt.Println()
	fmt.Println(descStyle.Render(fmt.Sprintf("  Use \"%s [command] --help\" for more information about a command.", cmd.Name())))
}
