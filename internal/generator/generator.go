package generator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"helmi/internal/errs"
	"helmi/internal/resources"
	"helmi/internal/yamlutil"

	"gopkg.in/yaml.v3"
)

var (
	chartNameRe = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	semverRe    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	safeNameRe  = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
	nonAlnumRe  = regexp.MustCompile(`[^a-z0-9]+`)
	multiDashRe = regexp.MustCompile(`-{2,}`)
)

// LiteralString marshals as literal block scalar.
type LiteralString string

// MarshalYAML implements yaml.Marshaler.
func (l LiteralString) MarshalYAML() (interface{}, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(l), Style: yaml.LiteralStyle}, nil
}

// ResourceValue for values file.
type ResourceValue struct {
	Enabled  bool          `yaml:"enabled"`
	Manifest LiteralString `yaml:"manifest,omitempty"`
}

// ValuesFile structure.
type ValuesFile struct {
	Resources map[string]ResourceValue `yaml:"resources"`
}

// ChartMetadata for Chart.yaml.
type ChartMetadata struct {
	APIVersion  string `yaml:"apiVersion"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"`
	Version     string `yaml:"version"`
	AppVersion  string `yaml:"appVersion"`
}

// Generator generates Helm charts.
type Generator struct {
	ChartName            string
	OutputDir            string
	ChartDir             string
	TemplatesDir         string
	Overwrite            bool
	ChartMetadata        ChartMetadata
	GeneratedValuesFiles []string
}

// ValidateChartName validates chart name.
func ValidateChartName(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errs.Configurationf("Chart name must be a non-empty string")
	}
	if len(name) > 253 || !chartNameRe.MatchString(name) {
		return "", errs.Configurationf("Invalid chart name %q. Use lowercase letters, numbers, or '-', starting and ending with a letter or number (maximum 253 characters).", name)
	}
	return name, nil
}

// NormalizeChartName normalizes chart name from directory.
func NormalizeChartName(name string) (string, error) {
	normalized := nonAlnumRe.ReplaceAllString(strings.ToLower(name), "-")
	normalized = strings.Trim(normalized, "-")
	normalized = multiDashRe.ReplaceAllString(normalized, "-")
	if len(normalized) > 253 {
		normalized = strings.TrimRight(normalized[:253], "-")
	}
	if normalized == "" {
		return "", errs.Configurationf("Cannot derive a valid chart name from %q", name)
	}
	return ValidateChartName(normalized)
}

// NewGenerator creates a generator.
func NewGenerator(chartName, outputDir string, overwrite bool) (*Generator, error) {
	cn, err := ValidateChartName(chartName)
	if err != nil {
		return nil, err
	}
	out, err := filepath.Abs(filepath.Clean(outputDir))
	if err != nil {
		return nil, errs.Generationf("%v", err)
	}
	chartDir := filepath.Join(out, cn)
	tmplDir := filepath.Join(chartDir, "templates")
	g := &Generator{ChartName: cn, OutputDir: out, ChartDir: chartDir, TemplatesDir: tmplDir, Overwrite: overwrite}
	g.ChartMetadata = ChartMetadata{APIVersion: "v2", Name: cn, Description: "Helm chart for " + cn + " (migrated from Kustomize)", Type: "application", Version: "0.1.0", AppVersion: "1.0.0"}
	return g, nil
}

// ValidateChartVersion validates SemVer.
func ValidateChartVersion(version string) (string, error) {
	if strings.TrimSpace(version) == "" || !semverRe.MatchString(version) {
		return "", errs.Configurationf("Invalid chart version %q; expected semantic versioning", version)
	}
	return version, nil
}

