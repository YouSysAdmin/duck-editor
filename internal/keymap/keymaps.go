package keymap

// Mode selects which keymap is active.
type Mode int

// Editor modes.
const (
	Normal Mode = iota
	Select
	Insert
)

// String returns the config name of the mode.
func (m Mode) String() string {
	switch m {
	case Select:
		return "select"
	case Insert:
		return "insert"
	default:
		return "normal"
	}
}

// ParseMode reads a mode name from config.
func ParseMode(s string) (Mode, bool) {
	switch s {
	case "normal":
		return Normal, true
	case "select":
		return Select, true
	case "insert":
		return Insert, true
	}

	return 0, false
}

// ResultKind tells what a key lookup produced.
type ResultKind int

// Lookup outcomes.
const (
	// Pending needs more keys, Node holds the valid continuations.
	Pending ResultKind = iota

	// Matched resolved to one command.
	Matched

	// MatchedSequence resolved to several commands.
	MatchedSequence

	// NotFound means the first key is unbound in this mode.
	NotFound

	// Cancelled means a later key was invalid, Keys holds the pending
	// keys including the invalid one.
	Cancelled
)

// Result of a key lookup.
type Result struct {
	Kind     ResultKind
	Command  string
	Sequence []string
	Node     *Node
	Keys     []Key
}

// Keymaps holds the tries per mode plus the pending key state.
type Keymaps struct {
	maps    map[Mode]*Trie
	pending []Key
	sticky  *Node
}

// New wraps mode tries.
func New(maps map[Mode]*Trie) *Keymaps {
	return &Keymaps{maps: maps}
}

// Pending returns the keys waiting for disambiguation.
func (k *Keymaps) Pending() []Key {
	return k.pending
}

// Sticky returns the active sticky node, if any.
func (k *Keymaps) Sticky() *Node {
	return k.sticky
}

// Map returns the trie of a mode.
func (k *Keymaps) Map(mode Mode) *Trie {
	return k.maps[mode]
}

// ContainsKey reports whether key continues the pending sequence in mode.
func (k *Keymaps) ContainsKey(mode Mode, key Key) bool {
	trie, ok := k.maps[mode].Search(k.pending)
	if !ok || trie.Node == nil {
		return false
	}

	_, ok = trie.Node.Get(key)

	return ok
}

// Get resolves key in mode. Esc cancels pending keys, or clears the
// sticky node when nothing is pending.
func (k *Keymaps) Get(mode Mode, key Key) Result {
	km := k.maps[mode]

	if key == (Key{Code: KeyEsc}) {
		if len(k.pending) > 0 {
			keys := k.pending
			k.pending = nil

			return Result{Kind: Cancelled, Keys: keys}
		}

		k.sticky = nil
	}

	first := key
	if len(k.pending) > 0 {
		first = k.pending[0]
	}

	root := km
	if k.sticky != nil {
		root = &Trie{Node: k.sticky}
	}

	trie, ok := root.Search([]Key{first})
	switch {
	case !ok:
		return Result{Kind: NotFound}
	case trie.Node == nil && len(trie.Sequence) > 0:
		return Result{Kind: MatchedSequence, Sequence: trie.Sequence}
	case trie.Node == nil:
		return Result{Kind: Matched, Command: trie.Command}
	}

	k.pending = append(k.pending, key)
	found, ok := trie.Search(k.pending[1:])
	switch {
	case !ok:
		keys := k.pending
		k.pending = nil

		return Result{Kind: Cancelled, Keys: keys}
	case found.Node != nil:
		if found.Node.Sticky {
			k.pending = nil
			k.sticky = found.Node
		}

		return Result{Kind: Pending, Node: found.Node}
	case len(found.Sequence) > 0:
		k.pending = nil

		return Result{Kind: MatchedSequence, Sequence: found.Sequence}
	default:
		k.pending = nil

		return Result{Kind: Matched, Command: found.Command}
	}
}

// MergeUser overlays user bindings per mode onto the defaults.
func MergeUser(defaults map[Mode]*Trie, user map[Mode]*Trie) {
	for mode, trie := range user {
		if base, ok := defaults[mode]; ok && base.Node != nil && trie.Node != nil {
			base.Node.Merge(trie.Node)
		}
	}
}
