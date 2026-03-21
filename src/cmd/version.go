package cmd

import (
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, _ []string) {
		w := cmd.OutOrStdout()

		writeln(w, brandStyle.Render("SteerSpec CLI"))
		writeln(w)

		commit := versionInfo.GitCommit
		if len(commit) > 7 {
			commit = commit[:7]
		}

		writeln(w, labelStyle.Render("Version")+valueStyle.Render(versionInfo.Version))
		writeln(w, labelStyle.Render("Built")+valueStyle.Render(versionInfo.BuildTime))
		writeln(w, labelStyle.Render("Commit")+valueStyle.Render(commit))
		writeln(w, labelStyle.Render("Branch")+valueStyle.Render(versionInfo.GitBranch))
	},
}
