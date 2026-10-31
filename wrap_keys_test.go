package main

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// wrapDoc opens testdata/wrap.json in the viewer, the focus on the
// edited structure's .id (the first field).
func wrapDoc(t *testing.T) *App {
	t.Helper()
	a := open(t, "testdata/wrap.json", 120, 30)
	selectIn(t, a, a.rs.tree, a.fromPath(".id"))
	return a
}

// out is the restructured document, compact.
func out(t *testing.T, a *App) string {
	t.Helper()
	d := a.restructuredDoc()
	if d.err != nil {
		t.Fatal(d.err)
	}
	return Compact(d.root, 0)
}

func wantOut(t *testing.T, a *App, want string) {
	t.Helper()
	if got := out(t, a); got != want {
		t.Fatalf("document\n got %s\nwant %s", got, want)
	}
}

const wrapSource = `{"id":7,"name":"Ada","city":"London","zip":"N1","tags":["x","y"]}`

// The owner's sequence: v j j j { wraps the four fields in one new
// object, its key opened selected; typing names it, enter accepts.
func TestBraceWrapsTheVisualSelection(t *testing.T) {
	a := wrapDoc(t)
	press(t, a, "v")
	if a.r.Mode.Name != "visual-line" {
		t.Fatalf("v: mode %q", a.r.Mode.Name)
	}
	press(t, a, "j", "j", "j", "{")
	placeholderSelected(t, a, "new-key")
	Type(a.m, "person")
	press(t, a, "enter")
	wantOut(t, a, `{"person":{"id":7,"name":"Ada","city":"London","zip":"N1"},"tags":["x","y"]}`)
	if a.r.Mode.Name != "shape" {
		t.Fatalf("mode %q", a.r.Mode.Name)
	}
}

// enter on the placeholder still selected accepts it.
func TestBraceEnterKeepsThePlaceholder(t *testing.T) {
	a := wrapDoc(t)
	press(t, a, "v", "j", "{", "enter")
	wantOut(t, a, `{"new-key":{"id":7,"name":"Ada"},"city":"London","zip":"N1","tags":["x","y"]}`)
	if a.r.Mode.Name != "shape" {
		t.Fatalf("mode %q", a.r.Mode.Name)
	}
}

// v again ends the selection, as V does.
func TestVEndsTheSelection(t *testing.T) {
	a := wrapDoc(t)
	press(t, a, "v", "j", "v")
	if a.r.Mode.Name != "shape" || a.rs.tree.List().Visual() {
		t.Fatalf("mode %q visual %v", a.r.Mode.Name, a.rs.tree.List().Visual())
	}
}

// The wrap is one undo step: esc keeps the placeholder, one u brings the
// four fields back.
func TestBraceWrapIsOneUndoStep(t *testing.T) {
	a := wrapDoc(t)
	press(t, a, "v", "j", "j", "j", "{", "esc")
	wantOut(t, a, `{"new-key":{"id":7,"name":"Ada","city":"London","zip":"N1"},"tags":["x","y"]}`)
	press(t, a, "u")
	wantOut(t, a, wrapSource)
}

// [ makes the new key an array of the values, their keys dropped (one
// notice), as one undo step.
func TestBracketWrapsTheValuesInAnArray(t *testing.T) {
	a := wrapDoc(t)
	press(t, a, "v", "j", "j", "j", "[")
	placeholderSelected(t, a, "new-key")
	Type(a.m, "row")
	press(t, a, "enter")
	wantOut(t, a, `{"row":[7,"Ada","London","N1"],"tags":["x","y"]}`)
	press(t, a, "u", "u")
	wantOut(t, a, wrapSource)
}

func TestBracketTellsTheKeysWent(t *testing.T) {
	a := wrapDoc(t)
	press(t, a, "v", "j", "[")
	if v := view(a); !strings.Contains(v, "keys went (id, name)") {
		t.Fatalf("no notice:\n%s", v)
	}
	press(t, a, "esc")
	wantOut(t, a, `{"new-key":[7,"Ada"],"city":"London","zip":"N1","tags":["x","y"]}`)
}

