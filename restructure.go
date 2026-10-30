package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Restructuring works on shapes: a mapping names structure paths
// (".items[].owner.name"), so one entry maps that field of every item.
//
// A mapping is a list of entries {from, to}. "to" is the path in the new
// document; "from" is the shape path its value comes from. An entry
// without "from" is a wrapper: an object made in the new document. An
// entry whose "to" is null drops its "from".
//
// The entries' "to" paths make a tree. A node with children is built from
// them: an object from its keys, an array from its "[]" item. A node
// without children copies the value at its "from". Inside a "[]" item the
// "from" of every descendant lies under the item's own "from", so
//
//	{from: ".items[]",           to: ".records[]"}
//	{from: ".items[].owner.name", to: ".records[].owner"}
//
// makes one record per item with that item's owner name. A "[]" item's
// "from" may fan out further than one step (".items[].tags[]" collects
// every item's tags into one array). A key takes one value, so a key's
// "from" may not fan out past the item it is in.
//
// A field is copied only where it is present: an optional field gives
// its key to the objects that have it and no others, and a value of the
// wrong type (an index into a string) counts as absent, never as an
// error. The generated jq program (JQ) does the same, so it never fails
// on a document of this shape. Keys in the new document come in the order
// the mapping first names them.
//
// Duplicate keys: the parser reads a document as jq reads it (Build), the
// last value of a repeated key at the position of the first, so the Go
// result and the jq program agree.

// MapEntry is one line of a mapping. To nil drops From.
type MapEntry struct {
	From string  `json:"from,omitempty"`
	To   *string `json:"to"`
}

// UnmarshalJSON requires "to" (null drops) and refuses unknown fields.
func (e *MapEntry) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.New(`an entry is {"from": PATH, "to": PATH|null}`)
	}
	*e = MapEntry{}
	for k, v := range raw {
		switch k {
		case "from":
			if err := json.Unmarshal(v, &e.From); err != nil {
				return errors.New(`"from" is a path string`)
			}
		case "to":
			if string(v) == "null" {
				continue
			}
			var to string
			if err := json.Unmarshal(v, &to); err != nil {
				return errors.New(`"to" is a path string or null`)
			}
			e.To = &to
		default:
			return fmt.Errorf("unknown field %q (an entry has from and to)", k)
		}
	}
	if _, ok := raw["to"]; !ok {
		return errors.New(`an entry needs "to" (null drops its "from")`)
	}
	if e.From == "" && e.To == nil {
		return errors.New(`an entry with "to": null needs a "from"`)
	}
	return nil
}

// Mapping is a restructure spec: {"map": [entries]}, the value patches
// applied after it, {"patches": [...]} (patch.go), and the reshape steps
// that follow, each with its own map and patches, {"then": [...]}
// (reshape.go).
type Mapping struct {
	Map     []MapEntry `json:"map"`
	Patches []Patch    `json:"patches,omitempty"`
	Then    []Stage    `json:"then,omitempty"`
}

// ReadMapping parses a mapping file's bytes.
func ReadMapping(data []byte) (*Mapping, error) {
	var m Mapping
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf(`mapping: %w (a mapping is {"map": [{"from": PATH, "to": PATH|null}], "patches": [{"op": "set"|"delete"|"add"|"rename", "path": PATH, ...}], "then": [{"op": "flatten"|"key-by"|"values"|"entries"|"lift", ..., "map": [...], "patches": [...]}]})`, err)
	}
	return &m, nil
}

// target is a node of the new document.
type target struct {
	key      string // the object key; "" for the root and an item
	item     bool   // a "[]" item
	path     string
	from     []pathStep // nil without a from
	fromPath string
	hasFrom  bool
	declared bool // named by an entry (a wrapper or a from)
	fields   []*target
	items    *target

	// Set by plan: the from relative to the context, and the shape at
	// the context then after each relative step.
	rel    []pathStep
	shapes []*Shape
}

func (t *target) leaf() bool { return len(t.fields) == 0 && t.items == nil }

