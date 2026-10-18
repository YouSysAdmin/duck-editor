//go:build cgo

package syntax

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// Supported reports whether tree-sitter is compiled in.
const Supported = true

const maxInjectionDepth = 8

// Loader resolves languages to grammars and queries, caching them.
type Loader struct {
	runtimeDirs []string
	embedded    EmbeddedGrammars

	// mu guards the fields below, parsing runs in background jobs.
	mu     sync.Mutex
	langs  *Languages
	data   map[string]*langData
	failed map[string]error

	// retired holds the data of earlier language sets still used by
	// layers.
	retired []*langData
}

type langData struct {
	lang           *Language
	grammar        *tree_sitter.Language
	highlights     *tree_sitter.Query
	highlightNames []string
	injections     *tree_sitter.Query
	injectionNames []string

	// combined is set when an injection pattern joins its matches into
	// one layer, which needs the whole tree to refresh.
	combined       bool
	textobjects    *tree_sitter.Query
	textobjectName []string
	tags           *tree_sitter.Query
	indents        *tree_sitter.Query
	rainbows       *tree_sitter.Query

	// layers counts the parsed layers using these queries.
	layers atomic.Int32
}

// NewLoader creates a loader over the runtime dirs and language config.
func NewLoader(runtimeDirs []string, langs *Languages) *Loader {
	return &Loader{
		runtimeDirs: runtimeDirs,
		langs:       langs,
		data:        map[string]*langData{},
		failed:      map[string]error{},
	}
}

// ReplaceLanguages swaps the language configuration, used by config
// reloads. Queries are loaded again, compiled grammars stay cached.
func (l *Loader) ReplaceLanguages(langs *Languages) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Queries of the old set are freed once no layer uses them, which
	// is checked now and on later reloads.
	for _, d := range l.data {
		l.retired = append(l.retired, d)
	}

	l.retired = slices.DeleteFunc(l.retired, func(d *langData) bool {
		if d.layers.Load() > 0 {
			return false
		}

		d.close()

		return true
	})
	l.langs = langs
	l.data = map[string]*langData{}
	l.failed = map[string]error{}
}

// close frees the compiled queries, the grammar stays loaded.
func (d *langData) close() {
	for _, q := range []*tree_sitter.Query{d.highlights, d.injections, d.textobjects, d.tags, d.indents, d.rainbows} {
		if q != nil {
			q.Close()
		}
	}
}

// Languages returns the language configuration.
func (l *Loader) Languages() *Languages {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.langs
}

// RuntimeDirs returns the runtime search path.
func (l *Loader) RuntimeDirs() []string {
	return l.runtimeDirs
}

// get returns the grammar and queries of a language. Loading happens
// outside the lock, so two callers may load the same language once each.
func (l *Loader) get(lang *Language) (*langData, error) {
	l.mu.Lock()
	d, ok := l.data[lang.Name]
	err := l.failed[lang.Name]
	l.mu.Unlock()
	if ok {
		return d, nil
	}

	if err != nil {
		return nil, err
	}

	d, err = l.load(lang)
	l.mu.Lock()
	defer l.mu.Unlock()

	if err != nil {
		l.failed[lang.Name] = err

		return nil, err
	}

	if cached, ok := l.data[lang.Name]; ok {
		// Another caller loaded it first, this copy is not needed.
		d.close()

		return cached, nil
	}

	l.data[lang.Name] = d

	return d, nil
}

func (l *Loader) load(lang *Language) (*langData, error) {
	grammar, err := loadGrammar(l.runtimeDirs, lang.Grammar, l.embedded)
	if err != nil {
		return nil, err
	}

	d := &langData{lang: lang, grammar: grammar}
	if src := ReadQuery(l.runtimeDirs, lang.Name, "highlights.scm"); src != "" {
		q, qerr := tree_sitter.NewQuery(grammar, src)
		if qerr != nil {
			return nil, fmt.Errorf("highlights query for %s: %s", lang.Name, qerr.Message)
		}

		d.highlights = q
		d.highlightNames = q.CaptureNames()
	}

	if src := ReadQuery(l.runtimeDirs, lang.Name, "injections.scm"); src != "" {
		if q, qerr := tree_sitter.NewQuery(grammar, src); qerr == nil {
			d.injections = q
			d.injectionNames = q.CaptureNames()
			for i := range q.PatternCount() {
				for _, prop := range q.PropertySettings(i) {
					if prop.Key == "injection.combined" {
						d.combined = true
					}
				}
			}
		}
	}

	if src := ReadQuery(l.runtimeDirs, lang.Name, "textobjects.scm"); src != "" {
		if q, qerr := tree_sitter.NewQuery(grammar, src); qerr == nil {
			d.textobjects = q
			d.textobjectName = q.CaptureNames()
		}
	}

	if src := ReadQuery(l.runtimeDirs, lang.Name, "tags.scm"); src != "" {
		if q, qerr := tree_sitter.NewQuery(grammar, src); qerr == nil {
			d.tags = q
		}
	}

	if src := ReadQuery(l.runtimeDirs, lang.Name, "indents.scm"); src != "" {
		if q, qerr := tree_sitter.NewQuery(grammar, src); qerr == nil {
			d.indents = q
		}
	}

	if src := ReadQuery(l.runtimeDirs, lang.Name, "rainbows.scm"); src != "" {
		if q, qerr := tree_sitter.NewQuery(grammar, src); qerr == nil {
			d.rainbows = q
		}
	}

	return d, nil
}

// languageForInjection resolves an injection language name through the
// language names and injection regexes.
func (l *Loader) languageForInjection(name string) *Language {
	langs := l.Languages()
	if lang, ok := langs.Get(name); ok {
		return lang
	}

	for _, lang := range langs.ordered {
		if lang.InjectionRegex != nil && lang.InjectionRegex.MatchString(name) {
			return lang
		}
	}

	return nil
}

