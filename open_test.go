package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

// empty is the viewer with no file, as o-json with no file argument
// opens it, its recent list in a temporary state directory.
func empty(t *testing.T) *App {
	t.Helper()
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.r.Config().StateDir = t.TempDir()
	a.m.Width, a.m.Height = 120, 30
	view(a)
	return a
}

func viewerDo(t *testing.T, a *App, verb string, args ochazuke.Arguments) {
	t.Helper()
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: verb, Target: pageID, Args: args}); err != nil {
		t.Fatalf("viewer.%s: %v", verb, err)
	}
	settle(a)
}

func TestNoFileStartsEmpty(t *testing.T) {
	a := empty(t)
	frame := view(a)
	for _, want := range []string{"[no file]", "e open a file", "p paste JSON", "M restructure"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("no %q\n%s", want, frame)
		}
	}
	// Nothing to reload or write: those keys are not available.
	for _, verb := range []string{"load", "write"} {
		if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: verb, Target: pageID, Args: ochazuke.Arguments{"path": "x.json"}}); err == nil {
			t.Fatalf("viewer.%s ran on the empty document", verb)
		}
	}
	// The restructure page opens on it too.
	press(t, a, "M")
	if a.r.Page != rsPageID {
		t.Fatalf("page %q", a.r.Page)
	}
}

// With no file and a stdin that is not a terminal (a test's reader), the
// viewer reads stdin, as before; only a terminal gives the empty document
// (TestNoFileStartsEmpty is that frame).
func TestNoFileReadsStdinWhenItIsNotATerminal(t *testing.T) {
	var out strings.Builder
	// A reader that is not a terminal reads stdin; an empty one is an
	// error naming stdin, not "a file is required".
	err := runTUI([]string{"--smoke"}, strings.NewReader(""), &out, &out)
	if err == nil || strings.Contains(err.Error(), "required") {
		t.Fatalf("smoke from empty stdin: %v", err)
	}
}

