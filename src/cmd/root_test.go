package cmd

import (
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestHelpContainsAvailableCommands(t *testing.T) {
	output, err := testutil.ExecuteCommand(rootCmd, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Available Commands:")
	testutil.AssertContains(t, output, "version")
}

func TestHelpContainsFlags(t *testing.T) {
	output, err := testutil.ExecuteCommand(rootCmd, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Flags:")
	testutil.AssertContains(t, output, "--help")
}

func TestHelpContainsDescription(t *testing.T) {
	output, err := testutil.ExecuteCommand(rootCmd, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "steering specifications")
}

func TestUnknownCommandReturnsError(t *testing.T) {
	_, err := testutil.ExecuteCommand(rootCmd, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
}

func TestSetVersionInfo(t *testing.T) {
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