// Plan is a mapping checked against a document's shape, ready to apply or
// to print as jq.
type Plan struct {
	mapping *Mapping
	shape   *Shape
	root    *target

	// The reshape steps (Mapping.Then), made ready against the document
	// NewDocPlan read, and that document's result.
	stages []*stagePlan
	src    *Node
	out    *Node
	orph   []int
}

// NewPlan checks a mapping against a document's shape (Infer). A mapping
// with reshape steps needs the document itself (NewDocPlan): what a step
// makes depends on the values.
func NewPlan(m *Mapping, shape *Shape) (*Plan, error) {
	if len(m.Then) > 0 {
		return nil, errors.New("a mapping with reshape steps (then) is planned on the document")
	}
	return newPlan(m, shape)
}

// NewDocPlan checks a mapping against a document: the first map against
// its shape, then each reshape step and its map against what the steps
// before it made.
func NewDocPlan(m *Mapping, root *Node, shape *Shape) (*Plan, error) {
	first := *m
	first.Then = nil
	p, err := newPlan(&first, shape)
	if err != nil || len(m.Then) == 0 {
		return p, err
	}
	p.mapping = m
	cur, orphans := p.applyFirst(root)
	for i, s := range m.Then {
		sp, err := prepareStage(s, cur)
		if err != nil {
			return nil, fmt.Errorf("then step %d: %w", i+1, err)
		}
		out := normalized(sp.apply(cur))
		if len(s.Map) > 0 {
			if sp.plan, err = newPlan(&Mapping{Map: s.Map, Patches: s.Patches}, Infer(out)); err != nil {
				return nil, fmt.Errorf("then step %d: %w", i+1, err)
			}
		} else if err := checkPatches(s.Patches); err != nil {
			return nil, fmt.Errorf("then step %d: %w", i+1, err)
		}
		p.stages = append(p.stages, sp)
		cur, orphans = sp.applyMap(out)
	}
	p.src, p.out, p.orph = root, cur, orphans
	return p, nil
}

// normalized is n parsed again, so its nodes have their own parents and
// positions.
func normalized(n *Node) *Node {
	out, err := Build([]byte(Compact(n, 0)))
	if err != nil {
		return n
	}
	return out
}

// applyMap is the stage's map and patches on the step's output.
func (sp *stagePlan) applyMap(out *Node) (*Node, []int) {
	if sp.plan != nil {
		v, orphans := sp.plan.ApplyAll(out)
		return normalized(v), orphans
	}
	v, orphans := applyPatches(out, sp.st.Patches)
	return normalized(v), orphans
}

func newPlan(m *Mapping, shape *Shape) (*Plan, error) {
	p := &Plan{mapping: m, shape: shape, root: &target{path: "."}}
	for i, e := range m.Map {
		if err := p.add(e); err != nil {
			return nil, fmt.Errorf("mapping entry %d: %w", i+1, err)
		}
	}
	if !p.root.declared && p.root.leaf() {
		return nil, errors.New("the mapping maps nothing (every entry drops)")
	}
	if err := checkPatches(m.Patches); err != nil {
		return nil, err
	}
	if err := p.check(p.root, nil, shape); err != nil {
		return nil, err
	}
	return p, nil
}

// mappingPath parses a mapping path: keys and [] only.
func mappingPath(path string) ([]pathStep, error) {
	steps, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	for _, st := range steps {
		if st.kind == 'i' {
			return nil, fmt.Errorf("path %q: a mapping names shapes, so it uses keys and [] (no index)", path)
		}
	}
	return steps, nil
}

// stepsPath is the canonical path of steps, as Shape.Path writes it.
func stepsPath(steps []pathStep) string {
	path := "."
	for _, st := range steps {
		s := "[]"
		if st.kind == 'k' {
			s = keyStep(st.key)
		}
		path = joinPath(path, s)
	}
	return path
}

