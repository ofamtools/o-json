package main

import (
	"strings"
	"testing"
)

const catalogDoc = "testdata/catalog-sample.json"

// One object is one row: its keys the header in document order, its
// values the cells, a nested value by its size, and no "#".
func TestObjectIsOneRow(t *testing.T) {
	a := open(t, catalogDoc, 120, 24)
	frame := view(a)
	if a.tab == nil || a.tab.Items || len(a.tab.Rows) != 1 || a.fields != nil {
		t.Fatalf("root is not one row: tab %v fields %v\n%s", a.tab, a.fields, frame)
	}
	var keys []string
	for _, c := range a.tab.Columns {
		keys = append(keys, c.Name)
	}
	if got := strings.Join(keys, " "); got != "schema name version aliases nouns bindings pages" {
		t.Fatalf("columns %q", got)
	}
	head := line(frame, "schema")
	if !strings.Contains(frame, "Object .") || strings.Contains(head, "#") {
		t.Fatalf("header\n%s", frame)
	}
	cells := line(frame, "o-json  v0.4")
	for _, want := range []string{"{12}", "[8]", "[10]", "[]"} {
		if !strings.Contains(cells, want) {
			t.Fatalf("cells lack %q: %q", want, cells)
		}
	}
}

// A nested cell opens its sub-table under the row; j walks into it and k
// back out, as on any table.
func TestObjectNestedCellOpensSubTable(t *testing.T) {
	a := open(t, catalogDoc, 120, 24)
	press(t, a, "ctrl+a", "o", "l", "l", "l", "l") // nouns
	frame := view(a)
	if !strings.Contains(frame, "ochazuke.actions") || !strings.Contains(line(frame, "verbs"), "name") {
		t.Fatalf("nouns did not open\n%s", frame)
	}
	press(t, a, "j", "l", "l")
	if !a.hoverIn() || len(a.hover.path) != 1 {
		t.Fatalf("j did not walk in: %v", a.hover.path)
	}
	if frame = view(a); !strings.Contains(frame, "quit") {
		t.Fatalf("verbs of app did not open a level deeper\n%s", frame)
	}
	press(t, a, "k")
	if a.hoverIn() || a.data.list.Column() != a.tab.ListColumn(4) {
		t.Fatalf("k did not come back to the row on nouns: column %d", a.data.list.Column())
	}
}

// A plain path inside the object lights its key's column.
func TestObjectPlainPathLightsColumn(t *testing.T) {
	a := open(t, catalogDoc, 120, 24)
	a.on(t, ".version")
	if a.shown == nil || a.shown.Path != "." || a.data.litCol != a.tab.ListColumn(2) || a.data.list.Column() != a.data.litCol {
		t.Fatalf("version: lit %d column %d\n%s", a.data.litCol, a.data.list.Column(), view(a))
	}
}

// object.view fields is the view as it was, and changing the setting
// shows the open object again in the other form.
func TestObjectViewFieldsSetting(t *testing.T) {
	a := open(t, catalogDoc, 120, 24)
	fieldsView(t, a)
	frame := view(a)
	if a.fields == nil || a.tab != nil || !strings.Contains(frame, "Fields .") || !strings.Contains(line(frame, "aliases"), "object{12}") {
		t.Fatalf("fields\n%s", frame)
	}
	if err := a.r.Settings().Set(settingObjectView, objectRow); err != nil {
		t.Fatal(err)
	}
	if frame = view(a); a.tab == nil || !strings.Contains(frame, "Object .") {
		t.Fatalf("back to the row\n%s", frame)
	}
}

