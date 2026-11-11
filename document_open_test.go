package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// doc.open takes any source and refuses none: no source is an empty
// unnamed document, a path that names no file a new, named, unsaved one
// (nothing is written until a write, which then goes to that name).
func TestDocOpenNeverRefusesASource(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	docDo(t, a, "open", nil)
	if a.Doc != nil || a.Path != "" || a.newName != "" || !strings.Contains(view(a), noFile) {
		t.Fatalf("no source: path %q new %q status %q\n%s", a.Path, a.newName, a.m.Status, view(a))
	}
	fresh := filepath.Join(t.TempDir(), "fresh.json")
	docDo(t, a, "open", ochazuke.Arguments{"path": fresh})
	state := a.state()
	if a.Doc != nil || a.Path != "" || a.newName != fresh || state["source"] != fresh || state["is_new"] != true || a.LoadErr != nil {
		t.Fatalf("new path: path %q new %q state %v err %v", a.Path, a.newName, state, a.LoadErr)
	}
	if frame := view(a); !strings.Contains(frame, "fresh.json [new]") || strings.Contains(frame, "not found") {
		t.Fatalf("the new document is not named:\n%s", frame)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatalf("opening a new path wrote it: %v", err)
	}
	// What is pasted is that document, and a write with no path names it.
	viewerDo(t, a, "paste", ochazuke.Arguments{"text": `{"made":"here"}`})
	if a.Doc == nil || a.Doc.Name != fresh || a.state()["source"] != fresh {
		t.Fatalf("paste dropped the name: %v", a.state())
	}
	viewerDo(t, a, "save", nil) // :w
	finish(a, a.r.PendingCommands())
	settle(a)
	data, err := os.ReadFile(fresh)
	if err != nil || !strings.Contains(string(data), `"made": "here"`) {
		t.Fatalf("the write did not go to the name: %q %v (status %q)", data, err, a.m.Status)
	}
	if a.Path != fresh || a.newName != "" || a.state()["is_new"] != false {
		t.Fatalf("written, it is still new: path %q new %q", a.Path, a.newName)
	}
}

