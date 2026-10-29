package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// Documents in and out of the viewer, like a vim buffer: o-json with no
// file opens an empty document ("[no file]"); viewer.open reads another
// file (viewer.load reads the current one again), viewer.paste parses the
// clipboard into the buffer, viewer.new empties it, and viewer.write writes
// the shown document to a new file. Opening records the file in the recent
// list (recentFile under the app's state directory), which viewer.files
// offers first as viewer.open's path choices.

const (
	// noFile is the name of a document that came from no file: the empty
	// buffer and a pasted one.
	noFile        = "[no file]"
	recentFile    = "recent.json"
	settingRecent = "recent.size"
	// maxFolderFiles caps the folder listing viewer.files adds.
	maxFolderFiles = 200
)

// read reads a file ("-" is stdin) off the loop and shows it; a file is
// recorded in the recent list on the same goroutine, after it parsed.
func (a *App) read(p string) ochazuke.Result {
	stdin := a.Stdin
	a.Loading, a.LoadErr = p, nil
	state, size := a.stateDir(), a.recentSize()
	return ochazuke.Pending(func(context.Context) (any, error) {
		doc, err := ReadDoc(p, stdin)
		if err == nil && p != "-" {
			// A recent list that cannot be written never fails the read.
			_ = recordRecent(state, p, size)
		}
		return doc, err
	}).
		Replacing("load").
		Then(func(_ ochazuke.Ctx, v any, err error) {
			if err != nil {
				a.Loading, a.LoadErr = "", err
				return
			}
			if err := a.setDoc(p, v.(*Doc)); err != nil {
				a.LoadErr = err
			}
		})
}

// readAsking reads a file as read does, first asking (viewer.discard)
// when the restructure has edits no write saved.
func (a *App) readAsking(p string) ochazuke.Result {
	if a.unsaved() {
		return a.discardFirst(p)
	}
	return a.read(p)
}

// discardFirst asks once (viewer.discard) before dropping the edits:
// then it reads path, or without one empties the viewer.
func (a *App) discardFirst(path string) ochazuke.Result {
	args := ochazuke.Arguments{}
	if path != "" {
		args["path"] = path
	}
	return a.askDiscard(args)
}

// askDiscard runs viewer.discard with args, which asks first.
func (a *App) askDiscard(args ochazuke.Arguments) ochazuke.Result {
	v, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "discard", Target: pageID, Args: args})
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(v)
}

// pasteDoc parses the text argument, or the system clipboard off the
// loop, as the buffer's document (viewer.paste, and viewer.discard
// {paste} once it has asked).
func (a *App) pasteDoc(c ochazuke.Ctx) ochazuke.Result {
	if text, ok := c.Args["text"].(string); ok {
		return a.doneState(a.paste(text))
	}
	read := a.Paste
	if read == nil {
		return ochazuke.Failed(errors.New("no clipboard tool (pbpaste, wl-paste, xclip, xsel) on PATH"))
	}
	return ochazuke.Pending(func(context.Context) (any, error) { return read() }).
		Then(func(c ochazuke.Ctx, v any, err error) {
			if err == nil {
				err = a.paste(v.(string))
			}
			if err != nil {
				c.Error("paste: " + err.Error())
				return
			}
			c.Info("pasted the clipboard's JSON into the buffer")
		})
}

// guardQuit makes app.quit (q, ctrl+c) ask once, through viewer.quit,
// while there are edits no write saved, as vim refuses :q; with force
// (Q) it quits without asking. The framework's verb is kept and wrapped,
// so every key and caller of it asks the same way.
func (a *App) guardQuit() {
	key := a.r.CanonicalKey("app.quit")
	op, ok := a.r.Operations[key]
	if !ok {
		panic("guardQuit: no operation app.quit")
	}
	run := op.Run
	op.Run = func(r *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
		if args["force"] == true || !a.unsaved() {
			return run(r, c, args)
		}
		return r.ExecuteAt(c, ochazuke.Request{Noun: "viewer", Verb: "quit", Target: pageID})
	}
	op.Parameters = map[string]ochazuke.Parameter{"force": {Type: "boolean", Description: "quit without asking, even with edits no write saved (Q, as vim's :q!)"}}
	op.Description = "Exit the application; with edits no write saved it asks first (viewer.quit), unless force"
	a.r.Operations[key] = op
}

// paste parses text as the buffer's document, a document of no file.
func (a *App) paste(text string) error {
	root, err := Build([]byte(text))
	if err != nil {
		return err
	}
	return a.setDoc("", &Doc{Name: noFile, Size: len(text), Root: root, Shape: Infer(root)})
}

