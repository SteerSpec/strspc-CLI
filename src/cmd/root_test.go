package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/spf13/cobra"
)

// TestEveryCommandHasShortAndLong enforces CMD-001 on the whole command tree,
// hidden commands included, so a command added without a Long description is
// caught here rather than by an audit.
func TestEveryCommandHasShortAndLong(t *testing.T) {
	var walked int
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		walked++
		if strings.TrimSpace(c.Short) == "" {
			t.Errorf("%q has no Short description (CMD-001)", c.CommandPath())
		}
		if strings.TrimSpace(c.Long) == "" {
			t.Errorf("%q has no Long description (CMD-001)", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRootCmd())
	if walked < 30 {
		t.Errorf("walked %d commands, want the whole tree", walked)
	}
}

func TestHelpContainsAvailableCommands(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Available Commands:")
	testutil.AssertContains(t, output, "version")
}

func TestHelpContainsFlags(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Flags:")
	testutil.AssertContains(t, output, "--help")
}

func TestHelpContainsDescription(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "steering specifications")
}

func TestUnknownCommandReturnsError(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
}

func TestSetVersionInfo(t *testing.T) {
	prev := versionInfo
	t.Cleanup(func() { versionInfo = prev })

	info := VersionInfo{
		Version:   "1.2.3",
		BuildTime: "2026-01-01",
		GitCommit: "abc1234",
		GitBranch: "main",
	}
	SetVersionInfo(info)

	if versionInfo.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", versionInfo.Version)
	}
	if versionInfo.GitCommit != "abc1234" {
		t.Errorf("expected commit abc1234, got %s", versionInfo.GitCommit)
	}
}

// TestExecuteSucceeds runs the process entry point on a command that
// succeeds. The failure path exits the process, so it isn't run here.
func TestExecuteSucceeds(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	prevArgs, prevStdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = prevArgs, prevStdout })
	os.Args = []string{"strspc", "version"}
	os.Stdout = out

	Execute()

	os.Stdout = prevStdout
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, string(data), "SteerSpec CLI")
}

func TestHelpFallsBackToShortAndListsGlobalFlags(t *testing.T) {
	parent := &cobra.Command{Use: "p"}
	parent.PersistentFlags().Bool("verbose", false, "say more")
	child := &cobra.Command{Use: "c", Short: "the short description", Run: func(*cobra.Command, []string) {}}
	parent.AddCommand(child)
	var buf bytes.Buffer
	child.SetOut(&buf)

	customHelp(child, nil)

	testutil.AssertContains(t, buf.String(), "the short description")
	testutil.AssertContains(t, buf.String(), "Global Flags:")
	testutil.AssertContains(t, buf.String(), "--verbose")
}

func TestRootHelpHasLicenseFooter(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Apache 2.0")
	testutil.AssertContains(t, output, "https://steerspec.dev")
}

func TestBareRootShowsLicenseFooter(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Available Commands:")
	testutil.AssertContains(t, output, "Apache 2.0")
	testutil.AssertContains(t, output, "https://steerspec.dev")
}

func TestSubcommandHelpHasNoLicenseFooter(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "version", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertNotContains(t, output, "Licensed under")
}

func TestRootHelpHasGettingStarted(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Getting started:")
	for _, s := range gettingStarted {
		testutil.AssertContains(t, output, s.command)
	}
}

func TestSubcommandHelpHasNoGettingStarted(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "version", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertNotContains(t, output, "Getting started:")
}
