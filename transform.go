package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Operation is one step of a pipeline. Kinds: project, flatten,
// array-object, object-array, group, rename, drop, sort, filter, scope.
type Operation struct {
	Kind                     string
	Paths                    []Path
	Path                     Path
	Grain                    Path
	Cross, Inner, Desc       bool
	Field, Name, Mode, Query string
}

// FilterModes lists the filter modes in cycle order.
var FilterModes = []string{"substring", "fuzzy", "regex", "eq", "ne", "contains", "gt", "lt"}

// normalize fills defaults and rejects operations that cannot run.
func normalize(op Operation) (Operation, error) {
	need := func(v, what string) error {
		if v == "" {
			return fmt.Errorf("%s needs %s", op.Kind, what)
		}
		return nil
	}
	var err error
	switch op.Kind {
	case "project":
		if len(op.Paths) == 0 {
			err = fmt.Errorf("project needs at least one path")
		}
	case "flatten":
		if len(op.Paths) == 0 {
			err = fmt.Errorf("flatten needs fields (ResolveOperation fills them from the data)")
		} else if len(op.Grain) > 0 && !op.Grain[len(op.Grain)-1].Array {
			err = fmt.Errorf("row grain %s is not an array path", op.Grain)
		}
	case "array-object":
		if err = need(op.Field, "a key field"); err == nil {
			var p Path
			if p, err = ParsePath(op.Field); err == nil && (len(p) == 0 || hasWild(p)) {
				err = fmt.Errorf("array-object key field %q must name one value", op.Field)
			}
		}
	case "object-array":
		if op.Name == "" {
			op.Name = "key"
		}
	case "group":
		if op.Name == "" {
			op.Name = "rows"
		}
		if op.Mode == "" {
			op.Mode = "array"
		}
		if err = need(op.Field, "a column"); err == nil && op.Name == op.Field {
			err = fmt.Errorf("group column %q and nested column name are the same", op.Field)
		} else if op.Mode != "array" && op.Mode != "object" {
			err = fmt.Errorf("group mode %q is not array or object", op.Mode)
		}
	case "rename":
		if err = need(op.Field, "a column"); err == nil {
			if err = need(op.Name, "a new name"); err == nil && op.Name == op.Field {
				err = fmt.Errorf("rename %q to itself", op.Field)
			}
		}
	case "drop":
		err = need(op.Field, "a column")
	case "sort":
	case "filter":
		if op.Mode == "" {
			op.Mode = "substring"
		}
		switch op.Mode {
		case "regex":
			if _, e := regexp.Compile(op.Query); e != nil {
				err = fmt.Errorf("invalid regex %q: %v", op.Query, strings.TrimPrefix(e.Error(), "error parsing regexp: "))
			}
		case "gt", "lt":
			if _, ok := jsonNumber(op.Query); !ok {
				err = fmt.Errorf("filter %s needs a number, got %q", op.Mode, op.Query)
			}
		case "substring", "fuzzy", "eq", "ne", "contains":
		default:
			err = fmt.Errorf("unknown filter mode %q", op.Mode)
		}
	case "scope":
		if op.Mode != "" && op.Mode != "select" {
			err = fmt.Errorf("scope mode %q is not select", op.Mode)
		}
	default:
		err = fmt.Errorf("unknown operation %q", op.Kind)
	}
	return op, err
}

func hasWild(p Path) bool {
	for _, s := range p {
		if s.Array {
			return true
		}
	}
	return false
}

// jsonNumber reports whether s is a JSON number literal.
func jsonNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	var n json.Number
	if json.Unmarshal([]byte(s), &n) != nil || s == "" {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

// ApplyPipeline runs ops over v. A scope step makes the following steps
// update the values at its path in place (jq "path |= ..."); a scope with
// Mode "select" replaces the document with the values at the path.
func ApplyPipeline(v any, ops []Operation) (any, error) {
	var scope Path
	for i, raw := range ops {
		op, err := normalize(raw)
		if err == nil {
			switch {
			case op.Kind == "scope" && op.Mode == "select":
				v, scope = selectPath(v, op.Path), nil
			case op.Kind == "scope":
				scope = op.Path
			default:
				v, err = updatePath(v, scope, func(x any) (any, error) { return applyOp(x, op) })
			}
		}
		if err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, raw.Kind, err)
		}
	}
	return v, nil
}