// reset empties the viewer: no document, no filters, and a restructure
// back to its document node alone (one undo step on the restructure page).
func (a *App) reset() error {
	a.Doc, a.Path, a.Loading, a.LoadErr = nil, "", "", nil
	a.Query, a.Kind = "", ""
	a.shown, a.tab, a.fields, a.lines, a.scalar = nil, nil, nil, nil, nil
	if page := a.r.PageOf(pageID); page != nil {
		page.Title = "o-json"
	}
	if err := a.rebuild(); err != nil {
		return err
	}
	if err := a.data.set(rowSource{}, 0); err != nil {
		return err
	}
	if err := a.setOriginal(); err != nil {
		return err
	}
	if len(a.rs.tree.Nodes()) > 1 || len(a.rs.meta) > 0 || len(a.rs.patches) > 0 {
		root := []ochazuke.TreeNode{{ID: rsRootID, Label: ".", Noun: "node"}}
		if err := a.commitWith("new document", rsRootID, edit{nodes: root, meta: map[string]nodeMeta{}}, nil); err != nil {
			return err
		}
	}
	a.rs.saved, a.rs.base = "", mappingKey(&Mapping{Map: []MapEntry{}})
	if a.r.Page != pageID {
		return nil
	}
	return a.tree.Focus()
}

// unsaved reports restructure edits that no write or export-map has
// saved: a mapping other than the one the document started with (its
// copy, or the empty restructure) and the last one written.
func (a *App) unsaved() bool {
	key := mappingKey(a.Mapping())
	return key != a.rs.base && key != a.rs.saved
}

func mappingKey(m *Mapping) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// markSaved records the current mapping as written.
func (a *App) markSaved() { a.rs.saved = mappingKey(a.Mapping()) }

// stateDir is where the recent list lives: the runtime's state directory
// (Config.StateDir, else $XDG_STATE_HOME/o-json), "" for none (a test
// binary), when nothing is recorded.
func (a *App) stateDir() string {
	if c := a.r.Config(); c != nil {
		return c.StateDirectory()
	}
	return ""
}

func (a *App) recentSize() int { return a.r.Settings().Int(settingRecent) }

