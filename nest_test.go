package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// nest.json is {"a": 1, "x": {"b": 2, "c": 3}, "y": 4, "z": 5}.
const nestDoc = "testdata/nest.json"

// openNest opens a document's restructure page with paths sent and the
// focus on the restructured tree.
func openNest(t *testing.T, path string, sends ...string) *App {
	t.Helper()
	a := open(t, path, 120, 30)
	press(t, a, "M")
	sendPaths(t, a, sends...)
	press(t, a, "tab")
	return a
}

// output is the restructured document, compact.
func output(t *testing.T, a *App) string {
	t.Helper()
	p, err := a.plan()
	if err != nil {
		t.Fatalf("plan: %v (%v)", err, entries(a.Mapping()))
	}
	return Compact(p.Apply(a.Doc.Root), 0)
}

// on selects the restructure node that came from a shape.
func (a *App) on(t *testing.T, path string) {
	t.Helper()
	selectIn(t, a, a.rs.tree, a.fromPath(path))
}

// divider is the column the right panel starts at.
func divider(t *testing.T, a *App) int {
	t.Helper()
	for _, ln := range strings.Split(view(a), "\n") {
		if first := strings.Index(ln, "╭"); first >= 0 {
			if second := strings.Index(ln[first+1:], "╭"); second >= 0 {
				return len([]rune(ln[:first+1+second]))
			}
		}
	}
	t.Fatalf("no split\n%s", view(a))
	return 0
}

// F1: S wraps a node beside a sibling named x (the wrap's probe used the
// key "x", which collided, so the key went on to the divider).
func TestWrapBesideASiblingNamedX(t *testing.T) {
	a := openNest(t, nestDoc, ".a", ".x")
	a.on(t, ".a")
	if !a.available("node", "wrap") {
		t.Fatal("node.wrap not available on a beside x")
	}
	start := divider(t, a)
	press(t, a, "S")
	placeholderSelected(t, a, "new-key")
	press(t, a, "esc")
	if got := output(t, a); got != `{"new-key":{"a":1},"x":{"b":2,"c":3}}` {
		t.Fatalf("output %s", got)
	}
	if divider(t, a) != start {
		t.Fatal("S moved the divider")
	}
}

// F2: dedenting the last child leaves its parent an empty object, not a
// copy of the parent's original value; the origin stays in the from column.
func TestDedentLastChildLeavesEmptyObject(t *testing.T) {
	a := openNest(t, nestDoc, ".a", ".x")
	a.on(t, ".x.c")
	press(t, a, "<", "<")
	a.on(t, ".x.b")
	press(t, a, "<", "<")
	if got := output(t, a); got != `{"a":1,"c":3,"b":2,"x":{}}` {
		t.Fatalf("output %s", got)
	}
	if row := line(view(a), "  x "); !strings.Contains(row, "object") || !strings.Contains(row, ".x") {
		t.Fatalf("x row %q", row)
	}
	agreesWith(t, a, nestDoc)
	press(t, a, "u", "u")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3}}` {
		t.Fatalf("undo: %s", got)
	}
}

// F3: << lands just before the old parent, repeats stack before it in
// order.
func TestDedentLandsBeforeParent(t *testing.T) {
	a := openNest(t, nestDoc, ".a", ".x")
	a.on(t, ".x.c")
	press(t, a, "<", "<")
	if n, _ := a.rs.tree.Selected(); a.rs.meta[n.ID].from != ".x.c" {
		t.Fatalf("cursor on %q", a.rs.meta[n.ID].from)
	}
	a.on(t, ".x.b")
	press(t, a, "<", "<")
	var order []string
	for _, n := range a.rs.tree.Nodes() {
		order = append(order, n.Label)
	}
	if !slices.Equal(order, []string{".", "a", "c", "b", "x"}) {
		t.Fatalf("order %v", order)
	}
	// On the original: b lands before x, c stays in it.
	a = openNest(t, nestDoc, ".a", ".x")
	a.on(t, ".x.b")
	press(t, a, "<", "<")
	if got := output(t, a); got != `{"a":1,"b":2,"x":{"c":3}}` {
		t.Fatalf("output %s", got)
	}
}

// F4: g > nests into the object below as its first key; > > into the one
// above as its last, skipping plain siblings between; where there is
// none, the keys do nothing.
func TestIndentIntoNextObject(t *testing.T) {
	a := openNest(t, nestDoc, ".a", ".x", ".y", ".z")
	a.on(t, ".a")
	mapped := entries(a.Mapping())
	press(t, a, ">", ">")
	if got := entries(a.Mapping()); !slices.Equal(got, mapped) || a.m.Status != "" {
		t.Fatalf("> > with nothing above: %v (status %q)", got, a.m.Status)
	}
	press(t, a, "g", ">")
	if got := output(t, a); got != `{"x":{"a":1,"b":2,"c":3},"y":4,"z":5}` {
		t.Fatalf("g >: %s", got)
	}
	if n, _ := a.rs.tree.Selected(); a.rs.meta[n.ID].from != ".a" || a.r.Focus != n.ID {
		t.Fatalf("cursor on %q (focus %q)", a.rs.meta[n.ID].from, a.r.Focus)
	}
	press(t, a, "u")
	a.on(t, ".z")
	press(t, a, ">", ">")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3,"z":5},"y":4}` {
		t.Fatalf("> > past y: %s", got)
	}
	agreesWith(t, a, nestDoc)
}

