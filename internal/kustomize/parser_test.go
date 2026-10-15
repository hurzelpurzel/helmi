package kustomize

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"helmchen/internal/yamlutil"
)

func hasKustomize() bool {
	if _, err := exec.LookPath("kustomize"); err == nil {
		return true
	}
	_, err := exec.LookPath("kubectl")
	return err == nil
}

func fixture(t *testing.T, parts ...string) string {
	t.Helper()
	return filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
}

func TestBuildCommandArgumentsCannotBeEmpty(t *testing.T) {
	if _, err := NewParser(fixture(t, "application", "base"), []string{""}, 120); err == nil {
		t.Fatal("expected error for empty command argument")
	}
}

func TestLoadRejectsDuplicateKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte("resources: []\nresources: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := NewParser(dir, nil, 120)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Parse(); err == nil || !strings.Contains(err.Error(), "duplicate YAML key") {
		t.Fatalf("expected duplicate key error, got %v", err)
	}
}

func TestBuildAppliesGenerators(t *testing.T) {
	if !hasKustomize() {
		t.Skip("kustomize or kubectl required")
	}
	p, err := NewParser(fixture(t, "application", "base"), nil, 120)
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	var configMap, secret bool
	for _, r := range data.Resources {
		switch yamlutil.NodeString(yamlutil.MapGet(r, "kind")) {
		case "ConfigMap":
			configMap = true
		case "Secret":
			secret = true
		}
	}
	if !configMap || !secret {
		t.Fatal("expected ConfigMap and Secret generators")
	}
}

func TestOverlayBuild(t *testing.T) {
	if !hasKustomize() {
		t.Skip("kustomize or kubectl required")
	}
	p, err := NewParser(fixture(t, "application", "overlays", "dev"), nil, 120)
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Resources) == 0 {
		t.Fatal("expected overlay resources")
	}
}
