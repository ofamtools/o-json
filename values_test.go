package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// selected is the inline input's select range as text ("" for none).
func selected(a *App) string {
	start, end, ok := a.vedit.in.SelectedText()
	if !ok {
		return ""
	}
	return string(a.vedit.in.Text[start:end])
}

// typeText types text into the focused input, one key per rune.
func typeText(t *testing.T, a *App, text string) {
	t.Helper()
	for _, r := range text {
		key := string(r)
		if r == ' ' {
			key = "space"
		}
		press(t, a, key)
	}
}

// onData puts the focus on the data pane's row whose label (a field's
// key, a table row's path) is label, the structure at the node from
// path.
func onData(t *testing.T, a *App, path, label string) {
	t.Helper()
	if path == "." {
		onRoot(t, a)
	} else {
		a.on(t, path)
	}
	view(a)
	if err := a.data.list.Focus(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < a.data.src.n; i++ {
		if a.data.src.label(i) == label {
			if err := a.data.to(i); err != nil {
				t.Fatal(err)
			}
			if err := a.data.list.Focus(); err != nil {
				t.Fatal(err)
			}
			view(a)
			return
		}
	}
	t.Fatalf("no data row %q under %s\n%s", label, path, view(a))
}

// valueDo runs a value verb on the focus.
func valueDo(t *testing.T, a *App, verb string, args ochazuke.Arguments) {
	t.Helper()
	if _, err := a.r.Execute(ochazuke.Request{Noun: "value", Verb: verb, Args: args}); err != nil {
		t.Fatalf("value.%s: %v", verb, err)
	}
	view(a)
}

// field is the restructured document's value at a path, compact.
func field(t *testing.T, a *App, path string) string {
	t.Helper()
	d := a.restructuredDoc()
	if d.err != nil {
		t.Fatal(d.err)
	}
	n, err := Get(d.root, path)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return Compact(n, 0)
}

// r on a field opens the inline input with the value selected; typing
// replaces it and enter sets it, the type read from the text: a number, a
// string, a bool, null, an object. Each is one value edit, [+] shows, and
// the data pane keeps the row.
func TestEditValueTypes(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".", "version")
	press(t, a, "r")
	if a.r.Focus != valueInputID || a.r.Mode.Name != "insert" {
		t.Fatalf("r: focus %q mode %q", a.r.Focus, a.r.Mode.Name)
	}
	if sel := selected(a); sel != "2" {
		t.Fatalf("r did not select the value: %q", sel)
	}
	if !strings.Contains(view(a), "version (number): 2") {
		t.Fatalf("no inline input\n%s", view(a))
	}
	typeText(t, a, "42")
	press(t, a, "enter")
	if got := field(t, a, ".version"); got != "42" {
		t.Fatalf("number: %s", got)
	}
	if a.r.Focus == valueInputID || a.r.Mode.Name != "normal" || a.dataNode() == nil || a.dataNode().Key != "version" {
		t.Fatalf("after enter: focus %q mode %q", a.r.Focus, a.r.Mode.Name)
	}
	frame := view(a)
	if !strings.Contains(frame, "sample.json [+]") || !strings.Contains(frame, "1 value edit") || !a.unsaved() {
		t.Fatalf("no [+]\n%s", frame)
	}
	for _, c := range []struct{ typed, want string }{
		{"hello", `"hello"`},
		{"true", "true"},
		{"null", "null"},
		{`{"a":[1]}`, `{"a":[1]}`},
		{"-1.5", "-1.5"},
	} {
		press(t, a, "r")
		typeText(t, a, c.typed)
		if _, kind := inferValue(c.typed); !strings.Contains(view(a), "version ("+string(kind)+"): ") {
			t.Fatalf("%s: the prefix does not show %s\n%s", c.typed, kind, view(a))
		}
		press(t, a, "esc") // esc keeps the typing, as enter does
		if got := field(t, a, ".version"); got != c.want {
			t.Fatalf("%s: %s, want %s", c.typed, got, c.want)
		}
	}
	if n := len(a.rs.patches); n != 6 {
		t.Fatalf("%d patches, want 6", n)
	}
	// Unchanged text is no edit.
	press(t, a, "r", "enter")
	if n := len(a.rs.patches); n != 6 {
		t.Fatalf("an unchanged accept made a patch (%d)", n)
	}
	agrees(t, a)
}

