package yamlutil

import (
	"bytes"
	"fmt"
	"io"
	"reflect"

	"gopkg.in/yaml.v3"
)

// DecodeSingle decodes a single YAML document and returns the unwrapped root node.
func DecodeSingle(data []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var first yaml.Node
	if err := dec.Decode(&first); err == io.EOF {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	root, err := rootOf(&first)
	if err != nil {
		return nil, err
	}
	var second yaml.Node
	if err := dec.Decode(&second); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("expected a single YAML document")
		}
		return nil, err
	}
	return root, nil
}

// DecodeDocuments decodes all YAML documents and returns unwrapped root nodes.
func DecodeDocuments(data []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var node yaml.Node
		if err := dec.Decode(&node); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		root, err := rootOf(&node)
		if err != nil {
			return nil, err
		}
		docs = append(docs, root)
	}
	var filtered []*yaml.Node
	for _, d := range docs {
		if d == nil || IsNull(d) {
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered, nil
}

func rootOf(node *yaml.Node) (*yaml.Node, error) {
	if node == nil {
		return nil, nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil, nil
		}
		return node.Content[0], nil
	}
	return node, nil
}

// CheckDuplicateKeys checks for duplicate mapping keys.
func CheckDuplicateKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		for _, c := range node.Content {
			if err := CheckDuplicateKeys(c); err != nil {
				return err
			}
		}
		return nil
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{})
		for i := 0; i+1 < len(node.Content); i += 2 {
			k := node.Content[i]
			key := k.Value
			if _, ok := seen[key]; ok {
				return fmt.Errorf("duplicate YAML key %q at line %d", key, k.Line)
			}
			seen[key] = struct{}{}
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			if err := CheckDuplicateKeys(node.Content[i+1]); err != nil {
				return err
			}
		}
		return nil
	}
	for _, c := range node.Content {
		if err := CheckDuplicateKeys(c); err != nil {
			return err
		}
	}
	return nil
}

// MapGet returns the value node for key in a mapping node.
func MapGet(node *yaml.Node, key string) *yaml.Node {
	if !IsMapping(node) {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		if k != nil && k.Kind == yaml.ScalarNode && k.Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// ScalarString returns string value if node is string scalar.
func ScalarString(node *yaml.Node) (string, bool) {
	if node == nil || node.Kind != yaml.ScalarNode {
		return "", false
	}
	return node.Value, true
}

// NodeString returns string representation if scalar, else empty.
func NodeString(node *yaml.Node) string {
	s, ok := ScalarString(node)
	if !ok {
		return ""
	}
	return s
}

// IsMapping returns true if node is mapping.
func IsMapping(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.MappingNode
}

// IsSequence returns true if node is sequence.
func IsSequence(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.SequenceNode
}

// IsNull returns true if node is null-like.
func IsNull(node *yaml.Node) bool {
	if node == nil {
		return true
	}
	if node.Kind == yaml.ScalarNode && (node.Tag == "!!null" || node.Value == "~" || node.Value == "null" || node.Value == "") {
		return true
	}
	return false
}

// Marshal marshals a node to YAML with indent 2.
func Marshal(node *yaml.Node) ([]byte, error) {
	if node == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

// Decode decodes a node to interface{}.
func Decode(node *yaml.Node) (any, error) {
	if node == nil {
		return nil, nil
	}
	var v any
	if err := node.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// DeepEqual compares nodes by decoded value.
func DeepEqual(a, b *yaml.Node) (bool, error) {
	av, err := Decode(a)
	if err != nil {
		return false, err
	}
	bv, err := Decode(b)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(av, bv), nil
}

// Clone clones a node tree.
func Clone(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	c := *node
	c.Content = make([]*yaml.Node, len(node.Content))
	for i, ch := range node.Content {
		c.Content[i] = Clone(ch)
	}
	return &c
}

// StripComments removes all comments.
func StripComments(node *yaml.Node) {
	if node == nil {
		return
	}
	node.HeadComment = ""
	node.LineComment = ""
	node.FootComment = ""
	for _, ch := range node.Content {
		StripComments(ch)
	}
}
