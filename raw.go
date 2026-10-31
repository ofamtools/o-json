package main

// RAW (Design D): g r hands the edited document, or the value of the data
// pane's row, to a real editor in the foreground (ochazuke.Foreground:
// the TUI suspends, the editor has the terminal). The text is written
// pretty to a temp file; when the editor exits, the file is read back:
// unchanged says "raw: no change", valid JSON becomes value patches by
// path (the diff against what was sent, rawdiff.go), applied as one undo
// step, so u undoes the whole RAW session; invalid JSON says where and
// opens the editor again at that line, the text as left; a third invalid
// exit drops the RAW edits. R in VISUAL on the data pane sends the
// selected rows' values as one array, mapped back item by item.
//
// The editor is the setting raw.editor (a command line), else $VISUAL,
// $EDITOR, then nvim.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"ofam.dev/ochazuke"
)

const settingRawEditor = "raw.editor"

// rawSession is the open RAW edit: the temp file, the value's path and
// the text first written.
type rawSession struct {
	file, path, sent string
	broken           int // invalid exits so far
	// targets are the values sent: one (the text is that value), or the
	// selected rows (the text is an array of them, item i for target i).
	targets []*Node
	rows    bool
	window  string // the tuios window the editor runs in (rawpane.go)
}

// rawEditor is the editor's command line (raw.editor, $VISUAL, $EDITOR,
// nvim).
func (a *App) rawEditor() []string {
	for _, cmd := range []string{a.r.Settings().String(settingRawEditor), os.Getenv("VISUAL"), os.Getenv("EDITOR"), "nvim"} {
		if f := strings.Fields(cmd); len(f) > 0 {
			return f
		}
	}
	return nil
}

// rawTarget is what g r edits: the data pane row's value when the data
// pane has the focus, else the document.
func (a *App) rawTarget() ([]*Node, bool, error) {
	if !a.editedShown() {
		return nil, false, errors.New("raw edits the edited document: T shows it")
	}
	d := a.shownDoc()
	if d.err != nil {
		return nil, false, d.err
	}
	if inst := a.r.Instances[a.r.Focus]; inst != nil && a.inData(a.r.Focus) && a.overlay == "" {
		if a.data.list.Visual() {
			rows := a.selectedRows()
			if len(rows) == 0 {
				return nil, false, errors.New("raw: select rows, a value or the document")
			}
			return rows, true, nil
		}
		if n := a.hoverValue(inst); n != nil {
			return []*Node{n}, false, nil
		}
		if n := a.dataNode(); n != nil {
			return []*Node{n}, false, nil
		}
	}
	if a.structureFocused() && a.r.SelectionShape(a.r.BaseMode()) != "" {
		return nil, false, errors.New("raw: select rows, a value or the document (the structure's selection is shapes)")
	}
	return []*Node{d.root}, false, nil
}

// selectedRows is the data pane's visual selection as the rows' values,
// ending the selection.
func (a *App) selectedRows() []*Node {
	l := a.data.list
	var out []*Node
	for _, it := range l.Marked() {
		if n := nodeAt(a.r.Instances[it.ID]); n != nil {
			out = append(out, n)
		}
	}
	l.EndVisual()
	if a.r.SelectionShape(a.r.BaseMode()) != "" {
		_ = a.r.SetMode(a.backMode())
	}
	return out
}

// startRaw writes the target and runs the editor on it.
func (a *App) startRaw(c ochazuke.Ctx) ochazuke.Result {
	targets, rows, err := a.rawTarget()
	if err != nil {
		return ochazuke.Failed(err)
	}
	f, err := os.CreateTemp("", appName()+"-raw-*.json")
	if err != nil {
		return ochazuke.Failed(err)
	}
	text := Pretty(targets[0]) + "\n"
	path := targets[0].Path()
	if rows {
		items := make([]string, len(targets))
		for i, n := range targets {
			items[i] = Compact(n, 0)
		}
		arr, err := Build([]byte("[" + strings.Join(items, ",") + "]"))
		if err != nil {
			f.Close()
			os.Remove(f.Name())
			return ochazuke.Failed(err)
		}
		text = Pretty(arr) + "\n"
		path = fmt.Sprintf("%s (%d rows)", targets[0].Path(), len(targets))
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		os.Remove(f.Name())
		return ochazuke.Failed(err)
	}
	f.Close()
	a.raw = &rawSession{file: f.Name(), path: path, sent: text, targets: targets, rows: rows}
	return a.runRaw(c, 0)
}

