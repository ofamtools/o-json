package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	// settingReloadPlace keeps the viewer's place across reading the open
	// file again (R): the focus, each tree's selection by path, the data
	// pane's row and column. Off, a reload starts at the root.
	settingReloadPlace = "reload.keep-place"
	// settingEmptyHint: with no document open the data pane says so and
	// names the keys that open one (emptyHint); off, an empty list.
	settingEmptyHint = "empty.hint"
	// settingPasteOnTree says what a bracketed paste on the restructured
	// tree runs (node.paste): pasteOnJSON, pasteOnOutline or pasteOnViewer.
	settingPasteOnTree = "paste.on-tree"
	pasteOnJSON        = "json"
	pasteOnOutline     = "outline"
	pasteOnViewer      = "viewer"
	// settingPasteText says what node.paste does with text that is not
	// JSON under paste.on-tree json: pasteTextKeys, pasteTextValues or
	// pasteTextIgnore.
	settingPasteText = "paste.text-on-tree"
	pasteTextKeys    = "keys"
	pasteTextValues  = "values"
	pasteTextIgnore  = "ignore"
	// maxFolderFiles caps the folder listing viewer.files adds.
	maxFolderFiles = 200
)

// read reads a file ("-" is stdin) off the loop and shows it; a file is
// recorded in the recent list on the same goroutine, after it parsed.
func (a *App) read(p string) ochazuke.Result {
	stdin := a.Stdin
	var at *place // the open file read again: where the viewer is
	if a.Doc != nil && p == a.Path && p != "-" && a.r.Settings().Bool(settingReloadPlace) {
		pl := a.place()
		at = &pl
	}
	a.Loading, a.LoadErr = p, nil
	state, size := a.stateDir(), a.recentSize()
	var recent []string // written on the read's goroutine, read in Then
	return ochazuke.Pending(func(context.Context) (any, error) {
		doc, err := ReadDoc(p, stdin)
		if err == nil && p != "-" {
			// A recent list that cannot be written never fails the read.
			_ = recordRecent(state, p, size)
			recent = readRecent(state, size)
		}
		return doc, err
	}).
		Replacing("load").
		Then(func(c ochazuke.Ctx, v any, err error) {
			if err != nil {
				// The job's failure is the one report: the status line's
				// Error (cleared by the next key) and the ticker. The
				// document shown, and its breadcrumb, stay as they were.
				a.Loading = ""
				return
			}
			if recent != nil {
				a.recent = recent
			}
			if err := a.setDoc(p, v.(*Doc)); err != nil {
				c.Error(err.Error())
				return
			}
			if at != nil {
				if err := a.restore(*at); err != nil {
					c.Error(err.Error())
				}
			}
		})
}

// place is where the viewer is, by path, so it survives a reload: the
// focused pane, the side shown, each structure tree's selected path, the
// data pane's row and column.
type place struct {
	focus        string // treeID, rsTreeID, dataID or ""
	original     bool
	orig, edited string
	row, col     int
}

func (a *App) place() place {
	p := place{original: a.original, row: max(0, a.data.Selected()), col: a.data.list.Column()}
	anc := a.r.Ancestry(a.r.Focus)
	switch {
	case slices.Contains(anc, dataID):
		p.focus = dataID
	case a.sideOf(a.r.Focus) == rsTreeID:
		p.focus = rsTreeID
	case slices.Contains(anc, treeID):
		p.focus = treeID
	}
	if s, ok := a.selectedOriginal(); ok {
		p.orig = s.Path
	}
	if n, ok := a.rs.tree.Selected(); ok {
		p.edited = nodePaths(a.rs.tree.Nodes(), a.rs.meta)[n.ID]
	}
	return p
}