// The viewer's own reads stay strict: reading the open file again when it
// is gone fails and keeps the document shown (only doc.open makes a new
// document of a missing path).
func TestReloadOfAMissingFileKeepsTheDocument(t *testing.T) {
	file := filepath.Join(t.TempDir(), "gone.json")
	if err := os.WriteFile(file, []byte(`{"kept":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := empty(t)
	docDo(t, a, "open", ochazuke.Arguments{"path": file})
	if a.Doc == nil || a.Path != file {
		t.Fatalf("open: %q %v", a.Path, a.LoadErr)
	}
	if err := os.Rename(file, file+".moved"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "load", Target: pageID}); err != nil {
		t.Fatal(err)
	}
	finish(a, a.r.PendingCommands())
	settle(a)
	if a.Doc == nil || a.Path != file || a.newName != "" || !strings.Contains(a.m.Status, "not found") {
		t.Fatalf("reload of a missing file: path %q new %q status %q", a.Path, a.newName, a.m.Status)
	}
}

// Standard input is a source with a name: the document says it came from
// stdin and, read once and kept, opens again; a stdin that held nothing is
// the empty unnamed document.
func TestDocOpenStandardInput(t *testing.T) {
	a := empty(t)
	a.Stdin = keepInput(strings.NewReader(`{"piped":[1,2]}`))
	for range 2 { // the second open reads what the first kept
		docDo(t, a, "open", ochazuke.Arguments{"path": "-"})
		if a.Doc == nil || a.Doc.Name != stdinName || a.state()["source"] != stdinName || Compact(a.Doc.Root, 0) != `{"piped":[1,2]}` {
			t.Fatalf("stdin: doc %v state %v err %v", a.Doc, a.state(), a.LoadErr)
		}
		docDo(t, a, "open", nil)
	}
	a.Stdin = keepInput(strings.NewReader(" \n"))
	docDo(t, a, "open", ochazuke.Arguments{"path": "-"})
	if a.Doc != nil || a.Path != "" || a.newName != "" || a.LoadErr != nil || strings.Contains(a.m.Status, "empty input") {
		t.Fatalf("empty stdin: path %q new %q status %q", a.Path, a.newName, a.m.Status)
	}
}

// An open is a name and a reader, whatever the reader is.
func TestReadFromKeepsTheSourceName(t *testing.T) {
	doc, err := ReadFrom("a pipe", strings.NewReader(`{"a":1}`))
	if err != nil || doc.Name != "a pipe" || doc.Size != 7 {
		t.Fatalf("ReadFrom: %+v %v", doc, err)
	}
	if _, err := ReadDoc(filepath.Join(t.TempDir(), "none.json"), nil); err == nil || !unwritten(err) || strings.Contains(err.Error(), "no such file") {
		t.Fatalf("a missing file: %v", err)
	}
	if _, err := ReadFrom("stdin", strings.NewReader("")); !unwritten(err) {
		t.Fatalf("an empty source: %v", err)
	}
	if _, err := ReadFrom("stdin", strings.NewReader("{")); err == nil || unwritten(err) {
		t.Fatalf("a broken document is not an unwritten one: %v", err)
	}
}

// The command line's file word is doc.open's source, and from the command
// line doc.open runs the viewer on the same word.
func TestCommandLineOpensThroughDocOpen(t *testing.T) {
	a := empty(t)
	a.Stdin = keepInput(strings.NewReader(`{"piped":true}`))
	fresh := filepath.Join(t.TempDir(), "fresh.json")
	for _, c := range []struct {
		words []string
		ok    func() bool
	}{
		{[]string{sample}, func() bool { return a.Doc != nil && a.Path == sample }},
		{[]string{"-"}, func() bool { return a.Doc != nil && a.Doc.Name == stdinName }},
		{[]string{fresh}, func() bool { return a.Doc == nil && a.newName == fresh }},
		{nil, func() bool { return a.Doc == nil && a.newName == "" && a.Path == "" }},
	} {
		if err := openFromCommandLine(a, c.words); err != nil {
			t.Fatal(err)
		}
		finish(a, a.r.PendingCommands())
		settle(a)
		if !c.ok() {
			t.Fatalf("o-json %v: path %q new %q doc %v status %q", c.words, a.Path, a.newName, a.Doc, a.m.Status)
		}
	}
	ran := 0
	for _, entry := range a.r.History(0) {
		if entry.Depth == 0 && entry.Request.Noun == "doc" && entry.Request.Verb == "open" {
			ran++
		}
	}
	if ran != 4 {
		t.Fatalf("the command line ran doc.open %d times, not 4", ran)
	}
	for source, want := range map[string][]string{"": {"--data", "S", "view"}, "-": {"--data", "S", "view", "-"}, "new.json": {"--data", "S", "view", "new.json"}} {
		if got := viewerArgs("S", source); !slices.Equal(got, want) {
			t.Fatalf("viewerArgs(%q) = %v", source, got)
		}
	}
}

// Everything the command line does with a file is a doc verb whose file
// parameters are typed paths: the source, the mapping file, the out file.
func TestCommandLineFilesAreTypedVerbParameters(t *testing.T) {
	sh := newShell(strings.NewReader(""), io.Discard, io.Discard, t.TempDir())
	for verb, files := range map[string][]string{
		"open": {"path", "file"}, "launch": {"file"}, "get": {"file"}, "rows": {"file"}, "structure": {"file"}, "tabulate": {"file"},
		"query": {"file"}, "remap": {"file", "mapping", "out"}, "export": {"file", "mapping", "out"}, "move": {"to"},
	} {
		op, ok := sh.Runtime.Operation("doc." + verb)
		if !ok {
			t.Fatalf("doc.%s is not a registered verb", verb)
		}
		for _, name := range files {
			if p := op.Parameters[name]; p.Type != ochazuke.PropPath || p.Path != "file" {
				t.Errorf("doc.%s %s is not a typed file path: %+v", verb, name, p)
			}
		}
	}
	for _, name := range []string{"out"} {
		for _, verb := range []string{"remap", "export"} {
			if op, _ := sh.Runtime.Operation("doc." + verb); !op.Parameters[name].New {
				t.Errorf("doc.%s %s must take a path that is not there yet", verb, name)
			}
		}
	}
	if op, _ := sh.Runtime.Operation("doc.open"); !op.Parameters["path"].New || op.Parameters["path"].Required {
		t.Errorf("doc.open's source must be optional and may name no file yet: %+v", op.Parameters["path"])
	}
}

// The empty document's data pane names the keys from the bindings, and
// empty.hint off leaves it an empty list.
func TestEmptyHint(t *testing.T) {
	a := empty(t)
	frame := view(a)
	for _, want := range []string{"No document is open.", "e     open a JSON file (viewer.open)", "p     paste JSON"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("no %q\n%s", want, frame)
		}
	}
	if err := a.r.Settings().Set(settingEmptyHint, false); err != nil {
		t.Fatal(err)
	}
	if frame := view(a); strings.Contains(frame, "No document is open.") {
		t.Fatalf("empty.hint off:\n%s", frame)
	}
}

// doc.open from the palette of a viewer that shows a document replaces it
// and presents nothing over it (a read answers no data: the viewer showing
// the document is the answer), and with no source empties it at once: the
// identity is a choice, not a target the palette asks for.
func TestDocOpenFromThePalette(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	a.Stdin = keepInput(strings.NewReader(`{"piped":true}`))
	other := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(other, []byte(`{"other":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		line string
		ok   func() bool
	}{
		{"doc open " + other, func() bool { return a.Path == other }},
		{"doc open -", func() bool { return a.Doc != nil && a.Doc.Name == stdinName }},
		{"viewer open " + other, func() bool { return a.Path == other }},
		{"viewer load", func() bool { return a.Path == other }},
		{"doc open", func() bool { return a.Doc == nil && a.Path == "" }},
	} {
		press(t, a, ":")
		finish(a, Type(a.m, c.line))
		finish(a, Press(a.m, "enter"))
		settle(a)
		if !c.ok() || a.r.TopModal() != "" || a.m.Status != "" {
			t.Fatalf(":%s: path %q modal %q status %q\n%s", c.line, a.Path, a.r.TopModal(), a.m.Status, view(a))
		}
	}
	// :e PATH is the framework's Open line (ochazuke v0.123): it says what
	// enter would do, o-json's opener row, and enter does it.
	press(t, a, ":")
	finish(a, Type(a.m, "e "+other))
	finish(a, Press(a.m, "enter"))
	settle(a)
	if a.r.TopModal() != ochazuke.OpenModalID || !strings.Contains(view(a), "doc.open "+other[:20]) {
		t.Fatalf(":e did not open the Open line on doc.open: modal %q\n%s", a.r.TopModal(), view(a))
	}
	finish(a, Press(a.m, "enter"))
	finish(a, a.r.PendingCommands())
	settle(a)
	if a.Path != other || a.r.TopModal() != "" {
		t.Fatalf("the Open line did not open the file here: path %q modal %q status %q\n%s", a.Path, a.r.TopModal(), a.m.Status, view(a))
	}
}

// The registry answers a path that names no file yet as a new, unsaved
// document (a host opens doc.open's source through doc get): nothing is
// written or registered until its content is set, which makes it a file
// with a stable identity. A registered file that is gone answers too.
func TestDocumentRecordOfANewPath(t *testing.T) {
	dir := t.TempDir()
	d := persistentDocumentStore(dir)
	fresh := filepath.Join(dir, "fresh.json")
	doc, err := d.get(ochazuke.Ctx{}, fresh)
	if err != nil || doc["missing"] != true || doc["file"] != fresh || doc["name"] != "fresh.json" || doc["content"] != "" {
		t.Fatalf("a new path: %v %v", doc, err)
	}
	if rows, err := d.list(ochazuke.Ctx{}, ochazuke.ListQuery{}); err != nil || len(rows) != 0 {
		t.Fatalf("reading a new path registered it: %v %v", rows, err)
	}
	// A word that is no path is an identity nothing has, never a new file;
	// and there is nothing of a new path to delete.
	if _, err := d.get(ochazuke.Ctx{}, "j0123456789abcdef01234567"); err == nil || !strings.Contains(err.Error(), "no document") {
		t.Fatalf("an unknown identity: %v", err)
	}
	if err := d.delete(ochazuke.Ctx{}, fresh); err == nil || !strings.Contains(err.Error(), "nothing to delete") {
		t.Fatalf("deleting a new path: %v", err)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatalf("reading a new path wrote it: %v", err)
	}
	if _, err := d.patch(ochazuke.Ctx{}, fresh, ochazuke.Thing{"archived": true}); err == nil || !strings.Contains(err.Error(), "not written yet") {
		t.Fatalf("a change that writes nothing must say so: %v", err)
	}
	written, err := d.patch(ochazuke.Ctx{}, fresh, ochazuke.Thing{"content": "{\"made\":1}\n"})
	if err != nil || written["id"] == fresh || written["missing"] == true || written["content"] != "{\"made\":1}\n" {
		t.Fatalf("setting its content: %v %v", written, err)
	}
	if data, err := os.ReadFile(fresh); err != nil || string(data) != "{\"made\":1}\n" {
		t.Fatalf("the file: %q %v", data, err)
	}
	again, err := d.get(ochazuke.Ctx{}, fresh)
	if err != nil || again["id"] != written["id"] || again["missing"] == true {
		t.Fatalf("written, it is a registered document: %v %v", again, err)
	}
	// Gone from disk: the record answers as missing, and delete removes
	// the record without asking the Trash for a file that is not there.
	if err := os.Remove(fresh); err != nil {
		t.Fatal(err)
	}
	d.trash = func(context.Context, string) error { t.Fatal("trashed a file that is gone"); return nil }
	if gone, err := d.get(ochazuke.Ctx{}, fresh); err != nil || gone["missing"] != true || gone["id"] != written["id"] {
		t.Fatalf("a file that is gone: %v %v", gone, err)
	}
	if err := d.delete(ochazuke.Ctx{}, fresh); err != nil {
		t.Fatal(err)
	}
	if rows, _ := d.list(ochazuke.Ctx{}, ochazuke.ListQuery{}); len(rows) != 0 {
		t.Fatalf("delete kept the record: %v", rows)
	}
}

// A typed path's leading ~ is the home folder: a palette, a host and the
// Open line hand a path over as typed, with no shell to expand it.
func TestDocSourceExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	file := filepath.Join(home, "x.json")
	if err := os.WriteFile(file, []byte(`{"home":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := empty(t)
	docDo(t, a, "open", ochazuke.Arguments{"path": "~/x.json"})
	if a.Doc == nil || a.Path != file {
		t.Fatalf("doc open ~/x.json: path %q err %v status %q", a.Path, a.LoadErr, a.m.Status)
	}
	docDo(t, a, "open", ochazuke.Arguments{"target": "~/new.json"}) // as the Open line hands it over
	if a.Doc != nil || a.newName != filepath.Join(home, "new.json") {
		t.Fatalf("a new path under ~: new %q path %q", a.newName, a.Path)
	}
	doc, err := persistentDocumentStore(t.TempDir()).get(ochazuke.Ctx{}, "~/x.json")
	if err != nil || doc["file"] != file {
		t.Fatalf("doc get --id ~/x.json: %v %v", doc, err)
	}
	if got := expandHome("~other/x.json"); got != "~other/x.json" {
		t.Fatalf("another user's home is left as typed: %q", got)
	}
}
