// Package yamlnode holds lookups over yaml.Node mappings, for code that
// reads or rewrites YAML while keeping order and comments.
package yamlnode

import (
	"iter"

	"go.yaml.in/yaml/v3"
)

// Pairs iterates the key and value nodes of a mapping. A nil node has
// none.
func Pairs(m *yaml.Node) iter.Seq2[*yaml.Node, *yaml.Node] {
	return func(yield func(*yaml.Node, *yaml.Node) bool) {
		if m == nil {
			return
		}

		for i := 0; i+1 < len(m.Content); i += 2 {
			if !yield(m.Content[i], m.Content[i+1]) {
				return
			}
		}
	}
}

// Entry returns the key and value nodes of key in a mapping, nils when
// absent.
func Entry(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for k, v := range Pairs(m) {
		if k.Value == key {
			return k, v
		}
	}

	return nil, nil
}

// Value returns the value node of key in a mapping, nil when absent.
func Value(m *yaml.Node, key string) *yaml.Node {
	_, v := Entry(m, key)

	return v
}

// Scalar makes a scalar node.
func Scalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s}
}