// restore puts the viewer back at p where its paths are still there.
func (a *App) restore(p place) error {
	if a.editable() {
		a.original = p.original
	}
	if p.orig != "" && a.shapeAt(p.orig) != nil {
		_ = a.tree.Select(shapeID(a.shapeAt(p.orig)))
	}
	if p.edited != "" {
		for id, path := range nodePaths(a.rs.tree.Nodes(), a.rs.meta) {
			if path == p.edited {
				_ = a.rs.tree.Select(id)
				break
			}
		}
	}
	a.shown, a.note = nil, ""
	a.carry = &rowCarry{sel: p.row, col: p.col}
	if err := a.sync(); err != nil {
		return err
	}
	switch p.focus {
	case dataID:
		return a.data.list.Focus()
	case rsTreeID:
		if a.editedShown() {
			return a.rs.tree.Focus()
		}
	case treeID:
		if !a.editedShown() {
			return a.tree.Focus()
		}
	}
	return nil
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
	// Read and parse off the loop, so text that is not JSON fails the
	// job (the ticker says failed, the status line why), never "done".
	return ochazuke.Pending(func(context.Context) (any, error) {
		text, err := read()
		if err != nil {
			return nil, fmt.Errorf("paste: %w", err)
		}
		root, err := Build([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("paste: %w", err)
		}
		return &Doc{Name: noFile, Size: len(text), Root: root, Shape: Infer(root)}, nil
	}).
		Then(func(c ochazuke.Ctx, v any, err error) {
			if err != nil {
				return // the job's failure reports it
			}
			if err := a.setDoc("", v.(*Doc)); err != nil {
				c.Error("paste: " + err.Error())
				return
			}
			c.Info("pasted the clipboard's JSON into the buffer")
		})
}

// beforeQuit makes app.quit (q, ctrl+c) ask once while there are edits
// no write saved, as vim refuses :q; app.quit {force: true} (Q) skips the
// check (ochazuke's before-quit hook). viewer.quit stays the verb that
// quits without writing after its own confirm.
func (a *App) beforeQuit() error {
	return a.r.BeforeQuit("unsaved", func(r *ochazuke.Runtime, c ochazuke.Context) ochazuke.Verdict {
		if !a.unsaved() {
			return ochazuke.Allow()
		}
		return ochazuke.AskFirst("Quit without writing? The edits no write saved are lost.")
	})
}

// paste parses text as the buffer's document, a document of no file (the
// text argument; the clipboard's is parsed off the loop, pasteDoc).
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

// LoadRecent reads the recent list for the path chooser; the viewer runs
// it before the loop starts.
func (a *App) LoadRecent() { a.recent = readRecent(a.stateDir(), a.recentSize()) }

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
		}, "The recent files (newest first), then the files in the current file's folder: a choices source for an actions.json row or the palette (viewer.open itself takes a path, through the path chooser)").Fetch().
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
		}, "Quit without writing the edits no write saved, as vim's :q!, after one confirm; app.quit (q) asks the same through its before-quit check, app.quit {force: true} (Q) quits without asking").
		Confirm("Quit without writing? The edits no write saved are lost.").
		Verb("write", func(c ochazuke.Ctx) ochazuke.Result { return a.writeDoc(c, writeNewFile) },
			"Write the shown document as indented JSON to a new file; an existing file asks first (viewer.overwrite), the source never. A document of no file is then that file").When("viewer-document").
		Verb("overwrite", func(c ochazuke.Ctx) ochazuke.Result { return a.writeDoc(c, writeReplace) },
			"Replace an existing file with the shown document (never the source)").Confirm("Replace {{path}} with the shown document?").When("viewer-document").
		Verb("save", func(c ochazuke.Ctx) ochazuke.Result {
			force := c.Args["force"] == true
			switch {
			case r.Page != pageID || a.editedShown():
				if force {
					return a.write(c, writeForce)
				}
				return a.write(c, writeNewFile)
			case force:
				return a.writeDoc(c, writeReplace)
			}
			return a.writeDoc(c, writeNewFile)
		}, "Write what w writes where the focus is: the restructured document while the edited one is shown or on the restructure page (restructure.write), else the shown document (viewer.write). With force an existing file is replaced without asking (the source only through W's confirm); with quit, quit once written (the :w, :w!, :wq and :x aliases)").When("viewer-saveable")
	r.DefineGuard("viewer-saveable", func(r *ochazuke.Runtime, _ ochazuke.KeyEvent) bool {
		return a.Doc != nil || r.Page != pageID || a.editedShown()
	})
	path := ochazuke.Parameter{Type: ochazuke.PathParameterType, New: true, Required: true, Description: "the file to write, e.g. NAME.json"}
	params("viewer.open", map[string]ochazuke.Parameter{"path": {Type: ochazuke.PathParameterType, Required: true,
		Description: "the JSON file (- is stdin); the path chooser browses the open file's folder, the recent files and a typed path"}})
	// The path chooser starts in the open file's folder and offers
	// o-json's recent list (no I/O: the list read before the loop and
	// after each read).
	r.SetPathBase(func(ochazuke.PathPick) string {
		if a.Path == "" || a.Path == "-" {
			return ""
		}
		return filepath.Dir(a.Path)
	})
	r.SetRecentPaths(func(ochazuke.PathPick) []string { return slices.Clone(a.recent) })
	params("viewer.discard", map[string]ochazuke.Parameter{
		"path":  {Type: "string", Description: "the file to read once the edits are dropped (- is stdin); omit for an empty document"},
		"paste": {Type: "boolean", Description: "paste the clipboard (or text) once the edits are dropped, as viewer.paste does"},
		"text":  {Type: "string", Description: "with paste, the JSON to paste instead of the clipboard"},
	})
	params("viewer.paste", map[string]ochazuke.Parameter{"text": {Type: "string", Description: "the JSON; omit to read the system clipboard"}})
	r.Define("node").
		Verb("paste", func(c ochazuke.Ctx) ochazuke.Result {
			text := ochazuke.StringArg(c.Args, "text", "")
			mode := r.Settings().String(settingPasteOnTree)
			if !pasteLoads(mode, text) {
				if mode == pasteOnOutline {
					return a.pasteKeys(c, text)
				}
				switch r.Settings().String(settingPasteText) {
				case pasteTextValues:
					return a.pasteValues(c, text)
				case pasteTextIgnore:
					c.Info("not JSON; nothing pasted")
					return ochazuke.Done(nil)
				}
				return a.pasteKeys(c, text)
			}
			v, err := r.Execute(ochazuke.Request{Noun: "viewer", Verb: "paste", Target: pageID, Args: ochazuke.Arguments{"text": text}})
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(v)
		}, "A bracketed paste on the restructured tree, decided by paste.on-tree: json (the default) loads valid JSON as the buffer (viewer.paste, asking first with edits no write saved) and does with anything else what paste.text-on-tree says: keys (the default) adds them as keys, the outline's paste in restructure terms (one key per line below the node, nested by indent, one undo step); values adds each line as a string value (an array of them under a new placeholder key in an object, items appended to an array; one undo step); ignore pastes nothing. outline always adds keys; viewer always loads (invalid JSON is an error, the buffer kept)")
	params("node.paste", map[string]ochazuke.Parameter{"text": {Type: "string", Required: true, Description: "the pasted text"}})
	quit := ochazuke.Parameter{Type: "boolean", Description: "quit once written (:wq); the before-quit check still asks about edits the write did not save"}
	params("viewer.write", map[string]ochazuke.Parameter{"path": path, "quit": quit})
	params("viewer.overwrite", map[string]ochazuke.Parameter{"path": path, "quit": quit})
	params("viewer.save", map[string]ochazuke.Parameter{"path": path, "quit": quit,
		"force": {Type: "boolean", Description: "replace an existing file without asking (:w!)"}})
	setHistory(r, ochazuke.HistoryFetch, "viewer.open")
	setHistory(r, ochazuke.HistoryChange, "viewer.new", "viewer.discard", "viewer.paste", "viewer.write", "viewer.overwrite", "viewer.save")
	setHistory(r, ochazuke.HistoryAction, "viewer.quit", "node.paste")
	a.registerExportVerbs(params)
	if err := a.beforeQuit(); err != nil {
		panic(err) // app.quit is the framework's own verb: always there
	}
	if err := a.addAliases(); err != nil {
		panic(err) // o-json's own rows: a bad one is a bug
	}
	a.registerDocVerbs()
}