// The text reads as the type it looks like; "…" forces a string, and a
// string whose text reads as another type opens quoted, so it stays one.
func TestValueTypeInferenceAndForcedString(t *testing.T) {
	for text, want := range map[string]string{
		"42": "42 number", "-1.5e3": "-1.5e3 number", "007": `"007" string`, "true": "true bool", "false": "false bool",
		"null": "null null", `"42"`: `"42" string`, `"null"`: `"null" string`, `{"b":1,"a":2}`: `{"b":1,"a":2} object`,
		"[1, 2]": "[1,2] array", "{a:1}": `"{a:1}" string`, "": `"" string`, "hello world": `"hello world" string`, `"unclosed`: `"\"unclosed" string`,
	} {
		raw, kind := inferValue(text)
		if got := string(raw) + " " + string(kind); got != want {
			t.Errorf("%q: %s, want %s", text, got, want)
		}
	}
	for value, want := range map[string]string{`"42"`: `"42"`, `"ann"`: "ann", `"true"`: `"true"`, `12`: "12", `{"a":1}`: `{"a":1}`} {
		if got := editText(mustBuild(t, value)); got != want {
			t.Errorf("editText(%s) = %q, want %q", value, got, want)
		}
	}
	a := open(t, sample, 120, 30)
	onData(t, a, ".", "version")
	press(t, a, "r")
	typeText(t, a, `"7"`)
	if !strings.Contains(view(a), "version (string): ") {
		t.Fatalf("forced string prefix\n%s", view(a))
	}
	press(t, a, "enter")
	if got := field(t, a, ".version"); got != `"7"` {
		t.Fatalf("forced string: %s", got)
	}
	// It opens quoted now, so an edit keeps it a string.
	press(t, a, "r")
	if got := string(a.vedit.in.Text); got != `"7"` {
		t.Fatalf("a numeric string opens as %q", got)
	}
	press(t, a, "esc")
}

// d d deletes one field of an object, and one item of an array (a table
// row on its # column); a table cell's d d deletes that field of that row.
func TestDeleteFieldAndItem(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".", "version")
	press(t, a, "d", "d")
	if got := field(t, a, "."); strings.Contains(got, `"version"`) {
		t.Fatalf("version is there: %s", got)
	}
	onData(t, a, ".items", ".items[1]")
	press(t, a, "d", "d")
	if got := field(t, a, ".items"); strings.Contains(got, `"beta"`) || !strings.Contains(got, `"gamma"`) {
		t.Fatalf("item 1: %s", got)
	}
	// The cell under the cursor: column 2 is name.
	onData(t, a, ".items", ".items[0]")
	press(t, a, "l", "l", "d", "d")
	if got := field(t, a, ".items[0]"); !strings.HasPrefix(got, `{"id":1,"owner":`) {
		t.Fatalf("cell: %s", got)
	}
	if got := field(t, a, ".items[1].name"); got != `"gamma"` {
		t.Fatalf("another row's name: %s", got)
	}
	agrees(t, a)
}

// o adds a key after the field: its name first (a placeholder selected),
// then its value (null selected); in an array it adds an item after the
// row.
func TestAddField(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".", "name")
	press(t, a, "o")
	if sel := selected(a); sel != "new-key" || !strings.Contains(view(a), "new key: new-key") {
		t.Fatalf("o: %q\n%s", sel, view(a))
	}
	typeText(t, a, "nick")
	press(t, a, "enter")
	if sel := selected(a); sel != "null" || !strings.Contains(view(a), "nick (null): null") {
		t.Fatalf("value input: %q\n%s", sel, view(a))
	}
	typeText(t, a, "nj")
	press(t, a, "enter")
	if got := field(t, a, "."); !strings.HasPrefix(got, `{"name":"o-json sample","nick":"nj","version":2`) {
		t.Fatalf("add: %s", got)
	}
	if len(a.rs.patches) != 1 || a.rs.patches[0].After != "name" {
		t.Fatalf("one add patch after name: %+v", a.rs.patches)
	}
	// An item, after the row.
	onData(t, a, ".items", ".items[0]")
	valueDo(t, a, "add", ochazuke.Arguments{"text": `{"id": 9}`})
	if got := field(t, a, ".items[1]"); got != `{"id":9}` {
		t.Fatalf("item: %s", got)
	}
	agrees(t, a)
}

// K renames a key in that one object: the other items keep it, and the
// structure's mapping is unchanged.
func TestRenameOneKey(t *testing.T) {
	a := open(t, sample, 120, 30)
	before := mappingKey(&Mapping{Map: a.Mapping().Map})
	onData(t, a, ".items", ".items[0]")
	press(t, a, "l", "l", "K")
	if sel := selected(a); sel != "name" || !strings.Contains(view(a), "rename name to: name") {
		t.Fatalf("K: %q\n%s", sel, view(a))
	}
	typeText(t, a, "title")
	press(t, a, "enter")
	if got := field(t, a, ".items[0]"); !strings.HasPrefix(got, `{"id":1,"title":"alpha",`) {
		t.Fatalf("item 0: %s", got)
	}
	if got := field(t, a, ".items[1].name"); got != `"beta"` {
		t.Fatalf("item 1: %s", got)
	}
	if mappingKey(&Mapping{Map: a.Mapping().Map}) != before {
		t.Fatal("the rename changed the structure")
	}
	// A key the object has already is refused.
	onData(t, a, ".", "name")
	valueDo(t, a, "rename", nil)
	press(t, a, "ctrl+u")
	typeText(t, a, "version")
	Press(a.m, "enter")
	if !strings.Contains(a.m.Status, `key "version" already`) || len(a.rs.patches) != 1 {
		t.Fatalf("rename onto a key: %q %+v", a.m.Status, a.rs.patches)
	}
	agrees(t, a)
}

