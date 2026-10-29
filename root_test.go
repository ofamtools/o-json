package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// onRoot focuses the restructured tree's document node.
func onRoot(t *testing.T, a *App) {
	t.Helper()
	selectIn(t, a, a.rs.tree, func(n ochazuke.TreeNode) bool { return n.ID == rsRootID })
}

func (a *App) available(noun, verb string) bool {
	return a.r.Available(ochazuke.Request{Noun: noun, Verb: verb}, a.r.Focus, a.r.Mode)
}

// agrees checks the page's mapping against o-json restructure --map (the
// output and --mapping) and, with jq on PATH, the jq program against Go;
// it returns the output.
func agrees(t *testing.T, a *App) string {
	t.Helper()
	return agreesWith(t, a, sample)
}

// agreesWith is agrees for the document at path.
func agreesWith(t *testing.T, a *App, path string) string {
	t.Helper()
	p, err := a.plan()
	if err != nil {
		t.Fatalf("plan: %v (%v)", err, entries(a.Mapping()))
	}
	got := Compact(p.Apply(a.Doc.Root), 0)
	spec, _ := json.Marshal(a.Mapping())
	file := writeMap(t, string(spec))
	for _, args := range [][]string{nil, {"--mapping"}} {
		var b strings.Builder
		if err := Main(append([]string{"restructure", path, "--map", file}, args...), nil, &b, &b); err != nil {
			t.Fatalf("restructure %v: %v", args, err)
		}
		want := Pretty(p.Apply(a.Doc.Root)) + "\n"
		if args != nil {
			want, _ = a.mappingText()
		}
		if b.String() != want {
			t.Fatalf("restructure %v\n%s\npage\n%s", args, b.String(), want)
		}
	}
	raw, _ := json.Marshal(json.RawMessage(Compact(a.Doc.Root, 0)))
	t.Run("jq", func(t *testing.T) {
		if out := runJQ(t, p.JQ(), string(raw)); out != got {
			t.Errorf("jq\n%s\ngo\n%s\nprogram %s", out, got, p.JQ())
		}
	})
	return got
}

// The document node: shown on the empty tree, the document's own path,
// with no delete, rename, dedent or wrap (those keys pass on and change
// nothing), and nothing goes beside it.
func TestRootIsTheDocument(t *testing.T) {
	a := openRestructure(t, 120, 30)
	onRoot(t, a)
	frame := view(a)
	if row := line(frame, ". (root)"); !strings.Contains(row, "object") {
		t.Fatalf("root row %q\n%s", row, frame)
	}
	if !strings.Contains(frame, ". (the document)") {
		t.Fatalf("crumb\n%s", frame)
	}
	for _, verb := range []string{"delete", "rename", "dedent", "wrap", "indent", "object"} {
		if a.available("node", verb) {
			t.Errorf("node.%s available on the root", verb)
		}
	}
	if !a.available("node", "array") {
		t.Error("node.array not available on the empty root")
	}
	press(t, a, "d", "d")
	press(t, a, "r")
	if a.r.Input(a.r.Focus) != nil {
		t.Fatal("r opened a line on the root")
	}
	press(t, a, "<", "<")
	if nodes := a.rs.tree.Nodes(); len(nodes) != 1 || nodes[0].ID != rsRootID {
		t.Fatalf("after d d, r, <: %+v", nodes)
	}
	// Through the verb: the root is skipped, nothing else selected, so it fails.
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "delete", Target: rsRootID}); err == nil {
		t.Fatal("node.delete on the root")
	}
	// O above the root is refused; o below it adds its first key.
	Press(a.m, "O")
	if len(a.rs.tree.Nodes()) != 1 || !strings.Contains(a.m.Status, "nothing goes beside") {
		t.Fatalf("O on the root: %+v (status %q)", a.rs.tree.Nodes(), a.m.Status)
	}
	a.m.Status = ""
	press(t, a, "o")
	Type(a.m, "first")
	press(t, a, "esc")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{">.first"}) {
		t.Fatalf("o on the root: %v", got)
	}
	// A top-level key does not move out beside the root.
	if a.available("node", "dedent") || !a.available("node", "wrap") {
		t.Errorf("top-level key: dedent %v wrap %v", a.available("node", "dedent"), a.available("node", "wrap"))
	}
	// With keys of its own the root becomes an array of itself (T2).
	onRoot(t, a)
	if !a.available("node", "array") {
		t.Error("node.array not available on a root holding a plain key")
	}
}

