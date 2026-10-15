package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"helmi/internal/helm"
)

var helmiBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "helmi-cli")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	helmiBin = filepath.Join(dir, "helmi")
	build := exec.Command("go", "build", "-o", helmiBin, ".")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		panic("build failed: " + string(out))
	}
	os.Exit(m.Run())
}

func fixtures(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
}

func haveTools(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("kustomize"); err != nil {
		if _, err := exec.LookPath("kubectl"); err != nil {
			t.Skip("kustomize or kubectl required")
		}
	}
	if !helm.IsAvailable() {
		t.Skip("helm required")
	}
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(helmiBin, args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func TestMigrateSingleJSON(t *testing.T) {
	haveTools(t)
	out := t.TempDir()
	stdout, err := run(t, "migrate", fixtures("application", "base"), out, "-n", "sample", "-f", "json")
	if err != nil {
		t.Fatalf("migrate failed: %v\n%s", err, stdout)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("invalid json report: %v\n%s", err, stdout)
	}
	if report["status"] != "success" {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestMigrateBlockedByHelmHook(t *testing.T) {
	haveTools(t)
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "kustomization.yaml"), "resources:\n  - deploy.yaml\n")
	mustWrite(t, filepath.Join(src, "deploy.yaml"), `apiVersion: apps/v1
kind: Deployment
metadata:
  name: hooked
  annotations:
    helm.sh/hook: pre-install
spec: {}
`)
	out := t.TempDir()
	stdout, err := run(t, "migrate", src, out, "-n", "sample")
	if err == nil {
		t.Fatalf("expected failure, got success:\n%s", stdout)
	}
	if strings.Count(stdout, "helm.sh/hook") < 1 {
		t.Fatalf("expected hook error mention:\n%s", stdout)
	}
	if _, statErr := os.Stat(filepath.Join(out, "sample")); !os.IsNotExist(statErr) {
		t.Fatalf("chart directory should not be created on failure: %v", statErr)
	}
}

func TestMigrateExistingWithoutForceFails(t *testing.T) {
	haveTools(t)
	out := t.TempDir()
	if _, err := run(t, "migrate", fixtures("application", "base"), out, "-n", "sample"); err != nil {
		t.Fatalf("first migrate failed: %v", err)
	}
	stdout, err := run(t, "migrate", fixtures("application", "base"), out, "-n", "sample")
	if err == nil {
		t.Fatalf("expected overwrite failure:\n%s", stdout)
	}
}

func TestForceRemovesStaleFiles(t *testing.T) {
	haveTools(t)
	out := t.TempDir()
	if _, err := run(t, "migrate", fixtures("application", "base"), out, "-n", "sample"); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "sample", "templates", "stale.yaml")
	mustWrite(t, stale, "old\n")
	if _, err := run(t, "migrate", fixtures("application", "base"), out, "-n", "sample", "--force"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale file removed, err=%v", err)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	haveTools(t)
	out := t.TempDir()
	if _, err := run(t, "migrate", fixtures("application", "base"), out, "-n", "sample", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "sample")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created chart: %v", err)
	}
}

func TestAnalyzeReportsResources(t *testing.T) {
	haveTools(t)
	stdout, err := run(t, "analyze", fixtures("application", "base"), "-f", "json")
	if err != nil {
		t.Fatalf("analyze failed: %v\n%s", err, stdout)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout)
	}
	if report["resources_found"].(float64) != 4 {
		t.Fatalf("expected 4 resources: %#v", report)
	}
}

func TestInitScaffold(t *testing.T) {
	out := t.TempDir()
	stdout, err := run(t, "init", "-n", "scaffold", "-o", out)
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, stdout)
	}
	if _, err := os.Stat(filepath.Join(out, "scaffold", "Chart.yaml")); err != nil {
		t.Fatalf("Chart.yaml missing: %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
