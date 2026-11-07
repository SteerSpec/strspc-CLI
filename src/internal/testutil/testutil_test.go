package testutil

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func TestExecuteCommandCapturesOutputAndError(t *testing.T) {
	root := &cobra.Command{
		Use:           "x",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("out:", args[0])
			return errors.New("failed")
		},
	}
	out, err := ExecuteCommand(root, "a")
	if err == nil || err.Error() != "failed" {
		t.Errorf("err = %v, want failed", err)
	}
	AssertContains(t, out, "out: a")
	AssertNotContains(t, out, "out: b")
}

// The failing branches of the assertions run on a stand-in T, so this test
// itself doesn't fail.
func TestAssertionsReportMismatches(t *testing.T) {
	var missing testing.T
	AssertContains(&missing, "abc", "z")
	if !missing.Failed() {
		t.Error("AssertContains must fail when the text is missing")
	}
	var present testing.T
	AssertNotContains(&present, "abc", "b")
	if !present.Failed() {
		t.Error("AssertNotContains must fail when the text is present")
	}
}
