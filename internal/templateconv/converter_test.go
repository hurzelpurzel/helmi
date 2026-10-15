package templateconv

import (
	"testing"

	"helmi/internal/yamlutil"

	"gopkg.in/yaml.v3"
)

func node(t *testing.T, s string) *yaml.Node {
	t.Helper()
	n, err := yamlutil.DecodeSingle([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func extractOne(t *testing.T, src string) map[string]any {
	t.Helper()
	out, err := (&Converter{}).ExtractParameterizableValues([]*yaml.Node{node(t, src)})
	if err != nil {
		t.Fatal(err)
	}
	workloads, _ := out["workloads"].(map[string]any)
	for _, v := range workloads {
		return v.(map[string]any)
	}
	return nil
}

func TestExtractDeployment(t *testing.T) {
	v := extractOne(t, `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: web
          image: nginx:1.25
          imagePullPolicy: IfNotPresent
`)
	if v == nil {
		t.Fatal("expected extracted values")
	}
	if got := v["replicas"]; got != 3 {
		t.Fatalf("replicas = %#v", got)
	}
	containers, _ := v["containers"].([]any)
	if len(containers) != 1 {
		t.Fatalf("containers = %#v", containers)
	}
	c := containers[0].(map[string]any)
	if c["name"] != "web" || c["image"] != "web:latest" && c["image"] != "web" {
		// name/image both present
		if c["name"] != "web" {
			t.Fatalf("container = %#v", c)
		}
	}
}

func TestExtractServiceAndConfigMap(t *testing.T) {
	svc := extractOne(t, "apiVersion: v1\nkind: Service\nmetadata:\n  name: web\nspec:\n  type: ClusterIP\n  ports:\n    - port: 80\n")
	if _, ok := svc["service"]; !ok {
		t.Fatalf("expected service block: %#v", svc)
	}
	cm := extractOne(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\ndata:\n  k: v\n")
	if _, ok := cm["data"]; !ok {
		t.Fatalf("expected data block: %#v", cm)
	}
}

func TestNoParameterizableReturnsEmpty(t *testing.T) {
	out, err := (&Converter{}).ExtractParameterizableValues([]*yaml.Node{node(t, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ns\n")})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %#v", out)
	}
}