// t on the empty root makes the document an array; the first shape sent
// becomes its item (.items[] lands as .[]), so the output is the array of
// items, as o-json restructure --map and jq make it. t flips back (the
// array goes under the key it came from), u and ctrl+r undo and redo.
func TestRootFlipsToAnArray(t *testing.T) {
	a := openRestructure(t, 120, 40)
	onRoot(t, a)
	press(t, a, "t")
	if row := line(view(a), ". (root)"); !strings.Contains(row, "array") {
		t.Fatalf("root row %q", row)
	}
	if len(a.Mapping().Map) != 0 {
		t.Fatalf("an array waiting for its item maps nothing: %v", entries(a.Mapping()))
	}
	sendPaths(t, a, ".items[]")
	got := entries(a.Mapping())
	if len(got) == 0 || got[0] != ".items[]>.[]" || !slices.Contains(got, ".items[].id>.[].id") || slices.Contains(got, ".items>.items") {
		t.Fatalf("send onto the array root: %v", got)
	}
	items, err := Get(a.Doc.Root, ".items")
	if err != nil {
		t.Fatal(err)
	}
	if out := agrees(t, a); out != Compact(items, 0) {
		t.Fatalf("output %s\nwant the items %s", out, Compact(items, 0))
	}
	// Another field of the items joins the item.
	before := len(a.Mapping().Map)
	sendPaths(t, a, ".items[].name")
	if len(a.Mapping().Map) != before {
		t.Fatalf("resend: %v", entries(a.Mapping()))
	}
	// Flip back: the array goes back under its key.
	onRoot(t, a)
	if !a.available("node", "object") {
		t.Fatal("node.object not available on the array root")
	}
	press(t, a, "t")
	got = entries(a.Mapping())
	if len(got) < 2 || got[0] != ".items>.items" || got[1] != ".items[]>.items[]" {
		t.Fatalf("flip back: %v", got)
	}
	if out := agrees(t, a); out != `{"items":`+Compact(items, 0)+`}` {
		t.Fatalf("flip back output %s", out)
	}
	// And forward again from there: one array key lifts to the root.
	press(t, a, "t")
	if got := entries(a.Mapping()); got[0] != ".items[]>.[]" {
		t.Fatalf("lift: %v", got)
	}
	// Each flip is one undo step.
	press(t, a, "u")
	if got := entries(a.Mapping()); got[0] != ".items>.items" {
		t.Fatalf("undo the lift: %v", got)
	}
	press(t, a, "u")
	if got := entries(a.Mapping()); got[0] != ".items[]>.[]" {
		t.Fatalf("undo the flip back: %v", got)
	}
	press(t, a, "u")
	if len(a.Mapping().Map) != 0 || !a.current().hasItem(rsRootID) {
		t.Fatalf("undo the send: %v", entries(a.Mapping()))
	}
	press(t, a, "u")
	if a.current().hasItem(rsRootID) {
		t.Fatal("undo the flip: still an array")
	}
	press(t, a, "ctrl+r")
	if !a.current().hasItem(rsRootID) {
		t.Fatal("redo the flip")
	}
}

// W replaces an existing file without asking, and the source after one
// confirm; w still refuses the source and asks before replacing.
func TestForceWrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "people.json")
	data, _ := os.ReadFile(sample)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, src, 120, 30)
	press(t, a, "M")
	sendPaths(t, a, ".name")
	want := "{\n  \"name\": \"o-json sample\"\n}\n"
	// W without a path opens the path chooser, as w does.
	press(t, a, "W")
	if a.r.TopModal() != "path-chooser" {
		t.Fatalf("W: modal %q", a.r.TopModal())
	}
	press(t, a, "esc")
	if a.r.TopModal() != "" {
		press(t, a, "esc")
	}
	force := func(path string) {
		t.Helper()
		if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "write", Args: ochazuke.Arguments{"path": path, "force": true}}); err != nil {
			t.Fatal(err)
		}
		settle(a)
	}
	// Another existing file: replaced, no question.
	other := filepath.Join(dir, "out.json")
	if err := os.WriteFile(other, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	force(other)
	if a.r.TopModal() != "" {
		t.Fatalf("W asked about %s", other)
	}
	if got, _ := os.ReadFile(other); string(got) != want {
		t.Fatalf("W to an existing file wrote %q (status %q)", got, a.m.Status)
	}
	// The source: one confirm, then written.
	force(src)
	if a.r.TopModal() == "" || !strings.Contains(view(a), "Overwrite the source ") {
		t.Fatalf("W to the source did not ask\n%s", view(a))
	}
	if got, _ := os.ReadFile(src); string(got) != string(data) {
		t.Fatal("source written before the confirm")
	}
	finish(a, Press(a.m, "y"))
	settle(a)
	if got, _ := os.ReadFile(src); string(got) != want {
		t.Fatalf("after y: %q (status %q)", got, a.m.Status)
	}
	// w is unchanged: the source is refused.
	if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "write", Args: ochazuke.Arguments{"path": src}}); err == nil || !strings.Contains(err.Error(), "is the source") {
		t.Fatalf("w to the source: %v", err)
	}
	// n on the confirm leaves the source alone.
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	force(src)
	finish(a, Press(a.m, "n"))
	settle(a)
	if got, _ := os.ReadFile(src); string(got) != string(data) {
		t.Fatal("n wrote the source")
	}
}

// The document node is Fixed (rename, move, delete) and an array's item
// refuses a rename, from every path: the palette's ochazuke.outline verbs
// on the root are refused and change nothing, and their keys drop out.
func TestFixedRootRefusesOutlineVerbs(t *testing.T) {
	a := openPut(t)
	onRoot(t, a)
	before := output(t, a)
	for _, verb := range []string{"delete", "edit", "indent", "outdent"} {
		if a.available("ochazuke.outline", verb) {
			t.Errorf("ochazuke.outline.%s available on the root", verb)
		}
		if _, err := a.r.Execute(ochazuke.Request{Noun: "ochazuke.outline", Verb: verb, Target: rsRootID}); err == nil {
			t.Errorf("ochazuke.outline.%s on the root was not refused", verb)
		}
	}
	if got := output(t, a); got != before || len(a.rs.tree.Nodes()) == 0 {
		t.Fatalf("the root changed: %s", got)
	}
	if a.available("node", "delete") || a.available("node", "rename") {
		t.Fatal("node.delete / node.rename available on the root")
	}
	// An array item refuses a rename.
	b := openRestructure(t, 120, 30)
	sendPaths(t, b, ".items[].id")
	press(t, b, "tab")
	selectIn(t, b, b.rs.tree, func(n ochazuke.TreeNode) bool { return b.rs.meta[n.ID].item })
	if b.available("node", "rename") || b.available("ochazuke.outline", "edit") {
		t.Fatal("an item can be renamed")
	}
}
