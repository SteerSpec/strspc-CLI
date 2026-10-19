package cmd

import (
	"github.com/spf13/cobra"
)

func newNoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Manage notes attached to rules",
		Long:  "Commands for adding annotations (rationale, examples, references, ...) to rules in SteerSpec entity files.",
	}

	cmd.AddCommand(newNoteAddCmd())

	return cmd
}