func (p *Plan) add(e MapEntry) error {
	var from []pathStep
	if e.From != "" {
		var err error
		if from, err = mappingPath(e.From); err != nil {
			return err
		}
		if p.shape.Find(stepsPath(from)) == nil {
			return fmt.Errorf("from %s: the document has no such path", stepsPath(from))
		}
	}
	if e.To == nil {
		return nil
	}
	to, err := mappingPath(*e.To)
	if err != nil {
		return err
	}
	at := p.root
	for _, st := range to {
		if at, err = at.child(st); err != nil {
			return err
		}
	}
	if at.declared {
		return fmt.Errorf("to %s: an earlier entry maps it", at.path)
	}
	at.declared = true
	if e.From != "" {
		at.from, at.fromPath, at.hasFrom = from, stepsPath(from), true
	}
	return nil
}

// child is the node one step below t, made on first use.
func (t *target) child(st pathStep) (*target, error) {
	if st.kind == 'a' {
		if len(t.fields) > 0 {
			return nil, fmt.Errorf("to %s: %s already has keys, so it is an object, not an array", joinPath(t.path, "[]"), t.path)
		}
		if t.items == nil {
			t.items = &target{item: true, path: joinPath(t.path, "[]")}
		}
		return t.items, nil
	}
	if t.items != nil {
		return nil, fmt.Errorf("to %s: %s already has [], so it is an array, not an object", joinPath(t.path, keyStep(st.key)), t.path)
	}
	for _, f := range t.fields {
		if f.key == st.key {
			return f, nil
		}
	}
	f := &target{key: st.key, path: joinPath(t.path, keyStep(st.key))}
	t.fields = append(t.fields, f)
	return f, nil
}

// check validates t under a context (the from of the nearest "[]" item
// above it, and the shape there) and records its relative from.
func (p *Plan) check(t *target, ctx []pathStep, ctxShape *Shape) error {
	if t.item && !t.hasFrom {
		return fmt.Errorf("to %s: an array item needs a from (the values it is made of)", t.path)
	}
	if t.hasFrom {
		if len(t.from) < len(ctx) || !slices.Equal(t.from[:len(ctx)], ctx) {
			return fmt.Errorf("to %s: from %s is outside %s, the item it is in", t.path, t.fromPath, stepsPath(ctx))
		}
		t.rel = t.from[len(ctx):]
		t.shapes = shapesAlong(ctxShape, t.rel)
		if !t.item && slices.ContainsFunc(t.rel, func(st pathStep) bool { return st.kind == 'a' }) {
			return fmt.Errorf("to %s: from %s names many values for one key; end the to path with []", t.path, t.fromPath)
		}
	}
	if t.item {
		ctx, ctxShape = t.from, t.shapes[len(t.shapes)-1]
	}
	for _, f := range t.fields {
		if err := p.check(f, ctx, ctxShape); err != nil {
			return err
		}
	}
	if t.items != nil {
		return p.check(t.items, ctx, ctxShape)
	}
	return nil
}

// shapesAlong is the shape at from, then after each step (the steps are
// known to exist: add found the full path).
func shapesAlong(from *Shape, steps []pathStep) []*Shape {
	out := []*Shape{from}
	at := from
	for _, st := range steps {
		next := at.items
		if st.kind == 'k' {
			next = at.fields[st.key]
		}
		out = append(out, next)
		at = next
	}
	return out
}

// Apply builds the new document from a document: the restructure, then
// the value patches.
func (p *Plan) Apply(root *Node) *Node {
	out, _ := p.ApplyAll(root)
	return out
}

// ApplyAll is Apply and the patches it skipped, by index (orphans: their
// target is not in the restructured document): the last patches, the
// ones after the last reshape step when there are any.
func (p *Plan) ApplyAll(root *Node) (*Node, []int) {
	if len(p.stages) == 0 {
		return p.applyFirst(root)
	}
	if root == p.src {
		return p.out, p.orph
	}
	cur, orphans := p.applyFirst(root)
	for _, sp := range p.stages {
		cur, orphans = sp.applyMap(normalized(sp.apply(cur)))
	}
	return cur, orphans
}