func applyOp(v any, op Operation) (any, error) {
	switch op.Kind {
	case "project":
		return project(v, buildTree(op.Paths, nil)), nil
	case "flatten":
		r, err := flatten(v, op)
		return r.Value(), err
	case "array-object":
		return arrayToObject(v, MustPath(op.Field))
	case "object-array":
		return objectToArray(v, op.Name)
	case "group":
		return group(v, op)
	case "rename", "drop":
		return eachRow(v, func(o Object) (Object, error) {
			i := o.index(op.Field)
			switch {
			case i < 0:
				return o, nil
			case op.Kind == "drop":
				return append(append(Object{}, o[:i]...), o[i+1:]...), nil
			case o.index(op.Name) >= 0:
				return nil, fmt.Errorf("rename: column %q already exists", op.Name)
			}
			out := append(Object{}, o...)
			out[i].Key = op.Name
			return out, nil
		})
	case "sort":
		a, err := needArray(v, "sort")
		if err != nil {
			return nil, err
		}
		return sortRows(a, op.Field, op.Desc), nil
	case "filter":
		return filterRows(v, op)
	}
	return v, nil
}

func (o Object) index(key string) int {
	for i, f := range o {
		if f.Key == key {
			return i
		}
	}
	return -1
}

// eachRow applies fn to v when it is an object, or to each object of an array.
func eachRow(v any, fn func(Object) (Object, error)) (any, error) {
	switch t := v.(type) {
	case Object:
		return fn(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = e
			if o, ok := e.(Object); ok {
				r, err := fn(o)
				if err != nil {
					return nil, err
				}
				out[i] = r
			}
		}
		return out, nil
	}
	return v, nil
}

func needArray(v any, op string) ([]any, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s needs an array, got %s", op, typeName(v))
	}
	return a, nil
}

// updatePath applies f at every match of p with jq "|=" semantics: a missing
// key or a null parent is created, a wildcard visits array elements and
// object values, and an index past the end pads with null.
func updatePath(v any, p Path, f func(any) (any, error)) (any, error) {
	if len(p) == 0 {
		return f(v)
	}
	s, rest := p[0], p[1:]
	switch t := v.(type) {
	case []any:
		if s.Array || s.Indexed {
			return updateElems(t, s, rest, f)
		}
	case Object:
		if s.Array {
			out := append(Object{}, t...)
			for i := range out {
				nv, err := updatePath(out[i].Value, rest, f)
				if err != nil {
					return nil, err
				}
				out[i].Value = nv
			}
			return out, nil
		}
		if !s.Indexed {
			cur, _ := t.Get(s.Key)
			nv, err := updatePath(cur, rest, f)
			if err != nil {
				return nil, err
			}
			return t.set(s.Key, nv), nil
		}
	case nil:
		if s.Indexed {
			return updateElems(nil, s, rest, f)
		}
		if !s.Array {
			nv, err := updatePath(nil, rest, f)
			return Object{{s.Key, nv}}, err
		}
	}
	what := "with " + strconv.Quote(s.Key)
	if s.Array {
		return nil, fmt.Errorf("cannot iterate over %s", typeName(v))
	} else if s.Indexed {
		what = "with a number"
	}
	return nil, fmt.Errorf("cannot index %s %s", typeName(v), what)
}

func updateElems(a []any, s PathPart, rest Path, f func(any) (any, error)) (any, error) {
	out := append([]any{}, a...)
	for s.Indexed && len(out) <= s.Index {
		out = append(out, nil)
	}
	for i := range out {
		if s.Indexed && i != s.Index {
			continue
		}
		nv, err := updatePath(out[i], rest, f)
		if err != nil {
			return nil, err
		}
		out[i] = nv
	}
	return out, nil
}

// selectPath is the value at p, or the list of values when p has wildcards.
// Missing keys read as null and wildcards visit arrays only.
func selectPath(v any, p Path) any {
	var out []any
	var walk func(v any, p Path)
	walk = func(v any, p Path) {
		switch {
		case len(p) == 0:
			out = append(out, v)
		case p[0].Array:
			a, _ := v.([]any)
			for _, e := range a {
				walk(e, p[1:])
			}
		default:
			x, _ := step(v, p[0])
			walk(x, p[1:])
		}
	}
	walk(v, p)
	if !hasWild(p) {
		return out[0]
	}
	if out == nil {
		out = []any{}
	}
	return out
}

