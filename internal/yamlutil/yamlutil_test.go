package yamlutil

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDecodeDocumentsSkipsNulls(t *testing.T) {
	docs, err := DecodeDocuments([]byte("a: 1\n---\n---\nb: 2\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}
}

func TestCheckDuplicateKeys(t *testing.T) {
	root, err := DecodeSingle([]byte("a: 1\na: 2\n"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := CheckDuplicateKeys(root); err == nil {
		t.Fatal("expected duplicate key error")
	} else if !strings.Contains(err.Error(), "duplicate YAML key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMapGetAndScalar(t *testing.T) {
	root, err := DecodeSingle([]byte("kind: ConfigMap\nmetadata:\n  name: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := NodeString(MapGet(root, "kind")); got != "ConfigMap" {
		t.Fatalf("kind = %q", got)
	}
	meta := MapGet(root, "metadata")
	if got := NodeString(MapGet(meta, "name")); got != "x" {
		t.Fatalf("name = %q", got)
	}
	if MapGet(root, "missing") != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestDeepEqualIgnoresKeyOrder(t *testing.T) {
	a, _ := DecodeSingle([]byte("a: 1\nb: 2\n"))
	b, _ := DecodeSingle([]byte("b: 2\na: 1\n"))
	eq, err := DeepEqual(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !eq {
		t.Fatal("expected deep equal for reordered keys")
	}
}

func TestMarshalIndentTwo(t *testing.T) {
	var n yaml.Node
	_ = yaml.Unmarshal([]byte("a:\n  b: 1\n"), &n)
	out, err := Marshal(n.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "\n  b: 1") {
		t.Fatalf("expected two-space indent, got:\n%s", out)
	}
}