func (g *Generator) GenerateChart(baseResources []*yaml.Node, kustomization *yaml.Node, overlayResources map[string][]*yaml.Node, preInstall func(string) error) error {
	if baseResources == nil {
		baseResources = []*yaml.Node{}
	}
	overlays := make(map[string][]*yaml.Node, len(overlayResources))
	for name, res := range overlayResources {
		if strings.TrimSpace(name) == "" {
			return errs.Configurationf("Invalid overlay name: %q", name)
		}
		overlays[name] = res
	}
	baseIdx, err := resourcesIndex(baseResources)
	if err != nil {
		return err
	}
	ovIdx := make(map[string]map[resources.Identity]*yaml.Node)
	ovOrder := make(map[string][]resources.Identity)
	for name, res := range overlays {
		oi, err := resourcesIndex(res)
		if err != nil {
			return err
		}
		ovIdx[name] = oi.Items
		ovOrder[name] = oi.Order
	}
	catalog, overlayValues, err := g.buildResourceValues(baseIdx.Items, overlays, ovIdx)
	if err != nil {
		return err
	}
	g.ChartMetadata.AppVersion = "1.0.0"
	g.updateChartMetadata(baseResources, kustomization)
	if err := os.MkdirAll(g.OutputDir, 0755); err != nil {
		return errs.Generationf("%v", err)
	}
	if fi, err := os.Lstat(g.ChartDir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errs.Generationf("Refusing to replace symlinked chart directory: %s", g.ChartDir)
	}
	if fi, err := os.Stat(g.ChartDir); err == nil && fi.IsDir() {
		if !g.Overwrite {
			return errs.Generationf("Chart directory already exists: %s. Use overwrite/--force explicitly.", g.ChartDir)
		}
	}
	stagingRoot, err := os.MkdirTemp(g.OutputDir, ".helmi-stage-")
	if err != nil {
		return errs.Generationf("%v", err)
	}
	defer os.RemoveAll(stagingRoot)
	stagedChart := filepath.Join(stagingRoot, g.ChartName)
	if err := g.writeChart(stagedChart, catalog, overlayValues); err != nil {
		return err
	}
	if preInstall != nil {
		if err := preInstall(stagedChart); err != nil {
			return err
		}
	}
	if err := g.installStaged(stagedChart); err != nil {
		return err
	}
	g.TemplatesDir = filepath.Join(g.ChartDir, "templates")
	return nil
}

func resourcesIndex(nodes []*yaml.Node) (*resources.Index, error) {
	return resources.IndexResources(nodes)
}

func (g *Generator) buildResourceValues(base map[resources.Identity]*yaml.Node, overlays map[string][]*yaml.Node, ovIdx map[string]map[resources.Identity]*yaml.Node) (map[string]ResourceValue, map[string]map[string]ResourceValue, error) {
	all := make(map[resources.Identity]struct{})
	for id := range base {
		all[id] = struct{}{}
	}
	for _, oi := range ovIdx {
		for id := range oi {
			all[id] = struct{}{}
		}
	}
	idents := make([]resources.Identity, 0, len(all))
	for id := range all {
		idents = append(idents, id)
	}
	sortIdent(idents)
	rep := make(map[resources.Identity]*yaml.Node)
	for id := range base {
		rep[id] = base[id]
	}
	ovNames := make([]string, 0, len(overlays))
	for n := range overlays {
		ovNames = append(ovNames, n)
	}
	// sort ovNames
	for i := 0; i < len(ovNames); i++ {
		for j := i + 1; j < len(ovNames); j++ {
			if ovNames[i] > ovNames[j] {
				ovNames[i], ovNames[j] = ovNames[j], ovNames[i]
			}
		}
	}
	for _, n := range ovNames {
		for id := range ovIdx[n] {
			if _, ok := rep[id]; !ok {
				rep[id] = ovIdx[n][id]
			}
		}
	}
	catalog := make(map[string]ResourceValue)
	keyByID := make(map[resources.Identity]string)
	for _, id := range idents {
		r := rep[id]
		key, err := resources.ResourceKey(r)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := catalog[key]; ok {
			return nil, nil, errs.Generationf("Internal resource-key collision for %s", id.Text())
		}
		keyByID[id] = key
		_, inBase := base[id]
		man := serializeResource(r)
		catalog[key] = ResourceValue{Enabled: inBase, Manifest: LiteralString(man)}
	}
	overlayValues := make(map[string]map[string]ResourceValue)
	for _, n := range ovNames {
		changes := make(map[string]ResourceValue)
		oi := ovIdx[n]
		for _, id := range idents {
			key := keyByID[id]
			inOv := false
			if _, ok := oi[id]; ok {
				inOv = true
			}
			cv := ResourceValue{Enabled: inOv}
			if inOv {
				eq, _ := yamlutil.DeepEqual(oi[id], rep[id])
				if !eq {
					cv.Manifest = LiteralString(serializeResource(oi[id]))
				}
			}
			changes[key] = cv
		}
		overlayValues[n] = changes
	}
	return catalog, overlayValues, nil
}