// selNode is one step of a set of selected paths. A terminal node ends a
// selected path; col is its flatten column name.
type selNode struct {
	part PathPart
	term bool
	col  string
	kids []*selNode
}

func buildTree(paths []Path, cols []string) *selNode {
	root := &selNode{}
	for i, p := range paths {
		n := root
		for _, s := range p {
			var next *selNode
			for _, k := range n.kids {
				if k.part == s {
					next = k
				}
			}
			if next == nil {
				next = &selNode{part: s}
				n.kids = append(n.kids, next)
			}
			n = next
		}
		if cols != nil && cols[i] != "" {
			n.term, n.col = true, cols[i]
		} else if cols == nil {
			n.term = true
		}
	}
	return root
}

func (n *selNode) hasArray() bool {
	if n.part.Array {
		return true
	}
	for _, k := range n.kids {
		if k.hasArray() {
			return true
		}
	}
	return false
}

// terms visits the terminal nodes under n in row order with their path from n.
func (n *selNode) terms(rel Path, fn func(*selNode, Path)) {
	if n.term {
		fn(n, rel)
	}
	for _, k := range n.kids {
		k.terms(rel.Child(k.part), fn)
	}
}

func (n *selNode) nullRow() Object {
	row := Object{}
	n.terms(nil, func(t *selNode, _ Path) { row = append(row, Field{t.col, nil}) })
	return row
}

// Project keeps only the selected paths of v, preserving structure. Missing
// keys read as null, arrays map element by element, and a value that is not
// the container a path expects becomes null.
func Project(v any, paths []Path) any { return project(v, buildTree(paths, nil)) }

func project(v any, n *selNode) any {
	if n.term {
		return v
	}
	var wild *selNode
	var keys, idx []*selNode
	for _, k := range n.kids {
		switch {
		case k.part.Array:
			wild = k
		case k.part.Indexed:
			idx = append(idx, k)
		default:
			keys = append(keys, k)
		}
	}
	switch t := v.(type) {
	case Object:
		if len(keys) > 0 {
			out := Object{}
			for _, k := range keys {
				x, _ := t.Get(k.part.Key)
				out = append(out, Field{k.part.Key, project(x, k)})
			}
			return out
		}
	case []any:
		if wild != nil {
			out := make([]any, len(t))
			for i, e := range t {
				out[i] = project(e, wild)
			}
			return out
		}
		if len(idx) > 0 {
			out := []any{}
			for _, k := range idx {
				if k.part.Index < len(t) {
					out = append(out, project(t[k.part.Index], k))
				}
			}
			return out
		}
	}
	return nil
}

// Relation is a flat table. Every row holds every column, in column order.
type Relation struct {
	Columns []string
	Rows    []Object
}

// Value is the relation as a JSON array of row objects.
func (r Relation) Value() []any {
	out := make([]any, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = row
	}
	return out
}

// GrainError reports selected fields under unrelated arrays tied at the
// deepest nesting depth. Pick one of the Candidates as the grain or cross join.
type GrainError struct{ Candidates []Path }

func (e *GrainError) Error() string {
	names := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		names[i] = c.String()
	}
	return "fields span unrelated arrays (" + strings.Join(names, ", ") + "): choose a row grain or a cross join"
}

// arrayPrefix is p up to and including its last wildcard step.
func arrayPrefix(p Path) Path {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i].Array {
			return p[:i+1]
		}
	}
	return nil
}

// arrayDepth counts the wildcard steps of p, its SQL nesting depth.
func arrayDepth(p Path) int {
	n := 0
	for _, s := range p {
		if s.Array {
			n++
		}
	}
	return n
}

// branchArrays is the deepest array of each branch the paths reach.
func branchArrays(paths []Path) []Path {
	var pre []Path
	for _, p := range paths {
		if a := arrayPrefix(p); a != nil {
			pre = append(pre, a)
		}
	}
	var out []Path
	seen := map[string]bool{}
	for _, a := range pre {
		deepest := !seen[a.String()]
		for _, b := range pre {
			if len(b) > len(a) && b.HasPrefix(a) {
				deepest = false
			}
		}
		if deepest {
			seen[a.String()] = true
			out = append(out, append(Path{}, a...))
		}
	}
	return out
}

