package cli

import (
	"path/filepath"

	"helmchen/internal/errs"
	"helmchen/internal/migrator"
	"helmchen/internal/multioverlay"

	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate KUSTOMIZE_DIR OUTPUT_DIR",
	Short: "Migrate KUSTOMIZE_DIR into a chart below OUTPUT_DIR",
	Args:  cobra.ExactArgs(2),
	RunE:  runMigrate,
}

var (
	chartName    string
	baseDir      string
	overlaysDir  string
	dryRun       bool
	force        bool
	noVerify     bool
	timeoutSec   int
	outputFormat string
)

func init() {
	migrateCmd.Flags().StringVarP(&chartName, "chart-name", "n", "", "Helm chart name")
	migrateCmd.Flags().StringVarP(&baseDir, "base-dir", "b", "", "Base directory for base-plus-overlays migration")
	migrateCmd.Flags().StringVarP(&overlaysDir, "overlays-dir", "o", "", "Directory whose immediate subdirs are overlays")
	migrateCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Render and analyze without writing")
	migrateCmd.Flags().BoolVar(&force, "force", false, "Transactionally replace existing chart")
	migrateCmd.Flags().BoolVar(&noVerify, "no-verify", false, "Skip Helm lint and equivalence checks")
	migrateCmd.Flags().IntVar(&timeoutSec, "timeout", 120, "Timeout in seconds for each command")
	migrateCmd.Flags().StringVarP(&outputFormat, "output-format", "f", "text", "text|json|yaml")
}

func runMigrate(cmd *cobra.Command, args []string) error {
	kDir := args[0]
	oDir := args[1]
	if baseDir != "" && overlaysDir == "" {
		return errs.Configurationf("--base-dir requires --overlays-dir")
	}
	if baseDir != "" {
		bd, _ := filepath.Abs(baseDir)
		kd, _ := filepath.Abs(kDir)
		if bd != kd {
			return errs.Configurationf("KUSTOMIZE_DIR and --base-dir must refer to the same directory")
		}
	}
	verify := !noVerify
	if overlaysDir != "" {
		mm, err := multioverlay.New(baseDirOr(kDir), overlaysDir, oDir, chartName, dryRun, force, verify, timeoutSec, nil)
		if err != nil {
			return err
		}
		report, err := mm.Migrate()
		if err != nil {
			return err
		}
		displayMigrationReport(report, outputFormat)
		return nil
	}
	m, err := migrator.New(kDir, oDir, chartName, dryRun, force, verify, timeoutSec, nil)
	if err != nil {
		return err
	}
	report, err := m.Migrate()
	if err != nil {
		return err
	}
	displayMigrationReport(report, outputFormat)
	return nil
}

func baseDirOr(k string) string {
	if baseDir != "" {
		return baseDir
	}
	return k
}
