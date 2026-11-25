package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Field is one key and value of an ordered JSON object.
type Field struct {
	Key   string
	Value any
}

// Object is a JSON object that keeps its key order. Duplicate keys in the
// source keep the first position and the last value, as jq does.
type Object []Field

// MarshalJSON writes the object in key order.
func (o Object) MarshalJSON() ([]byte, error) { return FormatJSON(o, false) }

// Get returns the value of key.
func (o Object) Get(key string) (any, bool) {
	for _, f := range o {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

// set returns a copy of o with key set, appended when new.
func (o Object) set(key string, v any) Object {
	out := make(Object, len(o), len(o)+1)
	copy(out, o)
	for i := range out {
		if out[i].Key == key {
			out[i].Value = v
			return out
		}
	}
	return append(out, Field{key, v})
}

// ErrEmpty reports a document with no JSON value in it.
var ErrEmpty = errors.New("empty document")

// ParseError locates a syntax error. Line and Col are 1-based.
type ParseError struct {
	Offset, Line, Col int
	Msg               string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("invalid JSON at line %d, col %d: %s", e.Line, e.Col, e.Msg)
}

func parseErr(raw string, off int64, err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		off, err = int64(len(raw)), errors.New("incomplete JSON")
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		off = se.Offset
	}
	if off > int64(len(raw)) {
		off = int64(len(raw))
	}
	head := raw[:off]
	line := strings.Count(head, "\n") + 1
	col := utf8.RuneCountInString(head[strings.LastIndexByte(head, '\n')+1:]) + 1
	return &ParseError{Offset: int(off), Line: line, Col: col, Msg: err.Error()}
}

// ParseValues decodes every top-level JSON value in raw (one for a JSON
// document, several for JSONL or a value stream).
func ParseValues(raw string) ([]any, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var out []any
	for {
		v, err := decodeValue(dec)
		if err == io.EOF {
			if len(out) == 0 {
				return nil, ErrEmpty
			}
			return out, nil
		}
		if err != nil {
			return out, parseErr(raw, dec.InputOffset(), err)
		}
		out = append(out, v)
	}
}

// ParseJSON decodes exactly one JSON value. Anything after it is an error.
func ParseJSON(raw string) (any, error) {
	vs, err := ParseValues(raw)
	if err != nil {
		return nil, err
	}
	if len(vs) > 1 {
		dec := json.NewDecoder(strings.NewReader(raw))
		_, _ = decodeValue(dec)
		return nil, parseErr(raw, dec.InputOffset(), errors.New("unexpected data after the JSON value"))
	}
	return vs[0], nil
}

// ParseDocument decodes a JSON document or a JSONL stream. A stream of more
// than one value becomes an array of the values.
func ParseDocument(raw string) (any, error) {
	if v, err := ParseJSON(raw); err == nil {
		return v, nil
	} else if errors.Is(err, ErrEmpty) {
		return nil, err
	} else if !strings.Contains(strings.TrimSpace(raw), "\n") {
		return nil, err
	} else {
		return parseLines(raw, err)
	}
}

// parseLines reads raw as JSONL, one JSON value per nonblank line. A failure
// on the first nonblank line reports docErr, the error of reading raw as one
// multiline document. Other failures report the line's own error, located in
// raw.
func parseLines(raw string, docErr error) (any, error) {
	var out []any
	for start := 0; start < len(raw); {
		end := strings.IndexByte(raw[start:], '\n')
		next := len(raw)
		if end >= 0 {
			end += start
			next = end + 1
		} else {
			end = len(raw)
		}
		line := strings.TrimRight(raw[start:end], "\r")
		if strings.TrimSpace(line) != "" {
			v, err := ParseJSON(line)
			if err != nil {
				var pe *ParseError
				if len(out) == 0 || !errors.As(err, &pe) {
					return nil, docErr
				}
				return nil, parseErr(raw, int64(start+pe.Offset), errors.New(pe.Msg))
			}
			out = append(out, v)
		}
		start = next
	}
	return out, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '[' {
			arr := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, eofIsUnexpected(err)
				}
				arr = append(arr, v)
			}
			_, err := dec.Token()
			return arr, eofIsUnexpected(err)
		}
		obj := Object{}
		var idx map[string]int
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, eofIsUnexpected(err)
			}
			key, _ := kt.(string)
			v, err := decodeValue(dec)
			if err != nil {
				return nil, eofIsUnexpected(err)
			}
			if idx == nil {
				idx = map[string]int{}
			}
			if i, dup := idx[key]; dup {
				obj[i].Value = v
				continue
			}
			idx[key] = len(obj)
			obj = append(obj, Field{key, v})
		}
		_, err := dec.Token()
		return obj, eofIsUnexpected(err)
	}
	return tok, nil
}

