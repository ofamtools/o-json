package main

import (
	"errors"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
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

// RAW maps the edit back as path-addressed patches (a diff), one undo
// step for the whole session; R in VISUAL on the data pane sends the
// selected rows as one array and maps it back item by item.
func TestRawDiffAndSelection(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	if err := a.r.Settings().Set(settingRawEditor, "fake-editor"); err != nil {
		t.Fatal(err)
	}
	var argv [][]string
	rawEditorFake(a, func(s string) string {
		s = strings.Replace(s, `"alpha"`, `"ALPHA"`, 1)
		return strings.Replace(s, `"beta"`, `"BETA"`, 1)
	}, &argv)
	rawKey(t, a)
	if len(a.rs.patches) != 2 || a.rs.patches[0].Op != opSet || !strings.HasPrefix(a.rs.patches[0].Path, ".items[0]") {
		t.Fatalf("diff patches %+v (%q)", a.rs.patches, a.m.Info)
	}
	press(t, a, "u")
	if len(a.rs.patches) != 0 {
		t.Fatalf("u left %+v", a.rs.patches)
	}
	selectPath(t, a, ".items")
	press(t, a, "ctrl+w", "l", "V", "j")
	var sent string
	rawEditorFake(a, func(s string) string {
		sent = s
		return strings.Replace(s, `"beta"`, `"B"`, 1)
	}, &argv)
	finish(a, Press(a.m, "R"))
	settle(a)
	b, err := Build([]byte(sent))
	if err != nil || b.Kind != KArray || len(b.Children) != 2 {
		t.Fatalf("sent %q", sent)
	}
	if len(a.rs.patches) != 1 || a.rs.patches[0].Path != ".items[1].name" {
		t.Fatalf("selection patches %+v (%q %q)", a.rs.patches, a.m.Info, a.m.Status)
	}
	rawEditorFake(a, func(s string) string { return "[1]" }, &argv)
	press(t, a, "V", "j")
	finish(a, Press(a.m, "R"))
	settle(a)
	if len(a.rs.patches) != 1 || !strings.Contains(a.m.Status, "nothing applied") {
		t.Fatalf("shape change: patches %+v status %q", a.rs.patches, a.m.Status)
	}
}

// Inside tuios (raw.pane auto) g r opens the editor in a tuios window
// (no foreground), the breadcrumb says RAW running, g r again focuses the
// window, and the window's end reads the file back as one undo step.
func TestRawInTuiosPane(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	if err := a.r.Settings().Set(settingRawEditor, "nvim"); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"TUIOS_ENV": "1", "TUIOS_SESSION": "sess", "TUIOS_PANE_ID": "me"}
	a.getenv = func(k string) string { return env[k] }
	var calls [][]string
	var windowOpen atomic.Bool
	windowOpen.Store(true)
	var file string
	a.tuios = func(args ...string) ([]byte, error) {
		if args[0] == "get-window" { // the watch's goroutine: touch nothing else
			if windowOpen.Load() {
				return []byte("{}"), nil
			}
			return nil, errors.New("no window found")
		}
		calls = append(calls, args)
		if args[0] == "new-window" {
			file = args[len(args)-1]
			return []byte("win-1\n"), nil
		}
		return nil, nil
	}
	a.m.Foreground = func(ochazuke.ForegroundMsg) tea.Cmd {
		t.Fatal("RAW ran in the foreground inside tuios")
		return nil
	}
	rawKey(t, a)
	if a.raw == nil || a.raw.window != "win-1" || !strings.Contains(view(a), "RAW running") {
		t.Fatalf("pane: raw %+v calls %v", a.raw, calls)
	}
	nw := calls[0]
	if !slices.Contains(nw, "--grants") || !slices.Contains(nw, "-c") || !strings.Contains(strings.Join(nw, " "), "focus-window', '-s', 'sess', 'me'") {
		t.Fatalf("new-window argv %v", nw)
	}
	rawKey(t, a) // g r again: re-attach
	if last := calls[len(calls)-1]; last[0] != "focus-window" || last[len(last)-1] != "win-1" {
		t.Fatalf("re-attach %v", last)
	}
	b, _ := os.ReadFile(file)
	_ = os.WriteFile(file, []byte(strings.Replace(string(b), `"alpha"`, `"A"`, 1)), 0o600)
	windowOpen.Store(false)
	_, next := a.m.Update(rawPaneDone{window: "win-1"})
	finish(a, next)
	if a.raw != nil || len(a.rs.patches) != 1 || strings.Contains(view(a), "RAW running") {
		t.Fatalf("closed: raw %+v patches %+v", a.raw, a.rs.patches)
	}
}
