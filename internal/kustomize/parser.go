package kustomize

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"helmi/internal/errs"
	"helmi/internal/resources"
	"helmi/internal/yamlutil"

	"gopkg.in/yaml.v3"
)

const (
	defaultBuildTimeout   = 120
	defaultMaxOutputBytes = 50 * 1024 * 1024
	kustomizationYAML     = "kustomization.yaml"
	kustomizationYML      = "kustomization.yml"
	kustomizationName     = "Kustomization"
)

// PatchEntry describes a patch in kustomization.
type PatchEntry struct {
	Field string
	Entry *yaml.Node
}

// GeneratorEntry wraps generator nodes (not heavily used).
type GeneratorEntry struct {
	Node *yaml.Node
}

// ParsedData holds parsed Kustomize output.
type ParsedData struct {
	Kustomization *yaml.Node
	Resources     []*yaml.Node
	Patches       []PatchEntry
	ConfigMaps    []*yaml.Node
	Secrets       []*yaml.Node
	Namespace     string
	NamePrefix    string
	NameSuffix    string
	Images        []*yaml.Node
	Replicas      []*yaml.Node
	Warnings      []string
	Renderer      string
}

// Parser parses and renders Kustomize configurations.
type Parser struct {
	dir               string
	buildCommand      []string
	timeout           time.Duration
	maxOutputBytes    int
	kustomizationFile string
	kustomization     *yaml.Node
	data              *ParsedData
	parsed            bool
	warnings          []string
}

// NewParser creates a new Parser.
func NewParser(dir string, buildCommand []string, timeoutSec int) (*Parser, error) {
	resolved, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return nil, errs.Configurationf("cannot resolve directory: %v", err)
	}
	stat, err := os.Stat(resolved)
	if err != nil {
		return nil, errs.Configurationf("Kustomize directory does not exist: %s", resolved)
	}
	if !stat.IsDir() {
		return nil, errs.Configurationf("Kustomize path is not a directory: %s", resolved)
	}
	kfile, err := findKustomization(resolved)
	if err != nil {
		return nil, err
	}
	if timeoutSec <= 0 {
		return nil, errs.Configurationf("Kustomize build timeout must be greater than zero")
	}
	maxBytes := defaultMaxOutputBytes
	cmd := make([]string, len(buildCommand))
	copy(cmd, buildCommand)
	if len(cmd) == 0 {
		discovered, err := discoverBuildCommand()
		if err != nil {
			return nil, err
		}
		cmd = discovered
	} else {
		if anyEmpty(cmd) {
			return nil, errs.Configurationf("Kustomize build command arguments cannot be empty")
		}
	}
	return &Parser{dir: resolved, buildCommand: cmd, timeout: time.Duration(timeoutSec) * time.Second, maxOutputBytes: maxBytes, kustomizationFile: kfile}, nil
}

func anyEmpty(args []string) bool {
	for _, a := range args {
		if a == "" {
			return true
		}
	}
	return false
}

func sameFile(p1, p2 string) bool {
	fi1, err1 := os.Stat(p1)
	fi2, err2 := os.Stat(p2)
	if err1 != nil || err2 != nil {
		return p1 == p2
	}
	return os.SameFile(fi1, fi2)
}

func findKustomization(dir string) (string, error) {
	var matches []string
	cands := []string{kustomizationYAML, kustomizationYML, kustomizationName}
	for _, n := range cands {
		path := filepath.Join(dir, n)
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			matches = append(matches, path)
		}
	}
	if len(matches) == 0 {
		return "", errs.Configurationf("No kustomization.yaml, kustomization.yml, or Kustomization file found in %s", dir)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	unique := []string{matches[0]}
	for i := 1; i < len(matches); i++ {
		isDup := false
		for _, u := range unique {
			if sameFile(matches[i], u) {
				isDup = true
				break
			}
		}
		if !isDup {
			unique = append(unique, matches[i])
		}
	}
	if len(unique) > 1 {
		names := make([]string, len(unique))
		for i, u := range unique {
			names[i] = filepath.Base(u)
		}
		return "", errs.Configurationf("Multiple Kustomization files found in %s: %s", dir, strings.Join(names, ", "))
	}
	return unique[0], nil
}