// runRaw runs the editor on the session's file, at line when it is set.
func (a *App) runRaw(c ochazuke.Ctx, line int) ochazuke.Result {
	argv := a.rawEditor()
	if len(argv) == 0 {
		return ochazuke.Failed(errors.New("raw: no editor (raw.editor, $VISUAL, $EDITOR)"))
	}
	s := a.raw
	args := append([]string{}, argv[1:]...)
	if line > 0 {
		args = append(args, "+"+strconv.Itoa(line))
	}
	args = append(args, s.file)
	if a.rawInPane() {
		return a.runRawPane(c, append([]string{argv[0]}, args...))
	}
	cmd := exec.Command(argv[0], args...)
	return ochazuke.Foreground(cmd).Label("RAW " + s.path).Then(func(c ochazuke.Ctx, _ any, err error) {
		a.rawDone(c, err)
	})
}

// rawDone reads the file back when the editor exits.
func (a *App) rawDone(c ochazuke.Ctx, runErr error) {
	s := a.raw
	if s == nil {
		return
	}
	finish := func() {
		os.Remove(s.file)
		a.raw = nil
	}
	data, err := os.ReadFile(s.file)
	if err != nil {
		c.Error("raw: " + err.Error())
		finish()
		return
	}
	if runErr != nil && string(data) == s.sent {
		finish()
		return // the editor failed; its exit is told already
	}
	if string(data) == s.sent || bytes.Equal(bytes.TrimSpace(data), bytes.TrimSpace([]byte(s.sent))) {
		c.Info("raw: no change")
		finish()
		return
	}
	got, err := Build(data)
	if err != nil {
		var pe *ParseError
		if !errors.As(err, &pe) {
			c.Error("raw: " + err.Error())
			finish()
			return
		}
		s.broken++
		if s.broken > 2 {
			c.Error(fmt.Sprintf("raw: still invalid JSON at line %d, column %d; the RAW edits are dropped", pe.Line, pe.Col))
			finish()
			return
		}
		c.Error(fmt.Sprintf("raw: invalid JSON at line %d, column %d: %s (reopened there)", pe.Line, pe.Col, pe.Msg))
		if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "raw-edit", Args: ochazuke.Arguments{"line": pe.Line}}); err != nil {
			c.Error("raw: " + err.Error())
			finish()
		}
		return
	}
	patches, err := s.diff(got)
	if err != nil {
		c.Error("raw: " + err.Error())
		finish()
		return
	}
	if len(patches) == 0 {
		c.Info("raw: no change")
		finish()
		return
	}
	if err := a.commitPatches(fmt.Sprintf("raw %s", s.path), append(slices.Clone(a.rs.patches), patches...)); err != nil {
		c.Error("raw: " + err.Error())
		finish()
		return
	}
	c.Info(fmt.Sprintf("raw: %d patch%s applied as one undo step · u reverts the RAW session", len(patches), pluralES(len(patches))))
	finish()
}

// diff is the patches the edited text makes of the values sent.
func (s *rawSession) diff(got *Node) ([]Patch, error) {
	if !s.rows {
		return diffPatches(s.targets[0], got, s.targets[0].Path()), nil
	}
	if got.Kind != KArray || len(got.Children) != len(s.targets) {
		return nil, fmt.Errorf("the selection was %d rows; the text must stay an array of %d (nothing applied)", len(s.targets), len(s.targets))
	}
	var out []Patch
	for i, n := range s.targets {
		out = append(out, diffPatches(n, got.Children[i], n.Path())...)
	}
	return out, nil
}

func (a *App) registerRaw() error {
	r := a.r
	if err := declareOwn(r, ochazuke.Setting{
		Name: settingRawEditor, Type: ochazuke.SettingString, Default: "",
		Description: "The editor g r (RAW) runs on the document as JSON, a command line (nvim -c 'set ft=json'); empty uses $VISUAL, then $EDITOR, then nvim",
	}); err != nil {
		return err
	}
	if err := a.registerRawPane(); err != nil {
		return err
	}
	r.DefineGuard("raw-editable", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.Doc != nil && a.editedShown() })
	r.Define("viewer").
		Verb("raw-edit", func(c ochazuke.Ctx) ochazuke.Result {
			line := ochazuke.IntArg(c.Args, "line", 0)
			switch {
			case line > 0 && a.raw != nil:
				return a.runRaw(c, line) // reopen at a parse error
			case a.raw != nil && a.raw.window != "":
				return a.attachRawPane(c) // the editor runs in a tuios pane
			}
			return a.startRaw(c)
		}, "RAW: edit the document (the data pane's row value, when the data pane has the focus) as JSON in raw.editor in the foreground (g r); a change is one undoable value edit, invalid JSON reopens at its line").When("raw-editable")
	setParams(r, "viewer.raw-edit", map[string]ochazuke.Parameter{"line": {Type: "integer", Description: "reopen the open RAW edit at this line"}})
	setHistory(r, ochazuke.HistoryAction, "viewer.raw-edit")
	return nil
}