// applyFirst is the first map and its patches.
func (p *Plan) applyFirst(root *Node) (*Node, []int) {
	v, ok := p.value(p.root, root)
	if !ok {
		v = &Node{Kind: KNull, Value: "null"}
	}
	return applyPatches(v, p.mapping.Patches)
}

// value is t's value with ctx as the context, and whether t is present.
func (p *Plan) value(t *target, ctx *Node) (*Node, bool) {
	var found []*Node
	if t.hasFrom && !t.item {
		found = stream(ctx, t.rel)
		if len(found) == 0 {
			return nil, false
		}
	}
	switch {
	case t.items != nil:
		out := &Node{Kind: KArray}
		for _, v := range stream(ctx, t.items.rel) {
			item := v
			if !t.items.leaf() {
				item, _ = p.value(t.items, v)
			}
			out.Children = append(out.Children, rekey(item, ""))
		}
		return out, true
	case len(t.fields) > 0:
		out := &Node{Kind: KObject}
		for _, f := range t.fields {
			if v, ok := p.value(f, ctx); ok {
				out.Children = append(out.Children, rekey(v, f.key))
			}
		}
		return out, true
	case t.item:
		return ctx, true // an item without children is the value itself
	case t.hasFrom:
		return found[0], true
	}
	return &Node{Kind: KObject}, true // an empty wrapper
}

// stream is every value at steps below n: a key on an object that has it,
// every item of an array; anything else gives nothing.
func stream(n *Node, steps []pathStep) []*Node {
	at := []*Node{n}
	for _, st := range steps {
		var next []*Node
		for _, v := range at {
			switch {
			case st.kind == 'k':
				if c := v.Field(st.key); c != nil {
					next = append(next, c)
				}
			case v.Kind == KArray:
				next = append(next, v.Children...)
			}
		}
		at = next
	}
	return at
}

// rekey is n under a new key (a shallow copy; the children are shared).
func rekey(n *Node, key string) *Node {
	c := *n
	c.Key = key
	return &c
}

// JQ is a jq program that makes the same document from any document of
// this shape, without failing: iterations are guarded ("[]?", or a type
// test where the values may be objects), an optional field is copied
// under "if has(k)", and a step into a value that may not be an object
// tests its type first.
//
// The value patches follow as setpath / delpaths steps, each testing its
// target first (patch.go), so the program's output is Apply's.
func (p *Plan) JQ() string {
	first := p.firstJQ()
	if len(p.stages) == 0 {
		return first
	}
	parts := []string{"(" + strings.TrimSuffix(first, "\n") + ")"}
	for _, sp := range p.stages {
		parts = append(parts, "("+sp.opJQ()+")")
		if m := sp.mapJQ(); m != "" {
			parts = append(parts, "("+m+")")
		}
	}
	return strings.Join(parts, " | ") + "\n"
}

// firstJQ is the first map and its patches as jq.
func (p *Plan) firstJQ() string {
	expr := p.jqValue(p.root)
	if p.root.hasFrom {
		if cond := jqCond(p.root.rel, p.root.shapes); cond != "" {
			expr = "if " + cond + " then " + expr + " else null end"
		}
	}
	steps, uses := patchesJQ(p.mapping.Patches)
	if steps == "" {
		return expr + "\n"
	}
	expr = "(" + expr + ")" + steps
	if uses {
		expr = jqHasPath + " " + expr
	}
	return expr + "\n"
}

func (p *Plan) jqValue(t *target) string {
	switch {
	case t.items != nil:
		s := jqStream(t.items.rel, t.items.shapes)
		if !t.items.leaf() {
			s += " | " + p.jqValue(t.items)
		}
		return "[" + s + "]"
	case len(t.fields) > 0:
		var pieces, group []string
		flush := func() {
			if len(group) > 0 {
				pieces = append(pieces, "{"+strings.Join(group, ", ")+"}")
				group = nil
			}
		}
		for _, f := range t.fields {
			kv := quote(f.key) + ": " + jqParen(p.jqValue(f))
			cond := ""
			if f.hasFrom {
				cond = jqCond(f.rel, f.shapes)
			}
			if cond == "" {
				group = append(group, kv)
				continue
			}
			flush()
			pieces = append(pieces, "(if "+cond+" then {"+kv+"} else {} end)")
		}
		flush()
		return strings.Join(pieces, " + ")
	case t.item:
		return "."
	case t.hasFrom:
		return jqPath(t.rel)
	}
	return "{}"
}

