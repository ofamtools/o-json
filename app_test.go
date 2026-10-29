package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/strictcheck"
)

func open(t *testing.T, path string, width, height int) *App {
	t.Helper()
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Load(path); err != nil {
		t.Fatal(err)
	}
	a.m.Width, a.m.Height = width, height
	view(a) // the first frame measures the windows
	return a
}

// openOriginal opens a document with the viewer showing the original
// (T): the read-only structure tree of shapes and the document's data.
func openOriginal(t *testing.T, path string, width, height int) *App {
	t.Helper()
	a := open(t, path, width, height)
	press(t, a, "T")
	if !a.original {
		t.Fatal("T did not show the original")
	}
	return a
}

// readOnly opens a document with structure.editable off: the viewer as
// it was, the restructure page alone editing.
func readOnly(t *testing.T, path string, width, height int) *App {
	t.Helper()
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.r.Settings().Set(settingEditable, false); err != nil {
		t.Fatal(err)
	}
	if err := a.Load(path); err != nil {
		t.Fatal(err)
	}
	a.m.Width, a.m.Height = width, height
	view(a)
	return a
}

func view(a *App) string { return ansi.Strip(a.m.View().Content) }

// press sends keys one by one, failing on the first error the model
// shows. A yank's "copied ..." is the plain tone (Model.Info), not an error.
func press(t *testing.T, a *App, keys ...string) {
	t.Helper()
	for _, key := range keys {
		Press(a.m, key)
		if a.m.Status != "" {
			t.Fatalf("key %q: %s", key, a.m.Status)
		}
		view(a)
	}
}

// selectPath selects a structure path in the tree as the keys would.
func selectPath(t *testing.T, a *App, path string) {
	t.Helper()
	for range 40 {
		if s := a.selectedShape(); s != nil && s.Path == path {
			return
		}
		press(t, a, "k")
	}
	for range 40 {
		if s := a.selectedShape(); s != nil && s.Path == path {
			return
		}
		press(t, a, "j")
	}
	t.Fatalf("no %s in the tree", path)
}

// line is the first frame line containing text.
func line(frame, text string) string {
	for _, l := range strings.Split(frame, "\n") {
		if strings.Contains(l, text) {
			return l
		}
	}
	return ""
}

func TestTableShowsOptionalColumnsLast(t *testing.T) {
	a := open(t, sample, 120, 30)
	selectPath(t, a, ".items")
	frame := view(a)
	head := line(frame, "owner")
	order := []string{"#", "id", "name", "owner", "tags", "archived?", "note?"}
	at := 0
	for _, title := range order {
		i := strings.Index(head[at:], title)
		if i < 0 {
			t.Fatalf("header %q: %s missing or out of order\n%s", head, title, frame)
		}
		at += i + len(title)
	}
	if !strings.Contains(frame, "Table .items  1/4") {
		t.Fatalf("title\n%s", frame)
	}
	// Moving in the tree changes the data: the owners of every item.
	selectPath(t, a, ".items[].owner")
	frame = view(a)
	if !strings.Contains(line(frame, "email?"), "name") || !strings.Contains(frame, "ann@example.com") {
		t.Fatalf("owners\n%s", frame)
	}
}

func TestEnterOnRowShowsItemFields(t *testing.T) {
	a := open(t, sample, 120, 30)
	selectPath(t, a, ".items")
	press(t, a, "tab", "j", "enter")
	if a.r.TopModal() != detailID {
		t.Fatalf("no detail open: %q", a.r.TopModal())
	}
	frame := view(a)
	for _, want := range []string{".items[1]  object{5}", "archived", "true", "owner", `{"name":"bob"}`} {
		if !strings.Contains(frame, want) {
			t.Fatalf("detail lacks %q\n%s", want, frame)
		}
	}
	// Drill into owner, back out, close.
	press(t, a, "j", "j", "enter")
	if frame = view(a); !strings.Contains(frame, ".items[1].owner  object{1}") || !strings.Contains(line(frame, "bob"), "name") {
		t.Fatalf("drilled\n%s", frame)
	}
	press(t, a, "h")
	if frame = view(a); !strings.Contains(frame, ".items[1]  object{5}") {
		t.Fatalf("back out\n%s", frame)
	}
	if n := a.detail.Selected(); n != 2 {
		t.Fatalf("back out selects the owner entry, got %d", n)
	}
	press(t, a, "q")
	if a.r.TopModal() != "" || !a.data.list.Focused() {
		t.Fatalf("q closes the detail back onto the table (focus %s)", a.r.Focus)
	}
}

