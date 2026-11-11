package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func documentFixture(t *testing.T) (*ochazuke.Runtime, *documentStore, func(string, ochazuke.Arguments) any) {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	d := registerDocuments(r, nil, nil)
	registerDocumentActions(r, d, nil)
	var out, stderr bytes.Buffer
	sh := ocli.New(r, "o-json", strings.NewReader(""), &out, &stderr)
	owner := ochazuke.Owner()
	sh.Actor = &owner
	call := func(verb string, args ochazuke.Arguments) any {
		t.Helper()
		out.Reset()
		stderr.Reset()
		if args == nil {
			args = ochazuke.Arguments{}
		}
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		if err := sh.Run(context.Background(), []string{"doc", verb, "--json", string(raw), "--yes"}); err != nil {
			t.Fatalf("doc.%s: %v\n%s", verb, err, stderr.String())
		}
		var result any
		if out.Len() > 0 {
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("%s: %v (%s)", verb, err, out.String())
			}
		}
		return result
	}
	return r, d, call
}
func record(t *testing.T, value any) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("not a record: %T %#v", value, value)
	}
	return m
}

func TestDocumentFileLifecycle(t *testing.T) {
	r, d, call := documentFixture(t)
	root := t.TempDir()
	file := filepath.Join(root, "original.json")
	created := record(t, call("new", ochazuke.Arguments{"file": file, "content": "{\"n\":1}\n"}))
	id := created["id"].(string)
	if created["file"] != file {
		t.Fatalf("created %v", created)
	}
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	call("annotate", ochazuke.Arguments{"id": id, "text": "keep this identity"})
	call("relate", ochazuke.Arguments{"id": id, "to": "project:temp", "kind": "document"})
	moved := filepath.Join(root, "moved.json")
	call("move", ochazuke.Arguments{"id": id, "to": moved})
	got, err := d.get(ochazuke.Ctx{}, id)
	if err != nil {
		t.Fatal(err)
	}
	if got["file"] != moved || got["id"] != id {
		t.Fatalf("move changed identity %v", got)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old file remains: %v", err)
	}
	call("rename", ochazuke.Arguments{"id": id, "name": "renamed.json"})
	renamed := filepath.Join(root, "renamed.json")
	if data, err := os.ReadFile(renamed); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("rename %q %v", data, err)
	}
	call("archive", ochazuke.Arguments{"id": id})
	if list := record(t, call("list", nil)); len(list["items"].([]any)) != 0 {
		t.Fatalf("archive is not hidden %v", list)
	}
	call("restore", ochazuke.Arguments{"id": id})
	duplicate := record(t, call("duplicate", ochazuke.Arguments{"id": id}))
	if duplicate["id"] == id || duplicate["file"] == renamed {
		t.Fatalf("not a separate file: %v", duplicate)
	}
	if data, err := os.ReadFile(duplicate["file"].(string)); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("copy %q %v", data, err)
	}
	call("set", ochazuke.Arguments{"id": id, "props": map[string]any{"content": "{\"n\":2}"}})
	if data, _ := os.ReadFile(renamed); string(data) != "{\"n\":2}" {
		t.Fatalf("explicit set did not write source: %q", data)
	}
	if history := r.ThingHistory("doc", id); len(history) < 4 {
		t.Fatalf("history lost identity: %v", history)
	}
	notes, err := d.state.NotesForTest()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0]["about"] != ochazuke.AnyRef("doc", id) {
		t.Fatalf("annotations lost identity: %v", notes)
	}
	relations, err := d.state.Relations(ochazuke.RelationQuery{Thing: ochazuke.AnyRef("doc", id)})
	if err != nil || len(relations) != 1 {
		t.Fatalf("relations: %v %v", relations, err)
	}
	var trashed string
	d.trash = func(_ context.Context, path string) error {
		trashed = path
		return os.Rename(path, filepath.Join(root, "fake-system-trash.json"))
	}
	call("delete", ochazuke.Arguments{"id": id, "archive-first": false})
	if trashed != renamed {
		t.Fatalf("trash got %q", trashed)
	}
	if _, err := os.Stat(renamed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file remains: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "fake-system-trash.json")); err != nil || string(data) != "{\"n\":2}" {
		t.Fatalf("trash lost content: %q %v", data, err)
	}
	op, _ := r.Operation("doc.delete")
	if !strings.Contains(op.Confirm, "system Trash") {
		t.Fatalf("dishonest confirmation %q", op.Confirm)
	}
}
func (s *documentState) NotesForTest() ([]ochazuke.Thing, error) {
	out := []ochazuke.Thing{}
	err := s.transaction(false, func(state *documentRecords) error {
		for _, t := range state.Notes {
			out = append(out, t)
		}
		return nil
	})
	return out, err
}