func eofIsUnexpected(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// FormatJSON writes v compactly or indented by two spaces (jq style).
func FormatJSON(v any, pretty bool) ([]byte, error) {
	var b bytes.Buffer
	if err := writeJSON(&b, v, pretty, ""); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeJSON(b *bytes.Buffer, v any, pretty bool, ind string) error {
	nl := func(i string) {
		if pretty {
			b.WriteString("\n" + i)
		}
	}
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case json.Number:
		b.WriteString(string(t))
	case string:
		writeString(b, t)
	case Object:
		if len(t) == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteByte('{')
		for i, f := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			nl(ind + "  ")
			writeString(b, f.Key)
			b.WriteByte(':')
			if pretty {
				b.WriteByte(' ')
			}
			if err := writeJSON(b, f.Value, pretty, ind+"  "); err != nil {
				return err
			}
		}
		nl(ind)
		b.WriteByte('}')
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			nl(ind + "  ")
			if err := writeJSON(b, e, pretty, ind+"  "); err != nil {
				return err
			}
		}
		nl(ind)
		b.WriteByte(']')
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return err
		}
		if pretty {
			var out bytes.Buffer
			if err := json.Indent(&out, raw, ind, "  "); err == nil {
				raw = out.Bytes()
			}
		}
		b.Write(raw)
	}
	return nil
}

// writeString escapes like jq: quotes, backslash, control characters and DEL.
func writeString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range strings.ToValidUTF8(s, "�") {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}

// Lookup returns the value of key when v is an object holding it.
func Lookup(v any, key string) (any, bool) {
	if o, ok := v.(Object); ok {
		return o.Get(key)
	}
	return nil, false
}

// PathPart is one step of a Path: a key, a wildcard array step (Array) or a
// concrete array step (Indexed with Index).
type PathPart struct {
	Key     string
	Array   bool
	Index   int
	Indexed bool
}

// Path addresses values in a document. The empty path is the whole document.
type Path []PathPart

// MarshalJSON encodes p as its canonical path string (e.g. "orders[].id").
func (p Path) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}

// UnmarshalJSON decodes p from a canonical path string.
func (p *Path) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParsePath(s)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

func isIdent(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func quoteKey(k string) string {
	var b bytes.Buffer
	writeString(&b, k)
	return b.String()
}

// String is the canonical form: "$" for the root, "orders[].id",
// "$[].name", `meta["a b"]`, "items[2]".
func (p Path) String() string {
	if len(p) == 0 {
		return "$"
	}
	var b strings.Builder
	for i, s := range p {
		switch {
		case s.Array:
			b.WriteString("[]")
		case s.Indexed:
			fmt.Fprintf(&b, "[%d]", s.Index)
		case isIdent(s.Key):
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.Key)
		default:
			b.WriteString("[" + quoteKey(s.Key) + "]")
		}
	}
	out := b.String()
	if out[0] == '[' {
		out = "$" + out
	}
	return out
}

// Child returns p extended by one step, without sharing p's storage.
func (p Path) Child(s PathPart) Path {
	return append(append(Path{}, p...), s)
}

// Wild returns p with concrete indexes turned into wildcards.
func (p Path) Wild() Path {
	out := append(Path{}, p...)
	for i := range out {
		if out[i].Indexed {
			out[i] = PathPart{Array: true}
		}
	}
	return out
}

// HasPrefix reports whether q is a prefix of p.
func (p Path) HasPrefix(q Path) bool {
	return len(q) <= len(p) && p[:len(q)].String() == q.String()
}