// > j and > k are directions to the target object, not ranges: > j nests
// the node into the nearest object below (plain values skipped) as its
// first key, > k into the nearest above as its last; with none that way
// the key does nothing (it never falls through to >).
func TestIndentDirections(t *testing.T) {
	doc := filepath.Join(t.TempDir(), "abc.json")
	if err := os.WriteFile(doc, []byte(`{"a":1,"b":2,"c":{"d":33}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := openNest(t, doc, ".a", ".b", ".c")
	a.on(t, ".a")
	press(t, a, ">", "j")
	if got := output(t, a); got != `{"b":2,"c":{"a":1,"d":33}}` {
		t.Fatalf("> j on a: %s", got)
	}
	press(t, a, "u")
	if got := output(t, a); got != `{"a":1,"b":2,"c":{"d":33}}` {
		t.Fatalf("one undo step: %s", got)
	}
	a.on(t, ".b")
	press(t, a, ">", "k")
	if got := output(t, a); got != `{"a":1,"b":2,"c":{"d":33}}` || a.r.Mode.Name != "normal" {
		t.Fatalf("> k with no object above: %s (mode %q)", got, a.r.Mode.Name)
	}

	a = openNest(t, nestDoc, ".a", ".x", ".y", ".z")
	a.on(t, ".z")
	press(t, a, ">", "k")
	// z's nearest object above is x, past the plain y.
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3,"z":5},"y":4}` {
		t.Fatalf("> k on z: %s", got)
	}
	press(t, a, "u")
	a.on(t, ".a")
	press(t, a, ">", "j")
	if got := output(t, a); got != `{"x":{"a":1,"b":2,"c":3},"y":4,"z":5}` {
		t.Fatalf("> j on a: %s", got)
	}
	press(t, a, "u")
	a.on(t, ".y")
	press(t, a, ">", "j")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3},"y":4,"z":5}` || a.r.Mode.Name != "normal" {
		t.Fatalf("> j with no object below: %s (mode %q)", got, a.r.Mode.Name)
	}
	press(t, a, ">", ">")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3,"y":4},"z":5}` {
		t.Fatalf("> > (as > k): %s", got)
	}
}

// V enters visual-line, where > and < act on every selected node as one
// undo step and end the selection.
func TestVisualNestsAndDedentsSeveral(t *testing.T) {
	a := openNest(t, nestDoc, ".a", ".x", ".y", ".z")
	a.on(t, ".y")
	press(t, a, "V", "j", ">")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3,"y":4,"z":5}}` {
		t.Fatalf("V j >: %s", got)
	}
	press(t, a, "u")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3},"y":4,"z":5}` {
		t.Fatalf("one undo step: %s", got)
	}
	a.on(t, ".x.b")
	press(t, a, "V", "j", "<")
	if got := output(t, a); got != `{"a":1,"b":2,"c":3,"x":{},"y":4,"z":5}` {
		t.Fatalf("V j <: %s", got)
	}
}

// F5: every node row that would answer has its hint, and no other does.
func TestRestructureHintsFollowAvailability(t *testing.T) {
	a := openNest(t, nestDoc, ".a", ".x", ".y")
	for _, path := range []string{".a", ".x", ".x.b", ".x.c", ".y"} {
		a.on(t, path)
		hinted := map[string]bool{}
		for _, b := range ochazuke.HintBindings(a.r, a.r.Focus) {
			hinted[b.Keys] = true
		}
		for _, b := range a.r.ListBindings(a.r.Focus) {
			if b.Request.Noun != "node" || b.Hidden {
				continue
			}
			if b.Hint == "" {
				t.Errorf("%s: row %s has no hint", path, b.Keys)
			}
			if avail := a.r.Available(b.Request, a.r.Focus, a.r.Mode); avail != hinted[b.Keys] {
				t.Errorf("%s: %s available %v, hinted %v", path, b.Keys, avail, hinted[b.Keys])
			}
		}
	}
	a.on(t, ".x.c")
	if hints := ochazuke.BindingHints(a.r, a.r.Focus); !strings.Contains(hints, "< < out") || !strings.Contains(hints, "S wrap") {
		t.Fatalf("hints on c: %s", hints)
	}
	a.on(t, ".y")
	if hints := ochazuke.BindingHints(a.r, a.r.Focus); !strings.Contains(hints, "> k nest above") || strings.Contains(hints, "> > nest") {
		t.Fatalf("hints on y: %s", hints)
	}
}