// layer is one parsed language region: the root document or an injection.
type layer struct {
	data     *langData
	parser   *tree_sitter.Parser
	tree     *tree_sitter.Tree
	ranges   []tree_sitter.Range
	depth    int
	children []*layer

	// pattern is the injection pattern that made the layer, it orders
	// layers over the same text.
	pattern uint

	// dirty marks a layer whose ranges were touched by an edit.
	dirty bool
}

func (ly *layer) close() {
	for _, c := range ly.children {
		c.close()
	}

	if ly.tree != nil {
		ly.tree.Close()
	}

	ly.parser.Close()
	ly.data.layers.Add(-1)
}

// intersects reports whether the layer covers any byte of [start, end].
// A child layer lies inside its parent, so a miss skips its subtree too.
func (ly *layer) intersects(start, end int) bool {
	if ly.ranges == nil {
		return true
	}

	for _, r := range ly.ranges {
		if int(r.StartByte) <= end && start <= int(r.EndByte) {
			return true
		}
	}

	return false
}

func (ly *layer) containsByte(b int) bool {
	if ly.ranges == nil {
		return true
	}

	for _, r := range ly.ranges {
		if int(r.StartByte) <= b && b <= int(r.EndByte) {
			return true
		}
	}

	return false
}

// Syntax is the parsed tree of one document with its injection layers.
type Syntax struct {
	loader     *Loader
	root       *layer
	text       buffer.Rope
	textBytes  []byte
	bytesValid bool
	cursor     *tree_sitter.QueryCursor
}

// Highlight is one styled byte range with the capture scope that
// produced it. Ranges are ordered so that later entries take precedence.
type Highlight struct {
	Start int
	End   int
	Scope string
}

// New parses text as lang and resolves its injections.
func New(loader *Loader, lang *Language, text buffer.Rope) (*Syntax, error) {
	data, err := loader.get(lang)
	if err != nil {
		return nil, err
	}

	if data.highlights == nil {
		return nil, fmt.Errorf("no highlights query for %s", lang.Name)
	}

	s := &Syntax{loader: loader, text: text, cursor: tree_sitter.NewQueryCursor()}
	s.root, err = s.newLayer(data, nil, 0)
	if err != nil {
		return nil, err
	}

	s.parseLayer(s.root, nil)
	s.buildInjections(s.root)

	return s, nil
}

func (s *Syntax) newLayer(data *langData, ranges []tree_sitter.Range, depth int) (*layer, error) {
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(data.grammar); err != nil {
		parser.Close()

		return nil, fmt.Errorf("grammar %s: %w", data.lang.Grammar, err)
	}

	if ranges != nil {
		if err := parser.SetIncludedRanges(ranges); err != nil {
			parser.Close()

			return nil, err
		}
	}

	data.layers.Add(1)

	return &layer{data: data, parser: parser, ranges: ranges, depth: depth}, nil
}

// Language returns the root language of the document.
func (s *Syntax) Language() *Language {
	return s.root.data.lang
}

// Close frees the tree-sitter resources.
func (s *Syntax) Close() {
	s.root.close()
	s.cursor.Close()
}

func (s *Syntax) readFn() func(int, tree_sitter.Point) []byte {
	text := s.text

	return func(offset int, _ tree_sitter.Point) []byte {
		chunk, start := text.ChunkAtByte(offset)
		if chunk == "" {
			return nil
		}

		rest := chunk[offset-start:]

		return unsafe.Slice(unsafe.StringData(rest), len(rest))
	}
}

func (s *Syntax) parseLayer(ly *layer, old *tree_sitter.Tree) {
	if ly.ranges != nil {
		// Ranges may have shifted since the parser was configured.
		_ = ly.parser.SetIncludedRanges(ly.ranges)
	}

	tree := ly.parser.ParseWithOptions(s.readFn(), old, nil)
	if old != nil {
		old.Close()
	}

	ly.tree = tree
}

// patchBytes keeps the byte copy of the text current through edits, a
// copy of the whole rope on every keystroke costs more on large files.
func (s *Syntax) patchBytes(edits []tree_sitter.InputEdit) {
	if !s.bytesValid {
		return
	}

	for _, e := range edits {
		from := s.text.ByteToChar(int(e.StartByte))
		to := s.text.ByteToChar(int(e.NewEndByte))
		s.textBytes = slices.Replace(s.textBytes, int(e.StartByte), int(e.OldEndByte), []byte(s.text.Slice(from, to))...)
	}
}

func (s *Syntax) bytes() []byte {
	if !s.bytesValid {
		s.textBytes = []byte(s.text.String())
		s.bytesValid = true
	}

	return s.textBytes
}

// Update applies the edits of changes, which turned oldText into
// newText, reparses the root incrementally and refreshes the injections
// in the part of the tree that changed, reusing the other child layers.
func (s *Syntax) Update(oldText, newText buffer.Rope, changes *edit.ChangeSet) {
	edits := inputEdits(oldText, newText, changes)
	s.text = newText
	s.patchBytes(edits)
	s.root.applyEdits(edits)
	changed, ok := s.reparse(s.root)
	if !ok {
		s.refreshInjections(s.root, nil)

		return
	}

	// The edits and the syntax changes they caused, in new coordinates.
	// Each stays its own span, an edit at the start and one at the end
	// must not refresh everything between them.
	var spans []byteSpan
	for _, e := range edits {
		spans = append(spans, byteSpan{int(e.StartByte), int(e.NewEndByte)})
	}

	for _, r := range changed {
		spans = append(spans, byteSpan{int(r.StartByte), int(r.EndByte)})
	}

	if spans = mergeSpans(spans); len(spans) > 0 {
		s.refreshInjections(s.root, spans)
	}
}

// byteSpan is the bytes lo to hi of the text, both included.
type byteSpan struct{ lo, hi int }

