package integration

import (
	"os/exec"
	"path/filepath"
	"testing"

	"helmchen/internal/helm"
	"helmchen/internal/kustomize"
	"helmchen/internal/migrator"
	"helmchen/internal/multioverlay"
	"helmchen/internal/resources"
)

func fixtures(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
}

func hasKustomize() bool {
	if _, err := exec.LookPath("kustomize"); err == nil {
		return true
	}
	_, err := exec.LookPath("kubectl")
	return err == nil
}

func requireTools(t *testing.T) {
	t.Helper()
	if !hasKustomize() {
		t.Skip("kustomize or kubectl required")
	}
	if !helm.IsAvailable() {
		t.Skip("helm required")
	}
}

func TestSingleMigrationIsEquivalent(t *testing.T) {
	requireTools(t)
	out := t.TempDir()
	m, err := migrator.New(fixtures("application", "base"), out, "sample", false, false, true, 120, nil)
	if err != nil {
		t.Fatal(err)
	}
	report, err := m.Migrate()
	if err != nil {
		t.Fatal(err)
	}
	if report["status"] != "success" || report["validation"] != "passed" {
		t.Fatalf("unexpected report: %#v", report)
	}
	chart := filepath.Join(out, "sample")
	hv, err := helm.NewValidator("", 120)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := hv.Render(chart, nil, "helmi-validation")
	if err != nil {
		t.Fatal(err)
	}
	kp, err := kustomize.NewParser(fixtures("application", "base"), nil, 120)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := kp.GetAllResources()
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.AssertResourceEquivalence(expected, rendered, "test base"); err != nil {
		t.Fatal(err)
	}
}

func TestMultiOverlayMigration(t *testing.T) {
	requireTools(t)
	out := t.TempDir()
	m, err := multioverlay.New(fixtures("application", "base"), fixtures("application", "overlays"), out, "sample", false, false, true, 120, nil)
	if err != nil {
		t.Fatal(err)
	}
	report, err := m.Migrate()
	if err != nil {
		t.Fatal(err)
	}
	if report["overlays_processed"] != 1 {
		t.Fatalf("expected 1 overlay, got %#v", report["overlays_processed"])
	}
	if report["values_files_generated"] != 2 {
		t.Fatalf("expected 2 values files, got %#v", report["values_files_generated"])
	}
}

func TestCriticalBaseIsExact(t *testing.T) {
	requireTools(t)
	out := t.TempDir()
	m, err := migrator.New(fixtures("critical", "base"), out, "critical", false, false, true, 120, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(); err != nil {
		t.Fatal(err)
	}
	hv, err := helm.NewValidator("", 120)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := hv.Render(filepath.Join(out, "critical"), nil, "helmi-validation")
	if err != nil {
		t.Fatal(err)
	}
	kp, err := kustomize.NewParser(fixtures("critical", "base"), nil, 120)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := kp.GetAllResources()
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.AssertResourceEquivalence(expected, rendered, "critical base"); err != nil {
		t.Fatal(err)
	}
}

func TestCriticalOverlaysAreExact(t *testing.T) {
	requireTools(t)
	out := t.TempDir()
	m, err := multioverlay.New(fixtures("critical", "base"), fixtures("critical", "overlays"), out, "critical", false, false, true, 120, nil)
	if err != nil {
		t.Fatal(err)
	}
	report, err := m.Migrate()
	if err != nil {
		t.Fatal(err)
	}
	if report["status"] != "success" {
		t.Fatalf("unexpected report %#v", report)
	}
	chart := filepath.Join(out, "critical")
	hv, err := helm.NewValidator("", 120)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"production", "canary"} {
		kp, err := kustomize.NewParser(fixtures("critical", "overlays", name), nil, 120)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := kp.GetAllResources()
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := hv.Render(chart, []string{filepath.Join(chart, "values-"+name+".yaml")}, "helmi-validation")
		if err != nil {
			t.Fatal(err)
		}
		if err := resources.AssertResourceEquivalence(expected, rendered, "overlay "+name); err != nil {
			t.Fatal(err)
		}
	}
}
