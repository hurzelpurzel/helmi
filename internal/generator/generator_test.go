package generator

import (
	"os"
	"path/filepath"
	"testing"

	"helmi/internal/yamlutil"

	"gopkg.in/yaml.v3"
)

func TestValidateChartName(t *testing.T) {
	bad := []string{"Bad_Name", "bad.name", "-bad", "bad-", ""}
	for _, name := range bad {
		if _, err := ValidateChartName(name); err == nil {
			t.Errorf("expected error for %q", name)
		}
	}
	good := []string{"app", "my-app", "app1"}
	for _, name := range good {
		if _, err := ValidateChartName(name); err != nil {
			t.Errorf("unexpected error for %q: %v", name, err)
		}
	}
}

func TestNormalizeChartName(t *testing.T) {
	got, err := NormalizeChartName("My_App.d")
	if err != nil {
		t.Fatal(err)
	}
	if got != "my-app-d" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateChartVersion(t *testing.T) {
	if _, err := ValidateChartVersion("not-semver"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ValidateChartVersion("0.1.0"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func node(t *testing.T, s string) *yaml.Node {
	t.Helper()
	n, err := yamlutil.DecodeSingle([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDuplicateResourceIdentityRejected(t *testing.T) {
	r := node(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: same\n")
	g, err := NewGenerator("valid", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.GenerateChart([]*yaml.Node{r, r}, nil, nil, nil); err == nil {
		t.Fatal("expected duplicate identity error")
	}
}

func TestFailedPreInstallValidationPreservesExistingChart(t *testing.T) {
	dir := t.TempDir()
	chart := filepath.Join(dir, "valid")
	if err := os.MkdirAll(chart, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(chart, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := NewGenerator("valid", dir, true)
	if err != nil {
		t.Fatal(err)
	}
	r := node(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: safe\n")
	err = g.GenerateChart([]*yaml.Node{r}, nil, nil, func(string) error {
		return os.ErrInvalid
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("sentinel removed: %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("sentinel modified: %q", data)
	}
}

func TestOverlayFilenameCollisionsRejected(t *testing.T) {
	g, err := NewGenerator("valid", t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	r := node(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: safe\n")
	err = g.GenerateChart([]*yaml.Node{}, nil, map[string][]*yaml.Node{"DEV": {r}, "dev": {r}}, nil)
	if err == nil {
		t.Fatal("expected collision error")
	}
}