func TestObjectFieldsAndScalars(t *testing.T) {
	a := open(t, sample, 120, 30)
	frame := view(a)
	if !strings.Contains(frame, "Fields .") || !strings.Contains(line(frame, "version"), "2") {
		t.Fatalf("root fields\n%s", frame)
	}
	selectPath(t, a, ".name")
	if frame = view(a); !strings.Contains(frame, "Value .name") || !strings.Contains(frame, `"o-json sample"`) {
		t.Fatalf("scalar\n%s", frame)
	}
	// A field holding an object opens in the detail: meta, the root's last.
	selectPath(t, a, ".")
	press(t, a, "tab", "G", "enter")
	if a.r.TopModal() != detailID || !strings.Contains(view(a), "generated") {
		t.Fatalf("field open\n%s", view(a))
	}
}

func TestSearchAndTypeFilter(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	// With the query line off, / is the path search (viewer.search).
	if err := a.r.Settings().Set(settingQueryLine, false); err != nil {
		t.Fatal(err)
	}
	press(t, a, "/")
	press(t, a, "e", "m", "a", "i", "l", "enter")
	if s := a.selectedShape(); s == nil || s.Path != ".items[].owner.email" {
		t.Fatalf("search selected %v", s)
	}
	frame := view(a)
	if strings.Contains(frame, "version") || !strings.Contains(frame, "Structure · original (filtered)") {
		t.Fatalf("filter keeps the match and its ancestors only\n%s", frame)
	}
	press(t, a, "esc")
	if !strings.Contains(view(a), "version") {
		t.Fatal("esc clears the filter")
	}
	press(t, a, "t") // string
	if s := a.selectedShape(); s == nil || s.Path != ".name" {
		t.Fatalf("type filter selected %v", s)
	}
	press(t, a, "n")
	if s := a.selectedShape(); s.Path != ".items[].name" {
		t.Fatalf("n selected %s", s.Path)
	}
	press(t, a, "N", "N")
	if s := a.selectedShape(); s.Path != ".meta.generated" {
		t.Fatalf("N wraps to %s", s.Path)
	}
}

func TestRawToggle(t *testing.T) {
	a := open(t, sample, 120, 30)
	selectPath(t, a, ".items[].owner")
	press(t, a, "v")
	frame := view(a)
	if !strings.Contains(frame, "JSON .items[].owner") || !strings.Contains(frame, `"email": "ann@example.com"`) {
		t.Fatalf("raw\n%s", frame)
	}
}

// The frames at 80 and 120 columns with .items selected.
func TestRenderGolden(t *testing.T) {
	for _, width := range []int{80, 120} {
		a := open(t, sample, width, 24)
		selectPath(t, a, ".items")
		got := view(a)
		golden := filepath.Join("testdata", "view-"+itoa(width)+".golden")
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

func itoa(n int) string { return strconv.Itoa(n) }

// A big array: the data panel's list holds a screen of rows, so G and the
// page motions cost a screen however many rows there are.
func TestLargeArrayWindow(t *testing.T) {
	n := 100_000
	if raceOn || testing.Short() {
		n = 10_000
	}
	path := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(path, bigDocument(n), 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, path, 120, 30)
	press(t, a, "tab")
	start := time.Now()
	press(t, a, "G")
	if got := a.data.Selected(); got != n-1 {
		t.Fatalf("G selected %d", got)
	}
	press(t, a, "ctrl+u", "k", "g", "g", "5", "j")
	if got := a.data.Selected(); got != 5 {
		t.Fatalf("5j from the top selected %d", got)
	}
	t.Logf("%d rows: 7 keys and frames in %v", n, time.Since(start))
	if rows := a.data.list.Len(); rows > 30 {
		t.Fatalf("the list holds %d rows, not a screen", rows)
	}
	if cols := a.tab.Columns; cols[len(cols)-1].Name != "extra" || !cols[len(cols)-1].Optional {
		t.Fatalf("optional column last: %+v", cols)
	}
}

// The TUI schedules viewer.open before the program starts; the model's
// first update hands the job to Bubble Tea and its completion fills the
// viewer (the read runs off the loop).
func TestStartupLoadThroughTheModel(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "open", Target: pageID, Args: ochazuke.Arguments{"path": sample}}); err != nil {
		t.Fatal(err)
	}
	if a.Loading != sample {
		t.Fatalf("loading %q", a.Loading)
	}
	_, cmd := a.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	finish(a, cmd)
	if a.Doc == nil || a.Loading != "" || !strings.Contains(view(a), "Fields .") {
		t.Fatalf("startup load: loading %q err %v\n%s", a.Loading, a.LoadErr, view(a))
	}
}

func TestLoadErrorShowsPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(path, []byte("{\n  \"a\": 1,\n  \"b\" 2\n}"), 0o644)
	a, _ := New()
	if err := a.Load(path); err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("load error %v", err)
	}
}

// Nothing the viewer does writes a file: after a session of every key the
// directory holds the same file with the same bytes.
// The viewer never writes the source (nor anything else); restructure's
// own writes are pinned in TestRestructureNeverWritesTheSource.
func TestViewerNeverWritesTheSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	data, _ := os.ReadFile(sample)
	_ = os.WriteFile(path, data, 0o644)
	a := open(t, path, 100, 30)
	for _, k := range []string{"j", "j", "j", "l", "h", "enter", "enter", "y", "t", "esc", "v", "v", "tab", "j", "enter", "j", "enter", "h", "q", "y", "R"} {
		Press(a.m, k)
		view(a)
	}
	entries, _ := os.ReadDir(dir)
	after, _ := os.ReadFile(path)
	if len(entries) != 1 || string(after) != string(data) {
		t.Fatalf("the directory changed: %d entries", len(entries))
	}
}

func TestBindingRowsAreAtomic(t *testing.T) {
	a := open(t, sample, 100, 30)
	if _, err := a.r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
	var rows []ochazuke.Binding
	for _, noun := range a.r.Nouns {
		for _, table := range noun.Bindings {
			rows = append(rows, table...)
		}
	}
	for _, inst := range a.r.Instances {
		rows = append(rows, inst.Bindings...)
	}
	for _, b := range rows {
		for _, key := range strings.Fields(b.Keys) {
			if len(key) >= 2 && key[0] == 'f' && key[1] >= '1' && key[1] <= '9' {
				t.Errorf("function key bound: %s", b.Keys)
			}
		}
	}
}

func TestYankToNamedRegister(t *testing.T) {
	a := openOriginal(t, sample, 100, 30)
	selectPath(t, a, ".items[].owner.email")
	press(t, a, "\"", "a", "y")
	if y, _ := a.r.ReadYank("a"); y.Text != ".items[].owner.email" {
		t.Fatalf(`"a y on the tree stored %q`, y.Text)
	}
	selectPath(t, a, ".items")
	press(t, a, "tab", "2", "G", "y")
	if y, _ := a.r.ReadYank(""); !strings.Contains(y.Text, `"name": "beta"`) {
		t.Fatalf("y on row 2 stored %q", y.Text)
	}
	press(t, a, "enter", "esc")
	if a.r.TopModal() != "" {
		t.Fatal("esc closes the detail")
	}
}

// The tree and the table are read-only: space marks nothing in either,
// and no hint offers list.mark.
func TestReadOnlyViewsTakeNoMarks(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	selectPath(t, a, ".items")
	for _, panel := range []string{"tree", "table"} {
		Press(a.m, "space")
		frame := view(a)
		if marked := a.data.list.Marked(); len(marked) != 0 || strings.Contains(frame, "list.mark") {
			t.Fatalf("%s: space marked %v\n%s", panel, marked, frame)
		}
		if marked := a.tree.List().Marked(); len(marked) != 0 {
			t.Fatalf("%s: the tree took a mark %v", panel, marked)
		}
		press(t, a, "tab")
	}
}

