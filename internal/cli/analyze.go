package cli

import (
	"helmchen/internal/migrator"

	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze KUSTOMIZE_DIR",
	Short: "Render and analyze KUSTOMIZE_DIR without generating a chart",
	Args:  cobra.ExactArgs(1),
	RunE:  runAnalyze,
}

var (
	aOutputFormat string
	aOutputFile   string
	aTimeoutSec   int
)

func init() {
	analyzeCmd.Flags().StringVarP(&aOutputFormat, "output-format", "f", "text", "text|json|yaml")
	analyzeCmd.Flags().StringVarP(&aOutputFile, "output-file", "o", "", "output file")
	analyzeCmd.Flags().IntVar(&aTimeoutSec, "timeout", 120, "timeout in seconds")
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	kDir := args[0]
	m, err := migrator.New(kDir, ".", "", true, false, false, aTimeoutSec, nil)
	if err != nil {
		return err
	}
	report, err := m.AnalyzeOnly()
	if err != nil {
		return err
	}
	if aOutputFile != "" {
		return saveReport(report, aOutputFile, aOutputFormat)
	}
	displayAnalysisReport(report, aOutputFormat)
	return nil
}
