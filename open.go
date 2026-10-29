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
	if len(a.rs.tree.Nodes()) > 1 || len(a.rs.meta) > 0 {
		root := []ochazuke.TreeNode{{ID: rsRootID, Label: ".", Noun: "node"}}
		if err := a.commit("new document", rsRootID, edit{nodes: root, meta: map[string]nodeMeta{}}); err != nil {
			return err
		}
	}
	a.rs.saved = ""
	return a.tree.Focus()
}

// unsaved reports restructure edits that no write or export-map has
// saved: a mapping with entries, other than the last one written.
func (a *App) unsaved() bool {
	m := a.Mapping()
	return len(m.Map) > 0 && mappingKey(m) != a.rs.saved
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
			return a.read(p)
		}, "Read another JSON file off the loop and show it (- is stdin); its path's choices are the recent files, then the files beside the current one").
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
				// Restructure edits nothing saved: ask once, then discard.
				v, err := r.Execute(ochazuke.Request{Noun: "viewer", Verb: "discard", Target: pageID})
				if err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Done(v)
			}
			return a.doneState(a.reset())
		}, "Start an empty document ([no file]), as vim's :enew; with restructure edits no write saved, it asks first (viewer.discard)").
		Verb("discard", func(c ochazuke.Ctx) ochazuke.Result { return a.doneState(a.reset()) },
			"Start an empty document, dropping the restructure's edits (u on the restructure page brings them back); it always asks first").
		Confirm("Start an empty document? The restructure has edits no write saved.").
		Verb("paste", func(c ochazuke.Ctx) ochazuke.Result {
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
		}, "Parse the clipboard's JSON (or text) into the buffer, a document of no file; invalid JSON is an error naming its line and column").
		Verb("write", func(c ochazuke.Ctx) ochazuke.Result { return a.writeDoc(c, writeNewFile) },
			"Write the shown document as indented JSON to a new file; an existing file asks first (viewer.overwrite), the source never. A document of no file is then that file").When("viewer-document").
		Verb("overwrite", func(c ochazuke.Ctx) ochazuke.Result { return a.writeDoc(c, writeReplace) },
			"Replace an existing file with the shown document (never the source)").Confirm("Replace {{path}} with the shown document?").When("viewer-document")
	path := ochazuke.Parameter{Type: "string", Required: true, Description: "the file to write, e.g. NAME.json"}
	params("viewer.open", map[string]ochazuke.Parameter{"path": {Type: "string", Required: true, Source: "viewer.files", Free: true,
		Description: "the JSON file (- is stdin); choices: recent files, then the current folder"}})
	params("viewer.paste", map[string]ochazuke.Parameter{"text": {Type: "string", Description: "the JSON; omit to read the system clipboard"}})
	params("viewer.write", map[string]ochazuke.Parameter{"path": path})
	params("viewer.overwrite", map[string]ochazuke.Parameter{"path": path})
	setHistory(r, ochazuke.HistoryFetch, "viewer.open")
	setHistory(r, ochazuke.HistoryChange, "viewer.new", "viewer.discard", "viewer.paste", "viewer.write", "viewer.overwrite")
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