// A yank says what it copied on the status line; a clipboard that fails is
// a notice, and the registers still hold the text.
func TestYankTellsTheUser(t *testing.T) {
	a := openOriginal(t, sample, 100, 30)
	selectPath(t, a, ".items[].owner.email")
	press(t, a, "y")
	if a.m.Info != "copied .items[].owner.email" || a.m.Status != "" {
		t.Fatalf("status after y: info %q, error %q", a.m.Info, a.m.Status)
	}
	if v := view(a); !strings.Contains(v, "copied .items[].owner.email") || strings.Contains(v, "Error:") {
		t.Fatalf("a yank reads as an error:\n%s", v)
	}
	a.Clipboard = func(string) error { return errors.New("no clipboard tool") }
	finish(a, Press(a.m, "y"))
	if got := strings.Join(a.m.Notices, "\n"); !strings.Contains(got, "clipboard: no clipboard tool") {
		t.Fatalf("notices: %q", got)
	}
	if y, _ := a.r.ReadYank(""); y.Text != ".items[].owner.email" {
		t.Fatalf("register: %q", y.Text)
	}
}

// finish runs a key's commands and hands their completions to the model,
// as Bubble Tea would (timers aside).
func finish(a *App, cmd tea.Cmd) { finishWithin(a, cmd, 200*time.Millisecond) }

// finishWithin is finish waiting up to d for each command.
func finishWithin(a *App, cmd tea.Cmd, d time.Duration) {
	if cmd == nil {
		return
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		switch msg := msg.(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				finishWithin(a, c, d)
			}
		case ochazuke.Completion, ochazuke.ForegroundMsg:
			_, next := a.m.Update(msg)
			finishWithin(a, next, d)
		}
	case <-time.After(d):
	}
}

// > and < move the divider between the tree and the data panel, = puts it
// back; stacked, they move it between the panels' heights.
func TestPaneKeysResizeTheSplit(t *testing.T) {
	// divider is the column the data panel starts at, side by side.
	divider := func(a *App) int {
		for _, line := range strings.Split(view(a), "\n") {
			if first := strings.Index(line, "╭"); first >= 0 {
				if second := strings.Index(line[first+1:], "╭"); second >= 0 {
					return len([]rune(line[:first+1+second]))
				}
			}
		}
		t.Fatalf("no split\n%s", view(a))
		return 0
	}
	a := openOriginal(t, sample, 120, 24)
	start := divider(a)
	press(t, a, ">")
	if grown := divider(a); grown <= start {
		t.Fatalf("> left the tree at %d (was %d)\n%s", grown, start, view(a))
	}
	press(t, a, "=")
	if reset := divider(a); reset != start {
		t.Fatalf("= put the divider at %d, want %d", reset, start)
	}
	press(t, a, "<")
	if shrunk := divider(a); shrunk >= start {
		t.Fatalf("< left the tree at %d (was %d)", shrunk, start)
	}

	// top is the line the data panel starts on, stacked.
	top := func(a *App) int {
		seen := 0
		for i, line := range strings.Split(view(a), "\n") {
			if strings.Contains(line, "╭") {
				if seen++; seen == 2 {
					return i
				}
			}
		}
		t.Fatalf("no stacked split\n%s", view(a))
		return 0
	}
	a = openOriginal(t, sample, 50, 30)
	start = top(a)
	press(t, a, "3", ">")
	if grown := top(a); grown <= start {
		t.Fatalf("3> left the data panel at line %d (was %d)\n%s", grown, start, view(a))
	}
}

// . after a yank yanks again at the focus: each yank reads the focused row
// and the repeat records no value, so y j . copies the next row, in the
// tree, the table, an object's fields, the detail and the raw lines.
func TestDotRepeatsAYankAtTheFocus(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	var clip []string
	a.Clipboard = func(s string) error { clip = append(clip, s); return nil }
	key := func(keys ...string) {
		t.Helper()
		for _, k := range keys {
			if cmd := Press(a.m, k); k == "y" || k == "." {
				finish(a, cmd) // the clipboard copy
			}
			if s := a.m.Status; s != "" {
				t.Fatalf("key %q: %s", k, s)
			}
			view(a)
		}
	}
	yanked := func(label, want string) {
		t.Helper()
		if y, _ := a.r.ReadYank(""); y.Text != want || len(clip) == 0 || clip[len(clip)-1] != want {
			t.Fatalf("%s: register %q, clipboard %q, want %q", label, y.Text, clip, want)
		}
	}
	selectPath(t, a, ".name")
	key("y", "j", ".")
	yanked("tree", ".version")
	selectPath(t, a, ".items")
	key("tab", "y", "j", ".")
	if y, _ := a.r.ReadYank(""); !strings.Contains(y.Text, `"name": "beta"`) {
		t.Fatalf("table: register %q", y.Text)
	}
	key("enter", "y", "j", ".")
	yanked("detail", "beta")
	key("esc", "tab")
	selectPath(t, a, ".meta")
	key("tab", "y", "j", ".")
	yanked("fields", "4")
	key("tab")
	selectPath(t, a, ".items[].owner")
	key("v", "tab", "y", "j", ".")
	yanked("raw lines", "  {")
	for _, e := range a.r.HistoryOf(0, ochazuke.HistoryChange) {
		if strings.HasSuffix(e.Request.Key(), ".yank") && len(e.Request.Args) != 0 {
			t.Fatalf("%s recorded arguments %v", e.Request.Key(), e.Request.Args)
		}
	}
}

