package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// openPut is the nest document's restructure with a, x (b, c) and y sent:
// {"a":1,"x":{"b":2,"c":3},"y":4}, the focus on the restructured tree.
func openPut(t *testing.T) *App {
	t.Helper()
	a := openNest(t, nestDoc, ".a", ".x", ".y")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3},"y":4}` {
		t.Fatalf("start %s", got)
	}
	return a
}

// idOf is the id of the restructure node that came from path (not an
// item); with several, the first in tree order.
func (a *App) idOf(t *testing.T, path string) string {
	t.Helper()
	for _, n := range a.rs.tree.Nodes() {
		if a.fromPath(path)(n) {
			return n.ID
		}
	}
	t.Fatalf("no node from %s: %v", path, entries(a.Mapping()))
	return ""
}

// dd then p is a move: the node comes back with its id, its origin and
// its subtree, and the mapping is what o-json restructure --map runs (and
// jq, where it is on PATH).
func TestDeletePutMovesTheNode(t *testing.T) {
	a := openPut(t)
	x, b := a.idOf(t, ".x"), a.idOf(t, ".x.b")
	a.on(t, ".x")
	press(t, a, "d", "d")
	if got := output(t, a); got != `{"a":1,"y":4}` {
		t.Fatalf("delete %s", got)
	}
	if !strings.Contains(a.m.Info, "into the register") {
		t.Fatalf("info %q", a.m.Info)
	}
	a.on(t, ".y")
	press(t, a, "p")
	if got := agreesWith(t, a, nestDoc); got != `{"a":1,"y":4,"x":{"b":2,"c":3}}` {
		t.Fatalf("move %s", got)
	}
	if a.idOf(t, ".x") != x || a.idOf(t, ".x.b") != b {
		t.Fatalf("ids changed: %s %s, want %s %s", a.idOf(t, ".x"), a.idOf(t, ".x.b"), x, b)
	}
	if n, _ := a.rs.tree.Selected(); n.ID != x {
		t.Fatalf("cursor on %s, want the put node %s", n.ID, x)
	}
	// The same register again is a copy: the ids are in the tree now.
	press(t, a, "p")
	if got := output(t, a); got != `{"a":1,"y":4,"x":{"x":{"b":2,"c":3},"b":2,"c":3}}` {
		t.Fatalf("second put %s", got)
	}
}

// yy then p is a copy: the same shape mapped again under new ids; a key
// that lands beside a sibling of its name is numbered, as placeholders are.
func TestYankPutCopies(t *testing.T) {
	a := openPut(t)
	orig := a.idOf(t, ".a")
	a.on(t, ".a")
	press(t, a, "y", "y")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3},"y":4}` {
		t.Fatalf("yank changed the tree: %s", got)
	}
	a.on(t, ".y")
	press(t, a, "p")
	if got := agreesWith(t, a, nestDoc); got != `{"a":1,"x":{"b":2,"c":3},"y":4,"a-2":1}` {
		t.Fatalf("copy %s", got)
	}
	var froms []string
	for _, n := range a.rs.tree.Nodes() {
		if a.rs.meta[n.ID].from == ".a" {
			froms = append(froms, n.ID)
		}
	}
	if len(froms) != 2 || froms[0] != orig || froms[1] == orig {
		t.Fatalf("nodes from .a: %v (original %s)", froms, orig)
	}
	if got := entries(a.Mapping()); !slices.Contains(got, ".a>.a") || !slices.Contains(got, `.a>.["a-2"]`) {
		t.Fatalf("mapping %v", got)
	}
}

