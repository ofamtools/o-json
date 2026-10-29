package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
)

// openRestructure opens the sample and the restructure page with M.
func openRestructure(t *testing.T, width, height int) *App {
	t.Helper()
	a := open(t, sample, width, height)
	press(t, a, "M")
	if a.r.Page != rsPageID {
		t.Fatalf("page %q", a.r.Page)
	}
	return a
}

// selectIn moves a page tree's selection to a node with the keys.
func selectIn(t *testing.T, a *App, tree *ochazuke.Tree, match func(ochazuke.TreeNode) bool) {
	t.Helper()
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"k", "j"} {
		for range 40 {
			if n, ok := tree.Selected(); ok && match(n) {
				return
			}
			press(t, a, key)
		}
	}
	t.Fatalf("not in %s: %+v", tree.ID(), tree.Nodes())
}

func origPath(path string) func(ochazuke.TreeNode) bool {
	return func(n ochazuke.TreeNode) bool { return n.ID == "o:s:"+path }
}

// fromPath matches the restructure node that came from a shape.
func (a *App) fromPath(path string) func(ochazuke.TreeNode) bool {
	return func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].from == path && !a.rs.meta[n.ID].item }
}

// entries is the mapping as "from>to" lines (a wrapper's from is "").
func entries(m *Mapping) []string {
	var out []string
	for _, e := range m.Map {
		out = append(out, e.From+">"+*e.To)
	}
	return out
}

func sendPaths(t *testing.T, a *App, paths ...string) {
	t.Helper()
	for _, p := range paths {
		selectIn(t, a, a.rs.orig, origPath(p))
		press(t, a, "s")
	}
}

func TestRestructurePageOpensEmpty(t *testing.T) {
	a := openRestructure(t, 120, 30)
	// The document node is there, and only it.
	if nodes := a.rs.tree.Nodes(); len(nodes) != 1 || nodes[0].ID != rsRootID || nodes[0].Parent != "" {
		t.Fatalf("nodes %+v", nodes)
	}
	if len(a.Mapping().Map) != 0 {
		t.Fatalf("mapping %+v", a.Mapping())
	}
	frame := view(a)
	for _, want := range []string{"Original", "Restructured  0 nodes", ". (root)", "empty: s on a shape", "Mapping", "owner", "email?"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("no %q\n%s", want, frame)
		}
	}
	press(t, a, "q")
	if a.r.Page != pageID {
		t.Fatalf("q: page %q", a.r.Page)
	}
}

// s sends a shape with the ancestors it needs and its fields; sending
// one already there adds nothing; the focus stays on the original.
func TestSendShapeAcross(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".items[].owner")
	want := []string{".items>.items", ".items[]>.items[]", ".items[].owner>.items[].owner", ".items[].owner.name>.items[].owner.name", ".items[].owner.email>.items[].owner.email"}
	if got := entries(a.Mapping()); !slices.Equal(got, want) {
		t.Fatalf("mapping %v", got)
	}
	if a.sideOf(a.r.Focus) != rsOrigID {
		t.Fatalf("focus %q", a.r.Focus)
	}
	if !strings.Contains(a.m.Info, "sent 5 node(s)") {
		t.Fatalf("info %q", a.m.Info)
	}
	sendPaths(t, a, ".items[].id")
	if got := entries(a.Mapping()); len(got) != 6 || got[5] != ".items[].id>.items[].id" {
		t.Fatalf("id joins the item: %v", got)
	}
	press(t, a, "s")
	if len(a.Mapping().Map) != 6 || a.m.Info != "already in the restructure" {
		t.Fatalf("resend: %v %q", entries(a.Mapping()), a.m.Info)
	}
	// The optional email shows as optional, the from beside it.
	if row := line(view(a), ".items[].owner.email"); !strings.Contains(row, "string|null?") {
		t.Errorf("email row %q", row)
	}
	// Undo takes the last send back, from the original side too.
	press(t, a, "u")
	if len(a.Mapping().Map) != 5 {
		t.Fatalf("undo: %v", entries(a.Mapping()))
	}
	press(t, a, "ctrl+r")
	if len(a.Mapping().Map) != 6 {
		t.Fatalf("redo: %v", entries(a.Mapping()))
	}
}