// autoGrain is the row grain of paths without an explicit one: the array
// with the most wildcard steps. Unrelated arrays tied at that depth come
// back as ties instead, for a caller to choose from.
func autoGrain(paths []Path) (grain Path, ties []Path) {
	for _, a := range branchArrays(paths) {
		switch d := arrayDepth(a); {
		case len(ties) == 0 || d > arrayDepth(ties[0]):
			ties = []Path{a}
		case d == arrayDepth(ties[0]):
			ties = append(ties, a)
		}
	}
	if len(ties) == 1 {
		return ties[0], nil
	}
	return nil, ties
}

// rootRelative adds the wildcard steps a path omits when it starts with a
// key at an array, so "id" on a root array reads "$[].id". A path that
// starts with a wildcard, an index or a key of an object stays as it is.
func rootRelative(s *Schema, paths []Path) []Path {
	out := make([]Path, len(paths))
	for i, p := range paths {
		out[i] = p
		if len(p) == 0 || p[0].Array || p[0].Indexed {
			continue
		}
		var pre Path
		for n := s; n != nil && n.elem != nil && n.keys[p[0].Key] == nil; n = n.elem {
			pre = append(pre, PathPart{Array: true})
		}
		out[i] = pre.concat(p)
	}
	return out
}

// SiblingArrays returns the unrelated arrays tied at the deepest nesting
// depth the paths (made root relative and expanded over v) reach, or nil
// when the deepest array is the one row grain.
func SiblingArrays(v any, paths []Path) []Path {
	_, ties := autoGrain(ExpandPaths(v, rootRelative(InferSchema(v), paths)))
	return ties
}

// flattenPlan picks the paths a flatten keeps and names their columns.
// Without a cross join, the grain is the given one or the most deeply nested
// array, and fields under arrays off the grain's branch are left out.
func flattenPlan(op Operation) (*selNode, error) {
	paths := op.Paths
	if !op.Cross {
		grain := op.Grain
		if len(grain) == 0 {
			var ties []Path
			if grain, ties = autoGrain(paths); ties != nil {
				return nil, &GrainError{ties}
			}
		}
		paths = nil
		for _, p := range op.Paths {
			if grain.HasPrefix(arrayPrefix(p)) {
				paths = append(paths, p)
			}
		}
	}
	cols := columnNames(paths)
	if len(op.Grain) > 0 {
		paths, cols = append(paths, op.Grain), append(cols, "")
	}
	return buildTree(paths, cols), nil
}

// columnNames joins keys with dots, skipping wildcards (no prefix for a root
// array). Names that collide fall back to the canonical path, then a counter.
func columnNames(paths []Path) []string {
	names := make([]string, len(paths))
	count := map[string]int{}
	for i, p := range paths {
		var b strings.Builder
		for _, s := range p {
			switch {
			case s.Indexed:
				fmt.Fprintf(&b, "[%d]", s.Index)
			case !s.Array:
				if b.Len() > 0 {
					b.WriteByte('.')
				}
				b.WriteString(s.Key)
			}
		}
		names[i] = b.String()
		if names[i] == "" {
			names[i] = "value"
		}
		count[names[i]]++
	}
	used := map[string]bool{}
	for i, p := range paths {
		if count[names[i]] > 1 {
			names[i] = p.String()
		}
		base := names[i]
		for n := 2; used[names[i]]; n++ {
			names[i] = fmt.Sprintf("%s#%d", base, n)
		}
		used[names[i]] = true
	}
	return names
}

func flatten(v any, op Operation) (Relation, error) {
	root, err := flattenPlan(op)
	if err != nil {
		return Relation{}, err
	}
	r := Relation{Columns: []string{}, Rows: root.rows(v, false, op.Inner)}
	root.terms(nil, func(t *selNode, _ Path) { r.Columns = append(r.Columns, t.col) })
	return r, nil
}

// rows joins the rows of n's parts laterally, first part outermost. Unless
// inner is set, an empty or missing array keeps its parent row with null
// cells (left join). Nested is false at the root: an empty root array has no
// parent row and yields none.
func (n *selNode) rows(v any, nested, inner bool) []Object {
	out := []Object{{}}
	if n.term {
		out = []Object{{{n.col, v}}}
	}
	for _, k := range n.kids {
		var part []Object
		if k.part.Array {
			a, _ := v.([]any)
			for _, e := range a {
				part = append(part, k.rows(e, true, inner)...)
			}
			if len(part) == 0 && nested && !inner {
				part = []Object{k.nullRow()}
			}
		} else {
			x, _ := step(v, k.part)
			part = k.rows(x, true, inner)
		}
		next := make([]Object, 0, len(out)*len(part))
		for _, a := range out {
			for _, b := range part {
				next = append(next, append(append(make(Object, 0, len(a)+len(b)), a...), b...))
			}
		}
		out = next
	}
	return out
}