// docVerbs are the doc noun's verbs that are the viewer's document verbs
// under the document's name: one operation each (AliasAction), so the
// handler, the parameters (doc.open's path chooser), the confirm and the
// history are the viewer verb's. doc.open {path} replaces the open
// document the way viewer.open does, asking first (viewer.discard) when
// the restructure has edits no write saved.
var docVerbs = []ochazuke.ActionAlias{
	{Alias: "doc.open", Action: "viewer.open"},
	{Alias: "doc.reload", Action: "viewer.load"},
	{Alias: "doc.new", Action: "viewer.new"},
	{Alias: "doc.paste", Action: "viewer.paste"},
	{Alias: "doc.write", Action: "viewer.write"},
}

// registerDocVerbs defines the doc noun, the open document: its file is
// App.Path ("" for none, or a document of no file), doc.show reads it, and
// docVerbs name the viewer's document verbs on it. The command line's doc
// noun (shell.go) is another runtime's: it reads a FILE per call.
func (a *App) registerDocVerbs() {
	r := a.r
	r.Define("doc").
		Verb("show", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(a.docState()) },
			"The open document: its file (path, empty for none or a document of no file, such as a pasted one), its name, whether one is open, its size, a read in progress and whether the restructure has edits no write saved").Fetch()
	for _, al := range docVerbs {
		if err := r.AddActionAlias(al); err != nil {
			panic(err) // o-json's own rows: a bad one is a bug
		}
	}
}