// The viewer's structure sends too, without opening the page.
func TestSendFromTheViewer(t *testing.T) {
	a := open(t, sample, 120, 30)
	selectPath(t, a, ".name")
	press(t, a, "s")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.name"}) || !strings.Contains(a.m.Info, "M opens it") {
		t.Fatalf("mapping %v info %q", got, a.m.Info)
	}
}

// > nests under the node above when it takes keys, < moves out; where
// the JSON rules refuse, > passes on to the divider. Out of an array
// item, < collects the values into an array.
func TestIndentAndDedent(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".items[].owner", ".name", ".version")
	press(t, a, "tab")
	if a.sideOf(a.r.Focus) != rsTreeID {
		t.Fatalf("tab: focus %q", a.r.Focus)
	}
	// email moves out of owner, just before it, then g > nests it back
	// into owner (the object below), as its first key.
	selectIn(t, a, a.rs.tree, a.fromPath(".items[].owner.email"))
	press(t, a, "<", "<")
	if got := entries(a.Mapping())[2]; got != ".items[].owner.email>.items[].email" {
		t.Fatalf("< < %v", entries(a.Mapping()))
	}
	press(t, a, "g", ">")
	if got := entries(a.Mapping())[3]; got != ".items[].owner.email>.items[].owner.email" {
		t.Fatalf("g > %v", entries(a.Mapping()))
	}
	// > > on an item neither nests nor wraps, and never resizes.
	selectIn(t, a, a.rs.tree, func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].item })
	before := strings.Index(line(view(a), "Restructured"), "Restructured")
	mapped := entries(a.Mapping())
	press(t, a, ">", ">")
	if got := entries(a.Mapping()); !slices.Equal(got, mapped) {
		t.Fatalf("> > on an item: %v", got)
	}
	if after := strings.Index(line(view(a), "Restructured"), "Restructured"); after != before {
		t.Errorf("> > moved the divider: %d then %d", before, after)
	}
	// name out of the item collects every name.
	selectIn(t, a, a.rs.tree, a.fromPath(".items[].owner.name"))
	press(t, a, "<", "<", "<", "<")
	// The first < < leaves owner for the item; the second would collect
	// the names into a top-level name, which the root already has, so it
	// is not available (and does nothing).
	got := entries(a.Mapping())
	if !slices.Contains(got, ".items[].owner.name>.items[].name") {
		t.Fatalf("after < <: %v", got)
	}
	p, err := a.plan()
	if err != nil {
		t.Fatalf("plan after collect: %v (%v)", err, got)
	}
	out := Pretty(p.Apply(a.Doc.Root))
	if !strings.Contains(out, `"ann"`) {
		t.Errorf("collected names %s", out)
	}
	// One u per step.
	press(t, a, "u", "u")
	if got := entries(a.Mapping()); !slices.Contains(got, ".items[].owner.name>.items[].owner.name") {
		t.Fatalf("undo: %v", got)
	}
}

// Dedent out of an item collects: .items[].id becomes .id[], an array of
// every id, and the root object is then {items, id}.
func TestDedentOutOfAnItemCollects(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".items[].id")
	press(t, a, "tab")
	selectIn(t, a, a.rs.tree, a.fromPath(".items[].id"))
	press(t, a, "<", "<")
	want := []string{">.id", ".items[].id>.id[]", ".items>.items", ".items[]>.items[]"}
	if got := entries(a.Mapping()); !slices.Equal(got, want) {
		t.Fatalf("mapping %v", got)
	}
	p, err := a.plan()
	if err != nil {
		t.Fatal(err)
	}
	// The item left without children copies each item whole, as a
	// leaf does.
	if got := Compact(p.Apply(a.Doc.Root), 0); !strings.HasPrefix(got, `{"id":[1,2,3,4],"items":[{"id":1,"name":"alpha"`) {
		t.Errorf("apply %s", got)
	}
	// An item's key cannot move out of the root's item: < is not there
	// for the item row itself.
	selectIn(t, a, a.rs.tree, func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].item && a.rs.meta[n.ID].from == ".items[]" })
	if a.r.Available(ochazuke.Request{Noun: "node", Verb: "dedent"}, a.r.Focus, a.r.Mode) {
		t.Error("dedent available on an item")
	}
}

