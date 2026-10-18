// Package buffer stores document text in a persistent rope and handles
// graphemes, line endings and file IO.
package buffer

import (
	"fmt"
	"iter"
	"strings"
	"unicode/utf8"
)

// Tree limits. A leaf holds at most maxLeaf bytes, concat merges small
// neighbours, and a tree deeper than maxDepth is rebalanced.
const (
	maxLeaf  = 1024
	maxDepth = 48
)

// node is an immutable rope node. Leaves carry text, branches carry two
// children. Every node caches the sizes of its subtree so that index
// conversions run in O(log n).
type node struct {
	text        string
	left, right *node
	bytes       int
	chars       int
	lines       int
	depth       int
}

func newLeaf(s string) *node {
	return &node{
		text:  s,
		bytes: len(s),
		chars: utf8.RuneCountInString(s),
		lines: strings.Count(s, "\n"),
	}
}

func newBranch(l, r *node) *node {
	return &node{
		left:  l,
		right: r,
		bytes: l.bytes + r.bytes,
		chars: l.chars + r.chars,
		lines: l.lines + r.lines,
		depth: max(l.depth, r.depth) + 1,
	}
}

func (n *node) isLeaf() bool {
	return n.left == nil
}

// Rope is a text buffer indexed by Unicode scalar values (chars), the same
// unit used for all positions. Nodes are immutable, so copying a Rope
// value is an O(1) snapshot. Mutating methods use pointer receivers and
// replace the root.
type Rope struct {
	root *node
}

// NewRope builds a rope from a string.
func NewRope(s string) Rope {
	return Rope{root: build(s)}
}

// Len returns the number of chars.
func (r Rope) Len() int {
	if r.root == nil {
		return 0
	}

	return r.root.chars
}

// LenBytes returns the number of UTF-8 bytes.
func (r Rope) LenBytes() int {
	if r.root == nil {
		return 0
	}

	return r.root.bytes
}

// LenLines returns the number of lines. An empty rope has one line and a
// trailing newline starts a new, empty line.
func (r Rope) LenLines() int {
	if r.root == nil {
		return 1
	}

	return r.root.lines + 1
}

// String materializes the whole text.
func (r Rope) String() string {
	var sb strings.Builder
	sb.Grow(r.LenBytes())
	for chunk := range r.Chunks() {
		sb.WriteString(chunk)
	}

	return sb.String()
}

// Chunks iterates over the leaf strings in document order.
func (r Rope) Chunks() iter.Seq[string] {
	return func(yield func(string) bool) {
		walkChunks(r.root, yield)
	}
}

// Equal reports whether both ropes hold the same text. Ropes sharing
// their tree compare in constant time, others chunk by chunk.
func (r Rope) Equal(o Rope) bool {
	if r.root == o.root {
		return true
	}

	if r.LenBytes() != o.LenBytes() {
		return false
	}

	next, stop := iter.Pull(o.Chunks())
	defer stop()

	var pending string
	for chunk := range r.Chunks() {
		for chunk != "" {
			if pending == "" {
				var ok bool
				if pending, ok = next(); !ok {
					return false
				}
			}

			n := min(len(chunk), len(pending))
			if chunk[:n] != pending[:n] {
				return false
			}

			chunk, pending = chunk[n:], pending[n:]
		}
	}

	return true
}

func walkChunks(n *node, yield func(string) bool) bool {
	if n == nil {
		return true
	}

	if n.isLeaf() {
		return yield(n.text)
	}

	return walkChunks(n.left, yield) && walkChunks(n.right, yield)
}

// Slice returns the text between two char indexes.
func (r Rope) Slice(start, end int) string {
	r.checkRange(start, end)
	if start == end {
		return ""
	}

	var sb strings.Builder
	sliceNode(r.root, start, end, &sb)

	return sb.String()
}

func sliceNode(n *node, start, end int, sb *strings.Builder) {
	if n == nil || start >= end {
		return
	}

	if n.isLeaf() {
		bs := n.charToByte(start)
		be := n.charToByte(end)
		sb.WriteString(n.text[bs:be])

		return
	}

	lc := n.left.chars
	if start < lc {
		sliceNode(n.left, start, min(end, lc), sb)
	}

	if end > lc {
		sliceNode(n.right, max(start-lc, 0), end-lc, sb)
	}
}

