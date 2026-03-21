package cmd

import (
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestVersionCommandOutput(t *testing.T) {
	SetVersionInfo(VersionInfo{
		Version:   "1.0.0",
		BuildTime: "2026-01-01T00:00:00Z",
		GitCommit: "abc1234",
		GitBranch: "main",
	})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, output, "1.0.0")
	testutil.AssertContains(t, output, "2026-01-01T00:00:00Z")
	testutil.AssertContains(t, output, "abc1234")
	testutil.AssertContains(t, output, "main")
}

func TestVersionCommandTruncatesCommit(t *testing.T) {
	SetVersionInfo(VersionInfo{
		Version:   "1.0.0",
		BuildTime: "2026-01-01T00:00:00Z",
		GitCommit: "abc1234567890",
		GitBranch: "main",
	})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, output, "abc1234")
	testutil.AssertNotContains(t, output, "abc1234567890")
}

func TestVersionCommandDefaults(t *testing.T) {
	SetVersionInfo(VersionInfo{
		Version:   "dev",
		BuildTime: "unknown",
		GitCommit: "unknown",
		GitBranch: "unknown",
	})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, output, "dev")
	testutil.AssertContains(t, output, "unknown")
}
