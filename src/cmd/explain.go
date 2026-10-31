package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/result"
)

// codeEntry is the JSON form of one code in `strspc explain`.
type codeEntry struct {
	explanation
	Module   string `json:"module"`
	Severity string `json:"severity"`
}

func newCodeEntry(info *result.CodeInfo) codeEntry {
	return codeEntry{explanation: newExplanation(info), Module: info.Module, Severity: info.Severity}
}

func newExplainCmd() *cobra.Command {
	var (
		list       bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "explain [code]",
		Short: "Explain a diagnostic code",
		Long: "Show what a diagnostic code means, when it fires and how to fix it.\n\n" +
			"Examples:\n" +
			"  strspc explain RM006\n" +
			"  strspc explain --list\n\n" +
			"To explain findings as they are reported, pass --explain to lint, diff, realm validate or check.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			switch {
			case list && len(args) > 0:
				return errors.New("pass either a code or --list, not both")
			case list:
				return writeCodeList(w, jsonOutput)
			case len(args) == 0:
				return errors.New("pass a diagnostic code, or --list to see all codes")
			}

			code := strings.ToUpper(strings.TrimSpace(args[0]))
			info, ok := lookupDiagnostic(code)
			if !ok {
				return fmt.Errorf("unknown diagnostic code %q: run strspc explain --list to see all codes", args[0])
			}
			if jsonOutput {
				return writeIndentedJSON(w, newCodeEntry(&info))
			}
			writeCode(w, &info)
			return nil
		},
	}

	cmd.Flags().BoolVar(&list, "list", false, "list all diagnostic codes")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

func writeCode(w io.Writer, info *result.CodeInfo) {
	writeln(w, brandStyle.Render(info.Code+": "+info.Title))
	writeln(w, labelStyle.Render("Severity")+valueStyle.Render(info.Severity))
	writeln(w, labelStyle.Render("Module")+valueStyle.Render(info.Module))
	writeln(w)
	writeln(w, plainText(info.Summary))
	writeln(w)
	writeln(w, "Fix: "+plainText(info.Fix))
	writeln(w, "Docs: "+newExplanation(info).Docs)
}

func writeCodeList(w io.Writer, jsonOutput bool) error {
	codes := allDiagnosticCodes()
	if jsonOutput {
		entries := make([]codeEntry, len(codes))
		for i := range codes {
			entries[i] = newCodeEntry(&codes[i])
		}
		return writeIndentedJSON(w, entries)
	}
	for i := range codes {
		writeln(w, fmt.Sprintf("%-7s%s", codes[i].Code, codes[i].Title))
	}
	return nil
}

func writeIndentedJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}