// ParsePath reads the canonical form. A leading "$" or "." is optional, and
// bracketed keys take JSON string syntax.
func ParsePath(s string) (Path, error) {
	in := strings.TrimPrefix(strings.TrimSpace(s), "$")
	bad := func(i int, msg string) (Path, error) {
		return nil, fmt.Errorf("path %q: %s at %d", s, msg, i)
	}
	p := Path{}
	for i := 0; i < len(in); {
		switch {
		case in[i] == '[':
			if i+1 < len(in) && in[i+1] == '"' {
				dec := json.NewDecoder(strings.NewReader(in[i+1:]))
				var k string
				if err := dec.Decode(&k); err != nil {
					return bad(i, "bad quoted key")
				}
				j := i + 1 + int(dec.InputOffset())
				if j >= len(in) || in[j] != ']' {
					return bad(j, "missing ]")
				}
				p, i = append(p, PathPart{Key: k}), j+1
				continue
			}
			end := strings.IndexByte(in[i:], ']')
			if end < 0 {
				return bad(i, "missing ]")
			}
			body := strings.TrimSpace(in[i+1 : i+end])
			if n, err := strconv.Atoi(body); err == nil && n >= 0 {
				p = append(p, PathPart{Index: n, Indexed: true})
			} else if body == "" {
				p = append(p, PathPart{Array: true})
			} else {
				return bad(i, "bad index")
			}
			i += end + 1
			continue
		case in[i] == '.':
			i++
			if i == len(in) && len(p) == 0 {
				return p, nil
			}
			if i < len(in) && in[i] == '[' {
				continue
			}
		case i > 0:
			return bad(i, "expected . or [")
		}
		j := i
		for j < len(in) && in[j] != '.' && in[j] != '[' {
			j++
		}
		if j == i {
			return bad(i, "empty key")
		}
		p, i = append(p, PathPart{Key: in[i:j]}), j
	}
	return p, nil
}

// MustPath parses a path and panics on error (tests and literals).
func MustPath(s string) Path {
	p, err := ParsePath(s)
	if err != nil {
		panic(err)
	}
	return p
}

// step follows one non-wildcard part.
func step(v any, s PathPart) (any, bool) {
	if s.Indexed {
		if a, ok := v.([]any); ok && s.Index >= 0 && s.Index < len(a) {
			return a[s.Index], true
		}
		return nil, false
	}
	return Lookup(v, s.Key)
}

// AtPath returns the value at p. A wildcard step takes the first element
// for which the rest of the path exists.
func AtPath(v any, p Path) (any, bool) {
	if len(p) == 0 {
		return v, true
	}
	if p[0].Array {
		a, _ := v.([]any)
		for _, e := range a {
			if r, ok := AtPath(e, p[1:]); ok {
				return r, true
			}
		}
		return nil, false
	}
	next, ok := step(v, p[0])
	if !ok {
		return nil, false
	}
	return AtPath(next, p[1:])
}

// ReplaceAt returns a copy of v with the value at p replaced. A wildcard step
// picks the same element AtPath picks. A missing last key of an object is
// added. The input is not modified.
func ReplaceAt(v any, p Path, repl any) (any, error) {
	if len(p) == 0 {
		return repl, nil
	}
	s := p[0]
	switch t := v.(type) {
	case []any:
		i := s.Index
		if s.Array {
			i = -1
			for j, e := range t {
				if _, ok := AtPath(e, p[1:]); ok {
					i = j
					break
				}
			}
		}
		if (s.Array || s.Indexed) && i >= 0 && i < len(t) {
			e, err := ReplaceAt(t[i], p[1:], repl)
			if err != nil {
				return nil, err
			}
			out := append([]any{}, t...)
			out[i] = e
			return out, nil
		}
	case Object:
		if !s.Array && !s.Indexed {
			cur, ok := t.Get(s.Key)
			if ok || len(p) == 1 {
				e, err := ReplaceAt(cur, p[1:], repl)
				if err != nil {
					return nil, err
				}
				return t.set(s.Key, e), nil
			}
		}
	}
	return nil, fmt.Errorf("path %s not found", p)
}