func discoverBuildCommand() ([]string, error) {
	if p, err := exec.LookPath("kustomize"); err == nil {
		return []string{p, "build"}, nil
	}
	if p, err := exec.LookPath("kubectl"); err == nil {
		return []string{p, "kustomize"}, nil
	}
	return nil, errs.Configurationf("Neither 'kustomize' nor 'kubectl' was found on PATH. Install Kustomize or pass an explicit build command through the Python API.")
}

// Parse parses metadata and returns parsed data.
func (p *Parser) Parse() (*ParsedData, error) {
	if p.parsed && p.data != nil {
		return p.data, nil
	}
	warnings := []string{}
	km, err := p.loadKustomization()
	if err != nil {
		return nil, err
	}
	p.kustomization = km
	patches := []PatchEntry{}
	for _, field := range []string{"patches", "patchesStrategicMerge", "patchesJson6902"} {
		v := yamlutil.MapGet(km, field)
		if v == nil {
			continue
		}
		if !yamlutil.IsSequence(v) {
			return nil, errs.Configurationf("Kustomization field '%s' must be a list", field)
		}
		for _, c := range v.Content {
			if c == nil {
				continue
			}
			patches = append(patches, PatchEntry{Field: field, Entry: yamlutil.Clone(c)})
		}
	}
	configMaps, err := p.validatedGeneratorList(km, "configMapGenerator")
	if err != nil {
		return nil, err
	}
	secrets, err := p.validatedGeneratorList(km, "secretGenerator")
	if err != nil {
		return nil, err
	}
	for _, dep := range []string{"bases", "patchesStrategicMerge", "patchesJson6902", "commonLabels"} {
		if yamlutil.MapGet(km, dep) != nil {
			warnings = append(warnings, fmt.Sprintf("Kustomize field '%s' is deprecated; it was rendered faithfully but should be modernized at the source", dep))
		}
	}
	for _, pf := range []string{"generators", "transformers", "validators"} {
		if yamlutil.MapGet(km, pf) != nil {
			warnings = append(warnings, fmt.Sprintf("Kustomize field '%s' may require plugin flags not enabled by default", pf))
		}
	}
	res, err := p.buildResources()
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, p.warnings...)
	ns := ""
	if n := yamlutil.MapGet(km, "namespace"); n != nil {
		if s, ok := yamlutil.ScalarString(n); ok {
			ns = s
		}
	}
	np := ""
	if n := yamlutil.MapGet(km, "namePrefix"); n != nil {
		if s, ok := yamlutil.ScalarString(n); ok {
			np = s
		}
	}
	nS := ""
	if n := yamlutil.MapGet(km, "nameSuffix"); n != nil {
		if s, ok := yamlutil.ScalarString(n); ok {
			nS = s
		}
	}
	imgs := []*yaml.Node{}
	if v := yamlutil.MapGet(km, "images"); v != nil && yamlutil.IsSequence(v) {
		for _, c := range v.Content {
			if c != nil {
				imgs = append(imgs, yamlutil.Clone(c))
			}
		}
	}
	repls := []*yaml.Node{}
	if v := yamlutil.MapGet(km, "replicas"); v != nil && yamlutil.IsSequence(v) {
		for _, c := range v.Content {
			if c != nil {
				repls = append(repls, yamlutil.Clone(c))
			}
		}
	}
	data := &ParsedData{Kustomization: yamlutil.Clone(km), Resources: res, Patches: patches, ConfigMaps: configMaps, Secrets: secrets, Namespace: ns, NamePrefix: np, NameSuffix: nS, Images: imgs, Replicas: repls, Warnings: warnings, Renderer: strings.Join(p.buildCommand, " ")}
	p.data = data
	p.parsed = true
	return data, nil
}