// Flatten denormalizes v into rows. Fields are made root relative and
// expanded over v (no fields means every leaf); the row grain is the most
// deeply nested array, the given grain, or every array when cross is set;
// inner drops rows of empty arrays.
func Flatten(v any, fields []Path, grain Path, cross, inner bool) (Relation, error) {
	op, err := ResolveOperation(v, Operation{Kind: "flatten", Paths: fields, Grain: grain, Cross: cross, Inner: inner})
	if err != nil {
		return Relation{}, err
	}
	return flatten(v, op)
}

// ResolveOperation makes op explicit against its input v so the pipeline
// (and its jq) no longer depend on the data: flatten fields become leaf
// paths, project paths take document order, and defaults are filled.
// Flatten fields and grain on an array may omit its leading wildcards.
// A *GrainError asks for a grain or a cross join.
func ResolveOperation(v any, op Operation) (Operation, error) {
	switch op.Kind {
	case "flatten":
		s := InferSchema(v)
		op.Paths = ExpandPaths(v, rootRelative(s, op.Paths))
		if len(op.Grain) > 0 {
			op.Grain = rootRelative(s, []Path{op.Grain})[0]
		}
		if !op.Cross && len(op.Grain) == 0 {
			if _, ties := autoGrain(op.Paths); ties != nil {
				return op, &GrainError{ties}
			}
		}
	case "project":
		op.Paths = schemaOrder(v, op.Paths)
	}
	return normalize(op)
}

// ResolveStep resolves op as the next step after pipeline, against the
// values the pipeline's current scope gives it.
func ResolveStep(input any, pipeline []Operation, op Operation) (Operation, error) {
	v, err := ApplyPipeline(input, pipeline)
	if err != nil {
		return op, err
	}
	var scope Path
	for _, s := range pipeline {
		if s.Kind == "scope" && s.Mode == "" {
			scope = s.Path
		} else if s.Kind == "scope" {
			scope = nil
		}
	}
	if len(scope) == 0 {
		return ResolveOperation(v, op)
	}
	// Resolve over the list of scoped values, then drop the list step.
	targets := []any{selectPath(v, scope)}
	if hasWild(scope) {
		targets, _ = selectPath(v, scope).([]any)
	}
	if op.Kind == "flatten" {
		if es := InferSchema(targets).elem; es != nil {
			op.Paths = rootRelative(es, op.Paths)
			if len(op.Grain) > 0 {
				op.Grain = rootRelative(es, []Path{op.Grain})[0]
			}
		}
	}
	each := Path{{Array: true}}
	wide := op
	wide.Paths = []Path{each}
	if len(op.Paths) > 0 {
		wide.Paths = nil
	}
	for _, p := range op.Paths {
		wide.Paths = append(wide.Paths, each.concat(p))
	}
	if len(op.Grain) > 0 {
		wide.Grain = each.concat(op.Grain)
	}
	res, err := ResolveOperation(targets, wide)
	if len(op.Paths) > 0 || op.Kind == "flatten" {
		for i := range res.Paths {
			res.Paths[i] = res.Paths[i][1:]
		}
	} else {
		res.Paths = nil
	}
	if len(res.Grain) > 0 {
		res.Grain = res.Grain[1:]
	}
	if ge, ok := err.(*GrainError); ok {
		for i := range ge.Candidates {
			ge.Candidates[i] = ge.Candidates[i][1:]
		}
	}
	return res, err
}

func (p Path) concat(q Path) Path { return append(append(Path{}, p...), q...) }

func arrayToObject(v any, field Path) (any, error) {
	a, err := needArray(v, "array-object")
	if err != nil {
		return nil, err
	}
	out := Object{}
	seen := map[string]int{}
	for i, e := range a {
		k := selectPath(e, field)
		switch k.(type) {
		case string, json.Number, bool:
		default:
			return nil, fmt.Errorf("array-object: element %d has no usable %s key", i, field)
		}
		key := jqString(k)
		if j, dup := seen[key]; dup {
			return nil, fmt.Errorf("array-object: duplicate key %q (elements %d and %d)", key, j, i)
		}
		seen[key] = i
		out = append(out, Field{key, e})
	}
	return out, nil
}

