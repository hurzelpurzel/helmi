package migrator

import (
	"path/filepath"
	"sort"
	"strings"

	"helmi/internal/errs"
	"helmi/internal/generator"
	"helmi/internal/helm"
	"helmi/internal/kustomize"
	"helmi/internal/resources"
	"helmi/internal/yamlutil"
)

const defaultTimeout = 120

// KustomizeToHelmMigrator migrates a single Kustomization.
type KustomizeToHelmMigrator struct {
	KustomizeDir string
	OutputDir    string
	ChartName    string
	DryRun       bool
	Overwrite    bool
	Verify       bool
	TimeoutSec   int
	parser       *kustomize.Parser
	generator    *generator.Generator
	data         *kustomize.ParsedData
	report       map[string]any
}

// New creates a new migrator.
func New(kustomizeDir, outputDir string, chartName string, dryRun, overwrite, verify bool, timeoutSec int, buildCmd []string) (*KustomizeToHelmMigrator, error) {
	ks, err := filepath.Abs(filepath.Clean(kustomizeDir))
	if err != nil {
		return nil, errs.Configurationf("%v", err)
	}
	out, err := filepath.Abs(filepath.Clean(outputDir))
	if err != nil {
		return nil, errs.Configurationf("%v", err)
	}
	cn := chartName
	if strings.TrimSpace(cn) == "" {
		base := filepath.Base(ks)
		cn, err = generator.NormalizeChartName(base)
		if err != nil {
			return nil, err
		}
	} else {
		cn, err = generator.ValidateChartName(cn)
		if err != nil {
			return nil, err
		}
	}
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeout
	}
	p, err := kustomize.NewParser(ks, buildCmd, timeoutSec)
	if err != nil {
		return nil, err
	}
	g, err := generator.NewGenerator(cn, out, overwrite)
	if err != nil {
		return nil, err
	}
	return &KustomizeToHelmMigrator{KustomizeDir: ks, OutputDir: out, ChartName: cn, DryRun: dryRun, Overwrite: overwrite, Verify: verify, TimeoutSec: timeoutSec, parser: p, generator: g}, nil
}

func (m *KustomizeToHelmMigrator) newReport() map[string]any {
	return map[string]any{
		"source_directory":      m.KustomizeDir,
		"target_directory":      filepath.Join(m.OutputDir, m.ChartName),
		"chart_name":            m.ChartName,
		"resources_migrated":    0,
		"patches_converted":     0,
		"config_maps_generated": 0,
		"secrets_generated":     0,
		"validation":            "not-run",
		"warnings":              []string{},
		"errors":                []string{},
		"status":                "pending",
	}
}

func (m *KustomizeToHelmMigrator) Migrate() (map[string]any, error) {
	m.report = m.newReport()
	err := func() error {
		if err := m.parseKustomize(); err != nil {
			return err
		}
		if err := m.analyzeConfiguration(true); err != nil {
			return err
		}
		if m.DryRun {
			m.report["validation"] = "dry-run"
			return nil
		}
		return m.generateHelmChart()
	}()
	if err != nil {
		m.report["status"] = "failed"
		msg := err.Error()
		if errs := m.report["errors"].([]string); !containsStr(errs, msg) {
			m.report["errors"] = append(errs, msg)
		}
		return m.report, err
	}
	m.report["status"] = "success"
	return m.report, nil
}

func (m *KustomizeToHelmMigrator) parseKustomize() error {
	data, err := m.parser.Parse()
	if err != nil {
		return err
	}
	m.data = data
	m.report["resources_migrated"] = len(data.Resources)
	m.report["patches_converted"] = len(data.Patches)
	cm := 0
	sec := 0
	for _, r := range data.Resources {
		if r == nil {
			continue
		}
		kind := yamlutil.NodeString(yamlutil.MapGet(r, "kind"))
		if kind == "ConfigMap" {
			cm++
		}
		if kind == "Secret" {
			sec++
		}
	}
	m.report["config_maps_generated"] = cm
	m.report["secrets_generated"] = sec
	w := m.report["warnings"].([]string)
	w = append(w, data.Warnings...)
	m.report["warnings"] = w
	return nil
}

