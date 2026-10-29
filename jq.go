package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/itchyny/gojq"
)

// The jq engine: gojq (github.com/itchyny/gojq, pure Go) runs a query
// against a document. gojq reads objects as Go maps, so its own output
// sorts object keys and cannot say where a value came from. A query that
// is a path expression is therefore run as path(Q) first: its results are
// paths, resolved to the document's own nodes, so key order and exact
// paths are kept (the data pane, the CLI). Only a query that builds new
// values (an object, arithmetic, map) shows gojq's output, keys sorted.

// maxJQResults caps the results one run collects (range(infinite) stops).
const maxJQResults = 10000

// jqSlot is an empty argument slot a function from the picker lays out
// (query.go): the query does not run until every slot is filled.
const jqSlot = '□'

// jqOut is one run's answer.
type jqOut struct {
	Query  string
	Values []*Node
	// Paths reports results that are the document's own nodes (path(Q)
	// resolved), in document order with exact paths.
	Paths bool
	// Sorted reports gojq's own output holding an object: its keys are
	// sorted, not in document order.
	Sorted bool
	// Reordered reports path results out of document order: as the
	// query asks (.b, .a), or gojq iterating an object (.[], .., paths)
	// in sorted key order where jq keeps the document's.
	Reordered bool
	Cut       bool // more than maxJQResults results; the first are kept
	Err       error
	// Pos is the rune offset in Query the error points at, -1 for none.
	Pos int
}

// queryDoc runs a query against a document root. An empty query is the
// document itself. It never panics: every failure is Err, with a position
// when the parser or the compiler gives one.
func queryDoc(ctx context.Context, root *Node, query string) (out jqOut) {
	out = jqOut{Query: query, Pos: -1}
	defer func() {
		if p := recover(); p != nil {
			out.Values, out.Err = nil, fmt.Errorf("jq failed: %v", p)
		}
	}()
	if strings.TrimSpace(query) == "" {
		out.Values, out.Paths = []*Node{root}, true
		return out
	}
	if i := strings.IndexRune(query, jqSlot); i >= 0 {
		out.Err, out.Pos = errors.New("an empty argument to fill"), len([]rune(query[:i]))
		return out
	}
	code, err := compileJQ(query)
	if err != nil {
		out.Err, out.Pos = err, errorPos(query, err)
		return out
	}
	input := jqValue(root)
	if paths, ok := runPaths(ctx, query, input); ok {
		nodes := make([]*Node, 0, len(paths))
		for _, p := range paths {
			n := nodeAtPath(root, p)
			if n == nil {
				break
			}
			nodes = append(nodes, n)
		}
		if len(nodes) == len(paths) {
			out.Values, out.Paths, out.Cut = nodes, true, len(paths) == maxJQResults
			for i := 1; i < len(nodes) && !out.Reordered; i++ {
				out.Reordered = docOrder(nodes[i], nodes[i-1]) < 0
			}
			return out
		}
	}
	iter := code.RunWithContext(ctx, input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			var halt *gojq.HaltError
			if errors.As(err, &halt) && halt.Value() == nil {
				break
			}
			out.Err = err
			return out
		}
		n, err := nodeOf(v)
		if err != nil {
			out.Err = err
			return out
		}
		if n.Kind == KObject || (n.Kind == KArray && hasObject(n)) {
			out.Sorted = true
		}
		out.Values = append(out.Values, n)
		if len(out.Values) == maxJQResults {
			out.Cut = true
			break
		}
	}
	return out
}

func compileJQ(query string) (*gojq.Code, error) {
	q, err := gojq.Parse(query)
	if err != nil {
		return nil, err
	}
	return gojq.Compile(q)
}

// runPaths runs path(Q); ok is false when Q is not a path expression (or
// fails), and the query is then run for its values.
func runPaths(ctx context.Context, query string, input any) ([][]any, bool) {
	code, err := compileJQ("path(" + query + ")")
	if err != nil {
		return nil, false
	}
	var out [][]any
	iter := code.RunWithContext(ctx, input)
	for {
		v, ok := iter.Next()
		if !ok {
			return out, true
		}
		p, ok := v.([]any)
		if !ok {
			return nil, false
		}
		out = append(out, p)
		if len(out) == maxJQResults {
			return out, true
		}
	}
}