func TestDocumentRefusesOverwriteAndInvalidJSON(t *testing.T) {
	_, d, call := documentFixture(t)
	root := t.TempDir()
	file := filepath.Join(root, "source.json")
	other := filepath.Join(root, "other.json")
	created := record(t, call("new", ochazuke.Arguments{"file": file, "content": "[1]"}))
	if err := os.WriteFile(other, []byte("[2]"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, changes := range []ochazuke.Thing{{"file": other}, {"content": "not JSON"}} {
		if _, err := d.patch(ochazuke.Ctx{}, created["id"].(string), changes); err == nil {
			t.Fatalf("accepted %v", changes)
		}
	}
	if data, _ := os.ReadFile(file); string(data) != "[1]" {
		t.Fatalf("source changed: %q", data)
	}
	if data, _ := os.ReadFile(other); string(data) != "[2]" {
		t.Fatalf("destination changed: %q", data)
	}
}

func TestDocumentTrashRunnerAndFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quote\" and space.json")
	for _, system := range []string{"darwin", "linux"} {
		called := false
		err := runDocumentTrash(context.Background(), system, path, func(cmd *exec.Cmd) error {
			called = true
			if cmd.Args[len(cmd.Args)-1] != path {
				t.Fatalf("path was interpolated into code: %v", cmd.Args)
			}
			return errors.New("trash unavailable")
		})
		if !called || err == nil || !strings.Contains(err.Error(), "system Trash") {
			t.Fatalf("%s: %v %v", system, called, err)
		}
	}
	_, d, call := documentFixture(t)
	created := record(t, call("new", ochazuke.Arguments{"file": filepath.Join(t.TempDir(), "keep.json")}))
	d.trash = func(context.Context, string) error { return errors.New("refused by trash") }
	if err := d.delete(ochazuke.Ctx{}, created["id"].(string)); err == nil {
		t.Fatal("lost trash failure")
	}
	if _, err := d.get(ochazuke.Ctx{}, created["id"].(string)); err != nil {
		t.Fatalf("failed trash removed registry record: %v", err)
	}
}

func TestDocumentRegistrySurvivesNewRuntime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	data := t.TempDir()
	file := filepath.Join(t.TempDir(), "persist.json")
	invoke := func(words ...string) map[string]any {
		t.Helper()
		var out, stderr bytes.Buffer
		if err := Main(append([]string{"--data", data}, words...), strings.NewReader(""), &out, &stderr); err != nil {
			t.Fatalf("%v: %v %s", words, err, stderr.String())
		}
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("%s: %v", out.String(), err)
		}
		return result
	}
	created := invoke("doc", "new", "file="+file, "content={\"persistent\":true}")
	id := created["id"].(string)
	got := invoke("doc", "get", "--id", id)
	if got["file"] != file || got["content"] != "{\"persistent\":true}" {
		t.Fatalf("restarted record: %v", got)
	}
	list := invoke("doc", "list")
	if len(list["items"].([]any)) != 1 {
		t.Fatalf("list: %v", list)
	}
}

func TestDocumentDeclarationsInBothRuntimes(t *testing.T) {
	a := empty(t)
	shell := newShell(nil, io.Discard, io.Discard)
	for _, r := range []*ochazuke.Runtime{a.r, shell.Runtime} {
		doc := r.Noun("doc")
		if doc.Store.Facts || doc.Store.Create == nil || doc.Store.Patch == nil || doc.Store.Delete == nil || doc.Store.List == nil {
			t.Fatal("read-only document scaffold")
		}
		for _, verb := range []string{"show", "new", "edit", "rename", "duplicate", "delete", "archive", "restore", "history", "yank", "annotate", "relate", "open", "open-with", "reveal", "yank-path", "edit-pane", "move", "send-pane", "send-note", "add-project"} {
			op, _, ok := r.ResolveAction("doc." + verb)
			if !ok || op.Unavailable != "" {
				t.Errorf("doc.%s: %v %s", verb, ok, op.Unavailable)
			}
		}
		for _, p := range doc.Props {
			if p.Name == "file" && (p.Type != "path" || p.Path != "file" || !p.Human) {
				t.Fatalf("file prop: %+v", p)
			}
		}
	}
	for _, noun := range []string{"shape", "item", "field", "entry", "line", "jq", "viewer", "search", valueEditorID, "restructure", "node", marksID, "value"} {
		n := a.r.Noun(noun)
		if n == nil || n.Store == nil || !n.Store.Facts {
			t.Errorf("%s lacks honest transient facts store", noun)
		}
	}
}