// F6: on the restructured tree > and < are prefixes that do nothing alone
// (never the divider); ctrl+w > < = move it there, counts work, and > <
// still move it on the original tree.
func TestNodePrefixNeverResizes(t *testing.T) {
	a := openNest(t, "testdata/sample.json", ".items[].id")
	start := divider(t, a)
	selectIn(t, a, a.rs.tree, func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].item })
	for _, keys := range [][]string{{">", "esc"}, {"<", "esc"}, {">", ">"}, {"<", "<"}, {">", "x"}} {
		press(t, a, keys...)
		if got := divider(t, a); got != start {
			t.Fatalf("%v moved the divider: %d, was %d", keys, got, start)
		}
	}
	onRoot(t, a)
	press(t, a, ">", "<", "<")
	if got := divider(t, a); got != start {
		t.Fatalf("on the root: %d, was %d", got, start)
	}
	press(t, a, "ctrl+w", ">")
	one := divider(t, a)
	if one <= start {
		t.Fatalf("ctrl+w >: %d, was %d", one, start)
	}
	press(t, a, "2", "ctrl+w", ">")
	if got := divider(t, a); got-one < 2*(one-start)-1 {
		t.Fatalf("2 ctrl+w >: %d after %d", got, one)
	}
	press(t, a, "ctrl+w", "=")
	if got := divider(t, a); got != start {
		t.Fatalf("ctrl+w =: %d, want %d", got, start)
	}
	press(t, a, "ctrl+w", "<")
	if got := divider(t, a); got >= start {
		t.Fatalf("ctrl+w <: %d, was %d", got, start)
	}
	press(t, a, "ctrl+w", "=")
	// The original tree keeps > and <.
	if err := a.rs.orig.Focus(); err != nil {
		t.Fatal(err)
	}
	press(t, a, ">")
	if got := divider(t, a); got <= start {
		t.Fatalf("> on the original: %d, was %d", got, start)
	}
}

// F7: t on the root holding keys makes [ {keys} ] and back, one undo step
// each, the cursor on the root.
func TestRootWithKeysBecomesArrayOfItself(t *testing.T) {
	a := openNest(t, nestDoc, ".a", ".x")
	start := entries(a.Mapping())
	onRoot(t, a)
	press(t, a, "t")
	if got := entries(a.Mapping()); len(got) == 0 || got[0] != ".>.[]" {
		t.Fatalf("mapping %v", got)
	}
	if got := output(t, a); got != `[{"a":1,"x":{"b":2,"c":3}}]` {
		t.Fatalf("output %s", got)
	}
	agreesWith(t, a, nestDoc)
	if a.r.Focus != rsRootID {
		t.Fatalf("focus %q", a.r.Focus)
	}
	press(t, a, "t")
	if got := entries(a.Mapping()); !slices.Equal(got, start) {
		t.Fatalf("back: %v", got)
	}
	press(t, a, "u")
	if got := output(t, a); got != `[{"a":1,"x":{"b":2,"c":3}}]` {
		t.Fatalf("undo: %s", got)
	}
}

// F8: an array root over the document's own array becomes an object
// holding it under one new key, its placeholder opened selected.
func TestTopArrayRootBecomesObject(t *testing.T) {
	a := openNest(t, "testdata/top-array.json", "[]")
	onRoot(t, a)
	if !a.available("node", "object") {
		t.Fatal("node.object not available on the array root")
	}
	press(t, a, "t")
	placeholderSelected(t, a, "new-key")
	press(t, a, "esc")
	if got := output(t, a); got != `{"new-key":[{"id":1,"n":"p"},{"id":2,"n":"q"}]}` {
		t.Fatalf("output %s", got)
	}
	agreesWith(t, a, "testdata/top-array.json")
	press(t, a, "u")
	if got := output(t, a); got != `[{"id":1,"n":"p"},{"id":2,"n":"q"}]` {
		t.Fatalf("undo: %s", got)
	}
}
