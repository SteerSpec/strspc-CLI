package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(brandStyle.Render("SteerSpec CLI"))
		fmt.Println()

		commit := versionInfo.GitCommit
		if len(commit) > 7 {
			commit = commit[:7]
		}

		fmt.Println(labelStyle.Render("Version") + valueStyle.Render(versionInfo.Version))
		fmt.Println(labelStyle.Render("Built") + valueStyle.Render(versionInfo.BuildTime))
		fmt.Println(labelStyle.Render("Commit") + valueStyle.Render(commit))
		fmt.Println(labelStyle.Render("Branch") + valueStyle.Render(versionInfo.GitBranch))
	},
}
