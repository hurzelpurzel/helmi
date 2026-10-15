package cli

import (
	"os"

	"github.com/spf13/cobra"

	"helmi/internal/version"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:           "helmi",
	Short:         "Convert rendered Kustomize configurations into verified Helm charts",
	Version:       version.Version,
	SilenceErrors: false,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show debug logging and tracebacks")
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(analyzeCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(initCmd)
}

// Execute runs the CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
