package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// sameJSON reports two JSON texts holding the same value (key order
// aside: the copy writes keys in the shape's order).
func sameJSON(t *testing.T, got, want string) bool {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("output %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("input %q: %v", want, err)
	}
	return reflect.DeepEqual(g, w)
}

// identical checks the state is the copy: every entry maps a path to
// itself, the output is the input, nothing unsaved.
func identical(t *testing.T, a *App) {
	t.Helper()
	m := a.Mapping()
	if len(m.Map) == 0 {
		t.Fatal("empty mapping")
	}
	for _, e := range m.Map {
		if e.To == nil || dotted(e.From) != *e.To { // an array document's shapes are [] and .[]
			t.Fatalf("entry %v is not the identity (%v)", e, entries(m))
		}
	}
	if got := output(t, a); !sameJSON(t, got, Compact(a.Doc.Root, 0)) {
		t.Fatalf("output %s\ninput %s", got, Compact(a.Doc.Root, 0))
	}
	if a.unsaved() || strings.Contains(view(a), "[+]") {
		t.Fatalf("the copy is unsaved\n%s", view(a))
	}
}

// The viewer opens with the structure pane a copy of the document's
// shape: the identity mapping, the output equal to the input, also for
// an array document, a repeated key and shapes that mix types.
func TestIdentityStart(t *testing.T) {
	mixed := filepath.Join(t.TempDir(), "mixed.json")
	if err := os.WriteFile(mixed, []byte(`{"a":[{"x":1},{"x":null},{"x":{"y":1}},{"x":[2]}],"b":null,"c":[[1],{"k":2}],"d":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{sample, nestDoc, "testdata/top-array.json", "testdata/duplicate-keys.json", mixed} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			a := open(t, path, 120, 30)
			if !a.editedShown() || a.sideOf(a.r.Focus) != rsTreeID {
				t.Fatalf("not the edited view: focus %q", a.r.Focus)
			}
			if !strings.Contains(view(a), "Structure · unchanged") {
				t.Fatalf("title\n%s", view(a))
			}
			identical(t, a)
			agreesWith(t, a, path)
		})
	}
}

// A document just opened reads as unchanged, not edited: the title says
// "unchanged" with no [+] (in the pane or the breadcrumb), nothing is
// unsaved, and q quits without asking. One edit, a structure move or a
// value edit, turns the title to "edited [+]" and q asks; undoing it
// back to the copy reads unchanged again.
func TestOpenedDocumentIsNotEdited(t *testing.T) {
	for _, path := range []string{sample, "testdata/top-array.json"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			a := open(t, path, 120, 30)
			frame := view(a)
			if !strings.Contains(frame, "Structure · unchanged ") || strings.Contains(frame, "edited") || strings.Contains(frame, "[+]") || a.unsaved() {
				t.Fatalf("opened as edited: unsaved %v\n%s", a.unsaved(), frame)
			}
			press(t, a, "q")
			if !a.r.Quit || a.r.TopModal() != "" {
				t.Fatalf("q on a document just opened asked: modal %q", a.r.TopModal())
			}
		})
	}
	// A structure edit.
	a := open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	if frame := view(a); !strings.Contains(frame, "Structure · edited [+]") || strings.Contains(frame, "unchanged") {
		t.Fatalf("a move does not read edited\n%s", frame)
	}
	press(t, a, "u")
	if frame := view(a); !strings.Contains(frame, "Structure · unchanged ") || strings.Contains(frame, "[+]") {
		t.Fatalf("undone to the copy does not read unchanged\n%s", frame)
	}
	// A value edit on the top-level array.
	a = open(t, "testdata/top-array.json", 120, 30)
	press(t, a, "q")
	if !a.r.Quit {
		t.Fatal("q on the array document asked")
	}
	a = open(t, "testdata/top-array.json", 120, 30)
	onRoot(t, a)
	view(a)
	if err := a.data.list.Focus(); err != nil {
		t.Fatal(err)
	}
	valueDo(t, a, "delete", nil) // the first item
	if frame := view(a); !strings.Contains(frame, "Structure · edited [+]") || !a.unsaved() {
		t.Fatalf("a value edit does not read edited\n%s", frame)
	}
	press(t, a, "q")
	if a.r.Quit || !strings.Contains(view(a), "Quit without writing?") {
		t.Fatalf("q after a value edit did not ask\n%s", view(a))
	}
}

// A move in the viewer's structure pane shifts the data pane at once,
// and the mapping is what o-json restructure --map (and jq) runs.
func TestMoveInTheViewerShiftsTheData(t *testing.T) {
	a := open(t, sample, 120, 30)
	fieldsView(t, a)
	scalarsAlone(t, a) // the data pane shows the plain value itself
	a.on(t, ".meta.count")
	if !strings.Contains(view(a), "Value .meta.count") {
		t.Fatalf("before\n%s", view(a))
	}
	press(t, a, "<", "<")
	frame := view(a)
	if !strings.Contains(frame, "Value .count") || !strings.Contains(line(frame, ".count ("), "← .meta.count") {
		t.Fatalf("the data pane did not follow the move\n%s", frame)
	}
	if !strings.Contains(frame, "Structure · edited [+]") || line(frame, "sample.json [+]  ·") == "" {
		t.Fatalf("no [+]\n%s", frame)
	}
	onRoot(t, a)
	if row := line(view(a), "count  "); !strings.Contains(row, "4") {
		t.Fatalf("the root's fields have no count\n%s", view(a))
	}
	got := agrees(t, a)
	if !strings.Contains(got, `"count":4,"meta":{"generated":"2026-10-25","source":null}`) {
		t.Fatalf("output %s", got)
	}
}

// u in the viewer undoes the move: the data and the mapping are the
// copy again, and nothing is unsaved.
func TestUndoRestoresTheOriginalData(t *testing.T) {
	a := open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	if !a.unsaved() {
		t.Fatal("the move is not unsaved")
	}
	press(t, a, "u")
	identical(t, a)
	press(t, a, "ctrl+r")
	if !strings.Contains(output(t, a), `"count":4,"meta"`) {
		t.Fatalf("redo: %s", output(t, a))
	}
}

// T flips the structure pane and the data pane between the original and
// the edited document, on the same shape; the header says which.
func TestToggleOriginalAndEdited(t *testing.T) {
	a := open(t, sample, 120, 30)
	scalarsAlone(t, a) // the data pane shows the plain value itself
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	press(t, a, "T")
	frame := view(a)
	if !a.original || a.sideOf(a.r.Focus) == rsTreeID || !strings.Contains(frame, "Structure · original [+]") {
		t.Fatalf("T: focus %q\n%s", a.r.Focus, frame)
	}
	if s := a.selectedShape(); s == nil || s.Path != ".meta.count" || !strings.Contains(frame, "Value .meta.count") {
		t.Fatalf("original shows %v\n%s", s, frame)
	}
	if !a.available("viewer", "search") || a.available("restructure", "write") || !a.available("viewer", "write") {
		t.Fatal("the original view's keys")
	}
	press(t, a, "T")
	frame = view(a)
	if a.original || a.sideOf(a.r.Focus) != rsTreeID || !strings.Contains(frame, "Value .count") || !strings.Contains(frame, "Structure · edited [+]") {
		t.Fatalf("T again: focus %q\n%s", a.r.Focus, frame)
	}
	if a.available("viewer", "search") || !a.available("restructure", "write") {
		t.Fatal("the edited view's keys")
	}
	// From the data pane T flips the data and leaves the focus there.
	press(t, a, "ctrl+a", "o")
	focus := a.r.Focus
	press(t, a, "T")
	if a.r.Focus != focus || !strings.Contains(view(a), "Value .meta.count") {
		t.Fatalf("T from the data pane: focus %q (was %q)\n%s", a.r.Focus, focus, view(a))
	}
}

// w in the edited view writes the restructured document by the write
// rules: a new file, an existing one asks, the source never; W on the
// source asks once. Nothing touches the source before that.
func TestWriteFromTheViewer(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.json")
	data, _ := os.ReadFile(sample)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, src, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	if after, _ := os.ReadFile(src); string(after) != string(data) {
		t.Fatal("an edit wrote the source")
	}
	press(t, a, "w")
	if a.r.TopModal() != "path-chooser" {
		t.Fatalf("w: modal %q", a.r.TopModal())
	}
	press(t, a, "esc")
	if a.r.TopModal() != "" {
		press(t, a, "esc")
	}
	write := func(args ochazuke.Arguments) error {
		_, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "write", Args: args})
		settle(a)
		return err
	}
	if err := write(ochazuke.Arguments{"path": src}); err == nil || !strings.Contains(err.Error(), "is the source") {
		t.Fatalf("write source: %v", err)
	}
	out := filepath.Join(dir, "doc.restructured.json")
	if err := write(ochazuke.Arguments{"path": out}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); !strings.Contains(string(got), `"count": 4,`) || !strings.HasSuffix(string(got), "}\n") {
		t.Fatalf("wrote %q (info %q, error %q)", got, a.m.Info, a.m.Status)
	}
	if a.unsaved() || strings.Contains(view(a), "[+]") {
		t.Fatal("still unsaved after the write")
	}
	// The file is there now: w asks before replacing it.
	if err := write(ochazuke.Arguments{"path": out}); err != nil || a.r.TopModal() == "" {
		t.Fatalf("no confirm: %v", err)
	}
	press(t, a, "n")
	// W on the source asks once; n leaves it as it was.
	if err := write(ochazuke.Arguments{"path": src, "force": true}); err != nil || a.r.TopModal() == "" {
		t.Fatalf("W source: no confirm: %v", err)
	}
	press(t, a, "n")
	if after, _ := os.ReadFile(src); string(after) != string(data) {
		t.Fatal("the source changed")
	}
}

// structure.editable off keeps the viewer read-only: the shape tree, no
// T, and the restructure page alone edits.
func TestReadOnlyViewerSetting(t *testing.T) {
	a := readOnly(t, sample, 120, 30)
	frame := view(a)
	if a.editedShown() || !strings.Contains(frame, "Structure ━") || strings.Contains(frame, "edited") {
		t.Fatalf("read-only viewer\n%s", frame)
	}
	if a.available("restructure", "show") || a.available("restructure", "write") || len(a.Mapping().Map) != 0 {
		t.Fatal("T or w on the read-only viewer, or a copy made")
	}
	selectPath(t, a, ".items")
	press(t, a, "T", ">", ">")
	if !strings.Contains(view(a), "Table .items") || a.selectedShape().Path != ".items" {
		t.Fatalf("keys changed the read-only viewer\n%s", view(a))
	}
	press(t, a, "M")
	if len(a.rs.tree.Nodes()) != 1 {
		t.Fatalf("the restructure page did not open empty: %+v", a.rs.tree.Nodes())
	}
}

// viewer.open and R with edits no write saved ask once (viewer.discard):
// n keeps the edits, y drops them and reads the file.
func TestOpenAndReloadWithUnsavedEditsAsk(t *testing.T) {
	a := open(t, sample, 120, 30)
	a.r.Config().StateDir = t.TempDir()
	edit := func() {
		t.Helper()
		a.on(t, ".meta.count")
		press(t, a, "<", "<")
	}
	edit()
	press(t, a, "R")
	if a.r.TopModal() == "" {
		t.Fatal("R did not ask")
	}
	press(t, a, "n")
	settle(a)
	if !a.unsaved() {
		t.Fatal("n dropped the edits")
	}
	press(t, a, "R")
	finish(a, Press(a.m, "y"))
	settle(a)
	identical(t, a)
	edit()
	viewerDo(t, a, "open", ochazuke.Arguments{"path": nestDoc})
	if a.r.TopModal() == "" || a.Path != sample {
		t.Fatalf("open did not ask: modal %q path %q", a.r.TopModal(), a.Path)
	}
	finish(a, Press(a.m, "y"))
	settle(a)
	if a.Path != nestDoc {
		t.Fatalf("y: path %q", a.Path)
	}
	identical(t, a)
	// Without edits it reads at once.
	viewerDo(t, a, "open", ochazuke.Arguments{"path": sample})
	settle(a)
	if a.r.TopModal() != "" || a.Path != sample {
		t.Fatalf("open without edits: modal %q path %q", a.r.TopModal(), a.Path)
	}
}

// The viewer and the restructure page are one state: M opens on the
// state as it is (the copy, or the in-place edits), and q leaves it so;
// restructure.clear empties it as one undo step.
func TestRestructurePageSharesTheState(t *testing.T) {
	a := open(t, sample, 120, 30)
	press(t, a, "M")
	if a.edited() || len(a.rs.tree.Nodes()) <= 1 || strings.Contains(view(a), "Restructured  0 nodes") {
		t.Fatalf("M did not open on the copy\n%s", view(a))
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "clear"}); err != nil || len(a.rs.tree.Nodes()) != 1 {
		t.Fatalf("clear: %v, %d nodes", err, len(a.rs.tree.Nodes()))
	}
	press(t, a, "u")
	press(t, a, "q")
	identical(t, a)
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	before := a.Mapping()
	press(t, a, "M")
	if mappingKey(a.Mapping()) != mappingKey(before) || !strings.Contains(view(a), "count") {
		t.Fatalf("M did not open on the edits: %v", entries(a.Mapping()))
	}
	// An edit on the page shows in the viewer.
	a.on(t, ".name")
	press(t, a, "d", "d", "q")
	if a.r.Page != pageID || !a.editedShown() || a.selectedShape() == nil {
		t.Fatalf("back: page %q", a.r.Page)
	}
	if got := output(t, a); strings.Contains(got, `"name":"o-json sample"`) || !strings.Contains(got, `"count":4`) {
		t.Fatalf("viewer output %s", got)
	}
	onRoot(t, a)
	if strings.Contains(view(a), "o-json sample") {
		t.Fatalf("the data pane shows the deleted key\n%s", view(a))
	}
}

// q (app.quit) with edits no write saved asks once, as vim refuses :q:
// n stays, y quits; Q (app.quit {force}) quits without asking; with no
// edits, or once they are written, q quits at once.
func TestQuitAsksWithUnsavedEdits(t *testing.T) {
	a := open(t, sample, 120, 30)
	press(t, a, "q")
	if !a.r.Quit {
		t.Fatal("q without edits did not quit")
	}
	a = open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	press(t, a, "q")
	if a.r.Quit || a.r.TopModal() == "" || !strings.Contains(view(a), "Quit without writing?") {
		t.Fatalf("q did not ask: quit %v modal %q\n%s", a.r.Quit, a.r.TopModal(), view(a))
	}
	press(t, a, "n")
	if a.r.Quit || !a.unsaved() {
		t.Fatal("n quit or dropped the edits")
	}
	press(t, a, "q", "y")
	if !a.r.Quit {
		t.Fatal("y did not quit")
	}
	a = open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<", "Q")
	if !a.r.Quit || a.r.TopModal() != "" {
		t.Fatalf("Q asked: modal %q", a.r.TopModal())
	}
	// Writing clears the unsaved state.
	a = open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	a.markSaved()
	press(t, a, "q")
	if !a.r.Quit {
		t.Fatal("q after a write asked")
	}
}

// p (viewer.paste) with edits pending asks once (viewer.discard): n keeps
// the edits, y drops them and pastes.
func TestPasteAsksWithEditsPending(t *testing.T) {
	a := open(t, sample, 120, 30)
	a.Paste = func() (string, error) { return `{"pasted": 1}`, nil }
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	viewerDo(t, a, "paste", nil)
	if a.r.TopModal() == "" || a.Path != sample {
		t.Fatalf("paste did not ask: modal %q", a.r.TopModal())
	}
	press(t, a, "n")
	settle(a)
	if !a.unsaved() || a.Doc.Name == noFile {
		t.Fatal("n pasted or dropped the edits")
	}
	viewerDo(t, a, "paste", nil)
	finish(a, Press(a.m, "y"))
	settle(a)
	if a.Doc == nil || a.Doc.Name != noFile || a.Doc.Root.Children[0].Key != "pasted" || a.unsaved() {
		t.Fatalf("y did not paste: %+v", a.Doc)
	}
	// With text, it pastes the text once confirmed.
	a.on(t, ".pasted")
	press(t, a, "d", "d")
	viewerDo(t, a, "paste", ochazuke.Arguments{"text": `[1]`})
	finish(a, Press(a.m, "y"))
	settle(a)
	if a.Doc.Root.Kind != KArray {
		t.Fatalf("paste text after asking: %v", a.Doc.Root.Kind)
	}
}

// F14: T keeps the cursor: the tree's path and the data pane's row.
func TestFlipKeepsTheRow(t *testing.T) {
	a := open(t, sample, 120, 30)
	a.on(t, ".items")
	view(a)
	if err := a.data.list.Focus(); err != nil {
		t.Fatal(err)
	}
	press(t, a, "j", "j")
	if a.data.Selected() != 2 {
		t.Fatalf("row %d", a.data.Selected())
	}
	for _, want := range []bool{true, false} {
		press(t, a, "T")
		if a.original != want || a.data.Selected() != 2 {
			t.Fatalf("T (original %v): row %d, want 2\n%s", a.original, a.data.Selected(), view(a))
		}
		if s, _ := a.selectedOriginal(); want && (s == nil || s.Path != ".items") {
			t.Fatalf("T lost the path: %v", s)
		}
	}
}

// F6: the copy a document starts with is the floor of the undo stack: u
// there changes nothing and marks nothing edited; undo.at-start says it
// (info, the default) or stays silent. An edit still undoes to the copy.
func TestUndoAtStartIsTheFloor(t *testing.T) {
	for _, setting := range []string{"info", "silent"} {
		a := open(t, nestDoc, 120, 30)
		if err := a.r.Settings().Set(settingUndoAtStart, setting); err != nil {
			t.Fatal(err)
		}
		before := output(t, a)
		press(t, a, "u")
		if output(t, a) != before || a.unsaved() || strings.Contains(view(a), "[+]") {
			t.Fatalf("%s: u at the start changed the state: %s", setting, output(t, a))
		}
		if said := a.m.Info == "already at oldest change"; said != (setting == "info") {
			t.Fatalf("%s: info %q", setting, a.m.Info)
		}
		a.on(t, ".z")
		press(t, a, "d", "d", "u", "u")
		if output(t, a) != before || a.unsaved() {
			t.Fatalf("%s: an edit's undo: %s", setting, output(t, a))
		}
	}
}

// The floor is only where nothing was replaced: R over edits (after its
// confirm) starts a new copy, and u brings the dropped edits back, as
// viewer.discard says.
func TestUndoAfterDiscardBringsEditsBack(t *testing.T) {
	a := open(t, nestDoc, 120, 30)
	a.on(t, ".z")
	press(t, a, "d", "d")
	edited := output(t, a)
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "discard", Target: pageID, Args: ochazuke.Arguments{"path": nestDoc}, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	settle(a)
	view(a)
	if a.unsaved() {
		t.Fatal("the reload kept the edits")
	}
	press(t, a, "u")
	if got := output(t, a); got != edited {
		t.Fatalf("u after the reload: %s, want %s", got, edited)
	}
}