// r edits the key in place; the node keeps its origin.
func TestRenameKeepsTheOrigin(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".name")
	press(t, a, "tab")
	selectIn(t, a, a.rs.tree, a.fromPath(".name"))
	press(t, a, "r")
	for range len("name") {
		press(t, a, "backspace")
	}
	Type(a.m, "title")
	press(t, a, "esc")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.title"}) {
		t.Fatalf("mapping %v", got)
	}
	// With a key, through the verb (palette, CLI).
	id := a.rs.tree.Nodes()[1].ID
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "rename", Target: id, Args: ochazuke.Arguments{"key": "label"}}); err != nil {
		t.Fatal(err)
	}
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.label"}) {
		t.Fatalf("rename {key}: %v", got)
	}
	press(t, a, "u")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.title"}) {
		t.Fatalf("undo rename: %v", got)
	}
}

// r opens the key selected (ochazuke's rename.select, on by default):
// typing replaces it, a motion keeps it and moves; with the setting off
// the cursor starts at the end.
func TestRenameOpensSelected(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".name")
	press(t, a, "tab")
	selectIn(t, a, a.rs.tree, a.fromPath(".name"))
	press(t, a, "r")
	in := a.r.Input(a.r.Focus)
	if in == nil {
		t.Fatal("r opened no line")
	}
	if lo, hi, ok := in.SelectedText(); !ok || lo != 0 || hi != len("name") || a.r.Mode.Name != "insert" {
		t.Fatalf("selected %d %d %v mode %s", lo, hi, ok, a.r.Mode.Name)
	}
	Type(a.m, "title")
	press(t, a, "esc")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.title"}) {
		t.Fatalf("typing over the selection: %v", got)
	}
	// end drops the selection and keeps the key; typing appends.
	press(t, a, "r", "end")
	Type(a.m, "s")
	press(t, a, "esc")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.titles"}) {
		t.Fatalf("end then typing: %v", got)
	}
	a.r.EnsureConfigSettings()
	if err := a.r.Settings().Set(ochazuke.RenameSelectSetting, false); err != nil {
		t.Fatal(err)
	}
	press(t, a, "r")
	in = a.r.Input(a.r.Focus)
	if _, _, ok := in.SelectedText(); ok || in.Cursor != len("titles") {
		t.Fatalf("rename.select off: selected %v cursor %d", ok, in.Cursor)
	}
	press(t, a, "esc")
}

// d d deletes a node and its children; u brings them back with their
// origins.
func TestDeleteAndUndo(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".items[].owner", ".name")
	press(t, a, "tab")
	selectIn(t, a, a.rs.tree, a.fromPath(".items[].owner"))
	press(t, a, "d", "d")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".items>.items", ".items[]>.items[]", ".name>.name"}) {
		t.Fatalf("delete: %v", got)
	}
	press(t, a, "u")
	if got := entries(a.Mapping()); len(got) != 6 || got[3] != ".items[].owner.name>.items[].owner.name" {
		t.Fatalf("undo: %v", got)
	}
}

// The mapping the page shows is what o-json restructure prints from the
// page's mapping, and writing it out runs the same.
func TestLiveMappingIsWhatRestructureRuns(t *testing.T) {
	a := openRestructure(t, 120, 40)
	sendPaths(t, a, ".items[].owner", ".name")
	press(t, a, "tab")
	selectIn(t, a, a.rs.tree, a.fromPath(".items[].owner.name"))
	press(t, a, "<", "<")
	shown, err := a.mappingText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view(a), `{"from": ".items", "to": ".items"},`) {
		t.Fatalf("mapping panel\n%s", view(a))
	}
	spec, _ := json.Marshal(a.Mapping())
	file := writeMap(t, string(spec))
	run := func(args ...string) string {
		var b strings.Builder
		if err := Main(append([]string{"restructure", sample, "--map", file}, args...), nil, &b, &b); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return b.String()
	}
	if got := run("--mapping"); got != shown {
		t.Errorf("--mapping\n%s\nshown\n%s", got, shown)
	}
	p, _ := a.plan()
	if got := run(); got != Pretty(p.Apply(a.Doc.Root))+"\n" {
		t.Errorf("apply %s", got)
	}
	if got := run("--jq"); got != p.JQ() {
		t.Errorf("jq %s", got)
	}
	// The --mapping form reads back as the same mapping.
	back := writeMap(t, shown)
	var b strings.Builder
	if err := Main([]string{"restructure", sample, "--map", back}, nil, &b, &b); err != nil || b.String() != run() {
		t.Errorf("round trip %v %s", err, b.String())
	}
}

