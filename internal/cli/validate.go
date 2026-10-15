package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"helmchen/internal/errs"
	"helmchen/internal/helm"

	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate CHART_DIR",
	Short: "Validate chart structure, lint it, and render every template",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

var vStrict bool

func init() {
	validateCmd.Flags().BoolVar(&vStrict, "strict", false, "Fail if Helm is unavailable")
}

func runValidate(cmd *cobra.Command, args []string) error {
	chartDir := args[0]
	abs, err := filepath.Abs(chartDir)
	if err != nil {
		return err
	}
	issues, warnings := helm.InspectStructure(abs)
	if len(warnings) > 0 {
		for _, w := range warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", w)
		}
	}
	if len(issues) == 0 {
		if helm.IsAvailable() {
			hv, err := helm.NewValidator("", 120)
			if err != nil {
				issues = append(issues, err.Error())
			} else {
				if err := hv.Lint(abs, nil); err != nil {
					issues = append(issues, err.Error())
				} else {
					if _, err := hv.Render(abs, nil, "helmi-validation"); err != nil {
						issues = append(issues, err.Error())
					} else {
						// check values-*.yaml
						matches, _ := filepath.Glob(filepath.Join(abs, "values-*.yaml"))
						for _, m := range matches {
							if err := hv.Lint(abs, []string{m}); err != nil {
								issues = append(issues, err.Error())
								break
							}
							if _, err := hv.Render(abs, []string{m}, "helmi-validation"); err != nil {
								issues = append(issues, err.Error())
								break
							}
						}
					}
				}
			}
		} else if vStrict {
			issues = append(issues, "Helm is unavailable, so strict validation cannot run")
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: Helm is unavailable; lint and render checks were skipped\n")
		}
	}
	if len(issues) > 0 {
		return errs.Validationf(strings.Join(issues, "; "))
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Chart validation passed")
	return nil
}