// docState is doc.show's answer.
func (a *App) docState() map[string]any {
	out := map[string]any{"path": a.Path, "open": a.Doc != nil, "unsaved": a.unsaved()}
	if a.Doc != nil {
		out["name"], out["size"] = a.Doc.Name, a.Doc.Size
	}
	if a.Loading != "" {
		out["loading"] = a.Loading
	}
	if a.LoadErr != nil {
		out["error"] = a.LoadErr.Error()
	}
	return out
}

// aliases are o-json's alias rows, vim's ex words: bare names that run a
// verb with preset arguments from the palette, the CLI and the API. A
// user's actions.json may add more; aliases.shadow decides a clash.
var aliases = []ochazuke.ActionAlias{
	{Alias: "w", Action: "viewer.save", Note: "write (vim :w)"},
	{Alias: "w!", Action: "viewer.save", Args: ochazuke.Arguments{"force": true}, Note: "write, replacing an existing file (vim :w!)"},
	{Alias: "wq", Action: "viewer.save", Args: ochazuke.Arguments{"quit": true}, Note: "write, then quit (vim :wq)"},
	{Alias: "x", Action: "viewer.save", Args: ochazuke.Arguments{"quit": true}, Note: "write, then quit (vim :x)"},
	{Alias: "e", Action: "viewer.open", Note: "open a file (vim :e)"},
	{Alias: "q", Action: "app.quit", Note: "quit; asks with edits no write saved (vim :q)"},
	{Alias: "q!", Action: "app.quit", Args: ochazuke.Arguments{"force": true}, Note: "quit without writing (vim :q!)"},
}

func (a *App) addAliases() error {
	for _, al := range aliases {
		if err := a.r.AddActionAlias(al); err != nil {
			return err
		}
	}
	return nil
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
	path := ochazuke.StringArg(c.Args, "path", "")
	data, isJSON, err := a.encodeFor(path, doc.Root)
	if err != nil {
		return ochazuke.Failed(err)
	}
	if !isJSON {
		// An export names no document: a document of no file stays one.
		return a.writeFile(c, path, data, mode, "the export", "viewer.overwrite", nil)
	}
	return a.writeFile(c, path, data, mode, "the document", "viewer.overwrite",
		func(path string) {
			if a.Doc == doc && a.Path == "" {
				a.Path, doc.Name = path, path
				if page := a.r.PageOf(pageID); page != nil {
					page.Title = path
				}
			}
		})
}

// pasteKeys adds the pasted lines as keys below the focused node of the
// restructured tree, one per non-blank line, a line indented deeper than
// the one above nesting under it (the outline's paste, through the
// restructure's own edit, as o is restructure.add-key): one undo step,
// refused when a key does not go there (into an array).
func (a *App) pasteKeys(c ochazuke.Ctx, text string) ochazuke.Result {
	e := a.current()
	at := a.focusedNode(c.Instance.ID)
	type level struct {
		indent int
		id     string
	}
	var stack []level
	last, top := "", at
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		key := strings.TrimSpace(line)
		if key == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		var err error
		var id string
		if len(stack) == 0 {
			e, id, err = a.addKey(e, top, "below", key)
			top = id
		} else {
			e, id, err = a.addKey(e, stack[len(stack)-1].id, "child", key)
		}
		if err != nil {
			return ochazuke.Failed(err)
		}
		stack = append(stack, level{indent, id})
		last = id
	}
	if last == "" {
		return ochazuke.Done(nil)
	}
	if !a.valid(e.nodes, e.meta) && a.valid(a.rs.tree.Nodes(), a.rs.meta) {
		return ochazuke.Failed(errors.New("a key does not go here: an array takes only its [] item"))
	}
	if err := a.commit("paste keys", last, e); err != nil {
		return ochazuke.Failed(err)
	}
	a.refocus(last)
	return ochazuke.Done(a.Mapping())
}