// settle runs the work a verb left pending, as the model would.
func settle(a *App) {
	_, cmd := a.m.Update(tea.WindowSizeMsg{Width: a.m.Width, Height: a.m.Height})
	finish(a, cmd)
}

// restructure.write writes a new file, never the source; an existing
// file asks first.
func TestWriteRefusesTheSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.json")
	data, _ := os.ReadFile(sample)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, src, 120, 30)
	press(t, a, "M")
	sendPaths(t, a, ".name")
	// w without a path opens the args form for it.
	press(t, a, "w")
	if a.r.TopModal() != ochazuke.ArgsFormID {
		t.Fatalf("w: modal %q", a.r.TopModal())
	}
	press(t, a, "esc")
	if a.r.TopModal() != "" {
		press(t, a, "esc")
	}
	write := func(path string) error {
		_, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "write", Args: ochazuke.Arguments{"path": path}})
		settle(a)
		return err
	}
	if err := write(src); err == nil || !strings.Contains(err.Error(), "is the source") {
		t.Fatalf("write source: %v", err)
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "overwrite", Confirmed: true, Args: ochazuke.Arguments{"path": src}}); err == nil {
		t.Fatal("overwrite source allowed")
	}
	if after, _ := os.ReadFile(src); string(after) != string(data) {
		t.Fatal("source changed")
	}
	out := filepath.Join(dir, "doc.restructured.json")
	if err := write(out); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != "{\n  \"name\": \"o-json sample\"\n}\n" {
		t.Fatalf("wrote %q (info %q, error %q)", got, a.m.Info, a.m.Status)
	}
	// Again: the file is there, so it asks.
	if err := os.WriteFile(out, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := write(out); err != nil {
		t.Fatal(err)
	}
	if a.r.TopModal() == "" {
		t.Fatalf("no confirm (info %q, error %q)", a.m.Info, a.m.Status)
	}
	if got, _ := os.ReadFile(out); string(got) != "keep" {
		t.Fatal("overwrote before the confirm")
	}
	finish(a, Press(a.m, "y"))
	settle(a)
	if got, _ := os.ReadFile(out); !strings.Contains(string(got), "o-json sample") {
		t.Fatalf("after y: %q", got)
	}
}

// restructure.layout single draws one tree; tab swaps original and
// restructured, and the Mapping stays below.
func TestSingleLayoutToggles(t *testing.T) {
	a := openRestructure(t, 100, 30)
	sendPaths(t, a, ".name")
	if err := a.r.Settings().Set(settingLayout, "single"); err != nil {
		t.Fatal(err)
	}
	frame := view(a)
	if !strings.Contains(frame, "Original") || strings.Contains(frame, "Restructured") || !strings.Contains(frame, "Mapping") {
		t.Fatalf("single, original\n%s", frame)
	}
	press(t, a, "tab")
	frame = view(a)
	if strings.Contains(frame, "Original") || !strings.Contains(frame, "Restructured  1 nodes") || a.sideOf(a.r.Focus) != rsTreeID {
		t.Fatalf("single, restructured\n%s", frame)
	}
	t.Logf("single layout, restructured side:\n%s", frame)
	press(t, a, "tab")
	if a.sideOf(a.r.Focus) != rsOrigID {
		t.Fatalf("tab back: %q", a.r.Focus)
	}
	// split: tab moves between the panels instead.
	if err := a.r.Settings().Set(settingLayout, "split"); err != nil {
		t.Fatal(err)
	}
	frame = view(a)
	if !strings.Contains(frame, "Original") || !strings.Contains(frame, "Restructured") {
		t.Fatalf("split\n%s", frame)
	}
	press(t, a, "tab")
	if a.sideOf(a.r.Focus) != rsTreeID {
		t.Fatalf("split tab: %q", a.r.Focus)
	}
	t.Logf("split layout:\n%s", view(a))
}