// y yanks a value with its type, and p puts it over another; text
// yanked elsewhere reads as typed text.
func TestPasteValue(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".", "version")
	press(t, a, "y")
	onData(t, a, ".", "name")
	press(t, a, "p")
	if got := field(t, a, ".name"); got != "2" {
		t.Fatalf("number put: %s", got)
	}
	// A string that reads as a number stays a string.
	onData(t, a, ".meta", "generated")
	valueDo(t, a, "edit", ochazuke.Arguments{"text": `"10"`})
	press(t, a, "y")
	onData(t, a, ".meta", "count")
	press(t, a, "p")
	if got := field(t, a, ".meta.count"); got != `"10"` {
		t.Fatalf("string put: %s", got)
	}
	// An object, from a table cell, into the cell of another row.
	onData(t, a, ".items", ".items[0]")
	press(t, a, "l", "l", "l", "y") // owner
	press(t, a, "j", "p")
	if got := field(t, a, ".items[1].owner"); got != `{"name":"ann","email":"ann@example.com"}` {
		t.Fatalf("object put: %s", got)
	}
	// Plain text (a yanked structure path) reads as typed text.
	if err := a.r.StoreYank(ochazuke.Yank{Text: "true"}, "", false); err != nil {
		t.Fatal(err)
	}
	onData(t, a, ".", "name")
	press(t, a, "p")
	if got := field(t, a, ".name"); got != "true" {
		t.Fatalf("text put: %s", got)
	}
	agrees(t, a)
}

// u undoes a value edit and ctrl+r redoes it, from the data pane, one
// step each and in order with the structure edits.
func TestValueUndoRedo(t *testing.T) {
	a := open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<") // a structure edit first
	onData(t, a, ".", "name")
	valueDo(t, a, "edit", ochazuke.Arguments{"text": "x"})
	valueDo(t, a, "delete", nil)
	press(t, a, "u")
	if got := field(t, a, ".name"); got != `"x"` || a.data.list.Focused() == false {
		t.Fatalf("u: %s (focus %q)", got, a.r.Focus)
	}
	press(t, a, "u")
	if got := field(t, a, ".name"); got != `"o-json sample"` || len(a.rs.patches) != 0 {
		t.Fatalf("u u: %s", got)
	}
	press(t, a, "ctrl+r")
	if got := field(t, a, ".name"); got != `"x"` {
		t.Fatalf("ctrl+r: %s", got)
	}
	press(t, a, "u", "u") // the value edit, then the structure edit
	identical(t, a)
}

// A value edit survives a structure edit that keeps its path: the patch
// is on .name, and moving .meta.count to the top level leaves .name.
func TestPatchSurvivesAStructureMove(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".", "name")
	valueDo(t, a, "edit", ochazuke.Arguments{"text": "renamed"})
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	got := agrees(t, a)
	if !strings.Contains(got, `"name":"renamed"`) || !strings.Contains(got, `"count":4,"meta"`) || len(a.orphans()) != 0 {
		t.Fatalf("output %s, orphans %v", got, a.orphans())
	}
}

// A structure edit that removes the path a value edit targets orphans
// it: kept and listed (value.patches), not applied; the breadcrumb says
// so; undoing the structure edit applies it again.
func TestOrphanedPatchWhenItsPathGoes(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".meta", "count")
	valueDo(t, a, "edit", ochazuke.Arguments{"text": "9"})
	if got := field(t, a, ".meta.count"); got != "9" {
		t.Fatalf("edit: %s", got)
	}
	a.on(t, ".meta.count")
	press(t, a, "<", "<") // .meta.count is now .count
	if got := field(t, a, ".count"); got != "4" {
		t.Fatalf("the orphan applied: %s", got)
	}
	v, err := a.r.Execute(ochazuke.Request{Noun: "value", Verb: "patches"})
	if err != nil {
		t.Fatal(err)
	}
	list := v.([]patchStatus)
	if len(list) != 1 || !list[0].Orphaned || list[0].Path != ".meta.count" {
		t.Fatalf("value.patches: %+v", list)
	}
	if !strings.Contains(view(a), "1 value edit (1 orphaned)") || !a.unsaved() {
		t.Fatalf("breadcrumb\n%s", view(a))
	}
	agrees(t, a) // the orphan is skipped by --map and by jq too
	press(t, a, "u")
	if got := field(t, a, ".meta.count"); got != "9" || len(a.orphans()) != 0 {
		t.Fatalf("after undo: %s, orphans %v", got, a.orphans())
	}
	// value.drop forgets it.
	valueDo(t, a, "drop", ochazuke.Arguments{"index": 1})
	identical(t, a)
}