// Schema is the merged shape of every value at one path. For a key child,
// Count is the number of parent objects holding the key and Total the number
// of values observed at the parent node (scalars and nulls included when the
// parent is mixed). For an element child ("[]"), Count is the number of
// elements and Total the number of arrays.
type Schema struct {
	Name     string
	Path     Path
	Type     string // object, array, string, number, bool, null or mixed
	Types    map[string]int
	Count    int
	Total    int
	Sample   any
	Children []*Schema

	objs, arrs int
	keys       map[string]*Schema
	elem       *Schema
}

// InferSchema merges the shapes of all array elements in v.
func InferSchema(v any) *Schema {
	s := &Schema{Name: "$", Path: Path{}, Total: 1}
	s.add(v)
	s.finish()
	return s
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case string:
		return "string"
	case Object:
		return "object"
	case []any:
		return "array"
	}
	return "number"
}

func (s *Schema) add(v any) {
	s.Count++
	if s.Types == nil {
		s.Types = map[string]int{}
	}
	tn := typeName(v)
	s.Types[tn]++
	switch t := v.(type) {
	case Object:
		s.objs++
		for _, f := range t {
			c := s.keys[f.Key]
			if c == nil {
				if s.keys == nil {
					s.keys = map[string]*Schema{}
				}
				c = &Schema{Name: f.Key, Path: s.Path.Child(PathPart{Key: f.Key})}
				s.keys[f.Key] = c
				s.Children = append(s.Children, c)
			}
			c.add(f.Value)
		}
	case []any:
		s.arrs++
		if s.elem == nil {
			s.elem = &Schema{Name: "[]", Path: s.Path.Child(PathPart{Array: true})}
			s.Children = append(s.Children, s.elem)
		}
		for _, e := range t {
			s.elem.add(e)
		}
	default:
		if s.Sample == nil {
			s.Sample = v
		}
	}
}

func (s *Schema) finish() {
	// "empty" is a shape never observed (the element of empty arrays),
	// distinct from an observed null.
	s.Type = "empty"
	switch len(s.Types) {
	case 0:
	case 1:
		for t := range s.Types {
			s.Type = t
		}
	default:
		s.Type = "mixed"
	}
	for _, c := range s.Children {
		c.Total = s.Count
		if c == s.elem {
			c.Total = s.arrs
		}
		c.finish()
	}
}

// Find returns the schema node for p; concrete indexes match the element node.
func (s *Schema) Find(p Path) *Schema {
	for _, part := range p {
		if s == nil {
			return nil
		}
		if part.Array || part.Indexed {
			s = s.elem
		} else {
			s = s.keys[part.Key]
		}
	}
	return s
}

// Walk visits s and its descendants depth first.
func (s *Schema) Walk(fn func(*Schema)) {
	fn(s)
	for _, c := range s.Children {
		c.Walk(fn)
	}
}

// ExpandPaths turns each path into the leaf paths under it in v: objects
// become their fields, arrays their elements, and an array with no elements
// its wildcard (rows with empty cells). Paths not in v stay as they are. No paths means the root.
func ExpandPaths(v any, paths []Path) []Path {
	if len(paths) == 0 {
		paths = []Path{{}}
	}
	s := InferSchema(v)
	var out []Path
	seen := map[string]bool{}
	add := func(p Path) {
		if k := p.String(); !seen[k] {
			seen[k] = true
			out = append(out, p)
		}
	}
	var leaves func(n *Schema, p Path)
	leaves = func(n *Schema, p Path) {
		if len(n.Children) == 0 {
			add(p)
			return
		}
		for _, c := range n.Children {
			part := PathPart{Key: c.Name}
			if c == n.elem {
				part = PathPart{Array: true}
			}
			leaves(c, p.Child(part))
		}
	}
	for _, p := range paths {
		if n := s.Find(p); n != nil {
			leaves(n, append(Path{}, p...))
		} else {
			add(p)
		}
	}
	return out
}

// schemaOrder ranks paths by their first appearance in the document.
func schemaOrder(v any, paths []Path) []Path {
	rank := map[string]int{}
	InferSchema(v).Walk(func(n *Schema) { rank[n.Path.String()] = len(rank) })
	out := append([]Path{}, paths...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, ok1 := rank[out[i].Wild().String()]
		rj, ok2 := rank[out[j].Wild().String()]
		return ok1 && (!ok2 || ri < rj)
	})
	return out
}