// mergeSpans sorts spans and joins the ones that touch.
func mergeSpans(spans []byteSpan) []byteSpan {
	slices.SortFunc(spans, func(a, b byteSpan) int { return cmp.Compare(a.lo, b.lo) })
	var out []byteSpan
	for _, sp := range spans {
		if n := len(out); n > 0 && sp.lo <= out[n-1].hi {
			out[n-1].hi = max(out[n-1].hi, sp.hi)

			continue
		}

		out = append(out, sp)
	}

	return out
}

// reparse parses a layer again from its edited tree and returns the
// byte ranges whose syntax changed. ok is false without an old tree to
// compare with.
func (s *Syntax) reparse(ly *layer) ([]tree_sitter.Range, bool) {
	old := ly.tree
	if ly.ranges != nil {
		_ = ly.parser.SetIncludedRanges(ly.ranges)
	}

	ly.tree = ly.parser.ParseWithOptions(s.readFn(), old, nil)
	if old == nil {
		return nil, false
	}

	defer old.Close()

	if ly.tree == nil {
		return nil, false
	}

	return ly.tree.ChangedRanges(old), true
}

// applyEdits shifts the trees and ranges of a layer subtree and marks
// layers touched by an edit as dirty.
func (ly *layer) applyEdits(edits []tree_sitter.InputEdit) {
	if ly.tree != nil {
		for i := range edits {
			ly.tree.Edit(&edits[i])
		}
	}

	for i := range ly.ranges {
		r := &ly.ranges[i]
		for _, e := range edits {
			if e.StartByte <= r.EndByte && e.OldEndByte >= r.StartByte {
				ly.dirty = true
			}

			r.StartByte, r.StartPoint = shiftPos(r.StartByte, r.StartPoint, e, true)
			r.EndByte, r.EndPoint = shiftPos(r.EndByte, r.EndPoint, e, false)
		}
	}

	for _, c := range ly.children {
		c.applyEdits(edits)
	}
}

// shiftPos maps a byte offset and its point through an edit. Offsets
// inside the edited span snap to its start or new end.
func shiftPos(b uint, p tree_sitter.Point, e tree_sitter.InputEdit, isStart bool) (uint, tree_sitter.Point) {
	switch {
	case b < e.StartByte:
		return b, p
	case b >= e.OldEndByte:
		if p.Row == e.OldEndPosition.Row {
			p.Column = p.Column - e.OldEndPosition.Column + e.NewEndPosition.Column
		}

		p.Row = p.Row - e.OldEndPosition.Row + e.NewEndPosition.Row

		return b - e.OldEndByte + e.NewEndByte, p
	case isStart:
		return e.StartByte, e.StartPosition
	default:
		return e.NewEndByte, e.NewEndPosition
	}
}

func rangesEqual(a, b []tree_sitter.Range) bool {
	return slices.EqualFunc(a, b, func(x, y tree_sitter.Range) bool {
		return x.StartByte == y.StartByte && x.EndByte == y.EndByte
	})
}

// refreshInjections recomputes the injections of a reparsed layer within
// the spans, the whole layer when spans is nil. Child layers with
// unchanged language and ranges are kept, dirty ones are reparsed
// incrementally, the rest are rebuilt.
func (s *Syntax) refreshInjections(ly *layer, spans []byteSpan) {
	if ly.data.combined {
		spans = nil
	}

	// Old children by language and first range, a file with thousands of
	// comment layers must not compare every pair.
	type key struct {
		lang       *Language
		start, end uint
	}
	keyOf := func(lang *Language, ranges []tree_sitter.Range) key {
		return key{lang, ranges[0].StartByte, ranges[0].EndByte}
	}
	old := map[key][]*layer{}
	for _, c := range ly.children {
		k := keyOf(c.data.lang, c.ranges)
		old[k] = append(old[k], c)
	}

	children := make([]*layer, 0, len(ly.children))
	used := map[*layer]bool{}
	for _, inj := range s.collectInjections(ly, spans) {
		var reused *layer
		for _, c := range old[keyOf(inj.lang, inj.ranges)] {
			if !used[c] && rangesEqual(c.ranges, inj.ranges) {
				reused = c

				break
			}
		}

		if reused == nil {
			if child := s.createLayer(ly, inj); child != nil {
				children = append(children, child)
			}

			continue
		}

		used[reused] = true
		if reused.dirty {
			reused.dirty = false
			s.reparse(reused)
			s.refreshInjections(reused, nil)
		}

		children = append(children, reused)
	}

	// Children outside the refreshed bytes stay as they are.
	for _, c := range ly.children {
		switch {
		case used[c]:
		case spans == nil || c.dirty || slices.ContainsFunc(spans, func(sp byteSpan) bool { return c.intersects(sp.lo, sp.hi) }):
			c.close()
		default:
			children = append(children, c)
		}
	}

	sortLayers(children)
	ly.children = children
}

type injection struct {
	lang    *Language
	ranges  []tree_sitter.Range
	pattern uint
}

// sortLayers orders child layers by their start and then by the pattern
// that made them, the same order whether built at once or refreshed.
func sortLayers(layers []*layer) {
	slices.SortStableFunc(layers, func(a, b *layer) int {
		return cmp.Or(cmp.Compare(a.ranges[0].StartByte, b.ranges[0].StartByte), cmp.Compare(a.pattern, b.pattern))
	})
}

// buildInjections parses every injected region of a layer as a child
// layer, recursively.
func (s *Syntax) buildInjections(ly *layer) {
	for _, inj := range s.collectInjections(ly, nil) {
		if child := s.createLayer(ly, inj); child != nil {
			ly.children = append(ly.children, child)
		}
	}

	sortLayers(ly.children)
}