// V selects several shapes; s sends them all as one change, one u.
func TestVisualSelectionSendsSeveral(t *testing.T) {
	a := openRestructure(t, 120, 30)
	selectIn(t, a, a.rs.orig, origPath(".name"))
	press(t, a, "V", "j", "s")
	if got := entries(a.Mapping()); !slices.Equal(got, []string{".name>.name", ".version>.version"}) {
		t.Fatalf("visual send: %v", got)
	}
	press(t, a, "esc", "u")
	if n := len(a.Mapping().Map); n != 0 {
		t.Fatalf("one undo: %v", entries(a.Mapping()))
	}
	// On the restructure tree: V and d delete every selected node.
	sendPaths(t, a, ".name", ".version", ".meta")
	press(t, a, "tab")
	selectIn(t, a, a.rs.tree, a.fromPath(".name"))
	press(t, a, "V", "j", "d")
	if got := entries(a.Mapping()); len(got) == 0 || got[0] != ".meta>.meta" {
		t.Fatalf("visual delete: %v", got)
	}
}

// M lands on the original at the shape the viewer has selected, in both
// layouts, and again on every reopen; with the viewer on its root, the
// original's first shape.
func TestOpenFocusesTheOriginal(t *testing.T) {
	for _, layout := range []string{"split", "single"} {
		t.Run(layout, func(t *testing.T) {
			a := open(t, sample, 120, 30)
			if err := a.r.Settings().Set(settingLayout, layout); err != nil {
				t.Fatal(err)
			}
			press(t, a, "M")
			if first := a.rs.orig.Nodes()[0].ID; a.r.Focus != first {
				t.Fatalf("first open: focus %q, want %q", a.r.Focus, first)
			}
			sendPaths(t, a, ".name")
			press(t, a, "q")
			selectPath(t, a, ".items[].owner")
			press(t, a, "M")
			if a.r.Focus != "o:s:.items[].owner" {
				t.Fatalf("focus %q", a.r.Focus)
			}
			if n, _ := a.rs.orig.Selected(); n.ID != "o:s:.items[].owner" {
				t.Fatalf("selected %q", n.ID)
			}
			// Leaving from the restructured tree, M still opens on the original.
			press(t, a, "tab")
			if a.sideOf(a.r.Focus) != rsTreeID {
				t.Fatalf("tab: focus %q", a.r.Focus)
			}
			press(t, a, "q")
			selectPath(t, a, ".version")
			press(t, a, "M")
			frame := view(a)
			if a.r.Focus != "o:s:.version" || !strings.Contains(frame, "Original") {
				t.Fatalf("reopen: focus %q\n%s", a.r.Focus, frame)
			}
			if layout == "single" && strings.Contains(frame, "Restructured") {
				t.Fatalf("single opens showing the original\n%s", frame)
			}
		})
	}
}

// L flips the layout and keeps the focused tree; the crumb names the
// layout and the key, the footer hints it.
func TestLayoutKeyFlips(t *testing.T) {
	a := openRestructure(t, 120, 30)
	sendPaths(t, a, ".name")
	frame := view(a)
	if !strings.Contains(frame, "split layout (L: single)") || !strings.Contains(frame, "L layout") {
		t.Fatalf("split crumb and hint\n%s", frame)
	}
	selectIn(t, a, a.rs.tree, a.fromPath(".name"))
	focus := a.r.Focus
	press(t, a, "L")
	frame = view(a)
	if a.layout() != "single" || a.r.Focus != focus || !strings.Contains(frame, "Restructured  1 nodes") || strings.Contains(frame, "Original") {
		t.Fatalf("L to single: layout %s focus %q\n%s", a.layout(), a.r.Focus, frame)
	}
	if !strings.Contains(frame, "single layout (L: split)") {
		t.Fatalf("single crumb\n%s", frame)
	}
	press(t, a, "tab")
	if a.sideOf(a.r.Focus) != rsOrigID {
		t.Fatalf("single tab swaps: %q", a.r.Focus)
	}
	focus = a.r.Focus
	press(t, a, "L")
	if a.layout() != "split" || a.r.Focus != focus {
		t.Fatalf("L to split: layout %s focus %q", a.layout(), a.r.Focus)
	}
	press(t, a, "tab")
	if a.sideOf(a.r.Focus) != rsTreeID {
		t.Fatalf("split tab moves: %q", a.r.Focus)
	}
}
