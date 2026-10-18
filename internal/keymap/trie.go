package keymap

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Trie is one binding: a command, a command sequence or a node with
// further keys.
type Trie struct {
	Command  string
	Sequence []string
	Node     *Node

	// Label names a user binding in key menus instead of its command.
	Label string
}

// Node is a submap such as the Goto menu.
type Node struct {
	Name     string
	Sticky   bool
	children map[Key]*Trie
	order    []Key
}

// NewNode creates an empty node.
func NewNode(name string) *Node {
	return &Node{Name: name, children: map[Key]*Trie{}}
}

// Set binds a key, keeping first insertion order for display.
func (n *Node) Set(key Key, t *Trie) {
	if _, ok := n.children[key]; !ok {
		n.order = append(n.order, key)
	}

	n.children[key] = t
}

// Get looks up one key.
func (n *Node) Get(key Key) (*Trie, bool) {
	t, ok := n.children[key]

	return t, ok
}

// Len returns the number of bindings.
func (n *Node) Len() int {
	return len(n.children)
}

// Keys returns the bound keys in insertion order.
func (n *Node) Keys() []Key {
	return n.order
}

// Merge overlays other onto n. Leaves replace, nodes merge recursively.
func (n *Node) Merge(other *Node) {
	for _, key := range other.order {
		trie := other.children[key]
		if cur, ok := n.children[key]; ok && cur.Node != nil && trie.Node != nil {
			cur.Node.Merge(trie.Node)

			continue
		}

		n.Set(key, trie)
	}
}

// Entry is one line of an info popup: a description plus the keys that
// trigger it.
type Entry struct {
	Keys []string
	Desc string
}

// Entries returns the node contents for display.
func (n *Node) Entries(describe func(command string) string) []Entry {
	byDesc := map[string]int{}
	out := []Entry{}
	for _, key := range n.order {
		t := n.children[key]
		var desc string
		switch {
		case t.Label != "":
			desc = t.Label
		case t.Node != nil:
			desc = t.Node.Name
		case len(t.Sequence) > 0:
			desc = "[Multiple commands]"
		case t.Command == "no_op":
			continue
		default:
			desc = describe(t.Command)
		}

		if idx, ok := byDesc[desc]; ok {
			out[idx].Keys = append(out[idx].Keys, key.String())

			continue
		}

		byDesc[desc] = len(out)
		out = append(out, Entry{Keys: []string{key.String()}, Desc: desc})
	}

	for i := range out {
		slices.Sort(out[i].Keys)
	}

	return out
}

// Search descends the trie along keys.
func (t *Trie) Search(keys []Key) (*Trie, bool) {
	cur := t
	for _, key := range keys {
		if cur.Node == nil {
			return nil, false
		}

		next, ok := cur.Node.Get(key)
		if !ok {
			return nil, false
		}

		cur = next
	}

	return cur, true
}

// FromValue builds a trie from a decoded config value: a string is a
// command, a list is a sequence, a table with a command field is a named
// binding and any other table is a node.
func FromValue(v any) (*Trie, error) {
	switch val := v.(type) {
	case string:
		return &Trie{Command: val}, nil
	case []any:
		seq := make([]string, 0, len(val))
		for _, item := range val {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("command sequence must hold strings, got %T", item)
			}

			seq = append(seq, s)
		}

		return &Trie{Sequence: seq}, nil
	case map[string]any:
		if cmd, ok := val["command"]; ok {
			// A named binding: {name: ..., command: ...}.
			trie, err := FromValue(cmd)
			if err != nil {
				return nil, err
			}

			if trie.Node != nil {
				return nil, errors.New("command of a named binding must be a command or a list")
			}

			for k := range val {
				if k != "command" && k != "name" {
					return nil, fmt.Errorf("unknown field %q in a named binding", k)
				}
			}

			trie.Label, _ = val["name"].(string)

			return trie, nil
		}

		node := NewNode("")
		for _, k := range slices.Sorted(maps.Keys(val)) {
			key, err := Parse(k)
			if err != nil {
				return nil, err
			}

			child, err := FromValue(val[k])
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", k, err)
			}

			node.Set(key, child)
		}

		return &Trie{Node: node}, nil
	default:
		return nil, fmt.Errorf("expected a command, list of commands or sub-keymap, got %T", v)
	}
}

// Bindings maps each command to the key paths bound to it, such as
// "space f", in the order the trie holds them.
func (t *Trie) Bindings() map[string][]string {
	out := map[string][]string{}
	var walk func(n *Node, prefix string)
	walk = func(n *Node, prefix string) {
		for _, key := range n.order {
			child := n.children[key]
			path := key.String()
			if prefix != "" {
				path = prefix + " " + path
			}

			switch {
			case child.Node != nil:
				walk(child.Node, path)
			case child.Command != "" && child.Command != "no_op":
				out[child.Command] = append(out[child.Command], path)
			}
		}
	}
	if t != nil && t.Node != nil {
		walk(t.Node, "")
	}

	return out
}
