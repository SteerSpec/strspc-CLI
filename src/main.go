package main

import "github.com/SteerSpec/strspc-CLI/src/cmd"

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
	GitBranch = "unknown"
)

func main() {
	cmd.SetVersionInfo(cmd.VersionInfo{
		Version:   Version,
		BuildTime: BuildTime,
		GitCommit: GitCommit,
		GitBranch: GitBranch,
	})
	cmd.Execute()
}
