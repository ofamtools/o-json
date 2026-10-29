package main

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Structure inference: one pass over the document builds a Shape per
// path. A path through an array uses "[]" for every item, so the items of
// .items share one shape, .items[], whose fields are the union of the
// items' keys. Each shape records the types it saw and how many values
// had it; a field is optional when fewer objects have it than there are
// objects at its parent. Fields are ordered by how many objects have them,
// most first, ties in the order they first appear, so optional fields come
// after the shared ones.

// Shape is what the values at one path look like.
type Shape struct {
	Key   string       `json:"key"`  // field name; "[]" for array items; "" for the root
	Path  string       `json:"path"` // ".items[].owner"
	Count int          `json:"count"`
	Of    int          `json:"of,omitempty"` // a field: the objects at its parent
	Kinds map[Kind]int `json:"types"`
	// Arrays among the values: the fewest and most items.
	MinItems int  `json:"min_items,omitempty"`
	MaxItems int  `json:"max_items,omitempty"`
	Optional bool `json:"optional,omitempty"`
	// Duplicates is set on a field some objects repeat.
	Duplicates *Duplicates `json:"duplicates,omitempty"`
	Children   []*Shape    `json:"children,omitempty"`

	parent *Shape
	values []*Node
	fields map[string]*Shape
	items  *Shape
	first  int // order of first appearance among the parent's fields
}

// Duplicates counts the objects that repeat a field's key. o-json reads
// the last value, as jq and every parser that builds a map do.
type Duplicates struct {
	Objects int `json:"objects"` // objects that repeat the key
	Most    int `json:"most"`    // the most times one object has it
}

// DuplicateNote is the warning a duplicated field carries, "" for none:
// `duplicate key "id" (2): jq keeps the last`.
func (s *Shape) DuplicateNote() string {
	if s.Duplicates == nil {
		return ""
	}
	return fmt.Sprintf("duplicate key %s (%d): jq keeps the last", quote(s.Key), s.Duplicates.Most)
}

// Values are the nodes at this path, in document order.
func (s *Shape) Values() []*Node { return s.values }

// Parent is the shape one step up, nil for the root.
func (s *Shape) Parent() *Shape { return s.parent }

// IsField reports a shape for an object key (not the root or "[]").
func (s *Shape) IsField() bool { return s.parent != nil && s.Key != "[]" }

// Types is the kinds seen, in kindOrder, as "string|null".
func (s *Shape) Types() string {
	var out []string
	for _, k := range kindOrder {
		if s.Kinds[k] > 0 {
			out = append(out, string(k))
		}
	}
	return strings.Join(out, "|")
}

// Presence is "7/9" for a field, else the count of values.
func (s *Shape) Presence() string {
	if s.IsField() {
		return fmt.Sprintf("%d/%d", s.Count, s.Of)
	}
	return strconv.Itoa(s.Count)
}

// Note is the extras a line of the structure shows: "optional" and an
// array's item count range.
func (s *Shape) Note() string {
	var parts []string
	if s.Optional {
		parts = append(parts, "optional")
	}
	if s.Duplicates != nil {
		parts = append(parts, fmt.Sprintf("duplicate key (%d)", s.Duplicates.Most))
	}
	if s.Kinds[KArray] > 0 {
		if s.MinItems == s.MaxItems {
			parts = append(parts, fmt.Sprintf("%d items", s.MaxItems))
		} else {
			parts = append(parts, fmt.Sprintf("%d..%d items", s.MinItems, s.MaxItems))
		}
	}
	return strings.Join(parts, ", ")
}

// Infer builds the shape tree of a document.
func Infer(root *Node) *Shape {
	s := &Shape{Path: ".", Kinds: map[Kind]int{}}
	s.add(root)
	s.finish()
	return s
}