// nodeAtPath is the document's node at a jq path ([]any of keys and
// indexes), or nil when it names nothing there (a missing key, a slice).
func nodeAtPath(root *Node, path []any) *Node {
	n := root
	for _, step := range path {
		switch s := step.(type) {
		case string:
			n = n.Field(s)
		case int:
			if n.Kind != KArray {
				return nil
			}
			if s < 0 {
				s += len(n.Children)
			}
			if s < 0 || s >= len(n.Children) {
				return nil
			}
			n = n.Children[s]
		default:
			return nil
		}
		if n == nil {
			return nil
		}
	}
	return n
}

// docOrder compares two nodes of one document in document order
// (preorder): negative when x comes first, 0 for the same node.
func docOrder(x, y *Node) int {
	trail := func(n *Node) []int {
		var out []int
		for at := n; at.Parent != nil; at = at.Parent {
			out = append(out, at.Index)
		}
		slices.Reverse(out)
		return out
	}
	return slices.Compare(trail(x), trail(y))
}

// errorPos is the rune offset an error points at in the query, -1 for
// none: the parser's offset, or a function the compiler does not know.
func errorPos(query string, err error) int {
	var pe *gojq.ParseError
	if errors.As(err, &pe) {
		off := min(max(0, pe.Offset-len(pe.Token)), len(query))
		return len([]rune(query[:off]))
	}
	const nf = "function not defined: "
	if msg := err.Error(); strings.HasPrefix(msg, nf) {
		name, _, _ := strings.Cut(strings.TrimPrefix(msg, nf), "/")
		if i := strings.Index(query, name); i >= 0 {
			return len([]rune(query[:i]))
		}
	}
	return -1
}

// jqValue is a node as gojq reads a value: maps, slices, numbers as int,
// *big.Int or float64.
func jqValue(n *Node) any {
	switch n.Kind {
	case KObject:
		m := make(map[string]any, len(n.Children))
		for _, c := range n.Children {
			m[c.Key] = jqValue(c)
		}
		return m
	case KArray:
		s := make([]any, len(n.Children))
		for i, c := range n.Children {
			s[i] = jqValue(c)
		}
		return s
	case KString:
		return n.Value
	case KNumber:
		if i, err := strconv.Atoi(n.Value); err == nil {
			return i
		}
		if !strings.ContainsAny(n.Value, ".eE") {
			if b, ok := new(big.Int).SetString(n.Value, 10); ok {
				return b
			}
		}
		f, _ := strconv.ParseFloat(n.Value, 64)
		return f
	case KBool:
		return n.Value == "true"
	}
	return nil
}

// nodeOf is one of gojq's values as a node of its own document.
func nodeOf(v any) (*Node, error) {
	data, err := gojq.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Build(data)
}

func hasObject(n *Node) bool {
	for _, c := range n.Children {
		if c.Kind == KObject || (c.Kind == KArray && hasObject(c)) {
			return true
		}
	}
	return false
}

// jqFunc is one builtin the function picker offers: its name and arity.
type jqFunc struct {
	Name  string
	Arity int
}

func (f jqFunc) String() string { return f.Name + "/" + strconv.Itoa(f.Arity) }

var (
	builtinsOnce sync.Once
	builtinList  []jqFunc
)

// jqBuiltins are gojq's builtins (jq's builtins/0, so the list follows
// the engine when the dependency is bumped), by name then arity.
func jqBuiltins() []jqFunc {
	builtinsOnce.Do(func() {
		code, err := compileJQ("builtins")
		if err != nil {
			return
		}
		iter := code.Run(nil)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			s, _ := v.(string)
			name, arity, ok := strings.Cut(s, "/")
			n, err := strconv.Atoi(arity)
			if !ok || err != nil || strings.HasPrefix(name, "_") {
				continue
			}
			builtinList = append(builtinList, jqFunc{Name: name, Arity: n})
		}
		slices.SortFunc(builtinList, func(a, b jqFunc) int {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return a.Arity - b.Arity
		})
		builtinList = slices.Compact(builtinList)
	})
	return builtinList
}
