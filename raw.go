package main

// RAW (Design D): g r hands the edited document, or the value of the data
// pane's row, to a real editor in the foreground (ochazuke.Foreground:
// the TUI suspends, the editor has the terminal). The text is written
// pretty to a temp file; when the editor exits, the file is read back:
// unchanged says "raw: no change", valid JSON becomes one value patch (a
// set at the value's path, so u undoes the whole RAW session), invalid
// JSON says where and opens the editor again at that line, the text as
// left; a third invalid exit drops the RAW edits.
//
// The editor is the setting raw.editor (a command line), else $VISUAL,
// $EDITOR, then nvim.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

const settingRawEditor = "raw.editor"

// rawSession is the open RAW edit: the temp file, the value's path and
// the text first written.
type rawSession struct {
	file, path, sent string
	broken           int // invalid exits so far
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
func (a *App) rawTarget() (*Node, error) {
	if !a.editedShown() {
		return nil, errors.New("raw edits the edited document: T shows it")
	}
	d := a.shownDoc()
	if d.err != nil {
		return nil, d.err
	}
	if a.inData(a.r.Focus) && a.overlay == "" {
		if n := a.dataNode(); n != nil {
			return n, nil
		}
	}
	return d.root, nil
}

// startRaw writes the target and runs the editor on it.
func (a *App) startRaw() ochazuke.Result {
	n, err := a.rawTarget()
	if err != nil {
		return ochazuke.Failed(err)
	}
	f, err := os.CreateTemp("", appName()+"-raw-*.json")
	if err != nil {
		return ochazuke.Failed(err)
	}
	text := Pretty(n) + "\n"
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		os.Remove(f.Name())
		return ochazuke.Failed(err)
	}
	f.Close()
	a.raw = &rawSession{file: f.Name(), path: n.Path(), sent: text}
	return a.runRaw(0)
}

// runRaw runs the editor on the session's file, at line when it is set.
func (a *App) runRaw(line int) ochazuke.Result {
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
	if _, err := Build(data); err != nil {
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
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		c.Error("raw: " + err.Error())
		finish()
		return
	}
	if err := a.addPatch(Patch{Op: opSet, Path: s.path, Value: json.RawMessage(compact.Bytes())}); err != nil {
		c.Error("raw: " + err.Error())
		finish()
		return
	}
	c.Info("raw: " + s.path + " changed · u undoes it")
	finish()
}

func (a *App) registerRaw() error {
	r := a.r
	if err := declareOwn(r, ochazuke.Setting{
		Name: settingRawEditor, Type: ochazuke.SettingString, Default: "",
		Description: "The editor g r (RAW) runs on the document as JSON, a command line (nvim -c 'set ft=json'); empty uses $VISUAL, then $EDITOR, then nvim",
	}); err != nil {
		return err
	}
	r.DefineGuard("raw-editable", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.Doc != nil && a.editedShown() })
	r.Define("viewer").
		Verb("raw-edit", func(c ochazuke.Ctx) ochazuke.Result {
			if line := ochazuke.IntArg(c.Args, "line", 0); line > 0 && a.raw != nil {
				return a.runRaw(line) // reopen at a parse error
			}
			return a.startRaw()
		}, "RAW: edit the document (the data pane's row value, when the data pane has the focus) as JSON in raw.editor in the foreground (g r); a change is one undoable value edit, invalid JSON reopens at its line").When("raw-editable")
	setParams(r, "viewer.raw-edit", map[string]ochazuke.Parameter{"line": {Type: "integer", Description: "reopen the open RAW edit at this line"}})
	setHistory(r, ochazuke.HistoryAction, "viewer.raw-edit")
	return nil
}
