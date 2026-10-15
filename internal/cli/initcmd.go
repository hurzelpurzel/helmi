package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"helmi/internal/errs"
	"helmi/internal/generator"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize an empty Helm chart scaffold",
	RunE:  runInit,
}

var (
	iChartName string
	iOutputDir string
	iDesc      string
	iVersion   string
	iAppVer    string
)

func init() {
	initCmd.Flags().StringVarP(&iChartName, "chart-name", "n", "", "Chart name")
	initCmd.Flags().StringVarP(&iOutputDir, "output-dir", "o", ".", "Output directory")
	initCmd.Flags().StringVarP(&iDesc, "description", "d", "", "Description")
	initCmd.Flags().StringVar(&iVersion, "version", "0.1.0", "Chart version")
	initCmd.Flags().StringVar(&iAppVer, "app-version", "1.0.0", "App version")
}

func runInit(cmd *cobra.Command, args []string) error {
	cn := iChartName
	if strings.TrimSpace(cn) == "" {
		fmt.Fprint(os.Stdout, "Chart name: ")
		scan := bufio.NewScanner(os.Stdin)
		scan.Scan()
		cn = strings.TrimSpace(scan.Text())
		if err := scan.Err(); err != nil {
			return err
		}
	}
	cn, err := generator.ValidateChartName(cn)
	if err != nil {
		return err
	}
	ver, err := generator.ValidateChartVersion(iVersion)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(filepath.Clean(iOutputDir))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	chartPath := filepath.Join(out, cn)
	if fi, err := os.Stat(chartPath); err == nil && fi.IsDir() {
		return errs.Configurationf("Directory already exists: %s", chartPath)
	}
	stagingRoot, err := os.MkdirTemp(out, ".helmi-init-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stagingRoot)
	staged := filepath.Join(stagingRoot, cn)
	templates := filepath.Join(staged, "templates")
	if err := os.MkdirAll(templates, 0755); err != nil {
		return err
	}
	desc := iDesc
	if strings.TrimSpace(desc) == "" {
		desc = "A Helm chart for " + cn
	}
	cm := map[string]any{"apiVersion": "v2", "name": cn, "description": desc, "type": "application", "version": ver, "appVersion": iAppVer}
	cy, _ := yaml.Marshal(cm)
	if err := os.WriteFile(filepath.Join(staged, "Chart.yaml"), cy, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staged, "values.yaml"), []byte("{}\n"), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staged, ".helmignore"), []byte(".git/\n.DS_Store\n*.tmp\n"), 0644); err != nil {
		return err
	}
	if err := os.Rename(staged, chartPath); err != nil {
		return err
	}
	fmt.Println(chartPath)
	return nil
}