// The mapping spec carries the value edits: o-json restructure --map with
// it, and the jq program, make what the data pane shows; a write clears
// the unsaved state.
func TestValueEditsInTheMapAgreeWithJQ(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".items", ".items[2]")
	press(t, a, "d", "d")
	onData(t, a, ".", "version")
	valueDo(t, a, "edit", ochazuke.Arguments{"text": `{"major": 2, "minor": 1}`})
	onData(t, a, ".", "name")
	valueDo(t, a, "add", ochazuke.Arguments{"key": "odd key", "text": "[true]"})
	spec, _ := json.Marshal(a.Mapping())
	if !strings.Contains(string(spec), `"patches":[{"op":"delete","path":".items[2]"}`) {
		t.Fatalf("spec %s", spec)
	}
	got := agrees(t, a)
	if !strings.Contains(got, `"odd key":[true]`) || !strings.Contains(got, `"version":{"major":2,"minor":1}`) {
		t.Fatalf("output %s", got)
	}
	text, err := a.mappingText()
	if err != nil || !strings.Contains(text, "\"patches\": [\n") {
		t.Fatalf("mapping text %v\n%s", err, text)
	}
	a.markSaved()
	if a.unsaved() || strings.Contains(view(a), "[+]") {
		t.Fatal("a write left the value edits unsaved")
	}
}

// data.editable off keeps the data pane read-only: the value keys are
// not available, so r, d d, o, K and p do nothing (p pastes a document,
// as before); on the original (T) they are not available either.
func TestDataEditableOffIsReadOnly(t *testing.T) {
	a := open(t, sample, 120, 30)
	if err := a.r.Settings().Set(settingDataEditable, false); err != nil {
		t.Fatal(err)
	}
	onData(t, a, ".", "version")
	for _, verb := range []string{"edit", "delete", "add", "rename", "paste"} {
		if a.available("value", verb) {
			t.Errorf("value.%s is available", verb)
		}
	}
	press(t, a, "r", "d", "d", "o", "K")
	if len(a.rs.patches) != 0 || a.vedit != nil || a.unsaved() {
		t.Fatalf("keys edited a read-only data pane: %+v", a.rs.patches)
	}
	if !a.available("value", "yank") {
		t.Error("y is not available on a read-only data pane")
	}
	if err := a.r.Settings().Set(settingDataEditable, true); err != nil {
		t.Fatal(err)
	}
	if !a.available("value", "edit") {
		t.Fatal("value.edit is not available on")
	}
	press(t, a, "T")
	onDataOriginal(t, a, "version")
	if a.available("value", "edit") || a.available("value", "delete") {
		t.Fatal("the original's data is editable")
	}
}

// onDataOriginal focuses the original's data row label at the root.
func onDataOriginal(t *testing.T, a *App, label string) {
	t.Helper()
	selectPath(t, a, ".")
	if err := a.data.list.Focus(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < a.data.src.n; i++ {
		if a.data.src.label(i) == label {
			_ = a.data.to(i)
			_ = a.data.list.Focus()
			return
		}
	}
	t.Fatalf("no row %q", label)
}

// q with only value edits asks, as with structure edits.
func TestQuitAsksWithValueEdits(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".", "version")
	valueDo(t, a, "delete", nil)
	press(t, a, "q")
	if a.r.Quit || a.r.TopModal() == "" {
		t.Fatal("q did not ask about a value edit")
	}
}

// A scalar shown alone (the data pane holds no rows) edits too, and the
// inline input carries the terminal cursor.
func TestEditAScalarShownAlone(t *testing.T) {
	a := open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	view(a)
	if err := a.data.list.Focus(); err != nil {
		t.Fatal(err)
	}
	press(t, a, "r")
	v := a.m.View()
	if v.Cursor == nil || !strings.Contains(view(a), "count (number): 4") {
		t.Fatalf("cursor %v\n%s", v.Cursor, view(a))
	}
	typeText(t, a, "5")
	press(t, a, "enter")
	if got := field(t, a, ".meta.count"); got != "5" {
		t.Fatalf("scalar: %s", got)
	}
}
