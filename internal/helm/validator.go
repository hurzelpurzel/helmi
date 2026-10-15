package helm

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"helmchen/internal/errs"
	"helmchen/internal/resources"
	"helmchen/internal/yamlutil"

	"gopkg.in/yaml.v3"
)

const defaultTimeout = 120

// Validator validates charts with Helm.
type Validator struct {
	binary  string
	timeout time.Duration
}

// IsAvailable checks if helm is on PATH.
func IsAvailable() bool {
	_, err := exec.LookPath("helm")
	return err == nil
}

// NewValidator creates a new validator.
func NewValidator(binary string, timeoutSec int) (*Validator, error) {
	bin := binary
	if bin == "" {
		path, err := exec.LookPath("helm")
		if err != nil {
			return nil, errs.Configurationf("Helm was not found on PATH. Install Helm to lint and verify generated charts.")
		}
		bin = path
	}
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeout
	}
	return &Validator{binary: bin, timeout: time.Duration(timeoutSec) * time.Second}, nil
}

// Lint lints the chart.
func (v *Validator) Lint(chartDir string, valuesFiles []string) error {
	chartPath, err := v.validateChartPath(chartDir)
	if err != nil {
		return err
	}
	args := []string{"lint", chartPath, "--strict"}
	for _, f := range valuesFiles {
		args = append(args, "--values", f)
	}
	if _, err := v.run(args, "Helm lint"); err != nil {
		return err
	}
	return nil
}

// Render renders the chart.
func (v *Validator) Render(chartDir string, valuesFiles []string, releaseName string) ([]*yaml.Node, error) {
	chartPath, err := v.validateChartPath(chartDir)
	if err != nil {
		return nil, err
	}
	if releaseName == "" {
		releaseName = "helmi-validation"
	}
	args := []string{"template", releaseName, chartPath, "--include-crds"}
	for _, f := range valuesFiles {
		abs := f
		if af, err := filepath.Abs(f); err == nil {
			abs = af
		}
		args = append(args, "--values", abs)
	}
	out, err := v.run(args, "Helm template")
	if err != nil {
		return nil, err
	}
	docs, err := yamlutil.DecodeDocuments([]byte(out))
	if err != nil {
		return nil, errs.Validationf("Helm produced invalid YAML: %v", err)
	}
	for _, d := range docs {
		if err := yamlutil.CheckDuplicateKeys(d); err != nil {
			return nil, errs.Validationf("Helm produced invalid YAML: %v", err)
		}
	}
	res := make([]*yaml.Node, len(docs))
	for i, d := range docs {
		res[i] = yamlutil.Clone(d)
	}
	if _, err := resources.IndexResources(res); err != nil {
		return nil, err
	}
	return res, nil
}

// VerifyEquivalence verifies equivalence.
func (v *Validator) VerifyEquivalence(chartDir string, expected []*yaml.Node, valuesFiles []string, context string) error {
	if err := v.Lint(chartDir, valuesFiles); err != nil {
		return err
	}
	actual, err := v.Render(chartDir, valuesFiles, "helmi-validation")
	if err != nil {
		return err
	}
	return resources.AssertResourceEquivalence(expected, actual, context)
}

// InspectStructure inspects chart structure.
func InspectStructure(chartDir string) (issues, warnings []string) {
	chartPath, err := filepath.Abs(filepath.Clean(chartDir))
	if err != nil {
		return []string{err.Error()}, nil
	}
	fi, err := os.Stat(chartPath)
	if err != nil || !fi.IsDir() {
		return []string{"Chart directory does not exist: " + chartPath}, nil
	}
	for _, fn := range []string{"Chart.yaml", "values.yaml"} {
		if _, err := os.Stat(filepath.Join(chartPath, fn)); err != nil {
			issues = append(issues, "Missing required file: "+fn)
		}
	}
	tmpl := filepath.Join(chartPath, "templates")
	if fi, err := os.Stat(tmpl); err != nil || !fi.IsDir() {
		issues = append(issues, "Missing templates directory")
	} else {
		entries, err := os.ReadDir(tmpl)
		if err == nil && len(entries) == 0 {
			warnings = append(warnings, "Templates directory is empty")
		}
	}
	cy := filepath.Join(chartPath, "Chart.yaml")
	if b, err := os.ReadFile(cy); err == nil {
		root, err := yamlutil.DecodeSingle(b)
		if err != nil {
			issues = append(issues, "Invalid Chart.yaml: "+err.Error())
		} else if !yamlutil.IsMapping(root) {
			issues = append(issues, "Chart.yaml must contain a mapping")
		} else {
			for _, f := range []string{"apiVersion", "name", "version"} {
				if v := yamlutil.MapGet(root, f); v == nil || strings.TrimSpace(yamlutil.NodeString(v)) == "" {
					issues = append(issues, "Chart.yaml missing required field: "+f)
				}
			}
			if v := yamlutil.MapGet(root, "apiVersion"); v != nil {
				s := yamlutil.NodeString(v)
				if s != "v1" && s != "v2" {
					issues = append(issues, "Chart.yaml apiVersion must be v1 or v2")
				}
			}
		}
	}
	vy := filepath.Join(chartPath, "values.yaml")
	if b, err := os.ReadFile(vy); err == nil {
		root, err := yamlutil.DecodeSingle(b)
		if err != nil {
			issues = append(issues, "Invalid values.yaml: "+err.Error())
		} else if root != nil && !yamlutil.IsMapping(root) {
			issues = append(issues, "values.yaml must contain a mapping")
		}
	}
	return
}

func (v *Validator) validateChartPath(chartDir string) (string, error) {
	p, err := filepath.Abs(filepath.Clean(chartDir))
	if err != nil {
		return "", errs.Validationf(err.Error())
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.IsDir() {
		return "", errs.Validationf("Chart directory does not exist: %s", p)
	}
	return p, nil
}

func (v *Validator) run(args []string, operation string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), v.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, v.binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", errs.Validationf("%s timed out after %d seconds", operation, int(v.timeout.Seconds()))
		}
		d := stderr.String()
		if d == "" {
			d = stdout.String()
		}
		if d == "" {
			d = err.Error()
		}
		if len(d) > 4000 {
			d = d[:4000] + "…"
		}
		return "", errs.Validationf("%s failed (exit error): %s", operation, d)
	}
	return stdout.String(), nil
}