func TestDocumentHostActionsUseTypedHostBoundary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()
	log := filepath.Join(root, "calls.jsonl")
	script := filepath.Join(root, "host.sh")
	if err := os.WriteFile(script, []byte("printf '%s %s %s\\n' \"$1\" \"$2\" \"$4\" >> \"$HOST_TEST_LOG\"\nprintf '%s\\n' '{\"ok\":true}'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	argv, _ := json.Marshal([]string{"/bin/sh", script})
	t.Setenv("OCHAZUKE_HOST", string(argv))
	t.Setenv("OCHAZUKE_EXTENSION", "json")
	t.Setenv("HOST_TEST_LOG", log)
	t.Setenv("VISUAL", "editor --wait")
	r, _, call := documentFixture(t)
	file := filepath.Join(root, "file with spaces.json")
	created := record(t, call("new", ochazuke.Arguments{"file": file, "content": "{\"safe\":true}"}))
	id := created["id"].(string)
	call("edit-pane", ochazuke.Arguments{"id": id, "session": "fixture"})
	call("send-pane", ochazuke.Arguments{"id": id, "pane": "fixture-pane", "session": "fixture"})
	call("send-note", ochazuke.Arguments{"id": id, "project": "fixture-project"})
	call("add-project", ochazuke.Arguments{"id": id, "project": "fixture-project"})
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 {
		t.Fatalf("host calls: %s", data)
	}
	for i, want := range []string{"pane open ", "pane send-text ", "note new ", "relation create "} {
		if !strings.HasPrefix(lines[i], want) {
			t.Fatalf("call %d: %s", i, lines[i])
		}
	}
	var open map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[0], "pane open ")), &open); err != nil {
		t.Fatal(err)
	}
	args := open["command"].([]any)
	if len(args) != 3 || args[0] != "editor" || args[1] != "--wait" || args[2] != file {
		t.Fatalf("editor argv %v", args)
	}
	for _, want := range []string{`"window":"fixture-pane"`, `"submit":false`, `"about":"json.doc:` + id + `"`, `"to":"json.doc:` + id + `"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("host calls lack %s: %s", want, data)
		}
	}
	for key, param := range map[string]string{"doc.send-pane": "pane", "doc.send-note": "note", "doc.add-project": "project"} {
		op, _ := r.Operation(key)
		p := op.Parameters[param]
		if p.Noun != param || p.HostNoun != param {
			t.Fatalf("%s untyped: %+v", key, p)
		}
	}
}

func TestDocumentFileMoveUndoAndSourceWriteUndo(t *testing.T) {
	r, d, call := documentFixture(t)
	file := filepath.Join(t.TempDir(), "before.json")
	created := record(t, call("new", ochazuke.Arguments{"file": file, "content": "[1]"}))
	id := created["id"].(string)
	after := filepath.Join(filepath.Dir(file), "after.json")
	call("move", ochazuke.Arguments{"id": id, "to": after})
	undo := func() {
		t.Helper()
		sh := ocli.New(r, "o-json", strings.NewReader(""), io.Discard, io.Discard)
		owner := ochazuke.Owner()
		sh.Actor = &owner
		if err := sh.Run(context.Background(), []string{"ochazuke.history-entry", "undo", "--yes"}); err != nil {
			t.Fatal(err)
		}
	}
	undo()
	got, err := d.get(ochazuke.Ctx{}, id)
	if err != nil || got["file"] != file {
		t.Fatalf("move undo: %v %v", got, err)
	}
	call("set", ochazuke.Arguments{"id": id, "props": map[string]any{"content": "[2]"}})
	undo()
	if data, err := os.ReadFile(file); err != nil || string(data) != "[1]" {
		t.Fatalf("content undo: %q %v", data, err)
	}
}

func TestDocumentMoveAcrossFilesystemsWithoutReplacement(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.json")
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(source, []byte("[1]"), 0600); err != nil {
		t.Fatal(err)
	}
	crossDevice := func(string, string) error { return syscall.EXDEV }
	if err := moveDocumentFile(source, target, crossDevice); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "[1]" {
		t.Fatalf("moved content: %q %v", data, err)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains: %v", err)
	}
	if err := os.WriteFile(source, []byte("[2]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := moveDocumentFile(source, target, crossDevice); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing destination: %v", err)
	}
	if data, _ := os.ReadFile(source); string(data) != "[2]" {
		t.Fatalf("failed move lost source: %q", data)
	}
	if data, _ := os.ReadFile(target); string(data) != "[1]" {
		t.Fatalf("failed move replaced destination: %q", data)
	}
}

func TestDocumentViewerMutationAndTypedOpeners(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "source.json")
	target := filepath.Join(root, "moved.json")
	if err := os.WriteFile(file, []byte("{\"source\":true}"), 0600); err != nil {
		t.Fatal(err)
	}
	a := empty(t)
	if err := a.Load(file); err != nil {
		t.Fatal(err)
	}
	sh := ocli.New(a.r, "o-json", strings.NewReader(""), io.Discard, io.Discard)
	owner := ochazuke.Owner()
	sh.Actor = &owner
	if err := sh.Run(context.Background(), []string{"doc", "move", "--id", file, "--to", target, "--yes"}); err != nil {
		t.Fatal(err)
	}
	if a.Path != target {
		t.Fatalf("viewer retained old source path %q", a.Path)
	}
	var calls [][]string
	a.r.SetOpener(ochazuke.Opener{GOOS: "darwin", Start: func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}})
	for _, args := range [][]string{{"doc", "reveal", "--id", target}, {"doc", "open-with", "--id", target, "--with", "FixtureEditor", "--yes"}} {
		if err := sh.Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 2 || strings.Join(calls[0], " ") != "open -R -- "+target || strings.Join(calls[1], " ") != "open -a FixtureEditor -- "+target {
		t.Fatalf("typed file openers: %v", calls)
	}
}