func objectToArray(v any, name string) (any, error) {
	o, ok := v.(Object)
	if !ok {
		return nil, fmt.Errorf("object-array needs an object, got %s", typeName(v))
	}
	out := make([]any, 0, len(o))
	for _, f := range o {
		row := Object{{name, f.Key}}
		if inner, ok := f.Value.(Object); ok {
			if inner.index(name) >= 0 {
				return nil, fmt.Errorf("object-array: key column %q collides with a field of %q", name, f.Key)
			}
			row = append(row, inner...)
		} else if name == "value" {
			return nil, fmt.Errorf("object-array: key column \"value\" collides with the value of %q", f.Key)
		} else {
			row = append(row, Field{"value", f.Value})
		}
		out = append(out, row)
	}
	return out, nil
}

// cell is a row's value for a flat column; missing reads as null.
func cell(row any, col string) any {
	x, _ := Lookup(row, col)
	return x
}

func sortRows(a []any, col string, desc bool) []any {
	key := func(e any) any {
		if col == "" {
			return e
		}
		return cell(e, col)
	}
	out := append([]any{}, a...)
	sort.SliceStable(out, func(i, j int) bool { return jqCompare(key(out[i]), key(out[j])) < 0 })
	if desc {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

func group(v any, op Operation) (any, error) {
	a, err := needArray(v, "group")
	if err != nil {
		return nil, err
	}
	sorted := sortRows(a, op.Field, false)
	var groups [][]any
	for i, e := range sorted {
		if i == 0 || jqCompare(cell(e, op.Field), cell(sorted[i-1], op.Field)) != 0 {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], e)
	}
	if op.Mode == "object" {
		out := Object{}
		for _, g := range groups {
			k := jqString(cell(g[0], op.Field))
			if out.index(k) >= 0 {
				return nil, fmt.Errorf("group: two groups share the key %q", k)
			}
			out = append(out, Field{k, g})
		}
		return out, nil
	}
	out := make([]any, len(groups))
	for i, g := range groups {
		out[i] = Object{{op.Field, cell(g[0], op.Field)}, {op.Name, g}}
	}
	return out, nil
}

// jqString is jq's tostring: strings as they are, anything else as JSON.
func jqString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := FormatJSON(v, false)
	return string(b)
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// cells visits the non-null scalars in v, depth first.
func cells(v any, fn func(any) bool) bool {
	switch t := v.(type) {
	case nil:
		return false
	case Object:
		for _, f := range t {
			if cells(f.Value, fn) {
				return true
			}
		}
		return false
	case []any:
		for _, e := range t {
			if cells(e, fn) {
				return true
			}
		}
		return false
	}
	return fn(v)
}

func fuzzyMatch(s, q string) bool {
	qr := []rune(q)
	i := 0
	for _, r := range s {
		if i < len(qr) && qr[i] == r {
			i++
		}
	}
	return i == len(qr)
}

func filterRows(v any, op Operation) (any, error) {
	a, err := needArray(v, "filter")
	if err != nil {
		return nil, err
	}
	q := op.Query
	qn, isNum := jsonNumber(q)
	re, _ := regexp.Compile(q)
	eq := func(x any) bool {
		n, ok := x.(json.Number)
		f, _ := strconv.ParseFloat(string(n), 64)
		return jqString(x) == q || isNum && ok && f == qn
	}
	test := func(x any) bool {
		switch op.Mode {
		case "substring":
			return strings.Contains(asciiLower(jqString(x)), asciiLower(q))
		case "fuzzy":
			return fuzzyMatch(asciiLower(jqString(x)), asciiLower(q))
		case "regex":
			return re.MatchString(jqString(x))
		case "contains":
			return strings.Contains(jqString(x), q)
		case "eq", "ne":
			return eq(x)
		}
		n, ok := x.(json.Number)
		f, _ := strconv.ParseFloat(string(n), 64)
		return ok && (op.Mode == "gt" && f > qn || op.Mode == "lt" && f < qn)
	}
	out := []any{}
	for _, e := range a {
		target := e
		if op.Field != "" {
			target = cell(e, op.Field)
		}
		var keep bool
		if op.Field != "" && (op.Mode == "eq" || op.Mode == "ne") {
			keep = eq(target)
		} else {
			keep = cells(target, test)
		}
		if keep != (op.Mode == "ne") {
			out = append(out, e)
		}
	}
	return out, nil
}

// jqCompare orders values as jq does: null, false, true, numbers, strings,
// arrays, objects (by sorted keys, then values).
func jqCompare(a, b any) int {
	rank := func(v any) int {
		switch t := v.(type) {
		case nil:
			return 0
		case bool:
			if t {
				return 2
			}
			return 1
		case json.Number:
			return 3
		case string:
			return 4
		case []any:
			return 5
		}
		return 6
	}
	ra, rb := rank(a), rank(b)
	if ra != rb {
		return ra - rb
	}
	switch x := a.(type) {
	case json.Number:
		fa, _ := x.Float64()
		fb, _ := b.(json.Number).Float64()
		switch {
		case fa < fb:
			return -1
		case fa > fb:
			return 1
		}
	case string:
		return strings.Compare(x, b.(string))
	case []any:
		y := b.([]any)
		for i := 0; i < len(x) && i < len(y); i++ {
			if c := jqCompare(x[i], y[i]); c != 0 {
				return c
			}
		}
		return len(x) - len(y)
	case Object:
		y := b.(Object)
		kx, ky := sortedKeys(x), sortedKeys(y)
		if c := jqCompare(kx, ky); c != 0 {
			return c
		}
		for _, k := range kx {
			if c := jqCompare(cell(x, k.(string)), cell(y, k.(string))); c != 0 {
				return c
			}
		}
	}
	return 0
}

func sortedKeys(o Object) []any {
	ks := make([]string, len(o))
	for i, f := range o {
		ks[i] = f.Key
	}
	sort.Strings(ks)
	out := make([]any, len(ks))
	for i, k := range ks {
		out[i] = k
	}
	return out
}

// TableRelation is the shallow table of a focused value: an array's
// elements as rows (object keys as columns, other elements under "value"),
// an object's entries as key rows, or one value row.
func TableRelation(v any) Relation {
	r := Relation{Columns: []string{}}
	have := map[string]bool{}
	addCol := func(c string) {
		if !have[c] {
			have[c] = true
			r.Columns = append(r.Columns, c)
		}
	}
	var elems []any
	keyCol := ""
	switch t := v.(type) {
	case []any:
		elems = t
	case Object:
		allObj := len(t) > 0
		for _, f := range t {
			_, ok := f.Value.(Object)
			allObj = allObj && ok
		}
		keyCol = "key"
		for _, f := range t {
			if !allObj {
				elems = append(elems, Object{{"key", f.Key}, {"value", f.Value}})
				continue
			}
			if f.Value.(Object).index(keyCol) >= 0 {
				keyCol = "$key"
			}
		}
		if allObj {
			for _, f := range t {
				elems = append(elems, append(Object{{keyCol, f.Key}}, f.Value.(Object)...))
			}
		}
	default:
		elems = []any{v}
	}
	valCol := "value"
	for _, e := range elems {
		if o, ok := e.(Object); ok {
			for _, f := range o {
				addCol(f.Key)
			}
		}
	}
	for _, e := range elems {
		if _, ok := e.(Object); !ok {
			if have[valCol] {
				valCol = "$value"
			}
			addCol(valCol)
			break
		}
	}
	for _, e := range elems {
		row := make(Object, len(r.Columns))
		for i, c := range r.Columns {
			row[i] = Field{c, cell(e, c)}
		}
		if _, ok := e.(Object); !ok {
			row[len(row)-1].Value = e
		}
		r.Rows = append(r.Rows, row)
	}
	return r
}

// ExportCSV writes r as CSV with a header row. Null is an empty cell and
// objects and arrays are compact JSON.
func ExportCSV(w io.Writer, r Relation) error { return exportDelimited(w, r, ',') }

// ExportTSV writes r as tab-separated values with the CSV quoting rules.
func ExportTSV(w io.Writer, r Relation) error { return exportDelimited(w, r, '\t') }

func exportDelimited(w io.Writer, r Relation, comma rune) error {
	cw := csv.NewWriter(w)
	cw.Comma = comma
	if err := cw.Write(r.Columns); err != nil {
		return err
	}
	rec := make([]string, len(r.Columns))
	for _, row := range r.Rows {
		for i, c := range r.Columns {
			x := cell(row, c)
			if i < len(row) && row[i].Key == c {
				x = row[i].Value
			}
			rec[i] = ""
			if x != nil {
				rec[i] = jqString(x)
			}
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
