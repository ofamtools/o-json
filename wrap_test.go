package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// wrapped opens the page with .name and .version sent, the focus on
// .version (under .name, a string, where > cannot nest).
func wrapped(t *testing.T) *App {
	t.Helper()
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".name", ".version")
	press(t, a, "tab")
	selectIn(t, a, a.rs.tree, a.fromPath(".version"))
	return a
}

// placeholderSelected reports the open line holding text, all of it
// selected (select mode).
func placeholderSelected(t *testing.T, a *App, text string) {
	t.Helper()
	in := a.r.Input(a.r.Focus)
	if in == nil || string(in.Text) != text {
		t.Fatalf("focus %q, want the line holding %q", a.r.Focus, text)
	}
	if lo, hi, ok := in.SelectedText(); !ok || lo != 0 || hi != len(text) {
		t.Fatalf("selected %d %d %v", lo, hi, ok)
	}
}

// > under a plain value wraps the node in a new key, opened selected:
// typing names it, and the mapping gains a wrapper (no from).
func TestWrapUnderAValueThenType(t *testing.T) {
	a := wrapped(t)
	press(t, a, ">")
	placeholderSelected(t, a, "new-key")
	Type(a.m, "meta")
	press(t, a, "esc")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.name", ">.meta", ".version>.meta.version"}) {
		t.Fatalf("mapping %v", got)
	}
	if a.r.Mode.Name != "normal" {
		t.Fatalf("mode %q", a.r.Mode.Name)
	}
	// The typed name undoes first, then the wrap, one step each.
	press(t, a, "u")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.name", ">.[\"new-key\"]", ".version>.[\"new-key\"].version"}) {
		t.Fatalf("undo the name: %v", got)
	}
	press(t, a, "u")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.name", ".version>.version"}) {
		t.Fatalf("undo the wrap: %v", got)
	}
}

// esc keeps the placeholder; one u takes the wrapper away, ctrl+r brings
// it back.
func TestWrapEscKeepsThePlaceholder(t *testing.T) {
	a := wrapped(t)
	press(t, a, ">")
	press(t, a, "esc")
	want := []string{".name>.name", ">.[\"new-key\"]", ".version>.[\"new-key\"].version"}
	if got := entries(a.Mapping()); !slices.Equal(got, want) {
		t.Fatalf("mapping %v", got)
	}
	if p, err := a.plan(); err != nil {
		t.Fatal(err)
	} else if got := Compact(p.Apply(a.Doc.Root), 0); got != `{"name":"o-json sample","new-key":{"version":2}}` {
		t.Fatalf("apply %s", got)
	}
	press(t, a, "u")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.name", ".version>.version"}) {
		t.Fatalf("undo: %v", got)
	}
	press(t, a, "ctrl+r")
	if got := entries(a.Mapping()); !slices.Equal(got, want) {
		t.Fatalf("redo: %v", got)
	}
}

// A placeholder is unique among its siblings: new-key, new-key-2, ...;
// the setting names it.
func TestPlaceholderIsUnique(t *testing.T) {
	a := wrapped(t)
	press(t, a, ">", "esc")
	press(t, a, "o", "esc")
	press(t, a, "O", "esc")
	var keys []string
	for _, n := range a.rs.tree.Nodes() {
		if n.Parent == rsRootID {
			keys = append(keys, n.Label)
		}
	}
	if !slices.Equal(keys, []string{"name", "new-key", "new-key-3", "new-key-2"}) {
		t.Fatalf("keys %v", keys)
	}
	if err := a.r.Settings().Set(settingNewKey, "field"); err != nil {
		t.Fatal(err)
	}
	press(t, a, "o")
	placeholderSelected(t, a, "field")
	press(t, a, "esc")
	if p := a.placeholder(a.current(), rsRootID, ""); p != "field-2" {
		t.Fatalf("next placeholder %q", p)
	}
}

// o and O add an empty key below and above in select mode, a adds one
// inside; each is one undo step; on the empty tree o adds the first key.
func TestNewKeyBelowAndAbove(t *testing.T) {
	a := openRestructure(t, 120, 30)
	if err := a.rs.tree.Focus(); err != nil {
		t.Fatal(err)
	}
	press(t, a, "o")
	placeholderSelected(t, a, "new-key")
	Type(a.m, "top")
	press(t, a, "esc")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{">.top"}) {
		t.Fatalf("o on the empty tree: %v", got)
	}
	sendPaths(t, a, ".name")
	press(t, a, "tab")
	selectIn(t, a, a.rs.tree, a.fromPath(".name"))
	press(t, a, "o")
	Type(a.m, "below")
	press(t, a, "esc")
	selectIn(t, a, a.rs.tree, a.fromPath(".name"))
	press(t, a, "O")
	Type(a.m, "above")
	press(t, a, "esc")
	press(t, a, "a")
	Type(a.m, "inner")
	press(t, a, "esc")
	want := []string{">.top", ">.above", ">.above.inner", ".name>.name", ">.below"}
	if got := entries(a.Mapping()); !slices.Equal(got, want) {
		t.Fatalf("mapping %v", got)
	}
	// Through the verb with a key: named at once, no line opened.
	if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "add-key", Args: ochazuke.Arguments{"where": "below", "key": "cli"}}); err != nil {
		t.Fatal(err)
	}
	if a.r.Input(a.r.Focus) != nil || !slices.Contains(entries(a.Mapping()), ">.above.cli") {
		t.Fatalf("add-key {key}: focus %q %v", a.r.Focus, entries(a.Mapping()))
	}
	// One u takes the key away; the typed name of inner undoes next, then
	// the key itself.
	press(t, a, "u")
	if got := entries(a.Mapping()); !slices.Equal(got, want) {
		t.Fatalf("u: %v", got)
	}
	press(t, a, "u", "u")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{">.top", ">.above", ".name>.name", ">.below"}) {
		t.Fatalf("u u: %v", got)
	}
}

