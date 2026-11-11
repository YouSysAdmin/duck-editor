package yamlnode

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestLookup(t *testing.T) {
	t.Parallel()

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("a: 1\nb:\n  c: x\n"), &doc); err != nil {
		t.Fatal(err)
	}

	root := doc.Content[0]
	if v := Value(root, "a"); v == nil || v.Value != "1" {
		t.Fatalf("a is %v", v)
	}

	if k, v := Entry(Value(root, "b"), "c"); k == nil || k.Value != "c" || v.Value != "x" {
		t.Fatalf("b.c is %v %v", k, v)
	}

	if Value(root, "missing") != nil || Value(nil, "a") != nil {
		t.Fatal("an absent key has no value")
	}

	var keys []string
	for k := range Pairs(root) {
		keys = append(keys, k.Value)
	}

	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("keys %v", keys)
	}

	if n := Scalar("s"); n.Kind != yaml.ScalarNode || n.Value != "s" {
		t.Fatalf("scalar %v", n)
	}
}