func (s *Syntax) createLayer(parent *layer, inj injection) *layer {
	data, err := s.loader.get(inj.lang)
	if err != nil || data.highlights == nil {
		return nil
	}

	child, err := s.newLayer(data, inj.ranges, parent.depth+1)
	if err != nil {
		return nil
	}

	child.pattern = inj.pattern
	s.parseLayer(child, nil)
	s.buildInjections(child)

	return child
}

// collectInjections runs the injection query of a layer within each of
// the spans, over the whole tree when spans is nil, and returns the
// injected languages with their ranges. A match reaching into two spans
// counts once.
func (s *Syntax) collectInjections(ly *layer, spans []byteSpan) []injection {
	if spans == nil {
		return s.collectInjectionsIn(ly, -1, -1)
	}

	var out []injection
	seen := map[injectionKey]bool{}
	for _, sp := range spans {
		for _, inj := range s.collectInjectionsIn(ly, sp.lo, sp.hi) {
			k := keyOfInjection(inj)
			if !seen[k] {
				seen[k] = true
				out = append(out, inj)
			}
		}
	}

	return out
}

// injectionKey identifies an injection match across overlapping spans.
type injectionKey struct {
	lang       *Language
	pattern    uint
	start, end uint
	count      int
}

func keyOfInjection(inj injection) injectionKey {
	return injectionKey{inj.lang, inj.pattern, inj.ranges[0].StartByte, inj.ranges[0].EndByte, len(inj.ranges)}
}

// collectInjectionsIn runs the injection query within the bytes lo to
// hi, the whole tree when lo is negative.
func (s *Syntax) collectInjectionsIn(ly *layer, lo, hi int) []injection {
	if ly.data.injections == nil || ly.depth >= maxInjectionDepth || ly.tree == nil {
		return nil
	}

	query := ly.data.injections
	contentIdx, hasContent := query.CaptureIndexForName("injection.content")
	if !hasContent {
		return nil
	}

	languageIdx, hasLanguage := query.CaptureIndexForName("injection.language")

	var injections []injection
	combined := map[string]int{}

	text := s.bytes()
	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()
	if lo >= 0 {
		// The spans include both ends and tree-sitter leaves out a node
		// that only touches the range, so it is widened by a byte.
		cursor.SetByteRange(uint(max(lo-1, 0)), uint(hi+1))
	}

	matches := cursor.Matches(query, ly.tree.RootNode(), text)
	for {
		match := matches.Next()
		if match == nil {
			break
		}

		var langName string
		includeChildren := false
		includeUnnamed := false
		isCombined := false
		for _, prop := range query.PropertySettings(match.PatternIndex) {
			switch prop.Key {
			case "injection.language":
				if prop.Value != nil {
					langName = *prop.Value
				}
			case "injection.include-children":
				includeChildren = true
			case "injection.include-unnamed-children":
				includeUnnamed = true
			case "injection.combined":
				isCombined = true
			}
		}

		if langName == "" && hasLanguage {
			for _, node := range match.NodesForCaptureIndex(languageIdx) {
				langName = node.Utf8Text(text)
			}
		}

		if langName == "" {
			continue
		}

		lang := s.loader.languageForInjection(langName)
		if lang == nil {
			continue
		}

		var ranges []tree_sitter.Range
		for _, node := range match.NodesForCaptureIndex(contentIdx) {
			ranges = append(ranges, contentRanges(&node, includeChildren, includeUnnamed)...)
		}

		if len(ranges) == 0 {
			continue
		}

		if isCombined {
			if idx, ok := combined[lang.Name]; ok {
				injections[idx].ranges = append(injections[idx].ranges, ranges...)

				continue
			}

			combined[lang.Name] = len(injections)
		}

		injections = append(injections, injection{lang: lang, ranges: ranges, pattern: match.PatternIndex})
	}

	return injections
}

// contentRanges returns the byte ranges of an injection content node.
// Without include-children the ranges of its child nodes are cut out.
func contentRanges(node *tree_sitter.Node, includeChildren, includeUnnamed bool) []tree_sitter.Range {
	if includeChildren {
		return []tree_sitter.Range{node.Range()}
	}

	var out []tree_sitter.Range
	cursor := node.Walk()
	defer cursor.Close()
	start := node.StartByte()
	startPoint := node.StartPosition()
	for _, child := range node.Children(cursor) {
		if includeUnnamed && !child.IsNamed() {
			continue
		}

		if child.StartByte() > start {
			out = append(out, tree_sitter.Range{
				StartByte: start, EndByte: child.StartByte(),
				StartPoint: startPoint, EndPoint: child.StartPosition(),
			})
		}

		start = child.EndByte()
		startPoint = child.EndPosition()
	}

	if node.EndByte() > start {
		out = append(out, tree_sitter.Range{
			StartByte: start, EndByte: node.EndByte(),
			StartPoint: startPoint, EndPoint: node.EndPosition(),
		})
	}

	return out
}

// inputEdits converts a changeset into tree-sitter edits, in order and
// each in the coordinates the earlier ones left, see makeEdit.
func inputEdits(oldText, newText buffer.Rope, changes *edit.ChangeSet) []tree_sitter.InputEdit {
	var edits []tree_sitter.InputEdit
	oldPos := 0
	newPos := 0
	ops := changes.Ops()
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		switch op.Kind {
		case edit.OpRetain:
			oldPos += op.N
			newPos += op.N
		case edit.OpDelete:
			edits = append(edits, makeEdit(oldText, newText, oldPos, oldPos+op.N, newPos, newPos))
			oldPos += op.N
		case edit.OpInsert:
			oldEnd := oldPos
			if i+1 < len(ops) && ops[i+1].Kind == edit.OpDelete {
				oldEnd = oldPos + ops[i+1].N
				i++
			}

			edits = append(edits, makeEdit(oldText, newText, oldPos, oldEnd, newPos, newPos+op.N))
			oldPos = oldEnd
			newPos += op.N
		}
	}

	return edits
}