func TestOpenWithAPath(t *testing.T) {
	a := empty(t)
	viewerDo(t, a, "open", ochazuke.Arguments{"path": sample})
	if a.Doc == nil || a.Path != sample || !strings.Contains(view(a), "o-json sample") {
		t.Fatalf("open: path %q err %v\n%s", a.Path, a.LoadErr, view(a))
	}
	// Reload (R) still reads the current file.
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	if err := os.WriteFile(path, []byte(`{"a": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	viewerDo(t, a, "open", ochazuke.Arguments{"path": path})
	if err := os.WriteFile(path, []byte(`{"reloaded": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	finish(a, Press(a.m, "R"))
	settle(a)
	if a.Path != path || !strings.Contains(view(a), "reloaded") {
		t.Fatalf("R: path %q\n%s", a.Path, view(a))
	}
}

// e without a path opens the path chooser directly (its one missing
// argument is a path: ochazuke's forms.single-path); typing finds a file
// beside the current one, and picking it opens the file.
func TestOpenWithoutAPathOpensTheForm(t *testing.T) {
	a := empty(t)
	viewerDo(t, a, "open", ochazuke.Arguments{"path": sample})
	finish(a, Press(a.m, "e")) // the folder's listing is the chooser's job
	settle(a)
	if a.r.TopModal() != "path-chooser" {
		t.Fatalf("e: modal %q", a.r.TopModal())
	}
	// The field is a path: the chooser starts in the open file's folder
	// and its recent source is o-json's recent list (read with the file,
	// off the loop).
	if p := a.r.Operations[a.r.CanonicalKey("viewer.open")].Parameters["path"]; p.Type != ochazuke.PathParameterType {
		t.Fatalf("path parameter %+v", p)
	}
	pick := ochazuke.PathPick{Name: "path"}
	if got := a.r.PathStart(pick); !strings.HasSuffix(got, "testdata") {
		t.Fatalf("browse starts in %q", got)
	}
	other := filepath.Join("testdata", "duplicate-keys.json")
	// Typing finds the file in the open file's folder once it is listed.
	finish(a, Type(a.m, "duplicate-keys"))
	for i := 0; i < 20 && (strings.Contains(view(a), "loading") || strings.Contains(view(a), "nothing matches")); i++ {
		settle(a)
		time.Sleep(20 * time.Millisecond)
	}
	finish(a, Press(a.m, "enter"))
	settle(a)
	if a.r.TopModal() != "" || !strings.HasSuffix(a.Path, other) {
		t.Fatalf("form: modal %q path %q status %q\n%s", a.r.TopModal(), a.Path, a.m.Status, view(a))
	}
}

func TestNewClearsTheDocument(t *testing.T) {
	a := empty(t)
	viewerDo(t, a, "open", ochazuke.Arguments{"path": sample})
	viewerDo(t, a, "new", nil)
	if a.Doc != nil || a.Path != "" || !strings.Contains(view(a), "[no file]") {
		t.Fatalf("new: path %q\n%s", a.Path, view(a))
	}
	// With restructure edits no write saved, it asks once.
	viewerDo(t, a, "open", ochazuke.Arguments{"path": sample})
	press(t, a, "M")
	cleared(t, a) // the page opens on the copy; build from nothing
	sendPaths(t, a, ".name")
	press(t, a, "q")
	viewerDo(t, a, "new", nil)
	if a.r.TopModal() == "" || a.Doc == nil {
		t.Fatalf("new did not ask: modal %q", a.r.TopModal())
	}
	finish(a, Press(a.m, "n"))
	if a.Doc == nil || len(a.Mapping().Map) == 0 {
		t.Fatal("n discarded")
	}
	viewerDo(t, a, "new", nil)
	finish(a, Press(a.m, "y"))
	settle(a)
	if a.Doc != nil || len(a.rs.tree.Nodes()) != 1 || len(a.Mapping().Map) != 0 {
		t.Fatalf("y: doc %v nodes %+v", a.Doc != nil, a.rs.tree.Nodes())
	}
	// Once written, the edits are saved: new does not ask.
	dir := t.TempDir()
	viewerDo(t, a, "open", ochazuke.Arguments{"path": sample})
	press(t, a, "M")
	cleared(t, a) // the page opens on the copy; build from nothing
	sendPaths(t, a, ".name")
	if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "write", Args: ochazuke.Arguments{"path": filepath.Join(dir, "out.json")}}); err != nil {
		t.Fatal(err)
	}
	settle(a)
	press(t, a, "q")
	viewerDo(t, a, "new", nil)
	if a.r.TopModal() != "" || a.Doc != nil {
		t.Fatalf("new after a write asked: modal %q", a.r.TopModal())
	}
}

func TestPasteJSON(t *testing.T) {
	a := empty(t)
	clip := `{"pasted": [1, 2]}`
	a.Paste = func() (string, error) { return clip, nil }
	finish(a, Press(a.m, "p"))
	settle(a)
	if a.Doc == nil || a.Doc.Name != noFile || a.Path != "" || !strings.Contains(view(a), "pasted") {
		t.Fatalf("paste: status %q\n%s", a.m.Status, view(a))
	}
	// p pastes on the original; on the edited structure it puts nodes.
	press(t, a, "T")
	// Invalid JSON is an error naming its position; the buffer stays.
	clip = "{\n  \"a\" 1\n}"
	finish(a, Press(a.m, "p"))
	settle(a)
	if !strings.Contains(a.m.Status, "line 2, column") || a.Doc == nil || a.Doc.Root.Children[0].Key != "pasted" {
		t.Fatalf("invalid paste: status %q", a.m.Status)
	}
	// The job failed, as the status line says: never "done" beside it.
	if jobs := a.r.Jobs(); len(jobs) == 0 || jobs[len(jobs)-1].Verb != "viewer.paste" || jobs[len(jobs)-1].State != "failed" {
		t.Fatalf("invalid paste: jobs %+v", jobs)
	}
	a.Paste = func() (string, error) { return "", errors.New("no clipboard") }
	finish(a, Press(a.m, "p"))
	settle(a)
	if !strings.Contains(a.m.Status, "no clipboard") {
		t.Fatalf("clipboard failure: status %q", a.m.Status)
	}
	// The text argument pastes without the clipboard (the CLI, a script).
	viewerDo(t, a, "paste", ochazuke.Arguments{"text": `[{"x": 1}]`})
	if a.Doc.Root.Kind != KArray {
		t.Fatalf("paste text: %v", a.Doc.Root.Kind)
	}
}

// A pasted buffer restructures and writes a new file; viewer.write names
// the buffer.
func TestRestructureAPastedBuffer(t *testing.T) {
	a := empty(t)
	data, _ := os.ReadFile(sample)
	viewerDo(t, a, "paste", ochazuke.Arguments{"text": string(data)})
	press(t, a, "M")
	cleared(t, a) // the page opens on the copy; build from nothing
	sendPaths(t, a, ".name")
	dir := t.TempDir()
	out := filepath.Join(dir, "new.json")
	if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "write", Args: ochazuke.Arguments{"path": out}}); err != nil {
		t.Fatal(err)
	}
	settle(a)
	if got, _ := os.ReadFile(out); string(got) != "{\n  \"name\": \"o-json sample\"\n}\n" {
		t.Fatalf("wrote %q (status %q)", got, a.m.Status)
	}
	press(t, a, "q")
	// w writes the buffer itself; the buffer is then that file.
	press(t, a, "w")
	if a.r.TopModal() != "path-chooser" {
		t.Fatalf("w: modal %q", a.r.TopModal())
	}
	press(t, a, "esc")
	if a.r.TopModal() != "" {
		press(t, a, "esc")
	}
	buf := filepath.Join(dir, "buffer.json")
	viewerDo(t, a, "write", ochazuke.Arguments{"path": buf})
	if got, _ := os.ReadFile(buf); string(got) != Pretty(a.Doc.Root)+"\n" || a.Path != buf {
		t.Fatalf("write: path %q status %q", a.Path, a.m.Status)
	}
	// The source is refused, as on the restructure page.
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "write", Target: pageID, Args: ochazuke.Arguments{"path": buf}}); err == nil || !strings.Contains(err.Error(), "is the source") {
		t.Fatalf("write the source: %v", err)
	}
	// An existing file asks first.
	viewerDo(t, a, "write", ochazuke.Arguments{"path": out})
	if a.r.TopModal() == "" || !strings.Contains(view(a), "Replace ") {
		t.Fatalf("write over a file did not ask\n%s", view(a))
	}
}