// A put takes the level of where it lands, for every cursor case.
func TestPutTakesTheLevelOfWhereItLands(t *testing.T) {
	cases := []struct {
		name     string
		cut, on  string // the shape deleted, the shape the cursor lands on ("" for the root)
		collapse bool
		key      string
		want     string
	}{
		{"plain p: next sibling", ".y", ".a", false, "p", `{"a":1,"y":4,"x":{"b":2,"c":3}}`},
		{"plain P: previous sibling", ".y", ".a", false, "P", `{"y":4,"a":1,"x":{"b":2,"c":3}}`},
		{"expanded object p: first child", ".y", ".x", false, "p", `{"a":1,"x":{"y":4,"b":2,"c":3}}`},
		{"expanded object P: previous sibling", ".y", ".x", false, "P", `{"a":1,"y":4,"x":{"b":2,"c":3}}`},
		{"collapsed object p: after the whole object", ".a", ".x", true, "p", `{"x":{"b":2,"c":3},"a":1,"y":4}`},
		{"collapsed object P: previous sibling", ".y", ".x", true, "P", `{"a":1,"y":4,"x":{"b":2,"c":3}}`},
		{"root p: first child", ".y", "", false, "p", `{"y":4,"a":1,"x":{"b":2,"c":3}}`},
		{"root P: first child too", ".y", "", false, "P", `{"y":4,"a":1,"x":{"b":2,"c":3}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel() // each case opens its own viewer
			a := openPut(t)
			a.on(t, tc.cut)
			press(t, a, "d", "d")
			if tc.on == "" {
				onRoot(t, a)
			} else {
				a.on(t, tc.on)
			}
			if tc.collapse {
				press(t, a, "h")
				if !a.rs.tree.Collapsed(a.idOf(t, tc.on)) {
					t.Fatal("h did not collapse")
				}
			}
			press(t, a, tc.key)
			if got := agreesWith(t, a, nestDoc); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

// "a d d deletes into register a; "a p puts from it, while p puts the
// unnamed register, which the last yank filled.
func TestNamedRegisterPut(t *testing.T) {
	a := openPut(t)
	a.on(t, ".a")
	press(t, a, `"`, "a", "d", "d")
	if !strings.Contains(a.m.Info, `"a`) {
		t.Fatalf("info %q", a.m.Info)
	}
	if y, _ := a.r.ReadYank("a"); y.Shape != nodesShape {
		t.Fatalf("register a %+v", y)
	}
	a.on(t, ".y")
	press(t, a, "y", "y")
	a.on(t, ".x.c")
	press(t, a, `"`, "a", "p")
	if got := output(t, a); got != `{"x":{"b":2,"c":3,"a":1},"y":4}` {
		t.Fatalf(`"a p %s`, got)
	}
	press(t, a, "p")
	if got := agreesWith(t, a, nestDoc); got != `{"x":{"b":2,"c":3,"a":1,"y":4},"y":4}` {
		t.Fatalf("p %s", got)
	}
	// "b y y then "B y y appends: "b p puts both.
	a.on(t, ".x.b")
	press(t, a, `"`, "b", "y", "y")
	a.on(t, ".x.c")
	press(t, a, `"`, "B", "y", "y")
	onRoot(t, a)
	press(t, a, `"`, "b", "p")
	if got := output(t, a); got != `{"b":2,"c":3,"x":{"b":2,"c":3,"a":1,"y":4},"y":4}` {
		t.Fatalf(`"B appends: %s`, got)
	}
}

// V ... d deletes every marked node as one undo step, V ... y yanks them,
// and a put of several nodes is one undo step too.
func TestMarkedDeletePutOneUndoStep(t *testing.T) {
	a := openPut(t)
	a.on(t, ".a")
	press(t, a, "V", "j", "d")
	if got := output(t, a); got != `{"y":4}` {
		t.Fatalf("V j d %s", got)
	}
	if a.rs.tree.List().Visual() {
		t.Fatal("the selection is still on")
	}
	a.on(t, ".y")
	press(t, a, "p")
	if got := agreesWith(t, a, nestDoc); got != `{"y":4,"a":1,"x":{"b":2,"c":3}}` {
		t.Fatalf("put %s", got)
	}
	press(t, a, "u")
	if got := output(t, a); got != `{"y":4}` {
		t.Fatalf("one u takes the put back: %s", got)
	}
	press(t, a, "u")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3},"y":4}` {
		t.Fatalf("one u takes the delete back: %s", got)
	}
	// V ... y yanks both; the tree is unchanged.
	a.on(t, ".a")
	press(t, a, "V", "j", "y")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3},"y":4}` {
		t.Fatalf("V j y changed the tree: %s", got)
	}
	a.on(t, ".y")
	press(t, a, "p")
	if got := agreesWith(t, a, nestDoc); got != `{"a":1,"x":{"b":2,"c":3},"y":4,"a-2":1,"x-2":{"b":2,"c":3}}` {
		t.Fatalf("put of the yank %s", got)
	}
	// In visual-line the single keys are node.delete and node.yank: a
	// lone y acts at once (it does not wait on y y).
	a.on(t, ".a")
	press(t, a, "V", "y")
	if a.rs.tree.List().Visual() || a.r.BaseMode() != "split" {
		t.Fatalf("V y left visual on (mode %s)", a.r.BaseMode())
	}
	if y, err := a.r.ReadYank(""); err != nil || !strings.Contains(y.Text, nodesShape) {
		t.Fatalf("V y did not yank nodes: %v %v", y, err)
	}
}

