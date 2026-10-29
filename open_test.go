package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
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

// e without a path opens the args form; its path field offers the recent
// files first, then the files beside the current one; filling it opens
// the file.
func TestOpenWithoutAPathOpensTheForm(t *testing.T) {
	a := empty(t)
	viewerDo(t, a, "open", ochazuke.Arguments{"path": sample})
	press(t, a, "e")
	if a.r.TopModal() != ochazuke.ArgsFormID {
		t.Fatalf("e: modal %q", a.r.TopModal())
	}
	// The field suggests viewer.files's answer (read off the loop).
	if p := a.r.Operations[a.r.CanonicalKey("viewer.open")].Parameters["path"]; p.Source != "viewer.files" || !p.Free {
		t.Fatalf("path parameter %+v", p)
	}
	choices := files(a.stateDir(), a.recentSize(), a.Path)
	abs, _ := filepath.Abs(sample)
	ids := make([]string, len(choices))
	for i, c := range choices {
		ids[i] = c.ID
	}
	if len(ids) < 2 || ids[0] != abs || !slices.Contains(ids, filepath.Join("testdata", "restructure.json")) || slices.Contains(ids, sample) {
		t.Fatalf("choices %v", ids)
	}
	other := filepath.Join("testdata", "duplicate-keys.json")
	Type(a.m, other)
	view(a)
	finish(a, Press(a.m, "enter"))
	settle(a)
	if a.r.TopModal() != "" || a.Path != other {
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
	// Invalid JSON is an error naming its position; the buffer stays.
	clip = "{\n  \"a\" 1\n}"
	finish(a, Press(a.m, "p"))
	settle(a)
	if !strings.Contains(a.m.Status, "line 2, column") || a.Doc == nil || a.Doc.Root.Children[0].Key != "pasted" {
		t.Fatalf("invalid paste: status %q", a.m.Status)
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
	if a.r.TopModal() != ochazuke.ArgsFormID {
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