// pasteValues adds the pasted lines as string values, one per non-blank
// line (trimmed), as value patches on the restructured document at the
// focused node: appended as items when its value is an array; as an
// array of them under a new placeholder key (restructure.new-key) inside
// it when it is an object, below it beside a plain value. One undo step
// (the key and its value together). A patch names one value, so a node
// under an array's [] item, which stands for every item, refuses.
func (a *App) pasteValues(c ochazuke.Ctx, text string) ochazuke.Result {
	var values []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if v := strings.TrimSpace(line); v != "" {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return ochazuke.Done(nil)
	}
	e := a.current()
	at := a.focusedNode(c.Instance.ID)
	if e.at(at) < 0 {
		at = e.root()
	}
	path := nodePaths(e.nodes, e.meta)[at]
	if strings.Contains(path, "[]") {
		return ochazuke.Failed(fmt.Errorf("%s stands for every item of an array: values go at one value (a node outside [])", path))
	}
	d := a.restructuredDoc()
	if d.err != nil || d.root == nil {
		return ochazuke.Failed(fmt.Errorf("no restructured document: %v", d.err))
	}
	n, err := Get(d.root, path)
	if err != nil {
		return ochazuke.Failed(err)
	}
	base := path
	if base == "." {
		base = ""
	}
	patches := slices.Clone(a.rs.patches)
	if n.Kind == KArray {
		for i, v := range values {
			raw, _ := json.Marshal(v)
			patches = append(patches, Patch{Op: opAdd, Path: base + "[" + strconv.Itoa(len(n.Children)+i) + "]", Value: raw})
		}
		if err := a.commitWith("paste values", at, e, patches); err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(a.patchList())
	}
	where := "child"
	if n.Kind != KObject {
		where = "below"
	}
	e, id, err := a.addKey(e, at, where, "")
	if err != nil {
		return ochazuke.Failed(err)
	}
	if !a.valid(e.nodes, e.meta) && a.valid(a.rs.tree.Nodes(), a.rs.meta) {
		return ochazuke.Failed(errors.New("a key does not go here: an array takes only its [] item"))
	}
	raw, _ := json.Marshal(values)
	patches = append(patches, Patch{Op: opSet, Path: nodePaths(e.nodes, e.meta)[id], Value: raw})
	if err := a.commitWith("paste values", id, e, patches); err != nil {
		return ochazuke.Failed(err)
	}
	a.refocus(id)
	return ochazuke.Done(a.Mapping())
}

// pasteLoads reports whether a paste on the restructured tree loads the
// text as the buffer (viewer.paste) under paste.on-tree mode, rather
// than adding it as nodes (ochazuke.outline.paste).
func pasteLoads(mode, text string) bool {
	switch mode {
	case pasteOnViewer:
		return true
	case pasteOnOutline:
		return false
	}
	return json.Valid([]byte(text))
}

// declareOpenSettings declares recent.size, reload.keep-place,
// empty.hint, paste.on-tree and paste.text-on-tree.
func declareOpenSettings(r *ochazuke.Runtime) error {
	return declareOwn(r, ochazuke.Setting{
		Name: settingRecent, Type: ochazuke.SettingInteger, Default: 10,
		Description: "How many recently opened files o-json remembers (viewer.open's first choices); 0 keeps none",
	}, ochazuke.Setting{
		Name: settingReloadPlace, Type: ochazuke.SettingBoolean, Default: true,
		Description: "Reading the open file again (R, viewer.load) keeps the focused pane, each structure tree's selection by path and the data pane's row and column where they are still there, as Vim's :e! keeps the cursor; off, a reload starts at the root as opening a file does",
	}, ochazuke.Setting{
		Name: settingEmptyHint, Type: ochazuke.SettingBoolean, Default: true,
		Description: "With no document open, the data pane says so and lists the keys that open one (doc.open, doc.paste, restructure.open, from the bindings); off, it shows an empty list. Nothing opens by itself either way",
	}, ochazuke.Setting{
		Name: settingPasteOnTree, Type: ochazuke.SettingString, Default: pasteOnJSON, Enum: []string{pasteOnJSON, pasteOnOutline, pasteOnViewer},
		Description: "A bracketed paste on the restructured tree: json loads valid JSON as the buffer, as on the viewer, and does with anything else what paste.text-on-tree says; outline always adds keys below the node, one per line, nested by indent; viewer always loads (invalid JSON is an error)",
	}, ochazuke.Setting{
		Name: settingPasteText, Type: ochazuke.SettingString, Default: pasteTextKeys, Enum: []string{pasteTextKeys, pasteTextValues, pasteTextIgnore},
		Description: "Text that is not JSON pasted on the restructured tree under paste.on-tree json: keys adds each line as a key below the node, nested by indent; values adds each line as a string value (an array of them under a new placeholder key in an object, items appended to an array); ignore pastes nothing (a plain notice). One undo step either way",
	})
}