// jqParen parenthesizes an expression that is more than one term: one
// with a space outside a single (…) or […] around all of it.
func jqParen(expr string) string {
	if !strings.ContainsAny(expr, " |") || enclosed(expr) {
		return expr
	}
	return "(" + expr + ")"
}

// enclosed reports an expression that is one bracketed group, "(…)" or
// "[…]", skipping quoted strings.
func enclosed(expr string) bool {
	if len(expr) < 2 || !(expr[0] == '(' && expr[len(expr)-1] == ')' || expr[0] == '[' && expr[len(expr)-1] == ']') {
		return false
	}
	depth, quoted := 0, false
	for i := 0; i < len(expr); i++ {
		switch c := expr[i]; {
		case quoted && c == '\\':
			i++
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
			if depth == 0 && i < len(expr)-1 {
				return false
			}
		}
	}
	return true
}

// jqPath is the plain path of key steps: ".a.b", `.["odd key"]`.
func jqPath(steps []pathStep) string {
	var b strings.Builder
	for _, st := range steps {
		b.WriteString(keyStep(st.key))
	}
	return dotted(b.String())
}

func dotted(path string) string {
	if path == "" || path[0] == '[' {
		return "." + path
	}
	return path
}

// objectOnly reports a shape whose values are all objects.
func objectOnly(s *Shape) bool { return s.Kinds[KObject] == s.Count }

// jqCond is the test that a single-valued path (keys only) is present,
// "" when it always is: at each step, that the value is an object when
// it may not be, and that it has the key when the key is optional.
func jqCond(steps []pathStep, shapes []*Shape) string {
	if len(steps) == 0 {
		return ""
	}
	var parts []string
	if !objectOnly(shapes[0]) {
		parts = append(parts, `type == "object"`)
	}
	if shapes[1].Optional {
		parts = append(parts, "has("+quote(steps[0].key)+")")
	}
	if rest := jqCond(steps[1:], shapes[1:]); rest != "" {
		parts = append(parts, "("+dotted(keyStep(steps[0].key))+" | "+rest+")")
	}
	return strings.Join(parts, " and ")
}

// jqStream is a filter giving every value at steps below ".", none where
// a key is missing or a value has the wrong type.
func jqStream(steps []pathStep, shapes []*Shape) string {
	var segs []string
	plain := ""
	flush := func() {
		if plain != "" {
			segs = append(segs, dotted(plain))
			plain = ""
		}
	}
	for i, st := range steps {
		parent, child := shapes[i], shapes[i+1]
		if st.kind == 'a' {
			switch {
			case parent.Kinds[KArray] == parent.Count:
				plain += "[]"
			case parent.Kinds[KObject] == 0:
				plain += "[]?" // a scalar or null iterates to nothing
			default:
				flush()
				segs = append(segs, `select(type == "array")`)
				plain = "[]"
			}
			continue
		}
		var test []string
		if !objectOnly(parent) {
			test = append(test, `type == "object"`)
		}
		if child.Optional {
			test = append(test, "has("+quote(st.key)+")")
		}
		if len(test) > 0 {
			flush()
			segs = append(segs, "select("+strings.Join(test, " and ")+")")
		}
		plain += keyStep(st.key)
	}
	flush()
	if len(segs) == 0 {
		return "."
	}
	return strings.Join(segs, " | ")
}

