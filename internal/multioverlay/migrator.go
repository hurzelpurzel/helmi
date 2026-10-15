package multioverlay

import (
	"helmi/internal/errs"
	"helmi/internal/generator"
	"helmi/internal/helm"
	"helmi/internal/kustomize"
	"helmi/internal/overlay"
	"helmi/internal/resources"
	"helmi/internal/yamlutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"sort"
	"strings"
)

const defaultTimeout = 120

// MultiOverlayMigrator migrates base plus overlays.
type MultiOverlayMigrator struct {
	BaseDir        string
	OverlaysDir    string
	OutputDir      string
	ChartName      string
	DryRun         bool
	Overwrite      bool
	Verify         bool
	TimeoutSec     int
	BuildCmd       []string
	baseParser     *kustomize.Parser
	overlayParsers map[string]*kustomize.Parser
	analyzer       *overlay.OverlayAnalyzer
	generator      *generator.Generator
	baseData       *kustomize.ParsedData
	overlayData    map[string]*kustomize.ParsedData
	overlayNames   []string
	report         map[string]any
}

// New creates multi-overlay migrator.
func New(baseDir, overlaysDir, outputDir string, chartName string, dryRun, overwrite, verify bool, timeoutSec int, buildCmd []string) (*MultiOverlayMigrator, error) {
	bd, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return nil, errs.Configurationf("%v", err)
	}
	od, err := filepath.Abs(filepath.Clean(overlaysDir))
	if err != nil {
		return nil, errs.Configurationf("%v", err)
	}
	out, err := filepath.Abs(filepath.Clean(outputDir))
	if err != nil {
		return nil, errs.Configurationf("%v", err)
	}
	cn := chartName
	if strings.TrimSpace(cn) == "" {
		cn, err = generator.NormalizeChartName(filepath.Base(bd))
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
	bp, err := kustomize.NewParser(bd, buildCmd, timeoutSec)
	if err != nil {
		return nil, err
	}
	fi, err := osStat(od)
	if err != nil || !fi.IsDir() {
		return nil, errs.Configurationf("Overlays directory does not exist: %s", od)
	}
	g, err := generator.NewGenerator(cn, out, overwrite)
	if err != nil {
		return nil, err
	}
	return &MultiOverlayMigrator{BaseDir: bd, OverlaysDir: od, OutputDir: out, ChartName: cn, DryRun: dryRun, Overwrite: overwrite, Verify: verify, TimeoutSec: timeoutSec, BuildCmd: buildCmd, baseParser: bp, overlayParsers: make(map[string]*kustomize.Parser), analyzer: &overlay.OverlayAnalyzer{}, generator: g, overlayData: make(map[string]*kustomize.ParsedData)}, nil
}

func osStat(p string) (os.FileInfo, error) {
	return os.Stat(p)
}
func (m *MultiOverlayMigrator) newReport() map[string]any {
	return map[string]any{
		"source_base_directory":     m.BaseDir,
		"source_overlays_directory": m.OverlaysDir,
		"target_directory":          filepath.Join(m.OutputDir, m.ChartName),
		"chart_name":                m.ChartName,
		"overlays_processed":        0,
		"base_resources":            0,
		"resources_migrated":        0,
		"values_files_generated":    0,
		"parameters_extracted":      0,
		"resource_differences":      0,
		"validation":                "not-run",
		"warnings":                  []string{},
		"errors":                    []string{},
		"status":                    "pending",
	}
}

func (m *MultiOverlayMigrator) Migrate() (map[string]any, error) {
	m.report = m.newReport()
	err := func() error {
		if err := m.parseBase(); err != nil {
			return err
		}
		if err := m.discoverAndParseOverlays(); err != nil {
			return err
		}
		if err := m.analyzeOverlayDifferences(); err != nil {
			return err
		}
		if m.DryRun {
			m.report["validation"] = "dry-run"
			return nil
		}
		return m.generateChart()
	}()
	if err != nil {
		m.report["status"] = "failed"
		msg := err.Error()
		if errs := m.report["errors"].([]string); !containsStrMM(errs, msg) {
			m.report["errors"] = append(errs, msg)
		}
		return m.report, err
	}
	m.report["status"] = "success"
	return m.report, nil
}

func (m *MultiOverlayMigrator) parseBase() error {
	data, err := m.baseParser.Parse()
	if err != nil {
		return err
	}
	m.baseData = data
	m.report["base_resources"] = len(data.Resources)
	w := m.report["warnings"].([]string)
	w = append(w, data.Warnings...)
	m.report["warnings"] = w
	return nil
}