// The detail shows an object as the same one row: enter steps into a
// nested cell, backspace back onto its column; fields keeps labeled rows.
func TestDetailObjectRow(t *testing.T) {
	a := open(t, catalogDoc, 120, 24)
	press(t, a, "ctrl+a", "o", "l", "l", "l", "l", "enter", "enter")
	frame := view(a)
	if a.detailTab == nil || !strings.Contains(frame, ".nouns[0]  object{2}") || !strings.Contains(line(frame, "name"), "verbs") {
		t.Fatalf("the detail's object is not one row\n%s", frame)
	}
	press(t, a, "l", "enter")
	if frame = view(a); !strings.Contains(frame, ".nouns[0].verbs  array[1]") {
		t.Fatalf("enter did not step into verbs\n%s", frame)
	}
	press(t, a, "backspace")
	if frame = view(a); !strings.Contains(frame, ".nouns[0]  object{2}") || a.detail.list.Column() != a.detailTab.ListColumn(1) {
		t.Fatalf("backspace: column %d\n%s", a.detail.list.Column(), frame)
	}
	press(t, a, "l")
	if a.detail.list.Column() != a.detailTab.ListColumn(1) {
		t.Fatalf("l at the last key moved: %d", a.detail.list.Column())
	}
}

// Value edits on the row act on the key under the cursor.
func TestObjectRowEdits(t *testing.T) {
	a := open(t, catalogDoc, 120, 24)
	press(t, a, "ctrl+a", "o", "l", "d", "d")
	if frame := view(a); strings.Contains(line(frame, "schema"), "name") || !strings.Contains(frame, "1 value edit") {
		t.Fatalf("d d did not delete name\n%s", frame)
	}
	press(t, a, "u")
	if frame := view(a); !strings.Contains(line(frame, "schema"), "name") {
		t.Fatalf("u did not bring name back\n%s", frame)
	}
}

// structure.lit-width fit widens the lit column to its data, a list of
// plain values written out; the widths go back when the light moves.
func TestLitColumnExpands(t *testing.T) {
	a := open(t, sample, 120, 24)
	selectPath(t, a, ".items[].tags")
	frame := view(a)
	if a.shown.Path != ".items" && a.shown.Path != ".items[]" || !strings.Contains(frame, "red · blue") {
		t.Fatalf("tags not written out (shown %s)\n%s", a.shown.Path, frame)
	}
	if lit := litCells(a); !lit[a.tab.ListColumn(3)] {
		t.Fatalf("tags not lit: %v (lit column %d)", lit, a.data.litCol)
	}
	selectPath(t, a, ".items[].name")
	if frame = view(a); strings.Contains(frame, "red · blue") || !strings.Contains(frame, "[2]") {
		t.Fatalf("widths did not go back\n%s", frame)
	}
	if err := a.r.Settings().Set(settingLitWidth, "normal"); err != nil {
		t.Fatal(err)
	}
	selectPath(t, a, ".items[].tags")
	if frame = view(a); strings.Contains(frame, "red · blue") {
		t.Fatalf("normal widened\n%s", frame)
	}
}

// fitColumns narrows the others first and keeps the kept column's width.
func TestFitColumnsKeepsOne(t *testing.T) {
	in := tableSource(&Table{Items: true, Columns: []TableColumn{{Name: "a"}, {Name: "bb"}, {Name: "c"}}})
	out := fitColumns(in.columns, []int{1, 20, 30, 20}, 50, 2)
	if out[2].Width != 30 || out[1].Width >= 20 {
		t.Fatalf("widths %d %d", out[1].Width, out[2].Width)
	}
}

func TestSummaryAndFull(t *testing.T) {
	doc, err := Build([]byte(`{"o":{"a":1},"e":{},"l":[1,"x",{"k":2}],"n":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ path, summary, full string }{
		{".o", "{1}", `{"a":1}`}, {".e", "{}", "{}"}, {".l", "[3]", `1 · x · {"k":2}`}, {".n", "[]", "[]"},
	} {
		n, err := Get(doc, c.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := Summary(n); got != c.summary {
			t.Errorf("%s summary %q", c.path, got)
		}
		if got := Full(n); got != c.full {
			t.Errorf("%s full %q", c.path, got)
		}
	}
}