// A repeat runs the same verb, so a tree yank repeated on a table row
// finds no path there and fails, leaving the register as it was.
func TestDotAcrossPanelsFailsCleanly(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	selectPath(t, a, ".items")
	press(t, a, "y", "tab")
	Press(a.m, ".")
	if a.m.Status != "not a path" {
		t.Fatalf(". on a table row after a tree yank: status %q", a.m.Status)
	}
	if y, _ := a.r.ReadYank(""); y.Text != ".items" {
		t.Fatalf("register %q", y.Text)
	}
}

// Dot repeats the last repeatable change: after a yank and an edit in the
// search prompt, the edit; after another yank, the yank.
func TestDotRepeatsTheLastChange(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	// With the query line off, / is the path search's prompt.
	if err := a.r.Settings().Set(settingQueryLine, false); err != nil {
		t.Fatal(err)
	}
	selectPath(t, a, ".name")
	press(t, a, "y", "/")
	for _, q := range []ochazuke.Request{{Noun: "input", Verb: "insert", Args: ochazuke.Arguments{"text": "o"}}, {Noun: "history", Verb: "repeat"}} {
		if _, err := a.r.Execute(q); err != nil {
			t.Fatalf("%s: %v", q.Key(), err)
		}
	}
	if got := string(a.in.Text); got != "oo" {
		t.Fatalf("the repeat after an insert: prompt %q", got)
	}
	if y, _ := a.r.ReadYank(""); y.Text != ".name" {
		t.Fatalf("the repeat after an insert yanked: %q", y.Text)
	}
	press(t, a, "esc")
	if a.r.TopModal() != "" {
		t.Fatal("esc closes the prompt")
	}
	press(t, a, "y", "j", ".")
	if y, _ := a.r.ReadYank(""); y.Text != ".version" {
		t.Fatalf("y j .: register %q", y.Text)
	}
}

// The data table is a cell table: l and h move the current column, j keeps
// it, and $ goes to the last; an entry row's own h and l still win in the
// detail (TestEnterOnRowShowsItemFields); z > gives a column a width and
// z = takes it back.
func TestDataTableMovesByCell(t *testing.T) {
	a := open(t, sample, 120, 30)
	selectPath(t, a, ".items")
	press(t, a, "tab", "l", "l")
	if got := a.data.list.Column(); got != 2 {
		t.Fatalf("l l: column %d, want 2", got)
	}
	press(t, a, "j")
	if got, row := a.data.list.Column(), a.data.Selected(); got != 2 || row != 1 {
		t.Fatalf("j: column %d row %d, want column 2 row 1", got, row)
	}
	press(t, a, "h")
	if got := a.data.list.Column(); got != 1 {
		t.Fatalf("h: column %d, want 1", got)
	}
	press(t, a, "$")
	if got, last := a.data.list.Column(), len(a.data.list.Columns())-1; got != last {
		t.Fatalf("$: column %d, want %d", got, last)
	}
	press(t, a, "0", "l", "z", ">")
	if got := a.data.list.ColumnWidths()[1]; got == 0 {
		t.Fatalf("z >: no width given to column 1: %v", a.data.list.ColumnWidths())
	}
	press(t, a, "z", "=")
	if got := a.data.list.ColumnWidths(); len(got) != 0 {
		t.Fatalf("z =: widths %v, want none", got)
	}
}

func TestNoDeprecatedOchazuke(t *testing.T) { strictcheck.Check(t) }