// makeEdit describes one change for tree-sitter, which applies edits in
// sequence: the start is where the text before it already took the
// earlier edits, the new end is in the new text.
func makeEdit(oldText, newText buffer.Rope, oldStart, oldEnd, newStart, newEnd int) tree_sitter.InputEdit {
	startByte := uint(newText.CharToByte(newStart))
	start := point(newText, newStart)
	removedFrom, removedTo := point(oldText, oldStart), point(oldText, oldEnd)
	removed := uint(oldText.CharToByte(oldEnd) - oldText.CharToByte(oldStart))
	oldEndPos := tree_sitter.Point{Row: start.Row + removedTo.Row - removedFrom.Row, Column: removedTo.Column}
	if removedTo.Row == removedFrom.Row {
		oldEndPos.Column = start.Column + removed
	}

	return tree_sitter.InputEdit{
		StartByte:      startByte,
		OldEndByte:     startByte + removed,
		NewEndByte:     uint(newText.CharToByte(newEnd)),
		StartPosition:  start,
		OldEndPosition: oldEndPos,
		NewEndPosition: point(newText, newEnd),
	}
}

func point(text buffer.Rope, charIdx int) tree_sitter.Point {
	line := text.CharToLine(charIdx)

	return tree_sitter.Point{
		Row:    uint(line),
		Column: uint(text.CharToByte(charIdx) - text.LineToByte(line)),
	}
}

// Highlights returns the captures of all layers intersecting the byte
// range, ordered so that painting them in sequence gives nested, deeper
// and later patterns precedence.
func (s *Syntax) Highlights(startByte, endByte int) []Highlight {
	// tree-sitter reads an end of zero as the end of the file.
	if startByte >= endByte {
		return nil
	}

	type entry struct {
		Highlight
		depth   int
		pattern uint
	}
	var out []entry
	text := s.bytes()

	var visit func(ly *layer)
	visit = func(ly *layer) {
		if !ly.intersects(startByte, endByte) {
			return
		}

		if ly.tree != nil && ly.data.highlights != nil {
			s.cursor.SetByteRange(uint(startByte), uint(endByte))
			captures := s.cursor.Captures(ly.data.highlights, ly.tree.RootNode(), text)
			for {
				match, idx := captures.Next()
				if match == nil {
					break
				}

				capture := match.Captures[idx]
				// Captures named with a leading underscore only feed predicates.
				if strings.HasPrefix(ly.data.highlightNames[capture.Index], "_") {
					continue
				}

				out = append(out, entry{
					Highlight: Highlight{
						Start: int(capture.Node.StartByte()),
						End:   int(capture.Node.EndByte()),
						Scope: ly.data.highlightNames[capture.Index],
					},
					depth:   ly.depth,
					pattern: match.PatternIndex,
				})
			}
		}

		for _, c := range ly.children {
			visit(c)
		}
	}
	visit(s.root)

	slices.SortStableFunc(out, func(a, b entry) int {
		return cmp.Or(
			cmp.Compare(a.Start, b.Start),
			cmp.Compare(b.End, a.End),
			cmp.Compare(a.depth, b.depth),
			cmp.Compare(a.pattern, b.pattern),
		)
	})

	result := make([]Highlight, len(out))
	for i, e := range out {
		result[i] = e.Highlight
	}

	return result
}

// layerFor returns the deepest layer covering the byte range.
func (s *Syntax) layerFor(startByte, endByte int) *layer {
	best := s.root
	var walk func(ly *layer)
	walk = func(ly *layer) {
		for _, c := range ly.children {
			if c.containsByte(startByte) && c.containsByte(endByte) && c.tree != nil {
				best = c
				walk(c)
			}
		}
	}
	walk(s.root)

	return best
}

// textObjectLayer is the deepest layer at a byte that has a textobjects
// query. An injected comment or string has none, objects around it come
// from the code it sits in.
func (s *Syntax) textObjectLayer(b int) *layer {
	best := s.root
	var walk func(ly *layer)
	walk = func(ly *layer) {
		for _, c := range ly.children {
			if c.containsByte(b) && c.tree != nil {
				if c.data.textobjects != nil {
					best = c
				}

				walk(c)
			}
		}
	}
	walk(s.root)

	return best
}