// readRecent is the recent list, newest first, without the files that are
// gone and cut to size.
func readRecent(dir string, size int) []string {
	if dir == "" || size <= 0 {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, recentFile))
	if err != nil {
		return nil
	}
	var paths []string
	if json.Unmarshal(data, &paths) != nil {
		return nil
	}
	out := make([]string, 0, min(len(paths), size))
	for _, p := range paths {
		if len(out) == size {
			break
		}
		if isFile(p) && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// recordRecent puts a file first in the recent list (by absolute path),
// pruning the files that are gone and cutting the list to size.
func recordRecent(dir, path string, size int) error {
	if dir == "" || size <= 0 {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	paths := []string{abs}
	for _, p := range readRecent(dir, size) {
		if len(paths) < size && p != abs {
			paths = append(paths, p)
		}
	}
	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeNew(filepath.Join(dir, recentFile), append(data, '\n'), true)
}

// files is viewer.open's path choices: the recent files, newest first,
// then the files in the current file's folder (the working directory for
// a document of no file), as paths beside the current one.
func files(state string, size int, current string) []ochazuke.Choice {
	var out []ochazuke.Choice
	seen := map[string]bool{}
	home, _ := os.UserHomeDir()
	for _, p := range readRecent(state, size) {
		label := p
		if home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
			label = "~" + strings.TrimPrefix(p, home)
		}
		out = append(out, ochazuke.Choice{ID: p, Label: label + "  (recent)"})
		seen[p] = true
	}
	dir := "."
	if current != "" && current != "-" {
		dir = filepath.Dir(current)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	n := 0
	for _, e := range entries {
		if n == maxFolderFiles {
			break
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || e.IsDir() {
			continue
		}
		p := filepath.Join(dir, name)
		if !isFile(p) {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil && seen[abs] {
			continue
		}
		out = append(out, ochazuke.Choice{ID: p, Label: p})
		n++
	}
	return out
}

// registerOpenVerbs adds the viewer's document verbs.
func (a *App) registerOpenVerbs(params func(string, map[string]ochazuke.Parameter)) {
	r := a.r
	// Availability: reload needs a file, a write a document.
	r.DefineGuard("viewer-file", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.Path != "" })
	r.DefineGuard("viewer-document", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.Doc != nil })
	r.Define("viewer").
		Verb("open", func(c ochazuke.Ctx) ochazuke.Result {
			p := strings.TrimSpace(ochazuke.StringArg(c.Args, "path", ""))
			if p == "" {
				return ochazuke.Failed(errors.New("path is required"))
			}
			return a.readAsking(p)
		}, "Read another JSON file off the loop and show it (- is stdin); its path's choices are the recent files, then the files beside the current one. With restructure edits no write saved, it asks first (viewer.discard)").
		Verb("files", func(c ochazuke.Ctx) ochazuke.Result {
			state, size, current := a.stateDir(), a.recentSize(), a.Path
			return ochazuke.Pending(func(context.Context) (any, error) { return files(state, size, current), nil })
		}, "The files viewer.open offers: the recent ones (newest first), then the files in the current file's folder").Fetch().
		Verb("recent", func(c ochazuke.Ctx) ochazuke.Result {
			state, size := a.stateDir(), a.recentSize()
			return ochazuke.Pending(func(context.Context) (any, error) { return readRecent(state, size), nil })
		}, "The recently opened files, newest first (setting recent.size; files that are gone are left out)").Fetch().
		Verb("new", func(c ochazuke.Ctx) ochazuke.Result {
			if a.unsaved() {
				return a.discardFirst("")
			}
			return a.doneState(a.reset())
		}, "Start an empty document ([no file]), as vim's :enew; with restructure edits no write saved, it asks first (viewer.discard)").
		Verb("discard", func(c ochazuke.Ctx) ochazuke.Result {
			if c.Args["paste"] == true {
				return a.pasteDoc(c)
			}
			if p := strings.TrimSpace(ochazuke.StringArg(c.Args, "path", "")); p != "" {
				return a.read(p)
			}
			return a.doneState(a.reset())
		}, "Drop the edits (structure and values) and read path (viewer.open and R ask with it), paste the clipboard or text (paste: viewer.paste asks with it), or without either start an empty document (viewer.new); u brings the edits back. It always asks first").
		Confirm("The restructure has edits no write saved. Drop them?").
		Verb("paste", func(c ochazuke.Ctx) ochazuke.Result {
			if a.unsaved() {
				args := ochazuke.Arguments{"paste": true}
				if text, ok := c.Args["text"].(string); ok {
					args["text"] = text
				}
				return a.askDiscard(args)
			}
			return a.pasteDoc(c)
		}, "Parse the clipboard's JSON (or text) into the buffer, a document of no file; invalid JSON is an error naming its line and column. With edits no write saved, it asks first (viewer.discard)").
		Verb("quit", func(c ochazuke.Ctx) ochazuke.Result {
			a.r.Quit = true
			return ochazuke.Done(true)
		}, "Quit without writing the edits no write saved, as vim's :q!; app.quit (q) asks through it when there are any, app.quit {force: true} (Q) quits without asking").
		Confirm("Quit without writing? The edits no write saved are lost.").
		Verb("write", func(c ochazuke.Ctx) ochazuke.Result { return a.writeDoc(c, writeNewFile) },
			"Write the shown document as indented JSON to a new file; an existing file asks first (viewer.overwrite), the source never. A document of no file is then that file").When("viewer-document").
		Verb("overwrite", func(c ochazuke.Ctx) ochazuke.Result { return a.writeDoc(c, writeReplace) },
			"Replace an existing file with the shown document (never the source)").Confirm("Replace {{path}} with the shown document?").When("viewer-document")
	path := ochazuke.Parameter{Type: "string", Required: true, Description: "the file to write, e.g. NAME.json"}
	params("viewer.open", map[string]ochazuke.Parameter{"path": {Type: "string", Required: true, Source: "viewer.files", Free: true,
		Description: "the JSON file (- is stdin); choices: recent files, then the current folder"}})
	params("viewer.discard", map[string]ochazuke.Parameter{
		"path":  {Type: "string", Description: "the file to read once the edits are dropped (- is stdin); omit for an empty document"},
		"paste": {Type: "boolean", Description: "paste the clipboard (or text) once the edits are dropped, as viewer.paste does"},
		"text":  {Type: "string", Description: "with paste, the JSON to paste instead of the clipboard"},
	})
	params("viewer.paste", map[string]ochazuke.Parameter{"text": {Type: "string", Description: "the JSON; omit to read the system clipboard"}})
	params("viewer.write", map[string]ochazuke.Parameter{"path": path})
	params("viewer.overwrite", map[string]ochazuke.Parameter{"path": path})
	setHistory(r, ochazuke.HistoryFetch, "viewer.open")
	setHistory(r, ochazuke.HistoryChange, "viewer.new", "viewer.discard", "viewer.paste", "viewer.write", "viewer.overwrite")
	setHistory(r, ochazuke.HistoryAction, "viewer.quit")
	a.guardQuit()
}

func (a *App) doneState(err error) ochazuke.Result {
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(a.state())
}

// writeDoc writes the shown document through writeFile. A document of no
// file (a pasted one) is that file once written, as vim's :w names a
// buffer.
func (a *App) writeDoc(c ochazuke.Ctx, mode writeMode) ochazuke.Result {
	if a.Doc == nil {
		return ochazuke.Failed(errors.New("no document to write: e opens a file, p pastes JSON"))
	}
	doc := a.Doc
	return a.writeFile(c, ochazuke.StringArg(c.Args, "path", ""), []byte(Pretty(doc.Root)+"\n"), mode, "the document", "viewer.overwrite",
		func(path string) {
			if a.Doc == doc && a.Path == "" {
				a.Path, doc.Name = path, path
				if page := a.r.PageOf(pageID); page != nil {
					page.Title = path
				}
			}
		})
}

// declareOpenSettings declares recent.size.
func declareOpenSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(ochazuke.Setting{
		Name: settingRecent, Type: ochazuke.SettingInteger, Default: 10,
		Description: "How many recently opened files o-json remembers (viewer.open's first choices); 0 keeps none",
	})
}
