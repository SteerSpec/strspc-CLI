package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// newRealmValidator creates a configured RealmLinter. Package-level so tests can override it.
var newRealmValidator = func(strict bool) *realmlint.RealmLinter {
	fetcher := schema.New()
	ruleLinter := rulelint.New(
		rulelint.WithSchemaFetcher(fetcher),
	)
	return realmlint.New(
		realmlint.WithSchemaFetcher(fetcher),
		realmlint.WithRuleLinter(ruleLinter),
		realmlint.WithStrict(strict),
	)
}

func newRealmValidateCmd() *cobra.Command {
	var (
		jsonOutput bool
		strict     bool
	)

	cmd := &cobra.Command{
		Use:           "validate [path]",
		Short:         "Validate a Realm directory",
		Long:          "Validate a SteerSpec Realm directory structure, realm.json, schemas, and entity files.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "./rules"
			if len(args) > 0 {
				dir = args[0]
			}

			linter := newRealmValidator(strict)
			res := linter.Lint(dir)

			w := cmd.OutOrStdout()

			if jsonOutput {
				if err := writeJSON(w, res); err != nil {
					return err
				}
				if !res.OK() {
					errs := res.Errors()
					return fmt.Errorf("realm validate found %d error(s)", len(errs))
				}
				return nil
			}

			outputText(w, res)

			if !res.OK() {
				errs := res.Errors()
				return fmt.Errorf("realm validate found %d error(s)", len(errs))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output diagnostics as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as errors")

	return cmd
}