// } on a wrapper lifts its keys back into its parent at its place.
func TestUnwrapLiftsAnObjectsKeys(t *testing.T) {
	a := wrapDoc(t)
	press(t, a, "v", "j", "j", "{", "esc")
	wantOut(t, a, `{"new-key":{"id":7,"name":"Ada","city":"London"},"zip":"N1","tags":["x","y"]}`)
	press(t, a, "v", "}")
	wantOut(t, a, wrapSource)
	press(t, a, "u")
	wantOut(t, a, `{"new-key":{"id":7,"name":"Ada","city":"London"},"zip":"N1","tags":["x","y"]}`)
}

// ] on an array lifts its items as numbered keys (restructure.new-key).
func TestUnwrapLiftsAnArraysItems(t *testing.T) {
	a := wrapDoc(t)
	selectIn(t, a, a.rs.tree, a.fromPath(".tags"))
	press(t, a, "v", "]")
	wantOut(t, a, `{"id":7,"name":"Ada","city":"London","zip":"N1","new-key-1":"x","new-key-2":"y"}`)
	press(t, a, "u")
	wantOut(t, a, wrapSource)
}

// A lifted key the parent has already refuses the unwrap, with why.
func TestUnwrapRefusesARepeatedKey(t *testing.T) {
	a := wrapDoc(t)
	press(t, a, "v", "j", "{", "esc")
	selectIn(t, a, a.rs.tree, a.fromPath(".zip"))
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "rename", Args: ochazuke.Arguments{"key": "id"}}); err != nil {
		t.Fatal(err)
	}
	selectIn(t, a, a.rs.tree, func(n ochazuke.TreeNode) bool { return n.Label == "new-key" })
	before := out(t, a)
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "unwrap"}); err == nil || !strings.Contains(err.Error(), "repeat the key id") {
		t.Fatalf("unwrap: %v", err)
	}
	wantOut(t, a, before)
}

// . repeats a wrap at the cursor (repeat.scope edit).
func TestDotRepeatsAWrap(t *testing.T) {
	a := wrapDoc(t)
	if err := a.r.Settings().Set(settingRepeatScope, "edit"); err != nil {
		t.Fatal(err)
	}
	press(t, a, "v", "{", "esc")
	selectIn(t, a, a.rs.tree, a.fromPath(".zip"))
	press(t, a, ".", "esc")
	wantOut(t, a, `{"new-key":{"id":7},"name":"Ada","city":"London","new-key-2":{"zip":"N1"},"tags":["x","y"]}`)
}

// v on the original tree starts the same line selection as V: v j s
// sends both shapes.
func TestVOnTheOriginalSelectsLikeV(t *testing.T) {
	a := openRestructure(t, 120, 30)
	selectIn(t, a, a.rs.orig, origPath(".name"))
	press(t, a, "v", "j", "s")
	if got := entries(a.Mapping()); len(got) != 2 || got[0] != ".name>.name" || got[1] != ".version>.version" {
		t.Fatalf("v send: %v", got)
	}
}

// With restructure.visual-wrap each, [ makes an array of each node's
// value, still one undo step; the mapping round-trips and jq agrees.
func TestBracketEachIsOneStep(t *testing.T) {
	a := wrapDoc(t)
	if err := a.r.Settings().Set(settingVisualWrap, "each"); err != nil {
		t.Fatal(err)
	}
	press(t, a, "v", "j", "[")
	want := `{"new-key":[7],"new-key-2":["Ada"],"city":"London","zip":"N1","tags":["x","y"]}`
	wantOut(t, a, want)
	p, err := a.plan()
	if err != nil {
		t.Fatal(err)
	}
	if g := runGoJQ(t, p.JQ(), a.Doc.Root); !sameJSON(t, g, want) {
		t.Fatalf("gojq %s", g)
	}
	press(t, a, "u")
	wantOut(t, a, wrapSource)
}
