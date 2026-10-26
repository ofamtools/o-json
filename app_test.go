package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
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

func view(a *App) string { return ansi.Strip(a.m.View().Content) }

// press sends keys one by one, failing on the first error the model shows.
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
	a := open(t, sample, 120, 30)
	press(t, a, "/")
	press(t, a, "e", "m", "a", "i", "l", "enter")
	if s := a.selectedShape(); s == nil || s.Path != ".items[].owner.email" {
		t.Fatalf("search selected %v", s)
	}
	frame := view(a)
	if strings.Contains(frame, "version") || !strings.Contains(frame, "Structure (filtered)") {
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

// The TUI schedules viewer.load before the program starts; the model's
// first update hands the job to Bubble Tea and its completion fills the
// viewer (the read runs off the loop).
func TestStartupLoadThroughTheModel(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "load", Target: pageID, Args: ochazuke.Arguments{"path": sample}}); err != nil {
		t.Fatal(err)
	}
	if a.Loading != sample {
		t.Fatalf("loading %q", a.Loading)
	}
	_, cmd := a.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		got := make(chan tea.Msg, 1)
		go func() { got <- cmd() }()
		var m tea.Msg
		select {
		case m = <-got:
		case <-time.After(200 * time.Millisecond):
			return // a timer (the jobs ticker's linger)
		}
		switch msg := m.(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		case ochazuke.Completion:
			_, next := a.m.Update(msg)
			run(next)
		}
	}
	run(cmd)
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
func TestReadOnlyNeverWrites(t *testing.T) {
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
