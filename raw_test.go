package main

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
)

// rawEditorFake answers the foreground program by changing its file with
// edit (the file's text in, the text to leave out), recording the argv.
func rawEditorFake(a *App, edit func(string) string, argv *[][]string) {
	a.m.Foreground = func(msg ochazuke.ForegroundMsg) tea.Cmd {
		args := msg.Cmd.Args
		*argv = append(*argv, args)
		file := args[len(args)-1]
		b, _ := os.ReadFile(file)
		_ = os.WriteFile(file, []byte(edit(string(b))), 0o600)
		return func() tea.Msg { return msg.Done(nil) }
	}
}

// g r: unchanged says so; a valid change is one undoable value edit; an
// invalid one reopens the editor at its line.
func TestRawEditRoundTrip(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	if err := a.r.Settings().Set(settingRawEditor, "fake-editor -n"); err != nil {
		t.Fatal(err)
	}
	var argv [][]string
	rawEditorFake(a, func(s string) string { return s }, &argv)
	rawKey(t, a)
	if len(argv) != 1 || argv[0][0] != "fake-editor" || argv[0][1] != "-n" || !strings.Contains(a.m.Info, "raw: no change") {
		t.Fatalf("unchanged: argv %v info %q", argv, a.m.Info)
	}
	rawEditorFake(a, func(s string) string { return strings.Replace(s, `"alpha"`, `"ALPHA"`, 1) }, &argv)
	rawKey(t, a)
	if len(a.rs.patches) != 1 || !strings.Contains(Compact(a.shownDoc().root, 0), `"ALPHA"`) {
		t.Fatalf("valid change: patches %v info %q", a.rs.patches, a.m.Info)
	}
	press(t, a, "u")
	if strings.Contains(Compact(a.shownDoc().root, 0), `"ALPHA"`) {
		t.Fatal("u kept the RAW edit")
	}
	calls := 0
	rawEditorFake(a, func(s string) string {
		calls++
		if calls == 1 {
			return strings.Replace(s, `"version"`, `version"`, 1) // line 3
		}
		return strings.Replace(s, `version"`, `"version"`, 1)
	}, &argv)
	n := len(argv)
	rawKey(t, a)
	settle(a)
	if len(argv) != n+2 || argv[n+1][len(argv[n+1])-2] != "+3" {
		t.Fatalf("invalid: argv %v", argv[n:])
	}
	if !strings.Contains(a.m.Info, "raw: no change") || a.raw != nil {
		t.Fatalf("fixed: info %q status %q", a.m.Info, a.m.Status)
	}
}

// rawKey runs g r's verb and lets the foreground program run.
func rawKey(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "raw-edit"}); err != nil {
		t.Fatal(err)
	}
	settle(a)
}

func TestRawKeyIsBound(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	for _, b := range a.r.ListBindings(a.r.Focus) {
		if b.Keys == "g r" && b.Request.Noun == "viewer" && b.Request.Verb == "raw-edit" {
			return
		}
	}
	t.Fatal("g r is not viewer.raw-edit on the structure pane")
}