func (m *MultiOverlayMigrator) discoverAndParseOverlays() error {
	entries, err := os.ReadDir(m.OverlaysDir)
	if err != nil {
		return errs.Configurationf("%v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	if len(dirs) == 0 {
		return errs.Configurationf("No overlay directories found in %s", m.OverlaysDir)
	}
	failures := []string{}
	for _, d := range dirs {
		odir := filepath.Join(m.OverlaysDir, d)
		op, err := kustomize.NewParser(odir, m.BuildCmd, m.TimeoutSec)
		if err != nil {
			failures = append(failures, d+": "+err.Error())
			continue
		}
		odata, err := op.Parse()
		if err != nil {
			failures = append(failures, d+": "+err.Error())
			continue
		}
		m.overlayParsers[d] = op
		m.overlayData[d] = odata
		w := m.report["warnings"].([]string)
		for _, ww := range odata.Warnings {
			w = append(w, "Overlay "+d+": "+ww)
		}
		m.report["warnings"] = w
	}
	if len(failures) > 0 {
		return errs.Configurationf("Failed to render overlays: " + strings.Join(failures, " | "))
	}
	m.overlayNames = append([]string(nil), dirs...)
	m.report["overlays_processed"] = len(m.overlayNames)
	return nil
}

func (m *MultiOverlayMigrator) analyzeOverlayDifferences() error {
	ovRes := make(map[string][]*yaml.Node)
	ovRes["base"] = m.baseData.Resources
	for _, n := range m.overlayNames {
		ovRes[n] = m.overlayData[n].Resources
	}
	if err := m.analyzer.AnalyzeDifferences(ovRes); err != nil {
		return err
	}
	summ := m.analyzer.GetAnalysisSummary()
	m.report["parameters_extracted"] = summ["parameterizable_paths"]
	if total, ok := summ["total_differences"].(int); ok {
		m.report["resource_differences"] = total
	} else {
		m.report["resource_differences"] = 0
	}
	// identities
	idSet := make(map[resources.Identity]struct{})
	for _, res := range ovRes {
		for _, r := range res {
			if r == nil {
				continue
			}
			id, err := resources.ResourceIdentity(r)
			if err == nil {
				idSet[id] = struct{}{}
			}
		}
	}
	m.report["resources_migrated"] = len(idSet)
	allRes := []*yaml.Node{}
	for _, res := range ovRes {
		allRes = append(allRes, res...)
	}
	if hasSecret(allRes) {
		w := m.report["warnings"].([]string)
		m.report["warnings"] = append(w, "Rendered Secret data is stored in values files; protect the generated chart as sensitive")
	}
	anns, err := resources.FindHelmBehaviorAnnotations(allRes)
	if err != nil {
		return err
	}
	if len(anns) > 0 {
		return errs.Configurationf("Source resources contain Helm behavioral annotations that are inert in Kustomize but active in Helm: " + strings.Join(anns, ", ") + ". Remove or explicitly redesign them first.")
	}
	if hasCRD(allRes) {
		w := m.report["warnings"].([]string)
		m.report["warnings"] = append(w, "CRDs are preserved in templates so upgrades remain faithful; review your organization's CRD lifecycle policy before installation")
	}
	return nil
}

func hasSecret(res []*yaml.Node) bool {
	for _, r := range res {
		if r == nil {
			continue
		}
		if yamlutil.NodeString(yamlutil.MapGet(r, "kind")) == "Secret" {
			return true
		}
	}
	return false
}
func hasCRD(res []*yaml.Node) bool {
	for _, r := range res {
		if r == nil {
			continue
		}
		if yamlutil.NodeString(yamlutil.MapGet(r, "kind")) == "CustomResourceDefinition" {
			return true
		}
	}
	return false
}

func (m *MultiOverlayMigrator) generateChart() error {
	ovRes := make(map[string][]*yaml.Node)
	for _, n := range m.overlayNames {
		ovRes[n] = m.overlayData[n].Resources
	}
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
			if err := hv.VerifyEquivalence(staged, m.baseData.Resources, nil, m.BaseDir); err != nil {
				return err
			}
			for _, n := range m.overlayNames {
				ovValues := filepath.Join(staged, "values-"+m.generator.SafeOverlayFilename(n)+".yaml")
				if err := hv.VerifyEquivalence(staged, m.overlayData[n].Resources, []string{ovValues}, "overlay "+n); err != nil {
					return err
				}
			}
			return nil
		}
		verified = true
	}
	if err := m.generator.GenerateChart(m.baseData.Resources, m.baseData.Kustomization, ovRes, preInstall); err != nil {
		return err
	}
	m.report["values_files_generated"] = len(m.generator.GeneratedValuesFiles)
	if verified {
		m.report["validation"] = "passed"
	} else if !m.Verify {
		m.report["validation"] = "skipped-by-user"
	}
	return nil
}

func containsStrMM(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