// t turns an object key into an array of itself and back; the live
// mapping is what o-json restructure --map runs, and jq agrees with Go.
func TestArrayObjectRoundTrip(t *testing.T) {
	a := openRestructure(t, 120, 40)
	sendPaths(t, a, ".items[].owner", ".meta")
	press(t, a, "tab")
	start := entries(a.Mapping())
	// .items is an array whose item fans out: t on its item is not
	// available (the key would hold many values), so t passes on.
	selectIn(t, a, a.rs.tree, func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].item })
	if a.r.Available(ochazuke.Request{Noun: "node", Verb: "object"}, a.r.Focus, a.r.Mode) {
		t.Fatal("to-object on .items[]")
	}
	check := func(want string) {
		t.Helper()
		shown, err := a.mappingText()
		if err != nil {
			t.Fatal(err)
		}
		spec, _ := json.Marshal(a.Mapping())
		file := writeMap(t, string(spec))
		var b strings.Builder
		if err := Main([]string{"restructure", sample, "--map", file, "--mapping"}, nil, &b, &b); err != nil || b.String() != shown {
			t.Fatalf("--mapping %v\n%s\nshown\n%s", err, b.String(), shown)
		}
		p, _ := a.plan()
		got := Compact(p.Apply(a.Doc.Root), 0)
		if !strings.Contains(got, want) {
			t.Fatalf("apply %s, want %s", got, want)
		}
		raw, _ := json.Marshal(json.RawMessage(Compact(a.Doc.Root, 0)))
		if out := runJQ(t, p.JQ(), string(raw)); out != got {
			t.Errorf("jq\n%s\ngo\n%s\nprogram %s", out, got, p.JQ())
		}
	}
	selectIn(t, a, a.rs.tree, a.fromPath(".items[].owner"))
	press(t, a, "t")
	if got := entries(a.Mapping()); !slices.Contains(got, ">.items[].owner") || !slices.Contains(got, ".items[].owner>.items[].owner[]") ||
		!slices.Contains(got, ".items[].owner.name>.items[].owner[].name") {
		t.Fatalf("to array: %v", got)
	}
	check(`"owner":[{"name":"ann","email":"ann@example.com"}]`)
	// A plain value becomes an array of one.
	selectIn(t, a, a.rs.tree, a.fromPath(".meta.count"))
	press(t, a, "t")
	check(`"count":[4]`)
	press(t, a, "t") // back: the array node is focused
	check(`"count":4`)
	selectIn(t, a, a.rs.tree, func(n ochazuke.TreeNode) bool {
		return a.rs.meta[n.ID].item && a.rs.meta[n.ID].from == ".items[].owner"
	})
	press(t, a, "t") // from the item itself
	if got := entries(a.Mapping()); !slices.Equal(got, start) {
		t.Fatalf("round trip\n%v\nstart\n%v", got, start)
	}
	check(`"owner":{"name":"ann","email":"ann@example.com"}`)
	press(t, a, "u")
	if got := entries(a.Mapping()); !slices.Contains(got, ".items[].owner>.items[].owner[]") {
		t.Fatalf("undo: %v", got)
	}
}

// Selecting a restructure node marks its shape on the original; selecting
// a shape marks where it went.
func TestOriginHighlight(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".items[].owner")
	press(t, a, "tab")
	selectIn(t, a, a.rs.tree, a.fromPath(".items[].owner.name"))
	view(a)
	lit := func(tr *ochazuke.Tree) []string {
		var out []string
		for _, n := range tr.Nodes() {
			if slices.Contains(n.Tones, ochazuke.CellTone(0, toneOrigin)) {
				out = append(out, n.ID)
			}
		}
		return out
	}
	if got := lit(a.rs.orig); !slices.Equal(got, []string{"o:s:.items[].owner.name"}) {
		t.Fatalf("original marks %v", got)
	}
	if got := lit(a.rs.tree); len(got) != 0 {
		t.Fatalf("restructure marks %v", got)
	}
	// Rename it: the origin still marks.
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "rename", Target: a.r.Focus, Args: ochazuke.Arguments{"key": "who"}}); err != nil {
		t.Fatal(err)
	}
	view(a)
	if got := lit(a.rs.orig); !slices.Equal(got, []string{"o:s:.items[].owner.name"}) {
		t.Fatalf("after rename %v", got)
	}
	// From the original: the email shape marks its node.
	selectIn(t, a, a.rs.orig, origPath(".items[].owner.email"))
	view(a)
	if got := lit(a.rs.orig); len(got) != 0 {
		t.Fatalf("original keeps marks %v", got)
	}
	got := lit(a.rs.tree)
	if len(got) != 1 || a.rs.meta[got[0]].from != ".items[].owner.email" {
		t.Fatalf("restructure marks %v", got)
	}
	// A shape not sent marks nothing.
	selectIn(t, a, a.rs.orig, origPath(".version"))
	view(a)
	if got := lit(a.rs.tree); len(got) != 0 {
		t.Fatalf("unsent marks %v", got)
	}
}