func sortIdent(s []resources.Identity) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i].APIVersion > s[j].APIVersion || (s[i].APIVersion == s[j].APIVersion && s[i].Kind > s[j].Kind) || (s[i].APIVersion == s[j].APIVersion && s[i].Kind == s[j].Kind && s[i].Namespace > s[j].Namespace) || (s[i].APIVersion == s[j].APIVersion && s[i].Kind == s[j].Kind && s[i].Namespace == s[j].Namespace && s[i].Name > s[j].Name) {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func serializeResource(r *yaml.Node) string {
	c := yamlutil.Clone(r)
	yamlutil.StripComments(c)
	b, err := yamlutil.Marshal(c)
	if err != nil {
		return ""
	}
	s := string(b)
	// ensure trailing newline as Python does rstrip + \n
	s = strings.TrimRight(s, "\r\n\t ")
	return s + "\n"
}

func (g *Generator) updateChartMetadata(baseResources []*yaml.Node, kustomization *yaml.Node) {
	if kustomization != nil {
		imgs := yamlutil.MapGet(kustomization, "images")
		if yamlutil.IsSequence(imgs) && len(imgs.Content) > 0 {
			first := imgs.Content[0]
			if yamlutil.IsMapping(first) {
				if nt := yamlutil.MapGet(first, "newTag"); nt != nil {
					if s, ok := yamlutil.ScalarString(nt); ok {
						g.ChartMetadata.AppVersion = s
						return
					}
				}
				if dg := yamlutil.MapGet(first, "digest"); dg != nil {
					if s, ok := yamlutil.ScalarString(dg); ok {
						g.ChartMetadata.AppVersion = s
						return
					}
				}
			}
		}
	}
	for _, r := range baseResources {
		if r == nil || !yamlutil.IsMapping(r) {
			continue
		}
		kind := yamlutil.NodeString(yamlutil.MapGet(r, "kind"))
		if kind == "Deployment" || kind == "StatefulSet" || kind == "DaemonSet" || kind == "ReplicaSet" || kind == "Job" {
			spec := yamlutil.MapGet(r, "spec")
			tmpl := yamlutil.MapGet(spec, "template")
			ps := yamlutil.MapGet(tmpl, "spec")
			conts := yamlutil.MapGet(ps, "containers")
			if yamlutil.IsSequence(conts) && len(conts.Content) > 0 {
				c0 := conts.Content[0]
				if yamlutil.IsMapping(c0) {
					img := yamlutil.NodeString(yamlutil.MapGet(c0, "image"))
					if img != "" {
						if strings.Contains(img, "@") {
							parts := strings.SplitN(img, "@", 2)
							if len(parts) == 2 {
								g.ChartMetadata.AppVersion = parts[1]
								return
							}
						}
						if strings.Contains(img, ":") {
							lc := img[strings.LastIndex(img, "/")+1:]
							if idx := strings.LastIndex(lc, ":"); idx > -1 {
								g.ChartMetadata.AppVersion = lc[idx+1:]
								return
							}
						}
						g.ChartMetadata.AppVersion = img
						return
					}
				}
			}
		}
	}
}

func (g *Generator) writeChart(chartDir string, catalog map[string]ResourceValue, overlayValues map[string]map[string]ResourceValue) error {
	tmpl := filepath.Join(chartDir, "templates")
	if err := os.MkdirAll(tmpl, 0755); err != nil {
		return errs.Generationf("%v", err)
	}
	if err := os.MkdirAll(filepath.Join(chartDir, "charts"), 0755); err != nil {
		return errs.Generationf("%v", err)
	}
	cm := g.ChartMetadata
	// write Chart.yaml
	cy := ChartMetadata{APIVersion: cm.APIVersion, Name: cm.Name, Description: cm.Description, Type: cm.Type, Version: cm.Version, AppVersion: cm.AppVersion}
	m := map[string]any{"apiVersion": cy.APIVersion, "name": cy.Name, "description": cy.Description, "type": cy.Type, "version": cy.Version, "appVersion": cy.AppVersion}
	buf, err := yamlMarshalMap(m)
	if err != nil {
		return errs.Generationf("%v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), buf, 0644); err != nil {
		return errs.Generationf("%v", err)
	}
	// values.yaml
	val := ValuesFile{Resources: catalog}
	vbuf, err := yamlMarshalValue(val)
	if err != nil {
		return errs.Generationf("%v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), vbuf, 0644); err != nil {
		return errs.Generationf("%v", err)
	}
	g.GeneratedValuesFiles = []string{"values.yaml"}
	// templates
	tpl := resourceTemplate()
	if err := os.WriteFile(filepath.Join(tmpl, "resources.yaml"), []byte(tpl), 0644); err != nil {
		return errs.Generationf("%v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpl, "NOTES.txt"), []byte(notesTemplate()), 0644); err != nil {
		return errs.Generationf("%v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, ".helmignore"), []byte(helmignore()), 0644); err != nil {
		return errs.Generationf("%v", err)
	}
	mig := g.migrationNotes(overlayValues)
	if err := os.WriteFile(filepath.Join(chartDir, "MIGRATION.md"), []byte(mig), 0644); err != nil {
		return errs.Generationf("%v", err)
	}
	// overlay values
	used := map[string]bool{"values.yaml": true}
	ovNames := make([]string, 0, len(overlayValues))
	for n := range overlayValues {
		ovNames = append(ovNames, n)
	}
	for i := 0; i < len(ovNames); i++ {
		for j := i + 1; j < len(ovNames); j++ {
			if ovNames[i] > ovNames[j] {
				ovNames[i], ovNames[j] = ovNames[j], ovNames[i]
			}
		}
	}
	for _, n := range ovNames {
		safe := g.SafeOverlayFilename(n)
		fn := "values-" + safe + ".yaml"
		fkey := strings.ToLower(fn)
		dup := false
		for k := range used {
			if strings.ToLower(k) == fkey {
				dup = true
				break
			}
		}
		if dup {
			return errs.Configurationf("Overlay names produce the same values filename: %q", n)
		}
		v := ValuesFile{Resources: overlayValues[n]}
		bv, err := yaml.Marshal(v)
		if err != nil {
			return errs.Generationf("%v", err)
		}
		if err := os.WriteFile(filepath.Join(chartDir, fn), bv, 0644); err != nil {
			return errs.Generationf("%v", err)
		}
		g.GeneratedValuesFiles = append(g.GeneratedValuesFiles, fn)
		used[fn] = true
	}
	return nil
}

func yamlMarshalMap(m map[string]any) ([]byte, error) {
	var buf strings.Builder
	// simple manual write for predictable order? order we want is specific; write in given order
	// but easier use yaml encoder on node? construct node
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	node := &yaml.Node{Kind: yaml.MappingNode}
	order := []string{"apiVersion", "name", "description", "type", "version", "appVersion"}
	for _, k := range order {
		if v, ok := m[k]; ok {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, toNode(v))
		}
	}
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	enc.Close()
	return []byte(strings.TrimRight(buf.String(), "\r\n") + string('\n')), nil
}

func yamlMarshalValue(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toNode(v any) *yaml.Node {
	var n yaml.Node
	_ = n.Encode(v)
	return &n
}

func resourceTemplate() string {
	return `{{- range $resourceID := keys .Values.resources | sortAlpha }}
{{- $resource := index $.Values.resources $resourceID }}
{{- if $resource.enabled }}
{{- if not $resource.manifest }}
{{- fail (printf "resources.%s.manifest is required when enabled" $resourceID) }}
{{- end }}
{{ $resource.manifest }}---
{{- end }}
{{- end }}
`
}

func notesTemplate() string {
	return `Kustomize migration installed {{ len .Values.resources }} catalogued resources.
Review MIGRATION.md before changing the generated manifest values.
`
}

func helmignore() string {
	return `.DS_Store
.git/
.gitignore
.idea/
.vscode/
*.swp
*.tmp
*.orig
*~
`
}

func (g *Generator) migrationNotes(overlays map[string]map[string]ResourceValue) string {
	lines := []string{"# " + g.ChartName + " migration notes", "", "This chart was generated from the fully rendered Kustomize output. Each entry in", "`values.yaml` contains one complete Kubernetes manifest, which avoids lossy attempts", "to reinterpret patches or custom resources.", "", "Render the base with:", "", "    helm template RELEASE ."}
	if len(overlays) > 0 {
		ovNames := make([]string, 0, len(overlays))
		for n := range overlays {
			ovNames = append(ovNames, n)
		}
		for i := 0; i < len(ovNames); i++ {
			for j := i + 1; j < len(ovNames); j++ {
				if ovNames[i] > ovNames[j] {
					ovNames[i], ovNames[j] = ovNames[j], ovNames[i]
				}
			}
		}
		lines = append(lines, "", "Render an overlay with:", "")
		for _, n := range ovNames {
			safe := g.SafeOverlayFilename(n)
			lines = append(lines, "    helm template RELEASE . -f values-"+safe+".yaml  # "+n)
		}
	}
	lines = append(lines, "", "Do not use `tpl` on manifest values: literal `{{ ... }}` content may belong to", "the application and is intentionally preserved verbatim.", "")
	return strings.Join(lines, "\n")
}

func (g *Generator) SafeOverlayFilename(name string) string {
	safe := safeNameRe.ReplaceAllString(name, "-")
	safe = strings.Trim(safe, ".-")
	if safe == "" || safe == "." || safe == ".." {
		panic(errs.Configurationf("Overlay name cannot be used as a values filename: %q", name).Error())
	}
	if len([]byte(safe)) > 180 {
		sum := sha256.Sum256([]byte(name))
		d := hex.EncodeToString(sum[:])[:10]
		base := safe
		if len(base) > 160 {
			base = base[:160]
		}
		base = strings.TrimRight(base, ".-")
		safe = base + "-" + d
	}
	return safe
}

func (g *Generator) installStaged(staged string) error {
	if _, err := os.Stat(g.ChartDir); err != nil {
		if err := os.Rename(staged, g.ChartDir); err != nil {
			return errs.Generationf("%v", err)
		}
		return nil
	}
	backup, err := os.MkdirTemp(g.OutputDir, ".helmi-backup-")
	if err != nil {
		return errs.Generationf("%v", err)
	}
	backupChart := filepath.Join(backup, g.ChartName)

	if err := os.Rename(g.ChartDir, backupChart); err != nil {
		os.RemoveAll(backup)
		return errs.Generationf("%v", err)
	}
	if err := os.Rename(staged, g.ChartDir); err != nil {
		_ = os.Rename(backupChart, g.ChartDir)
		os.RemoveAll(backup)
		return errs.Generationf("failed to install: %v", err)
	}
	os.RemoveAll(backup)
	return nil
}