// Line returns line i including its line ending.
func (r Rope) Line(i int) string {
	return r.Slice(r.LineToChar(i), r.LineToChar(i+1))
}

// Char returns the char at index i.
func (r Rope) Char(i int) rune {
	if i < 0 || i >= r.Len() {
		panic(fmt.Sprintf("rope: char index %d out of range %d", i, r.Len()))
	}

	return r.root.charAt(i)
}

func (n *node) charAt(i int) rune {
	for !n.isLeaf() {
		if i < n.left.chars {
			n = n.left
		} else {
			i -= n.left.chars
			n = n.right
		}
	}

	ch, _ := utf8.DecodeRuneInString(n.text[n.charToByte(i):])

	return ch
}

// Insert inserts s at char index idx.
func (r *Rope) Insert(idx int, s string) {
	if idx < 0 || idx > r.Len() {
		panic(fmt.Sprintf("rope: insert index %d out of range %d", idx, r.Len()))
	}

	if s == "" {
		return
	}

	r.root = insertNode(r.root, idx, s)
}

func insertNode(n *node, idx int, s string) *node {
	if n == nil {
		return build(s)
	}

	if n.isLeaf() {
		b := n.charToByte(idx)

		return build(n.text[:b] + s + n.text[b:])
	}

	var out *node
	if idx <= n.left.chars {
		out = newBranch(insertNode(n.left, idx, s), n.right)
	} else {
		out = newBranch(n.left, insertNode(n.right, idx-n.left.chars, s))
	}

	if out.depth > maxDepth {
		out = rebuild(out)
	}

	return out
}

// Remove deletes the chars in [start, end).
func (r *Rope) Remove(start, end int) {
	r.checkRange(start, end)
	if start == end {
		return
	}

	r.root = removeNode(r.root, start, end)
}

func removeNode(n *node, start, end int) *node {
	if n == nil || start >= end {
		return n
	}

	if start <= 0 && end >= n.chars {
		return nil
	}

	if n.isLeaf() {
		bs := n.charToByte(start)
		be := n.charToByte(end)

		return build(n.text[:bs] + n.text[be:])
	}

	lc := n.left.chars
	l := removeNode(n.left, start, min(end, lc))
	rt := removeNode(n.right, max(start-lc, 0), end-lc)

	return concat(l, rt)
}

// CharToByte converts a char index to a byte offset.
func (r Rope) CharToByte(c int) int {
	r.checkIndex(c)
	if r.root == nil {
		return 0
	}

	return r.root.charToByte(c)
}

func (n *node) charToByte(c int) int {
	if n.bytes == n.chars {
		return c
	}

	if n.isLeaf() {
		i := 0
		for ; c > 0 && i < len(n.text); c-- {
			_, size := utf8.DecodeRuneInString(n.text[i:])
			i += size
		}

		return i
	}

	if c <= n.left.chars {
		return n.left.charToByte(c)
	}

	return n.left.bytes + n.right.charToByte(c-n.left.chars)
}

// ByteToChar converts a byte offset to a char index. The offset must sit
// on a char boundary.
func (r Rope) ByteToChar(b int) int {
	if b < 0 || b > r.LenBytes() {
		panic(fmt.Sprintf("rope: byte index %d out of range %d", b, r.LenBytes()))
	}

	if r.root == nil {
		return 0
	}

	return r.root.byteToChar(b)
}

func (n *node) byteToChar(b int) int {
	if n.bytes == n.chars {
		return b
	}

	if n.isLeaf() {
		return utf8.RuneCountInString(n.text[:b])
	}

	if b <= n.left.bytes {
		return n.left.byteToChar(b)
	}

	return n.left.chars + n.right.byteToChar(b-n.left.bytes)
}

// CharToLine returns the line containing char index c.
func (r Rope) CharToLine(c int) int {
	r.checkIndex(c)
	if r.root == nil {
		return 0
	}

	return r.root.charToLine(c)
}

func (n *node) charToLine(c int) int {
	if n.lines == 0 {
		return 0
	}

	if n.isLeaf() {
		return strings.Count(n.text[:n.charToByte(c)], "\n")
	}

	if c <= n.left.chars {
		return n.left.charToLine(c)
	}

	return n.left.lines + n.right.charToLine(c-n.left.chars)
}

