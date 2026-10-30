package main

import (
	"strings"
	"testing"
)

func yanked(t *testing.T, a *App, register string) string {
	t.Helper()
	y, err := a.r.ReadYank(register)
	if err != nil {
		t.Fatal(err)
	}
	return y.Text
}

// keys presses keys one by one (Press, no status check), then draws.
func keys(a *App, ks ...string) {
	for _, k := range ks {
		Press(a.m, k)
	}
	view(a)
}

// y waits for a text object or a motion: the shortcut rows, i/a objects,
// counts and registers.
func TestOperatorYankObjects(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	selectPath(t, a, ".items")
	press(t, a, "ctrl+w", "l", "l", "l") // the name column of .items[0]
	cases := []struct {
		keys []string
		want string
	}{
		{[]string{"y", "y"}, "alpha"},
		{[]string{"y", "i", "v"}, "alpha"},
		{[]string{"y", "a", "v"}, `"name": "alpha"`},
		{[]string{"y", "p"}, ".items[0].name"},
		{[]string{"y", "a", "p"}, ".items[].name"},
		{[]string{"y", "k"}, "name"},
		{[]string{"y", "i", "c"}, `"delta"`},
		{[]string{"y", "a", "c"}, `"name": [`},
		{[]string{"Y"}, `"id": 1`},
		{[]string{"2", "y", "j"}, `"beta"`},
		{[]string{"y", "i", "t"}, `"gamma"`},
	}
	for _, c := range cases {
		keys(a, c.keys...)
		if got := yanked(t, a, ""); !strings.Contains(got, c.want) || a.m.Status != "" {
			t.Fatalf("%v: yank %q status %q, want %q", c.keys, got, a.m.Status, c.want)
		}
		if a.r.BaseMode() != "table" {
			t.Fatalf("%v: mode %q after", c.keys, a.r.BaseMode())
		}
	}
	keys(a, "y", "i")
	if a.r.Mode.Name != "operator_pending_i" || !strings.Contains(view(a), "OPERATOR PENDING I") {
		t.Fatalf("y i: mode %q", a.r.Mode.Name)
	}
	keys(a, "esc")
	keys(a, "\"", "a", "y", "i", "r")
	if got := yanked(t, a, "a"); !strings.Contains(got, `"alpha"`) {
		t.Fatalf(`"a y i r: %q`, got)
	}
}

// d and c change the document: with modes.edit-gate on they need EDIT;
// d + object or motion is one undo step; . repeats it.
func TestOperatorDeleteChangeRepeat(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	gated(t, a)
	selectPath(t, a, ".items")
	press(t, a, "ctrl+w", "l")
	keys(a, "d", "i", "r")
	if !strings.Contains(a.m.Status, "press i first") || len(a.rs.patches) != 0 {
		t.Fatalf("d i r in TABLE: %q %d", a.m.Status, len(a.rs.patches))
	}
	press(t, a, "i")
	keys(a, "d", "i", "r")
	if len(a.rs.patches) != 1 || a.rs.patches[0].Path != ".items[0]" {
		t.Fatalf("d i r: %+v (%q)", a.rs.patches, a.m.Status)
	}
	keys(a, ".")
	if len(a.rs.patches) != 2 {
		t.Fatalf(". after d i r: %+v (%q)", a.rs.patches, a.m.Status)
	}
	keys(a, "u", "u")
	keys(a, "2", "d", "j") // Vim's 2dj: this row and two below
	if len(a.rs.patches) != 3 || a.rs.patches[0].Path != ".items[2]" || a.rs.patches[2].Path != ".items[0]" {
		t.Fatalf("2 d j: %+v (%q)", a.rs.patches, a.m.Status)
	}
	keys(a, "u")
	if len(a.rs.patches) != 0 {
		t.Fatalf("u after 2 d j: %+v", a.rs.patches)
	}
	keys(a, "l", "l", "d", "i", "c")
	if len(a.rs.patches) != 4 || !strings.HasSuffix(a.rs.patches[3].Path, ".name") {
		t.Fatalf("d i c: %+v (%q)", a.rs.patches, a.m.Status)
	}
	keys(a, "u", "c", "i", "k")
	if a.vedit == nil || a.vedit.what != "rename" {
		t.Fatalf("c i k: %+v (%q)", a.vedit, a.m.Status)
	}
}

// s sorts the table by the column (a view: no patch); s s again reverses;
// s on # clears it. ! opens the jq line with the object's path.
func TestOperatorSortAndJQ(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	selectPath(t, a, ".items")
	press(t, a, "ctrl+w", "l", "l", "l")
	keys(a, "s", "i", "c")
	if a.sort == nil || a.sort.col != "name" || a.sort.reverse || len(a.rs.patches) != 0 {
		t.Fatalf("s i c: %+v (%q)", a.sort, a.m.Status)
	}
	keys(a, "s", "s")
	if !a.sort.reverse || nodeAt(a.r.Instances[a.r.Focus]).Field("name").Value != "gamma" && !strings.Contains(view(a), "3  4") {
		t.Fatalf("s s: %+v", a.sort)
	}
	keys(a, "0", "s", "i", "c")
	if a.sort != nil {
		t.Fatalf("s on #: %+v (%q)", a.sort, a.m.Status)
	}
	keys(a, "l", "l", "!", "i", "c")
	if a.r.Focus != jqFieldID || a.queryText() != ".items[].name" || !a.searchJQ() {
		t.Fatalf("! i c: focus %q query %q", a.r.Focus, a.queryText())
	}
}

// On the structure, y p copies the path, y i v the values at it, ! !
// opens the jq line with it.
func TestOperatorOnTheStructure(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	selectPath(t, a, ".items[].name")
	keys(a, "y", "p")
	if got := yanked(t, a, ""); got != ".items[].name" {
		t.Fatalf("y p: %q (%q)", got, a.m.Status)
	}
	keys(a, "y", "i", "v")
	if got := yanked(t, a, ""); !strings.Contains(got, `"delta"`) {
		t.Fatalf("y i v: %q (%q)", got, a.m.Status)
	}
	keys(a, "y", "k")
	if got := yanked(t, a, ""); got != "name" {
		t.Fatalf("y k: %q", got)
	}
	keys(a, "!", "!")
	if a.r.Focus != jqFieldID || a.queryText() != ".items[].name" {
		t.Fatalf("! !: focus %q query %q", a.r.Focus, a.queryText())
	}
}