// MatchingBracket finds the bracket matching the one at pos using the
// syntax tree. With fuzzy set, a cursor inside a pair resolves to the
// closing bracket of the enclosing pair.
func (s *Syntax) MatchingBracket(text buffer.Rope, pos int, fuzzy bool) (int, bool) {
	if pos >= text.Len() {
		return 0, false
	}

	if !fuzzy && !edit.IsValidPair(text.Char(pos)) {
		return 0, false
	}

	bytePos := uint(text.CharToByte(pos))
	ly := s.layerFor(int(bytePos), int(bytePos))
	if ly.tree == nil {
		return 0, false
	}

	root := ly.tree.RootNode()
	node := root.DescendantForByteRange(bytePos, bytePos)
	if node == nil {
		return 0, false
	}

	asChar := func(n *tree_sitter.Node) (int, rune, bool) {
		if n == nil || n.EndByte()-n.StartByte() != 1 {
			return 0, 0, false
		}

		p := text.ByteToChar(int(n.StartByte()))

		return p, text.Char(p), true
	}

	const matchLimit = 16
	findPairEnd := func(n *tree_sitter.Node, startChar, endChar rune, forward bool) (int, bool) {
		depth := 0
		for i := 0; n != nil && i < matchLimit; i++ {
			if p, c, ok := asChar(n); ok {
				switch c {
				case endChar:
					if depth == 0 {
						return p, true
					}

					depth--
				case startChar:
					depth++
				}
			}

			if forward {
				n = n.NextSibling()
			} else {
				n = n.PrevSibling()
			}
		}

		return 0, false
	}

	for {
		if node.IsNamed() && node.ChildCount() >= 2 {
			first := node.Child(0)
			last := node.Child(node.ChildCount() - 1)
			if sp, oc, ok1 := asChar(first); ok1 {
				if ep, cc, ok2 := asChar(last); ok2 && edit.IsPair(oc, cc) && sp <= pos && pos <= ep {
					if ep == pos {
						return sp, true
					}

					if fuzzy || sp == pos {
						return ep, true
					}
				}
			}
		}

		if _, c, ok := asChar(node); ok {
			if o, cl := edit.GetPair(c); cl == c && edit.IsPair(o, c) {
				if p, found := findPairEnd(node.PrevSibling(), c, o, false); found {
					return p, true
				}
			}

			if o, cl := edit.GetPair(c); o == c && edit.IsPair(c, cl) {
				if p, found := findPairEnd(node.NextSibling(), c, cl, true); found {
					return p, true
				}
			}
		}

		if fuzzy {
			sib := node.NextSibling()
			for i := 0; sib != nil && i < matchLimit; i++ {
				if _, c, ok := asChar(sib); ok {
					if o, cl := edit.GetPair(c); cl == c && edit.IsPair(o, c) {
						if _, found := findPairEnd(sib.PrevSibling(), c, o, false); found {
							return text.ByteToChar(int(sib.StartByte())), true
						}
					}
				}

				sib = sib.NextSibling()
			}
		} else if node.IsNamed() {
			break
		}

		parent := node.Parent()
		if parent == nil {
			break
		}

		node = parent
	}

	leaf := root.NamedDescendantForByteRange(bytePos, bytePos+1)
	if leaf == nil || leaf.ChildCount() != 0 {
		return 0, false
	}

	nodeStart := text.ByteToChar(int(leaf.StartByte()))
	nodeEnd := text.ByteToChar(int(leaf.EndByte()))
	sub := buffer.NewRope(text.Slice(nodeStart, nodeEnd))
	if p, ok := edit.FindMatchingBracketPlaintext(sub, pos-nodeStart); ok {
		return p + nodeStart, true
	}

	return 0, false
}

// nodeForRange returns the smallest node covering a char range, nil
// when the layer has no tree.
func (s *Syntax) nodeForRange(text buffer.Rope, r edit.Range) *tree_sitter.Node {
	from := text.CharToByte(r.From())
	to := text.CharToByte(r.To())
	ly := s.layerFor(from, to)
	if ly.tree == nil {
		return nil
	}

	return ly.tree.RootNode().DescendantForByteRange(uint(from), uint(to))
}

func nodeRange(text buffer.Rope, n *tree_sitter.Node, dir edit.Direction) edit.Range {
	from := text.ByteToChar(int(n.StartByte()))
	to := text.ByteToChar(int(n.EndByte()))

	return edit.NewRange(from, to).WithDirection(dir)
}

// ExpandSelection grows every range to the parent syntax node.
func (s *Syntax) ExpandSelection(text buffer.Rope, sel edit.Selection) edit.Selection {
	return sel.Transform(func(r edit.Range) edit.Range {
		node := s.nodeForRange(text, r)
		if node == nil {
			return r
		}

		from := uint(text.CharToByte(r.From()))
		to := uint(text.CharToByte(r.To()))
		for node.StartByte() == from && node.EndByte() == to {
			parent := node.Parent()
			if parent == nil {
				break
			}

			node = parent
		}

		return nodeRange(text, node, r.Direction())
	})
}

// ShrinkSelection moves every range to the first child node.
func (s *Syntax) ShrinkSelection(text buffer.Rope, sel edit.Selection) edit.Selection {
	return sel.Transform(func(r edit.Range) edit.Range {
		node := s.nodeForRange(text, r)
		if node == nil {
			return r
		}

		if child := node.Child(0); child != nil {
			node = child
		}

		return nodeRange(text, node, r.Direction())
	})
}

// SelectSibling moves every range to the next or previous sibling node,
// climbing to the parent when there is none.
func (s *Syntax) SelectSibling(text buffer.Rope, sel edit.Selection, dir edit.Direction) edit.Selection {
	return sel.Transform(func(r edit.Range) edit.Range {
		node := s.nodeForRange(text, r)
		if node == nil {
			return r
		}

		cur := node
		for {
			var sib *tree_sitter.Node
			if dir == edit.Forward {
				sib = cur.NextSibling()
			} else {
				sib = cur.PrevSibling()
			}

			if sib != nil {
				cur = sib

				break
			}

			parent := cur.Parent()
			if parent == nil {
				break
			}

			cur = parent
		}

		return nodeRange(text, cur, dir)
	})
}

// SelectAllSiblings replaces every range with the named children of its
// parent node.
func (s *Syntax) SelectAllSiblings(text buffer.Rope, sel edit.Selection) edit.Selection {
	return sel.TransformIter(func(r edit.Range) []edit.Range {
		node := s.nodeForRange(text, r)
		if node == nil {
			return []edit.Range{r}
		}

		parent := node.Parent()
		for parent != nil && parent.ChildCount() <= 1 {
			parent = parent.Parent()
		}

		if parent == nil {
			return []edit.Range{r}
		}

		return namedChildren(text, parent, r)
	})
}

// SelectAllChildren replaces every range with the named children of its
// node.
func (s *Syntax) SelectAllChildren(text buffer.Rope, sel edit.Selection) edit.Selection {
	return sel.TransformIter(func(r edit.Range) []edit.Range {
		node := s.nodeForRange(text, r)
		if node == nil {
			return []edit.Range{r}
		}

		return namedChildren(text, node, r)
	})
}