func (m *KustomizeToHelmMigrator) analyzeConfiguration(failOnHelm bool) error {
	res := m.data.Resources
	if len(res) == 0 {
		w := m.report["warnings"].([]string)
		m.report["warnings"] = append(w, "Kustomize rendered no resources; the generated chart will be empty")
	}
	sec := false
	for _, r := range res {
		if r == nil {
			continue
		}
		if yamlutil.NodeString(yamlutil.MapGet(r, "kind")) == "Secret" {
			sec = true
			break
		}
	}
	if sec {
		w := m.report["warnings"].([]string)
		m.report["warnings"] = append(w, "Rendered Secret data is stored in values.yaml; protect the generated chart as sensitive")
	}
	genKinds := []string{}
	for _, r := range res {
		if r == nil {
			continue
		}
		meta := yamlutil.MapGet(r, "metadata")
		if meta == nil {
			continue
		}
		if yamlutil.MapGet(meta, "name") == nil {
			if g := yamlutil.MapGet(meta, "generateName"); g != nil {
				k := yamlutil.NodeString(yamlutil.MapGet(r, "kind"))
				if k == "" {
					k = "Unknown"
				}
				genKinds = append(genKinds, k)
			}
		}
	}
	if len(genKinds) > 0 {
		sort.Strings(genKinds)
		w := m.report["warnings"].([]string)
		m.report["warnings"] = append(w, "Resources using metadata.generateName are not upgrade-stable in Helm: "+strings.Join(genKinds, ", "))
	}
	anns, err := resources.FindHelmBehaviorAnnotations(res)
	if err != nil {
		return err
	}
	if len(anns) > 0 {
		msg := "Source resources contain Helm behavioral annotations that are inert in Kustomize but active in Helm: " + strings.Join(anns, ", ")
		w := m.report["warnings"].([]string)
		m.report["warnings"] = append(w, msg)
		if failOnHelm {
			return errs.Configurationf(msg + ". Remove or explicitly redesign them first.")
		}
	}
	crd := false
	for _, r := range res {
		if r == nil {
			continue
		}
		if yamlutil.NodeString(yamlutil.MapGet(r, "kind")) == "CustomResourceDefinition" {
			crd = true
			break
		}
	}
	if crd {
		w := m.report["warnings"].([]string)
		m.report["warnings"] = append(w, "CRDs are preserved in templates so upgrades remain faithful; review your organization's CRD lifecycle policy before installation")
	}
	return nil
}

func (m *KustomizeToHelmMigrator) generateHelmChart() error {
	var preInstall func(string) error
	verified := false
	if m.Verify {
		if !helm.IsAvailable() {
			return errs.Configurationf("Helm was not found; install Helm or pass verify=False/--no-verify explicitly")
		}
		hv, err := helm.NewValidator("", m.TimeoutSec)
		if err != nil {
			return err
		}
		preInstall = func(staged string) error {
			return hv.VerifyEquivalence(staged, m.data.Resources, nil, m.KustomizeDir)
		}
		verified = true
	}
	if err := m.generator.GenerateChart(m.data.Resources, m.data.Kustomization, nil, preInstall); err != nil {
		return err
	}
	if verified {
		m.report["validation"] = "passed"
	} else if !m.Verify {
		m.report["validation"] = "skipped-by-user"
	}
	return nil
}

func (m *KustomizeToHelmMigrator) AnalyzeOnly() (map[string]any, error) {
	m.report = m.newReport()
	if err := m.parseKustomize(); err != nil {
		return nil, err
	}
	if err := m.analyzeConfiguration(false); err != nil {
		return nil, err
	}
	rt := m.getResourceTypes()
	comp := m.assessComplexity()
	return map[string]any{
		"source_directory":       m.KustomizeDir,
		"chart_name":             m.ChartName,
		"renderer":               m.data.Renderer,
		"resources_found":        len(m.data.Resources),
		"patches_found":          len(m.data.Patches),
		"config_maps_found":      m.report["config_maps_generated"],
		"secrets_found":          m.report["secrets_generated"],
		"warnings":               m.report["warnings"],
		"errors":                 m.report["errors"],
		"kustomization_features": m.kustomizationFeatures(),
		"resource_types":         rt,
		"migration_complexity":   comp,
	}, nil
}

func (m *KustomizeToHelmMigrator) kustomizationFeatures() []string {
	km := m.data.Kustomization
	if km == nil {
		return []string{}
	}
	var features []string
	for i := 0; i+1 < len(km.Content); i += 2 {
		if k := km.Content[i].Value; k != "" {
			features = append(features, k)
		}
	}
	sort.Strings(features)
	return features
}

func (m *KustomizeToHelmMigrator) getResourceTypes() map[string]int {
	rt := make(map[string]int)
	for _, r := range m.data.Resources {
		if r == nil {
			continue
		}
		k := yamlutil.NodeString(yamlutil.MapGet(r, "kind"))
		if k == "" {
			k = "Unknown"
		}
		rt[k]++
	}
	return rt
}

func (m *KustomizeToHelmMigrator) assessComplexity() string {
	score := len(m.data.Resources) + len(m.data.Patches)*2 + len(m.data.ConfigMaps) + len(m.data.Secrets)
	km := m.data.Kustomization
	for _, k := range []string{"generators", "transformers", "validators"} {
		if yamlutil.MapGet(km, k) != nil {
			score += 3
		}
	}
	if score < 5 {
		return "Low"
	}
	if score < 15 {
		return "Medium"
	}
	return "High"
}

func containsStr(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