func (p *Parser) loadKustomization() (*yaml.Node, error) {
	content, err := os.ReadFile(p.kustomizationFile)
	if err != nil {
		return nil, errs.Configurationf("Cannot read Kustomization file %s: %v", p.kustomizationFile, err)
	}
	root, err := yamlutil.DecodeSingle(content)
	if err != nil {
		return nil, errs.Configurationf("Invalid YAML in %s: %v", p.kustomizationFile, err)
	}
	if err := yamlutil.CheckDuplicateKeys(root); err != nil {
		return nil, errs.Configurationf(err.Error())
	}
	if yamlutil.IsNull(root) || root == nil {
		return nil, errs.Configurationf("Kustomization file is empty: %s", p.kustomizationFile)
	}
	if !yamlutil.IsMapping(root) {
		return nil, errs.Configurationf("Kustomization document must be a YAML mapping")
	}
	if kind := yamlutil.MapGet(root, "kind"); kind != nil {
		ks := yamlutil.NodeString(kind)
		if ks != "" && ks != "Kustomization" {
			return nil, errs.Configurationf("Expected kind 'Kustomization', found %q in %s", ks, p.kustomizationFile)
		}
	}
	return root, nil
}

func (p *Parser) validatedGeneratorList(km *yaml.Node, field string) ([]*yaml.Node, error) {
	v := yamlutil.MapGet(km, field)
	if v == nil {
		return nil, nil
	}
	if !yamlutil.IsSequence(v) {
		return nil, errs.Configurationf("Kustomization field '%s' must be a list", field)
	}
	var result []*yaml.Node
	for i, c := range v.Content {
		if c == nil || !yamlutil.IsMapping(c) {
			return nil, errs.Configurationf("%s[%d] must be a mapping", field, i)
		}
		name := yamlutil.NodeString(yamlutil.MapGet(c, "name"))
		if strings.TrimSpace(name) == "" {
			return nil, errs.Configurationf("%s[%d] is missing a non-empty name", field, i)
		}
		result = append(result, yamlutil.Clone(c))
	}
	return result, nil
}

func (p *Parser) buildResources() ([]*yaml.Node, error) {
	cmdArgs := append(append([]string(nil), p.buildCommand...), p.dir)
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, errs.Buildf("Kustomize build timed out after %d seconds for %s", int(p.timeout.Seconds()), p.dir)
		}
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		msg = strings.TrimSpace(msg)
		if len(msg) > 4000 {
			msg = msg[:4000] + "…"
		}
		code := "error"
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = fmt.Sprintf("%d", exitErr.ExitCode())
		}
		if msg == "" {
			msg = "no error output"
		}
		return nil, errs.Buildf("Kustomize build failed for %s (exit %s): %s", p.dir, code, msg)
	}
	stderrStr := stderr.String()
	if strings.TrimSpace(stderrStr) != "" {
		if len(stderrStr) > 2000 {
			stderrStr = stderrStr[:2000] + "…"
		}
		p.warnings = append(p.warnings, "Kustomize build warning: "+strings.TrimSpace(stderrStr))
	}
	out := stdout.Bytes()
	if len(out) > p.maxOutputBytes {
		return nil, errs.Buildf("Kustomize build produced %d bytes, exceeding the configured limit of %d bytes", len(out), p.maxOutputBytes)
	}
	docs, err := yamlutil.DecodeDocuments(out)
	if err != nil {
		return nil, errs.Buildf("Kustomize produced invalid YAML: %v", err)
	}
	for _, d := range docs {
		if err := yamlutil.CheckDuplicateKeys(d); err != nil {
			return nil, errs.Buildf("Kustomize produced invalid YAML: %v", err)
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

// GetAllResources returns all rendered resources.
func (p *Parser) GetAllResources() ([]*yaml.Node, error) {
	_, err := p.Parse()
	if err != nil {
		return nil, err
	}
	out := make([]*yaml.Node, len(p.data.Resources))
	for i, r := range p.data.Resources {
		out[i] = yamlutil.Clone(r)
	}
	return out, nil
}
