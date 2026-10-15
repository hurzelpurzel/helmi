package resources

import (
	"strings"
	"testing"

	"helmi/internal/yamlutil"

	"gopkg.in/yaml.v3"
)

func mustNode(t *testing.T, s string) *yaml.Node {
	t.Helper()
	n, err := yamlutil.DecodeSingle([]byte(s))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return n
}

func TestResourceIdentity(t *testing.T) {
	n := mustNode(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: ns\n")
	id, err := ResourceIdentity(n)
	if err != nil {
		t.Fatal(err)
	}
	if id.APIVersion != "v1" || id.Kind != "ConfigMap" || id.Namespace != "ns" || id.Name != "app" {
		t.Fatalf("unexpected identity: %#v", id)
	}
	if id.Text() != "v1:ConfigMap:ns/app" {
		t.Fatalf("text = %q", id.Text())
	}
}

func TestResourceIdentityGenerateName(t *testing.T) {
	n := mustNode(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  generateName: app-\n")
	id, err := ResourceIdentity(n)
	if err != nil {
		t.Fatal(err)
	}
	if id.Name != "generate:app-" {
		t.Fatalf("name = %q", id.Name)
	}
}

func TestResourceIdentityErrors(t *testing.T) {
	if _, err := ResourceIdentity(mustNode(t, "kind: ConfigMap\nmetadata:\n  name: x\n")); err == nil {
		t.Fatal("expected apiVersion error")
	}
	if _, err := ResourceIdentity(mustNode(t, "apiVersion: v1\nmetadata:\n  name: x\n")); err == nil {
		t.Fatal("expected kind error")
	}
	if _, err := ResourceIdentity(mustNode(t, "apiVersion: v1\nkind: ConfigMap\n")); err == nil {
		t.Fatal("expected metadata error")
	}
}

func TestResourceKey(t *testing.T) {
	n := mustNode(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n")
	key, err := ResourceKey(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "configmap-app-") {
		t.Fatalf("key = %q", key)
	}
}

func TestIndexResourcesRejectsDuplicates(t *testing.T) {
	r := mustNode(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: same\n")
	if _, err := IndexResources([]*yaml.Node{r, r}); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestAssertResourceEquivalence(t *testing.T) {
	a := mustNode(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n")
	b := mustNode(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n")
	if err := AssertResourceEquivalence([]*yaml.Node{a}, []*yaml.Node{a}, "ctx"); err != nil {
		t.Fatalf("expected equivalence: %v", err)
	}
	if err := AssertResourceEquivalence([]*yaml.Node{a}, []*yaml.Node{b}, "ctx"); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestFindHelmBehaviorAnnotations(t *testing.T) {
	n := mustNode(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n  annotations:\n    helm.sh/hook: pre-install\n")
	findings, err := FindHelmBehaviorAnnotations([]*yaml.Node{n})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "helm.sh/hook") {
		t.Fatalf("findings = %#v", findings)
	}
}