func (s *Shape) add(n *Node) {
	s.Count++
	s.Kinds[n.Kind]++
	s.values = append(s.values, n)
	switch n.Kind {
	case KObject:
		// The parser already read a repeated key as jq does (one child,
		// the last value), so a key counts once per object and the shape
		// takes the value jq would; the child's Repeats feeds the
		// field's Duplicates.
		for _, c := range n.Children {
			f := s.fields[c.Key]
			if f == nil {
				if s.fields == nil {
					s.fields = map[string]*Shape{}
				}
				f = &Shape{Key: c.Key, Path: joinPath(s.Path, keyStep(c.Key)), Kinds: map[Kind]int{}, parent: s, first: len(s.fields)}
				s.fields[c.Key] = f
			}
			if c.Repeats > 1 {
				if f.Duplicates == nil {
					f.Duplicates = &Duplicates{}
				}
				f.Duplicates.Objects++
				f.Duplicates.Most = max(f.Duplicates.Most, c.Repeats)
			}
			f.add(c)
		}
	case KArray:
		k := len(n.Children)
		if s.Kinds[KArray] == 1 {
			s.MinItems, s.MaxItems = k, k
		}
		s.MinItems, s.MaxItems = min(s.MinItems, k), max(s.MaxItems, k)
		if k > 0 && s.items == nil {
			s.items = &Shape{Key: "[]", Path: joinPath(s.Path, "[]"), Kinds: map[Kind]int{}, parent: s}
		}
		for _, c := range n.Children {
			s.items.add(c)
		}
	}
}

// joinPath appends a step to a path ("." is the root).
func joinPath(path, step string) string {
	if path == "." {
		return step
	}
	return path + step
}

// finish orders the fields and marks the optional ones, depth first.
func (s *Shape) finish() {
	objects := s.Kinds[KObject]
	fields := make([]*Shape, 0, len(s.fields))
	for _, f := range s.fields {
		f.Of = objects
		f.Optional = f.Count < objects
		fields = append(fields, f)
	}
	slices.SortFunc(fields, byPresence)
	s.Children = fields
	if s.items != nil {
		s.Children = append(s.Children, s.items)
	}
	for _, c := range s.Children {
		c.finish()
	}
}

// byPresence orders fields by count, most first, then by first appearance.
func byPresence(a, b *Shape) int {
	if a.Count != b.Count {
		return b.Count - a.Count
	}
	return a.first - b.first
}

// Walk visits the shape and its descendants in order with their depth.
func (s *Shape) Walk(fn func(*Shape, int)) {
	var walk func(*Shape, int)
	walk = func(at *Shape, depth int) {
		fn(at, depth)
		for _, c := range at.Children {
			walk(c, depth+1)
		}
	}
	walk(s, 0)
}

// Find is the shape at a structure path (".items[].owner"), or nil.
func (s *Shape) Find(path string) *Shape {
	var found *Shape
	s.Walk(func(at *Shape, _ int) {
		if found == nil && at.Path == path {
			found = at
		}
	})
	return found
}

// Label is how the structure names a shape: its key, "." for the root.
func (s *Shape) Label() string {
	switch {
	case s.parent == nil:
		return "."
	case s.Key == "[]":
		return "[]"
	case identRe.MatchString(s.Key):
		return s.Key
	}
	return quote(s.Key)
}

// WriteStructure prints the shape as an indented tree: name, types,
// presence, and the notes, in aligned columns.
func WriteStructure(w io.Writer, s *Shape) error {
	type line struct{ name, types, presence, note string }
	var lines []line
	nameW, typeW, presW := 0, 0, 0
	s.Walk(func(at *Shape, depth int) {
		l := line{strings.Repeat("  ", depth) + at.Label(), at.Types(), at.Presence(), at.Note()}
		nameW, typeW, presW = max(nameW, len([]rune(l.name))), max(typeW, len(l.types)), max(presW, len(l.presence))
		lines = append(lines, l)
	})
	var b strings.Builder
	for _, l := range lines {
		row := fmt.Sprintf("%-*s  %-*s  %*s  %s", nameW, l.name, typeW, l.types, presW, l.presence, l.note)
		b.WriteString(strings.TrimRight(row, " ") + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}