func TestRecentListOrderAndPruning(t *testing.T) {
	a := empty(t)
	dir := t.TempDir()
	var paths []string
	for _, name := range []string{"a.json", "b.json", "c.json"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
		viewerDo(t, a, "open", ochazuke.Arguments{"path": p})
	}
	recent := func() []string {
		t.Helper()
		// viewer.recent reads it off the loop; it answers readRecent.
		viewerDo(t, a, "recent", nil)
		return readRecent(a.stateDir(), a.recentSize())
	}
	if got := recent(); !slices.Equal(got, []string{paths[2], paths[1], paths[0]}) {
		t.Fatalf("newest first: %v", got)
	}
	// Opening again moves a file to the front, once.
	viewerDo(t, a, "open", ochazuke.Arguments{"path": paths[0]})
	if got := recent(); !slices.Equal(got, []string{paths[0], paths[2], paths[1]}) {
		t.Fatalf("reopened: %v", got)
	}
	// A file that is gone is dropped.
	if err := os.Remove(paths[2]); err != nil {
		t.Fatal(err)
	}
	if got := recent(); !slices.Equal(got, []string{paths[0], paths[1]}) {
		t.Fatalf("pruned: %v", got)
	}
	// recent.size cuts the list.
	if err := a.r.Settings().Set(settingRecent, 1); err != nil {
		t.Fatal(err)
	}
	if got := recent(); !slices.Equal(got, []string{paths[0]}) {
		t.Fatalf("size 1: %v", got)
	}
}