// Complete is the mapping with every shape it leaves out added as a drop
// ({from, to: null}), after the given entries, so it reads as the whole
// story of the source. A shape counts as kept when an entry maps it or a
// path under it, or when it is inside a value an entry copies whole.
func (p *Plan) Complete() *Mapping {
	out := &Mapping{Map: slices.Clone(p.mapping.Map), Patches: slices.Clone(p.mapping.Patches), Then: slices.Clone(p.mapping.Then)}
	kept, listed := map[*Shape]bool{}, map[*Shape]bool{}
	for _, e := range p.mapping.Map {
		if e.From == "" {
			continue
		}
		steps, _ := mappingPath(e.From)
		s := p.shape.Find(stepsPath(steps))
		if e.To == nil {
			listed[s] = true
			continue
		}
		for at := s; at != nil; at = at.parent {
			kept[at] = true
		}
	}
	var copied func(t *target)
	copied = func(t *target) {
		if t.hasFrom && t.leaf() {
			p.shape.Find(t.fromPath).Walk(func(s *Shape, _ int) { kept[s] = true })
		}
		for _, f := range t.fields {
			copied(f)
		}
		if t.items != nil {
			copied(t.items)
		}
	}
	copied(p.root)
	var walk func(s *Shape)
	walk = func(s *Shape) {
		if listed[s] {
			return
		}
		if !kept[s] {
			out.Map = append(out.Map, MapEntry{From: s.Path})
			return
		}
		for _, c := range s.Children {
			walk(c)
		}
	}
	walk(p.shape)
	return out
}

// FormatMapping writes a mapping one entry per line, as the family's row
// files are: {"map": [, then each {"from": …, "to": …} on its own line
// (no "from" on a wrapper, "to": null on a drop), then ]}; with value
// patches, "patches": [ follows with one patch per line.
func FormatMapping(m *Mapping) ([]byte, error) {
	str := func(v string) string {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v)
		return strings.TrimRight(b.String(), "\n")
	}
	var b strings.Builder
	b.WriteString("{\"map\": [\n")
	for i, e := range m.Map {
		b.WriteString("  " + formatEntry(e, str))
		if i < len(m.Map)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	if len(m.Patches) > 0 {
		b.WriteString("],\n\"patches\": [\n")
		for i, p := range m.Patches {
			b.WriteString("  " + formatPatch(p, str))
			if i < len(m.Patches)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
	}
	if len(m.Then) > 0 {
		b.WriteString("],\n\"then\": [\n")
		for i, s := range m.Then {
			b.WriteString(formatStage(s, str))
			if i < len(m.Then)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("]}\n")
	return []byte(b.String()), nil
}

// formatEntry is one mapping entry on a line: no "from" on a wrapper,
// "to": null on a drop.
func formatEntry(e MapEntry, str func(string) string) string {
	var fields []string
	if e.From != "" {
		fields = append(fields, `"from": `+str(e.From))
	}
	to := "null"
	if e.To != nil {
		to = str(*e.To)
	}
	fields = append(fields, `"to": `+to)
	return "{" + strings.Join(fields, ", ") + "}"
}

// restructureOutput is what the restructure command prints or writes.
func restructureOutput(doc *Doc, m *Mapping, as string) ([]byte, error) {
	src := doc.Root
	plan, err := NewDocPlan(m, doc.Root, doc.Shape)
	if err != nil {
		return nil, err
	}
	switch as {
	case "jq":
		return []byte(plan.JQ()), nil
	case "mapping":
		return FormatMapping(plan.Complete())
	}
	return []byte(Pretty(plan.Apply(src)) + "\n"), nil
}

// writeNew writes data to path. An existing file (the source among them)
// is replaced only with overwrite, and always atomically.
func writeNew(path string, data []byte, overwrite bool) error {
	if _, err := os.Stat(path); err == nil && !overwrite {
		return fmt.Errorf("%s exists; --overwrite replaces it", path)
	}
	return writeAtomic(path, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// writeAtomic replaces path with what write produces without ever leaving
// it half written: the bytes go to a temporary file beside it (same
// directory, so the rename stays on one file system), which gets the
// original's mode (0644 for a new file), is synced and closed, then
// renamed over path. On any error the temporary file is removed and path
// is untouched. (From o-csv.)
func writeAtomic(path string, write func(io.Writer) error) (err error) {
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err = write(f); err != nil {
		return err
	}
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