func namedChildren(text buffer.Rope, node *tree_sitter.Node, r edit.Range) []edit.Range {
	cursor := node.Walk()
	defer cursor.Close()
	out := []edit.Range{}
	for _, child := range node.NamedChildren(cursor) {
		out = append(out, nodeRange(text, &child, r.Direction()))
	}

	if len(out) == 0 {
		return []edit.Range{r}
	}

	return out
}

// MoveParentNodeEnd moves to the end or start of the enclosing node.
func (s *Syntax) MoveParentNodeEnd(text buffer.Rope, sel edit.Selection, dir edit.Direction, extend bool) edit.Selection {
	return sel.Transform(func(r edit.Range) edit.Range {
		from := text.CharToByte(r.From())
		to := text.CharToByte(r.To())
		ly := s.layerFor(from, to)
		if ly.tree == nil {
			return r
		}

		node := ly.tree.RootNode().NamedDescendantForByteRange(uint(from), uint(to))
		if node == nil {
			return r
		}

		var endHead int
		if dir == edit.Forward {
			endHead = text.ByteToChar(int(node.EndByte()))
		} else {
			endHead = text.ByteToChar(int(node.StartByte()))
			if endHead == r.Cursor(text) {
				start := node.StartByte()
				for node.StartByte() >= start || !node.IsNamed() {
					parent := node.Parent()
					if parent == nil {
						break
					}

					node = parent
				}

				endHead = text.ByteToChar(int(node.StartByte()))
			}
		}

		if !extend {
			// A node ending at the end of the text leaves no char after it.
			endHead = min(endHead, max(text.Len()-1, 0))
			end := min(endHead+1, text.Len())
			if r.Direction() == edit.Forward {
				return edit.NewRange(endHead, end)
			}

			return edit.NewRange(end, endHead)
		}

		if endHead >= r.Anchor {
			endHead++
		}

		return edit.NewRange(r.Anchor, min(endHead, text.Len()))
	})
}

// captureRanges returns the byte ranges of all nodes captured under any
// of the names, merging quantified captures of one match into one range.
func (s *Syntax) captureRanges(ly *layer, names []string) [][2]int {
	q := ly.data.textobjects
	if q == nil || ly.tree == nil {
		return nil
	}

	var indexes []uint
	for _, name := range names {
		if idx, ok := q.CaptureIndexForName(name); ok {
			indexes = append(indexes, idx)
		}
	}

	if len(indexes) == 0 {
		return nil
	}

	var out [][2]int
	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()
	matches := cursor.Matches(q, ly.tree.RootNode(), s.bytes())
	for {
		match := matches.Next()
		if match == nil {
			break
		}

		for _, idx := range indexes {
			nodes := match.NodesForCaptureIndex(idx)
			if len(nodes) == 0 {
				continue
			}

			start, end := int(nodes[0].StartByte()), int(nodes[0].EndByte())
			for _, n := range nodes[1:] {
				start = min(start, int(n.StartByte()))
				end = max(end, int(n.EndByte()))
			}

			out = append(out, [2]int{start, end})
		}
	}

	return out
}

// TextObject selects the smallest object of the given name around the
// cursor, such as "function" with kind inside or around.
func (s *Syntax) TextObject(text buffer.Rope, r edit.Range, kind edit.TextObjectKind, name string) (edit.Range, bool) {
	bytePos := text.CharToByte(r.Cursor(text))
	ly := s.textObjectLayer(bytePos)
	var best *[2]int
	for _, cr := range s.captureRanges(ly, []string{name + "." + kind.String()}) {
		if cr[0] > bytePos || bytePos >= cr[1] {
			continue
		}

		if best == nil || cr[1]-cr[0] < best[1]-best[0] {
			c := cr
			best = &c
		}
	}

	if best == nil || best[0] >= text.LenBytes() || best[1] > text.LenBytes() {
		return r, false
	}

	return edit.NewRange(text.ByteToChar(best[0]), text.ByteToChar(best[1])), true
}

// GotoTextObject moves to the next or previous object of the given name.
func (s *Syntax) GotoTextObject(text buffer.Rope, r edit.Range, name string, dir edit.Direction, count int) edit.Range {
	names := []string{name + ".movement", name + ".around", name + ".inside"}
	last := r
	for range count {
		bytePos := text.CharToByte(last.Cursor(text))
		ly := s.textObjectLayer(bytePos)
		var best *[2]int
		for _, cr := range s.captureRanges(ly, names) {
			if dir == edit.Forward {
				if cr[0] <= bytePos {
					continue
				}

				if best == nil || cr[0] < best[0] || (cr[0] == best[0] && cr[1] > best[1]) {
					c := cr
					best = &c
				}
			} else {
				if cr[1] >= bytePos {
					continue
				}

				if best == nil || cr[1] > best[1] || (cr[1] == best[1] && cr[0] < best[0]) {
					c := cr
					best = &c
				}
			}
		}

		if best == nil || best[0] >= text.LenBytes() || best[1] > text.LenBytes() {
			break
		}

		next := edit.NewRange(text.ByteToChar(best[0]), text.ByteToChar(best[1]))
		if next == last {
			break
		}

		last = next
	}

	return last
}

// Tag is a symbol definition found by the tags query.
type Tag struct {
	Kind  string
	Name  string
	Start int
	End   int
}

// tagKinds are the definition captures known in tags.scm.
var tagKinds = map[string]bool{
	"class": true, "constant": true, "enum": true, "field": true, "function": true,
	"interface": true, "macro": true, "module": true, "section": true, "struct": true, "type": true,
}