// A put into an array item, where the mapping would not check, is
// refused with a notice and changes nothing: no other key answers.
func TestPutRefusedIntoAnArrayItem(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".items[].id", ".name")
	press(t, a, "ctrl+a", "o")
	selectIn(t, a, a.rs.tree, a.fromPath(".name"))
	press(t, a, "y", "y")
	before := entries(a.Mapping())
	items := func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].from == ".items" }
	item := func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].item }
	for _, tc := range []struct {
		name string
		at   func(ochazuke.TreeNode) bool
		key  string
	}{
		{"p on the array: a key beside its item", items, "p"},
		{"p on the item: .name inside .items[]", item, "p"},
		{"P on the item: a key beside it", item, "P"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selectIn(t, a, a.rs.tree, tc.at)
			focus := a.r.Focus
			press(t, a, tc.key)
			if got := entries(a.Mapping()); !slices.Equal(got, before) {
				t.Fatalf("mapping changed: %v", got)
			}
			if !strings.HasPrefix(a.m.Info, "put does not go here") || a.r.Focus != focus {
				t.Fatalf("info %q, focus %s (was %s)", a.m.Info, a.r.Focus, focus)
			}
		})
	}
	// A key of the item's own values goes into it.
	selectIn(t, a, a.rs.tree, a.fromPath(".items[].id"))
	press(t, a, "y", "y")
	selectIn(t, a, a.rs.tree, item)
	press(t, a, "p")
	if got := agrees(t, a); got != `{"items":[{"id-2":1,"id":1},{"id-2":2,"id":2},{"id-2":3,"id":3},{"id-2":4,"id":4}],"name":"o-json sample"}` {
		t.Fatalf("put into the item %s", got)
	}
}

// Undo and redo walk a delete and its put back and forth.
func TestPutUndoRedoRoundTrip(t *testing.T) {
	a := openPut(t)
	start := output(t, a)
	a.on(t, ".a")
	press(t, a, "d", "d")
	deleted := output(t, a)
	a.on(t, ".y")
	press(t, a, "p")
	moved := output(t, a)
	for _, step := range []struct{ key, want string }{
		{"u", deleted}, {"u", start}, {"ctrl+r", deleted}, {"ctrl+r", moved},
	} {
		press(t, a, step.key)
		if got := output(t, a); got != step.want {
			t.Fatalf("%s: %s, want %s", step.key, got, step.want)
		}
	}
	if moved != `{"x":{"b":2,"c":3},"y":4,"a":1}` {
		t.Fatalf("moved %s", moved)
	}
}