// F13: a bracketed paste on the viewer (not in an input) is viewer.paste
// {text}: the pasted JSON becomes the buffer, no clipboard tool needed.
func TestBracketedPasteOpensBuffer(t *testing.T) {
	a := empty(t)
	a.Paste = nil // the clipboard is not read: the paste carries the text
	finish(a, Paste(a.m, `{"pasted": [1, 2]}`))
	view(a)
	if a.Doc == nil || a.Doc.Shape.Find(".pasted") == nil || a.m.Status != "" {
		t.Fatalf("paste did not open the buffer (status %q)\n%s", a.m.Status, view(a))
	}
}

// e, w and :w (one path parameter) open ochazuke's path chooser directly,
// not the args form first (ochazuke v0.46.0, forms.single-path).
func TestPathVerbsOpenTheChooser(t *testing.T) {
	for _, keys := range [][]string{{"e"}, {"w"}, {":", "w", "enter"}} {
		a := open(t, nestDoc, 120, 30)
		for _, k := range keys {
			finish(a, Press(a.m, k))
			view(a)
		}
		if a.r.TopModal() != "path-chooser" || a.r.Focus != "path-chooser/query" {
			t.Errorf("%v: top %q focus %q", keys, a.r.TopModal(), a.r.Focus)
		}
	}
}

// A failed open is one plain Error on the status line, cleared by the
// next key; the breadcrumb and the data pane keep the document shown (or
// the empty document's guidance) (F9).
func TestFailedOpenIsOnePlainError(t *testing.T) {
	for _, loaded := range []bool{true, false} {
		var a *App
		if loaded {
			a = openOriginal(t, sample, 120, 30)
		} else {
			a = empty(t)
		}
		crumb := a.breadcrumb()
		missing := filepath.Join(t.TempDir(), "nope.json")
		res, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "open", Target: pageID, Args: ochazuke.Arguments{"path": missing}})
		_ = res
		if err != nil {
			t.Fatal(err)
		}
		finish(a, a.r.PendingCommands())
		settle(a)
		frame := view(a)
		if !strings.Contains(a.m.Status, "nope.json not found") || strings.Contains(a.m.Status, "no such file") {
			t.Fatalf("loaded %v: status %q", loaded, a.m.Status)
		}
		if a.breadcrumb() != crumb || strings.Count(frame, "nope.json not found") > 2 {
			t.Fatalf("loaded %v: breadcrumb %q (was %q)\n%s", loaded, a.breadcrumb(), crumb, frame)
		}
		press(t, a, "j")
		if a.m.Status != "" || strings.Contains(view(a), "Error:") {
			t.Fatalf("loaded %v: the error outlived the next key: %q", loaded, a.m.Status)
		}
	}
}

// R keeps the viewer's place: the focused pane, the tree's path, the data
// pane's row and column (reload.keep-place); off, it starts at the root
// as opening a file does (F10).
func TestReloadKeepsPlace(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".items", ".items[1]")
	press(t, a, "l", "l")
	reload := func() {
		t.Helper()
		finish(a, Press(a.m, "R"))
		settle(a)
		view(a)
	}
	reload()
	if s := a.selectedShape(); s == nil || s.Path != ".items" || !a.data.list.Focused() || a.data.Selected() != 1 || a.data.list.Column() != 2 {
		t.Fatalf("R: shape %v focus %q row %d column %d", a.selectedShape(), a.r.Focus, a.data.Selected(), a.data.list.Column())
	}
	if err := a.r.Settings().Set(settingReloadPlace, false); err != nil {
		t.Fatal(err)
	}
	reload()
	if s := a.selectedShape(); s == nil || s.Path == ".items" || a.data.list.Focused() {
		t.Fatalf("R with reload.keep-place off kept the place: shape %v focus %q", s, a.r.Focus)
	}
}

// o-json - reads the document from stdin as - after a flag does: the word
// is a file word, never a command word oden resolves (it printed the
// usage). --smoke after it renders without a terminal.
func TestDashIsStdin(t *testing.T) {
	for _, args := range [][]string{{"-", "--smoke"}, {"--smoke", "-"}} {
		var out strings.Builder
		if err := Main(args, strings.NewReader(`{"a": 1}`), &out, &out); err != nil || !strings.Contains(out.String(), "stdin  ·  . (object") {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
	}
}
