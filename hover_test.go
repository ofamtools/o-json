package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// nestedDoc is an array of people whose cells hold objects (address,
// meta), arrays of objects (orders, their items) and scalar lists (tags).
const nestedDoc = "testdata/nested.json"

func build(t *testing.T, text string) *Node {
	t.Helper()
	n, err := Build([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHoverGridPerKind(t *testing.T) {
	for _, c := range []struct {
		name, doc string
		header    []string
		rows      [][]string
	}{
		{"object", `{"city": "Paris", "geo": {"lat": 1}, "tags": [1, 2]}`,
			[]string{"city", "geo", "tags"}, [][]string{{"Paris", "{1 key}", "[2]"}}},
		{"array of objects", `[{"id": 1, "x": {"a": 1, "b": 2}}, {"id": 2, "y": true}]`,
			[]string{"#", "id", "x", "y"}, [][]string{{"0", "1", "{2 keys}", ""}, {"1", "2", "", "true"}}},
		{"array of scalars", `["a", 2, null, true]`,
			nil, [][]string{{"a · 2 · null · true"}}},
		{"mixed array", `[1, [2, 3], "s"]`,
			[]string{"#", "value"}, [][]string{{"0", "1"}, {"1", "[2]"}, {"2", "s"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := gridOf(build(t, c.doc))
			if !slices.Equal(g.header, c.header) {
				t.Errorf("header %q, want %q", g.header, c.header)
			}
			if len(g.rows) != len(c.rows) {
				t.Fatalf("rows %q, want %q", g.rows, c.rows)
			}
			for i := range g.rows {
				if !slices.Equal(g.rows[i], c.rows[i]) {
					t.Errorf("row %d %q, want %q", i, g.rows[i], c.rows[i])
				}
			}
			if len(g.lines) != len(g.rows) {
				t.Errorf("%d line values for %d lines", len(g.lines), len(g.rows))
			}
		})
	}
}

// hoverAt opens the nested document with the data pane focused on row
// 2 (Cy C) and column col (6 address, 7 orders, 4 tags).
func hoverAt(t *testing.T, width, height, col int) *App {
	t.Helper()
	a := open(t, nestedDoc, width, height)
	press(t, a, "tab", "j", "j")
	for range col {
		press(t, a, "l")
	}
	if a.data.Selected() != 2 || a.data.list.Column() != col {
		t.Fatalf("row %d column %d", a.data.Selected(), a.data.list.Column())
	}
	return a
}

func TestHoverOpensUnderTheRowAndClosesOnMove(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	frame := view(a)
	if !strings.Contains(frame, "Austin  73301  US") {
		t.Fatalf("no address sub-table\n%s", frame)
	}
	if got := len(a.hoverLevels()); got != 1 {
		t.Fatalf("%d levels open", got)
	}
	// Along the row, the next cell (orders) replaces it.
	press(t, a, "l")
	frame = view(a)
	if strings.Contains(frame, "Austin") || !strings.Contains(frame, "o3-0") {
		t.Fatalf("the orders sub-table did not replace the address\n%s", frame)
	}
	// The # column is the row itself: nothing opens.
	press(t, a, "0")
	if len(a.hoverLevels()) != 0 || strings.Contains(view(a), "o3-0") {
		t.Fatalf("# opened a sub-table\n%s", view(a))
	}
	// The tree focused: the pane draws no sub-table.
	press(t, a, "$")
	press(t, a, "tab")
	if strings.Contains(view(a), "    #  id") {
		t.Fatalf("a sub-table with the tree focused\n%s", view(a))
	}
}

func TestHoverChainOnlyUnderTheCursor(t *testing.T) {
	a := hoverAt(t, 120, 30, 7) // orders
	press(t, a, "j", "l", "l", "l")
	if got := len(a.hoverLevels()); got != 2 {
		t.Fatalf("%d levels, want orders and its items", got)
	}
	frame := view(a)
	if !strings.Contains(frame, "mug") || strings.Contains(frame, "spring") {
		t.Fatalf("frame\n%s", frame)
	}
	// Down to order 1's line (the cursor keeps its items column): order
	// 1's items open, order 0's close.
	press(t, a, "0", "j")
	if p := a.hover.path; len(p) != 1 || p[0].row != 1 {
		t.Fatalf("path %v, want order 1", p)
	}
	press(t, a, "$")
	if got := len(a.hoverLevels()); got != 2 {
		t.Fatalf("%d levels", got)
	}
	frame = view(a)
	if strings.Contains(frame, "kettle") || !strings.Contains(frame, "tray") {
		t.Fatalf("order 0's items still open, or order 1's not\n%s", frame)
	}
}

func TestHoverWalkAcrossLevelsAndOutTheBottom(t *testing.T) {
	a := hoverAt(t, 120, 30, 7)
	press(t, a, "j") // order 0's line
	press(t, a, "l", "l", "l")
	press(t, a, "j") // into its items: mug
	if p := a.hover.path; len(p) != 2 || p[1] != (hoverPos{0, 0}) {
		t.Fatalf("path %v", p)
	}
	press(t, a, "j", "j") // kettle, filter
	press(t, a, "j")      // out of the items: order 1
	if p := a.hover.path; len(p) != 1 || p[0].row != 1 {
		t.Fatalf("path %v, want order 1", p)
	}
	// order 1's items are the next lines drawn: j walks into them.
	press(t, a, "j")
	if p := a.hover.path; len(p) != 2 || p[0].row != 1 {
		t.Fatalf("path %v, want order 1's first item", p)
	}
	press(t, a, "k", "0", "j") // on order 1's #: nothing open below
	if a.data.Selected() != 3 || len(a.hover.path) != 0 {
		t.Fatalf("row %d path %v, want row 3 on the row", a.data.Selected(), a.hover.path)
	}
	// k from row 3 goes to row 2 (its sub-table opens below it), and k
	// again to row 1: the sub-table under row 2 is not walked from below.
	press(t, a, "k")
	if a.data.Selected() != 2 || len(a.hover.path) != 0 {
		t.Fatalf("row %d path %v", a.data.Selected(), a.hover.path)
	}
}

func TestHoverCounts(t *testing.T) {
	one := hoverAt(t, 120, 30, 7)
	press(t, one, "j", "l", "l", "l", "j", "j", "j", "j")
	many := hoverAt(t, 120, 30, 7)
	press(t, many, "j", "3", "l", "4", "j")
	if one.data.Selected() != many.data.Selected() || !slices.Equal(one.hover.path, many.hover.path) {
		t.Fatalf("one at a time %d %v, counted %d %v", one.data.Selected(), one.hover.path, many.data.Selected(), many.hover.path)
	}
	// Up does not walk into a sub-table it meets from below (only the
	// chain under the cursor is open): order 0, then the row.
	press(t, many, "2", "k")
	if many.data.Selected() != 2 || len(many.hover.path) != 0 {
		t.Fatalf("2k: row %d path %v", many.data.Selected(), many.hover.path)
	}
}

func TestHoverJumpsLeaveTheSubTable(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	press(t, a, "j")
	if !a.hoverIn() {
		t.Fatal("j did not step into the address")
	}
	press(t, a, "G")
	if a.data.Selected() != 19 || a.hoverIn() {
		t.Fatalf("G: row %d in %v", a.data.Selected(), a.hoverIn())
	}
	b := open(t, nestedDoc, 120, 30)
	press(t, b, "tab")
	for range 6 {
		press(t, b, "l")
	}
	press(t, b, "j") // row 0's address line
	press(t, b, "g", "g")
	if b.data.Selected() != 0 || b.hoverIn() {
		t.Fatalf("gg: row %d in %v", b.data.Selected(), b.hoverIn())
	}
}

func TestHoverYankAndOpenTheCell(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	press(t, a, "j", "l", "l", "y")
	if y, _ := a.r.ReadYank(""); y.Text != "US" {
		t.Fatalf("yanked %q, want US (the country cell)", y.Text)
	}
	// enter on a scalar opens its line's object, on a nested cell the cell.
	press(t, a, "enter")
	if len(a.trail) != 1 || a.trail[0].Path() != "[2].address" {
		t.Fatalf("detail on %v", a.trail)
	}
	press(t, a, "esc")
	b := hoverAt(t, 120, 30, 7)
	press(t, b, "j", "l", "l", "l", "enter")
	if len(b.trail) != 1 || b.trail[0].Path() != "[2].orders[0].items" {
		t.Fatalf("detail on %v", b.trail)
	}
}

// Value edits act on the row; in a sub-table they refuse with a notice
// (and never pass the key on, so p does not load the clipboard).
func TestHoverEditsOnlyOnTheRow(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	press(t, a, "j")
	for _, key := range []string{"p", "r"} {
		Press(a.m, key)
		view(a)
		if !strings.Contains(a.m.Status, "not in a sub-table") || len(a.rs.patches) != 0 || a.vedit != nil {
			t.Fatalf("%s in a sub-table: status %q, %d patches", key, a.m.Status, len(a.rs.patches))
		}
	}
	if a.Path != nestedDoc {
		t.Fatalf("the document changed to %q", a.Path)
	}
}

func TestHoverSummarySetting(t *testing.T) {
	a := open(t, nestedDoc, 120, 30)
	if err := a.r.Settings().Set(settingNested, nestedSummary); err != nil {
		t.Fatal(err)
	}
	press(t, a, "tab", "j", "j", "l", "l", "l", "l", "l", "l")
	frame := view(a)
	if strings.Contains(frame, "Austin") {
		t.Fatalf("summary opened a sub-table\n%s", frame)
	}
	if !strings.Contains(frame, `{"city"…`) {
		t.Fatalf("summary lost the cell's summary\n%s", frame)
	}
	press(t, a, "j")
	if a.data.Selected() != 3 || a.hoverIn() {
		t.Fatalf("j under summary: row %d", a.data.Selected())
	}
}

// The frames at 80 and 120 columns with the orders sub-table open and
// its first order's items under the cursor.
func TestHoverRenderGolden(t *testing.T) {
	for _, width := range []int{80, 120} {
		a := hoverAt(t, width, 24, 7)
		press(t, a, "j", "l", "l", "l", "j")
		got := view(a)
		golden := filepath.Join("testdata", "hover-"+itoa(width)+".golden")
		if os.Getenv("UPDATE_GOLDEN") != "" {
			_ = os.WriteFile(golden, []byte(got), 0o644)
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%d columns\n%s\nwant\n%s", width, got, want)
		}
	}
}