// y y, p, P and d d answer with o-json's verbs on every kind of node the
// tree holds (not d d and y y on the document node, which stays: those
// keys pass on and change nothing), and over a V selection d and y do;
// each row has a hint.
func TestPutKeysOnEveryNode(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".items[].owner", ".name")
	press(t, a, "ctrl+a", "o")
	kinds := []struct {
		name  string
		match func(ochazuke.TreeNode) bool
	}{
		{"root", func(n ochazuke.TreeNode) bool { return n.ID == rsRootID }},
		{"array", func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].from == ".items" }},
		{"item", func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].item }},
		{"object", a.fromPath(".items[].owner")},
		{"plain", a.fromPath(".name")},
	}
	answers := func() (map[string]string, map[string]bool) {
		got, hints := map[string]string{}, map[string]bool{}
		for _, b := range a.r.ListBindings(a.r.Focus) {
			if b.Keys != "" && !b.Unbind && got[b.Keys] == "" {
				got[b.Keys] = a.r.CanonicalKey(b.Request.Key())
				hints[b.Keys] = b.Hint != ""
			}
		}
		return got, hints
	}
	expect := func(name string, want map[string]string) {
		t.Helper()
		got, hints := answers()
		for key, verb := range want {
			if got[key] != verb {
				t.Errorf("%s: %q answers %q, want %q", name, key, got[key], verb)
			}
			if verb != "" && !hints[key] {
				t.Errorf("%s: %q has no hint", name, key)
			}
		}
	}
	for _, k := range kinds {
		selectIn(t, a, a.rs.tree, k.match)
		normal := map[string]string{"y y": "node.yank", "d d": "node.delete", "p": "node.put", "P": "node.put", "d": ""}
		if k.name == "root" {
			normal["y y"], normal["d d"] = "", ""
		}
		expect(k.name, normal)
		press(t, a, "V")
		inV := map[string]string{"d": "node.delete", "y": "node.yank", "p": "node.put", "d d": "", "y y": ""}
		if k.name == "root" {
			inV["d"], inV["y"] = "", "ochazuke.list.yank"
		}
		expect(k.name+" in V", inV)
		press(t, a, "esc")
	}
}

// V j d deletes two nodes and V j y then p copies them: single keys in
// visual-line, one undo step each.
func TestVisualLineDeleteYankPut(t *testing.T) {
	a := openPut(t)
	a.on(t, ".a")
	press(t, a, "V", "j", "d")
	if got := output(t, a); got != `{"y":4}` {
		t.Fatalf("V j d %s", got)
	}
	press(t, a, "u")
	a.on(t, ".a")
	press(t, a, "V", "j", "y")
	a.on(t, ".y")
	press(t, a, "p")
	if got := agreesWith(t, a, nestDoc); got != `{"a":1,"x":{"b":2,"c":3},"y":4,"a-2":1,"x-2":{"b":2,"c":3}}` {
		t.Fatalf("V j y, p %s", got)
	}
}