// Tags lists the symbol definitions of every layer in document order.
func (s *Syntax) Tags(text buffer.Rope) []Tag {
	var out []Tag
	src := s.bytes()
	var visit func(ly *layer)
	visit = func(ly *layer) {
		if ly.tree != nil && ly.data.tags != nil {
			q := ly.data.tags
			nameIdx, hasName := q.CaptureIndexForName("name")
			names := q.CaptureNames()
			cursor := tree_sitter.NewQueryCursor()
			matches := cursor.Matches(q, ly.tree.RootNode(), src)
			for match := matches.Next(); match != nil; match = matches.Next() {
				kind := ""
				var def, name [2]int
				hasDef, gotName := false, false
				for _, c := range match.Captures {
					capture := names[c.Index]
					if k, ok := strings.CutPrefix(capture, "definition."); ok && tagKinds[k] {
						kind = k
						def = [2]int{int(c.Node.StartByte()), int(c.Node.EndByte())}
						hasDef = true
					} else if hasName && c.Index == uint32(nameIdx) {
						name = [2]int{int(c.Node.StartByte()), int(c.Node.EndByte())}
						gotName = true
					}
				}

				if !hasDef {
					continue
				}

				if !gotName {
					name = def
				}

				out = append(out, Tag{
					Kind:  kind,
					Name:  string(src[name[0]:name[1]]),
					Start: text.ByteToChar(def[0]),
					End:   text.ByteToChar(def[1]),
				})
			}

			cursor.Close()
		}

		for _, child := range ly.children {
			visit(child)
		}
	}
	visit(s.root)
	slices.SortStableFunc(out, func(a, b Tag) int { return cmp.Compare(a.Start, b.Start) })

	return out
}

// Scopes lists the node kinds from the node at pos up to the root.
func (s *Syntax) Scopes(text buffer.Rope, pos int) []string {
	b := text.CharToByte(pos)
	ly := s.layerFor(b, b)
	if ly == nil || ly.tree == nil {
		return nil
	}

	node := ly.tree.RootNode().DescendantForByteRange(uint(b), uint(b))
	var out []string
	for node != nil {
		out = append(out, node.Kind())
		node = node.Parent()
	}

	return out
}

// Subtree returns the S-expression of the smallest node covering r.
func (s *Syntax) Subtree(text buffer.Rope, r edit.Range) string {
	node := s.nodeForRange(text, r)
	if node == nil {
		return ""
	}

	return node.ToSexp()
}

// HighlightNamesAt returns the highlight scopes applied at pos, outer
// first.
func (s *Syntax) HighlightNamesAt(text buffer.Rope, pos int) []string {
	b := text.CharToByte(pos)
	var out []string
	for _, h := range s.Highlights(b, b+1) {
		if h.Start <= b && b < h.End {
			out = append(out, h.Scope)
		}
	}

	return out
}

// Layers describes the parsed layers: language and depth, root first.
func (s *Syntax) Layers() []string {
	var out []string
	var visit func(ly *layer)
	visit = func(ly *layer) {
		out = append(out, fmt.Sprintf("%s%s (%d ranges)", strings.Repeat("  ", ly.depth), ly.data.lang.Name, len(ly.ranges)))
		for _, child := range ly.children {
			visit(child)
		}
	}
	visit(s.root)

	return out
}

// RainbowBrackets returns the bracket tokens captured by rainbows.scm
// within the byte range with their nesting depth as the scope
// "rainbow.<depth>", depth counting from zero at the outermost bracket
// pair. The scopes around the range intersect it, so the depth is right
// without looking at the rest of the file.
func (s *Syntax) RainbowBrackets(startByte, endByte int) []Highlight {
	if startByte >= endByte {
		return nil
	}

	var out []Highlight
	src := s.bytes()
	var visit func(ly *layer, baseDepth int)
	visit = func(ly *layer, baseDepth int) {
		if !ly.intersects(startByte, endByte) {
			return
		}

		if ly.tree != nil && ly.data.rainbows != nil {
			q := ly.data.rainbows
			names := q.CaptureNames()
			type span struct{ start, end int }
			var scopes []span
			var brackets []span
			cursor := tree_sitter.NewQueryCursor()
			cursor.SetByteRange(uint(startByte), uint(endByte))
			matches := cursor.Matches(q, ly.tree.RootNode(), src)
			for match := matches.Next(); match != nil; match = matches.Next() {
				for _, c := range match.Captures {
					sp := span{int(c.Node.StartByte()), int(c.Node.EndByte())}
					switch names[c.Index] {
					case "rainbow.scope":
						scopes = append(scopes, sp)
					case "rainbow.bracket":
						brackets = append(brackets, sp)
					}
				}
			}

			cursor.Close()
			slices.SortFunc(scopes, func(a, b span) int {
				return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end))
			})
			slices.SortFunc(brackets, func(a, b span) int { return cmp.Compare(a.start, b.start) })
			// Walk scopes and brackets together keeping a stack of the open
			// scopes, the depth of a bracket is the number of scopes that
			// contain it minus its own.
			var stack []span
			si := 0
			seen := map[span]bool{}
			for _, b := range brackets {
				if seen[b] {
					continue
				}

				seen[b] = true
				for si < len(scopes) && scopes[si].start <= b.start {
					// Scopes that closed before this one opens leave the stack.
					for len(stack) > 0 && stack[len(stack)-1].end <= scopes[si].start {
						stack = stack[:len(stack)-1]
					}

					stack = append(stack, scopes[si])
					si++
				}

				for len(stack) > 0 && stack[len(stack)-1].end <= b.start {
					stack = stack[:len(stack)-1]
				}

				depth := max(baseDepth+len(stack)-1, 0)
				out = append(out, Highlight{Start: b.start, End: b.end, Scope: "rainbow." + strconv.Itoa(depth)})
			}
		}

		for _, child := range ly.children {
			visit(child, baseDepth)
		}
	}
	visit(s.root, 0)
	slices.SortFunc(out, func(a, b Highlight) int { return cmp.Compare(a.Start, b.Start) })

	return out
}

// Check loads the grammar and queries of a language, reporting why the
// editor could not highlight it.
func (l *Loader) Check(lang *Language) error {
	_, err := l.get(lang)

	return err
}