// LineToChar returns the char index where line l starts. Passing the
// line count returns the rope length.
func (r Rope) LineToChar(l int) int {
	if l < 0 || l > r.LenLines() {
		panic(fmt.Sprintf("rope: line index %d out of range %d", l, r.LenLines()))
	}

	if r.root == nil || l == 0 {
		return 0
	}

	if l == r.LenLines() {
		return r.Len()
	}

	return r.root.lineToChar(l)
}

func (n *node) lineToChar(l int) int {
	if l == 0 {
		return 0
	}

	if n.isLeaf() {
		chars := 0
		for i := 0; i < len(n.text); {
			ch, size := utf8.DecodeRuneInString(n.text[i:])
			i += size
			chars++
			if ch == '\n' {
				l--
				if l == 0 {
					return chars
				}
			}
		}

		return n.chars
	}

	if l <= n.left.lines {
		return n.left.lineToChar(l)
	}

	return n.left.chars + n.right.lineToChar(l-n.left.lines)
}

// LineToByte returns the byte offset where line l starts.
func (r Rope) LineToByte(l int) int {
	return r.CharToByte(r.LineToChar(l))
}

func (r Rope) checkIndex(c int) {
	if c < 0 || c > r.Len() {
		panic(fmt.Sprintf("rope: index %d out of range %d", c, r.Len()))
	}
}

func (r Rope) checkRange(start, end int) {
	if start < 0 || end > r.Len() || start > end {
		panic(fmt.Sprintf("rope: range %d..%d out of range %d", start, end, r.Len()))
	}
}

// build creates a balanced subtree for s, splitting it into leaves.
func build(s string) *node {
	if s == "" {
		return nil
	}

	if len(s) <= maxLeaf {
		return newLeaf(s)
	}

	return balance(splitLeaves(s))
}

func splitLeaves(s string) []*node {
	leaves := make([]*node, 0, len(s)/maxLeaf+1)
	for len(s) > maxLeaf {
		cut := maxLeaf
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}

		// Keep CRLF inside one leaf so line scans never see a bare CR.
		if cut > 0 && s[cut-1] == '\r' && s[cut] == '\n' {
			cut--
		}

		if cut == 0 {
			_, cut = utf8.DecodeRuneInString(s)
		}

		leaves = append(leaves, newLeaf(s[:cut]))
		s = s[cut:]
	}

	return append(leaves, newLeaf(s))
}

func balance(leaves []*node) *node {
	switch len(leaves) {
	case 0:
		return nil
	case 1:
		return leaves[0]
	}

	mid := len(leaves) / 2

	return newBranch(balance(leaves[:mid]), balance(leaves[mid:]))
}

func rebuild(n *node) *node {
	leaves := make([]*node, 0, 64)
	walkChunks(n, func(s string) bool {
		leaves = append(leaves, newLeaf(s))

		return true
	})

	return balance(leaves)
}

// concat joins two subtrees, merging small leaves at the seam.
func concat(a, b *node) *node {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case a.isLeaf() && b.isLeaf() && a.bytes+b.bytes <= maxLeaf:
		return newLeaf(a.text + b.text)
	case b.isLeaf() && !a.isLeaf() && a.right.isLeaf() && a.right.bytes+b.bytes <= maxLeaf:
		return newBranch(a.left, newLeaf(a.right.text+b.text))
	case a.isLeaf() && !b.isLeaf() && b.left.isLeaf() && a.bytes+b.left.bytes <= maxLeaf:
		return newBranch(newLeaf(a.text+b.left.text), b.right)
	}

	n := newBranch(a, b)
	if n.depth > maxDepth {
		n = rebuild(n)
	}

	return n
}

// ChunkAtByte returns the leaf text containing byte offset b and the byte
// offset where that leaf starts. At the end of the rope it returns an
// empty chunk at the rope length.
func (r Rope) ChunkAtByte(b int) (string, int) {
	if r.root == nil || b >= r.LenBytes() {
		return "", r.LenBytes()
	}

	if b < 0 {
		panic(fmt.Sprintf("rope: byte index %d out of range", b))
	}

	n := r.root
	start := 0
	for !n.isLeaf() {
		if b < start+n.left.bytes {
			n = n.left
		} else {
			start += n.left.bytes
			n = n.right
		}
	}

	return n.text, start
}