// p over a selection: replace (the default, Vim's V p) puts the register
// in the selection's place and the selection into the register; below
// puts after the selection and keeps it.
func TestVisualLinePut(t *testing.T) {
	a := openPut(t)
	a.on(t, ".y")
	press(t, a, "y", "y")
	a.on(t, ".a")
	press(t, a, "V", "j", "p")
	if got := agreesWith(t, a, nestDoc); got != `{"y-2":4,"y":4}` {
		t.Fatalf("V j p (replace) %s", got)
	}
	a.on(t, ".y")
	press(t, a, "p")
	if got := agreesWith(t, a, nestDoc); got != `{"y-2":4,"a":1,"x":{"b":2,"c":3},"y":4}` {
		t.Fatalf("p after V p puts the replaced nodes: %s", got)
	}
	press(t, a, "u", "u")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3},"y":4}` {
		t.Fatalf("V p is one undo step: %s", got)
	}
	if err := a.r.Settings().Set(settingVisualPut, "below"); err != nil {
		t.Fatal(err)
	}
	a.on(t, ".y")
	press(t, a, "y", "y")
	a.on(t, ".a")
	press(t, a, "V", "j", "p")
	if got := agreesWith(t, a, nestDoc); got != `{"a":1,"x":{"b":2,"c":3},"y-2":4,"y":4}` {
		t.Fatalf("V j p (below) %s", got)
	}
}

// S over a selection: together wraps the siblings in one new object;
// each wraps every node in its own.
func TestVisualLineWrap(t *testing.T) {
	a := openPut(t)
	a.on(t, ".a")
	press(t, a, "V", "j", "S")
	if got := agreesWith(t, a, nestDoc); got != `{"new-key":{"a":1,"x":{"b":2,"c":3}},"y":4}` {
		t.Fatalf("V j S (together) %s", got)
	}
	press(t, a, "esc", "u")
	if err := a.r.Settings().Set(settingVisualWrap, "each"); err != nil {
		t.Fatal(err)
	}
	a.on(t, ".a")
	press(t, a, "V", "j", "S")
	if got := agreesWith(t, a, nestDoc); got != `{"new-key":{"a":1},"new-key-2":{"x":{"b":2,"c":3}},"y":4}` {
		t.Fatalf("V j S (each) %s", got)
	}
	press(t, a, "u")
	if got := output(t, a); got != `{"a":1,"x":{"b":2,"c":3},"y":4}` {
		t.Fatalf("one undo step: %s", got)
	}
}

// . runs the last put again at the cursor (repeat.scope put, the
// default), even after another edit; with edit it runs the last edit of
// any kind.
func TestDotRepeatsThePut(t *testing.T) {
	a := openPut(t)
	a.on(t, ".a")
	press(t, a, "y", "y")
	a.on(t, ".y")
	press(t, a, "p")
	a.on(t, ".x.b")
	press(t, a, "<", "<") // an edit between: . still puts
	a.on(t, ".x.c")
	press(t, a, ".")
	if got := agreesWith(t, a, nestDoc); got != `{"a":1,"b":2,"x":{"c":3,"a":1},"y":4,"a-2":1}` {
		t.Fatalf(". after p: %s", got)
	}
	if err := a.r.Settings().Set(settingRepeatScope, "edit"); err != nil {
		t.Fatal(err)
	}
	a.on(t, ".y")
	press(t, a, ">", ">")
	selectIn(t, a, a.rs.tree, func(n ochazuke.TreeNode) bool { return n.Label == "a-2" })
	press(t, a, ".")
	if got := agreesWith(t, a, nestDoc); got != `{"a":1,"b":2,"x":{"c":3,"a":1,"y":4,"a-2":1}}` {
		t.Fatalf(". after > > (repeat.scope edit): %s", got)
	}
}

// y on the original tree yanks the shape's path; p / P on the
// restructured tree put that shape (with the fields under it), a copy of
// its identity mapping; V j y yanks several.
func TestYankOnTheOriginalPutsOnTheRight(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".name")
	selectIn(t, a, a.rs.orig, origPath(".meta"))
	press(t, a, "y")
	press(t, a, "ctrl+a", "o")
	a.on(t, ".name")
	press(t, a, "p")
	if got := agrees(t, a); got != `{"name":"o-json sample","meta":{"generated":"2026-10-25","count":4,"source":null}}` {
		t.Fatalf("p of a yanked shape: %s", got)
	}
	press(t, a, "u", "ctrl+a", "o")
	selectIn(t, a, a.rs.orig, origPath(".version"))
	press(t, a, "V", "k", "y")
	press(t, a, "ctrl+a", "o")
	a.on(t, ".name")
	press(t, a, "P")
	if got := agrees(t, a); got != `{"name-2":"o-json sample","version":2,"name":"o-json sample"}` {
		t.Fatalf("P of two yanked shapes: %s", got)
	}
	// In the viewer: y on the original (T), back to the edited, p.
	b := open(t, sample, 120, 30)
	press(t, b, "T")
	selectPath(t, b, ".version")
	press(t, b, "y", "T")
	b.on(t, ".meta")
	press(t, b, "p")
	if got := output(t, b); !strings.Contains(got, `"meta":{"version":2,"generated"`) {
		t.Fatalf("viewer p of a yanked shape: %s", got)
	}
}
