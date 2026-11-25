package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/config"
	"ofam.dev/ochazuke/core"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/bindings"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/pathpicker"
	"ofam.dev/ochazuke/widgets/picker"
)

// window is one data pane: which buffer it shows, its view and the view's
// own position. Scroll offsets are derived while drawing.
type window struct {
	buf, view            int
	kv, sub              bool   // pretty: key: value lines; raw: focused subtree only
	format               string // raw: "", "pretty" or "minified"
	open                 map[string]bool
	anchor               int // nested visual selection anchor row, -1 when off
	line, off            int // pretty cursor line and scroll
	seen                 string
	colOff, tOff         int
	rel                  Relation
	relPaths             []Path
	relKey               string
	ed                   *input.Model
	edRaw                string // the buffer text the editor last matched
	edBuf                int
	edPath               string
	edAt, edLen          int // byte range of the edited subtree in the buffer text
	rawFolds             map[int]bool
	readerIdx, readerOff int
}

func newWindow(buf, view int) *window {
	return &window{buf: buf, view: view, anchor: -1, open: map[string]bool{}, edBuf: -1, rawFolds: map[int]bool{}}
}

func (w *window) nestedOpen(p string, d int) bool {
	if v, ok := w.open[p]; ok {
		return v
	}
	return d == 0
}

// bufUI is the per-buffer interface state: structure tree folds and cursor,
// the field selection, the focused path and derived caches.
type bufUI struct {
	ver                    int
	open                   map[string]bool
	cursor                 int
	sel                    []Path
	focus                  Path
	prevKey, dispKey, dKey string
	preview, disp          any
	tree                   *Schema
	doc                    *textDoc
}

func (u *bufUI) schemaOpen(s *Schema, d int) bool {
	if v, ok := u.open[s.Path.String()]; ok {
		return v
	}
	return d < 2
}

type prompt struct {
	kind, label string // filter, field, rename, entries, wrap-object, nest, jq, saveas
	ed          *input.Model
	root        Path
	field       string
	fields      []string
}

type pickModal struct {
	kind   string // palette, buffers, grain, format, convert, nest, jqbuilder
	m      *picker.Model
	paths  []Path
	op     Operation
	target Path
	field  string
	fields []string
}

type helpModal struct {
	q   core.KeyQuery
	off int
}

// App is the whole session. Handlers run one at a time on the engine loop
// and change it in place; the view reads it.
type App struct {
	ring      *BufferRing
	uis       map[int]*bufUI
	wins      []*window
	win, pane int // active window; pane 0 is the structure tree, 1 the data window
	prompt    *prompt
	jq        struct {
		val                any
		err, expr          string
		ok, ready, running bool // ok: val is a result; ready: it is the current query's
		seq                int
	}
	saveFormat  string // W: the format picked for the next save, "" follows the extension
	match       string
	inner       bool
	follow      bool // the tree cursor follows the focus after the next step
	filesFor    string
	w, h        int
	status      string
	tone        string
	register    string
	confirm     string
	prevConfirm string

	km          *core.Keymap
	bindings    []core.Binding
	specs       []core.CommandSpec
	theme       ui.Theme
	help        *bindings.Model
	copy        func(string) error
	fs          pathpicker.FS
	cwd         string
	cfgDir      string
	reader      *Reader
	readerState ReaderState
	notes       []string
}

func (a *App) note(m string) { a.status, a.tone = m, "success" }
func (a *App) info(m string) { a.status, a.tone = m, "info" }
func (a *App) warn(m string) { a.status, a.tone = m, "warning" }
func (a *App) fail(m string) { a.status, a.tone = m, "error" }

func (a *App) cur() *Buffer    { return a.ring.Current() }
func (a *App) active() *window { return a.wins[a.win] }
func (a *App) touch(b *Buffer) { a.bufUI(b).ver++ }
func (a *App) curUI() *bufUI   { return a.bufUI(a.cur()) }

func (a *App) buf(id int) *Buffer {
	for _, b := range a.ring.List() {
		if b.ID == id {
			return b
		}
	}
	return a.cur()
}

func (a *App) bufUI(b *Buffer) *bufUI {
	u := a.uis[b.ID]
	if u == nil {
		u = &bufUI{open: map[string]bool{}, focus: Path{}}
		a.uis[b.ID] = u
	}
	return u
}

// preview is the buffer's value after its pipeline; the structure tree is
// inferred from it. Both keep the last valid parse while the text is broken.
func (a *App) preview(b *Buffer) any {
	u := a.bufUI(b)
	k := fmt.Sprint(u.ver, len(b.Pipeline), len(b.RawText), b.ParseErr == nil)
	if u.prevKey != k {
		u.prevKey, u.preview, u.tree = k, b.Value, b.Structure
		if len(b.Pipeline) > 0 {
			if v, err := b.Preview(); err == nil {
				u.preview, u.tree = v, InferSchema(v)
			} else {
				a.fail("pipeline: " + err.Error())
			}
		}
		if u.tree == nil && u.preview != nil {
			u.tree = InferSchema(u.preview)
		}
	}
	return u.preview
}

func (a *App) schema(b *Buffer) *Schema { a.preview(b); return a.bufUI(b).tree }

// display is what the data views show: the preview, narrowed by a live
// field selection, a filter being typed, or the jq bar's last result.
func (a *App) display(b *Buffer) any {
	v := a.preview(b)
	u := a.bufUI(b)
	key := u.prevKey
	for _, p := range u.sel {
		key += "|" + p.String()
	}
	p := a.prompt
	live := p != nil && b.ID == a.cur().ID
	if live {
		key += fmt.Sprintf("|%s:%s:%s:%d", p.kind, p.ed.Value(), a.match, a.jq.seq)
	}
	if u.dispKey == key {
		return u.disp
	}
	// The selection and the live filter run as the same steps J and y j
	// see. A filter still being typed that does not run (a half regex)
	// leaves the selection's result on screen.
	sel := a.visualSteps(b, false)
	if x, err := ApplyPipeline(v, sel); err == nil {
		v = x
	}
	if live {
		if steps := a.visualSteps(b, true)[len(sel):]; len(steps) > 0 {
			if x, err := ApplyPipeline(v, steps); err == nil {
				v = x
			}
		} else if p.kind == "jq" && a.jq.ok {
			v = a.jq.val
		}
	}
	u.dispKey, u.disp = key, v
	return v
}

func (a *App) textDoc(b *Buffer, kv bool) *textDoc {
	u := a.bufUI(b)
	v := a.display(b)
	if key := u.dispKey + "|" + strconv.FormatBool(kv); u.dKey != key {
		u.dKey, u.doc = key, prettyDoc(v)
		if kv {
			u.doc = kvDoc(v)
		}
	}
	return u.doc
}

type srow struct {
	s     *Schema
	depth int
	open  bool
}

func (a *App) structRows(b *Buffer) []srow {
	u := a.bufUI(b)
	var out []srow
	var walk func(s *Schema, d int)
	walk = func(s *Schema, d int) {
		open := len(s.Children) > 0 && u.schemaOpen(s, d)
		out = append(out, srow{s, d, open})
		if open {
			for _, c := range s.Children {
				walk(c, d+1)
			}
		}
	}
	if root := a.schema(b); root != nil {
		walk(root, 0)
	}
	return out
}

// sync runs after every handler: it keeps the focus on an existing value,
// lets the structure tree follow a focus moved in a data view, and sets the
// page's focus and mode from the pane, view and prompt.
func (a *App) sync(s core.State) core.State {
	b := a.cur()
	u := a.bufUI(b)
	v := a.display(b)
	if _, ok := AtPath(v, u.focus); !ok {
		u.focus = resolvePath(v, u.focus)
	}
	follow := a.pane == 1 || a.follow
	a.follow = false
	if root := a.schema(b); follow && root != nil {
		a.openTo(u, root, wildPath(u.focus))
	}
	rows := a.structRows(b)
	if follow {
		target, best := wildPath(u.focus), -1
		for i, r := range rows {
			if pathHasPrefix(target, r.s.Path) && (best < 0 || len(r.s.Path) > len(rows[best].s.Path)) {
				best = i
			}
		}
		u.cursor = max(best, 0)
	}
	u.cursor = min(max(u.cursor, 0), max(len(rows)-1, 0))
	for _, w := range a.wins {
		if w.view == viewRaw {
			a.ensureEditor(w, a.buf(w.buf))
		}
	}
	f := s.Pages[pageID]
	switch {
	case a.prompt != nil:
		f.FocusID, f.Mode = nodePrompt, "prompt"
	case a.pane == 0:
		f.FocusID, f.Mode = nodeTree, "normal"
	default:
		f.FocusID, f.Mode = winNode(a.win), "normal"
		if a.active().view == viewRaw {
			f.Mode = "vim"
		}
	}
	s.Pages = maps.Clone(s.Pages)
	s.Pages[pageID] = f
	return s
}

func (a *App) openTo(u *bufUI, s *Schema, target Path) {
	for _, c := range s.Children {
		if len(c.Path) < len(target) && pathHasPrefix(target, c.Path) {
			u.open[c.Path.String()] = true
			a.openTo(u, c, target)
		}
	}
}

// --- raw editor ---------------------------------------------------------------------

// ensureEditor (re)seeds window w's editor when it has none, shows another
// buffer, or the buffer text changed elsewhere (another window, undo).
func (a *App) ensureEditor(w *window, b *Buffer) {
	if w.ed != nil && w.edBuf == b.ID && w.edRaw == b.RawText {
		return
	}
	text, base := b.RawText, Path{}
	w.edAt, w.edLen, w.edPath = 0, len(text), "$"
	focus := a.bufUI(b).focus
	if w.sub {
		if sp, ok := deepestSpan(scanJSON(b.RawText), focus); ok && sp.start <= sp.end && sp.end <= len(b.RawText) {
			text, base, w.edAt, w.edLen, w.edPath = b.RawText[sp.start:sp.end], sp.path, sp.start, sp.end-sp.start, sp.path.String()
		}
	}
	if w.format != "" {
		if v, err := ParseJSON(text); err == nil {
			if out, err := FormatJSON(v, w.format == "pretty"); err == nil {
				text = strings.TrimRight(string(out), "\n")
			}
		}
	}
	w.ed, w.edRaw, w.edBuf = input.New(text, true), b.RawText, b.ID
	// Put the cursor on the focused value's first byte: the exact key span
	// when the text has it, else the deepest span along the focus path.
	rel := focus[min(len(base), len(focus)):]
	if sp, ok := deepestSpan(scanJSON(text), rel); ok && sp.start <= len(text) {
		moveEditor(w.ed, text, utf8.RuneCountInString(text[:sp.start]))
	}
}

// deepestSpan is the span of the longest path in spans that is a prefix of
// p (p itself when the text holds it).
func deepestSpan(spans []jspan, p Path) (jspan, bool) {
	best := -1
	for i, sp := range spans {
		if pathHasPrefix(p, sp.path) && (best < 0 || len(sp.path) > len(spans[best].path)) {
			best = i
		}
	}
	if best < 0 {
		return jspan{}, false
	}
	return spans[best], true
}

// moveEditor puts the editor's cursor on rune offset off of text.
func moveEditor(ed *input.Model, text string, off int) {
	rs := []rune(text)
	line, col := 0, 0
	for _, r := range rs[:min(off, len(rs))] {
		if r == '\n' {
			line, col = line+1, 0
		} else {
			col++
		}
	}
	if line > 0 {
		ed.HandleCommand(core.Command{ID: "motion.down", Count: line})
	}
	if col > 0 {
		ed.HandleCommand(core.Command{ID: "motion.right", Count: col})
	}
}

// editorKey forwards a key to the raw editor. A read-only buffer refuses any
// key that would change the text or start inserting.
func (a *App) editorKey(s core.State, c core.Command) (core.State, []core.Effect) {
	w := a.active()
	b := a.buf(w.buf)
	a.ensureEditor(w, b)
	before, at := w.ed.Value(), w.ed.Cursor()
	w.ed.HandleInput(c.Arg("key"), c.Arg("text"))
	after := w.ed.Value()
	if !b.Editable && (after != before || w.ed.Mode() == input.ModeInsert) {
		w.ed = input.New(before, true)
		moveEditor(w.ed, before, at)
		a.warn("READ-ONLY: ctrl+e enables editing (the file is never written until you save)")
		return s, nil
	}
	if after != before {
		a.writeBack(w, b, after)
		clear(w.rawFolds)
	} else if w.ed.Mode() == input.ModeInsert && len(w.rawFolds) > 0 {
		line := cursorLine(after, w.ed.Cursor())
		for _, blk := range findJSONBlocks(after) {
			if line >= blk.openLine && line <= blk.closeLine {
				delete(w.rawFolds, blk.openLine)
			}
		}
	}
	return s, nil
}

func (a *App) writeBack(w *window, b *Buffer, text string) {
	raw := text
	if w.sub {
		if w.edAt+w.edLen > len(b.RawText) {
			w.ed = nil
			return
		}
		raw = b.RawText[:w.edAt] + text + b.RawText[w.edAt+w.edLen:]
		w.edLen = len(text)
	}
	b.SetRaw(raw)
	w.edRaw = b.RawText
	a.touch(b)
}

// --- configuration -------------------------------------------------------------------

// Config is what newShell (or a test) supplies to build an App.
type Config struct {
	Bindings  []core.Binding
	Theme     *ui.Theme
	Settings  *config.Settings
	Copy      func(string) error
	FS        pathpicker.FS
	Cwd       string
	ConfigDir string
	Reader    string
	Notes     []string
}

// defaultSettings are the settings.json keys and their defaults.
var defaultSettings = map[string]any{
	"theme":     "",          // a preset: dark, light, compact, spacious, high-contrast
	"density":   "",          // compact, balanced or spacious; empty keeps the theme's
	"borders":   "",          // rounded, sharp, minimal or none; empty keeps the theme's
	"matchMode": "substring", // filter mode: substring, fuzzy or regex
	"clipboard": true,        // copy yanks to the system clipboard
	"innerJoin": false,       // flatten drops parents whose arrays are empty
}

// loadConfig reads bindings.json, themes.json and settings.json (JSONC) from
// dir. Missing files keep the library defaults; broken ones become notes.
func loadConfig(dir string) Config {
	var cfg Config
	read := func(name string) []byte {
		if dir == "" {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			cfg.Notes = append(cfg.Notes, name+": "+err.Error())
		}
		return data
	}
	if data := read("bindings.json"); data != nil {
		if bs, err := config.LoadBindings(data); err != nil {
			cfg.Notes = append(cfg.Notes, err.Error())
		} else {
			cfg.Bindings = bs
		}
	}
	set, err := config.LoadSettings(read("settings.json"), defaultSettings)
	if err != nil {
		cfg.Notes = append(cfg.Notes, err.Error())
		set, _ = config.LoadSettings(nil, defaultSettings)
	}
	cfg.Settings = set
	base := ui.DefaultDarkTheme()
	if name := set.String("theme", ""); name != "" {
		for _, t := range ui.ThemePresets() {
			if t.Name == name || strings.HasSuffix(t.Name, "-"+name) || strings.HasSuffix(t.Name, name+"-dark") {
				base = t
			}
		}
	}
	if data := read("themes.json"); data != nil {
		if th, err := config.LoadTheme(data, base); err != nil {
			cfg.Notes = append(cfg.Notes, err.Error())
		} else {
			base = th
		}
	}
	if d := set.String("density", ""); d != "" {
		base.Density = d
	}
	if bs := set.String("borders", ""); bs != "" {
		base.BorderStyle = bs
	}
	cfg.Theme = &base
	return cfg
}

// systemCopier copies with the first clipboard tool on PATH, or is nil.
func systemCopier() func(string) error {
	for _, tool := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		path, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		args := tool[1:]
		return func(text string) error {
			cmd := exec.Command(path, args...)
			cmd.Stdin = strings.NewReader(text)
			return cmd.Run()
		}
	}
	return nil
}

// newShell opens b with the configuration in configPath (a directory holding
// bindings.json, themes.json and settings.json; "" uses the defaults).
func newShell(b *Buffer, configPath string) *otea.Shell {
	return newShellWithReader(b, configPath, "")
}

// newShellWithReader opens b with configPath and an optional declarative reader.
func newShellWithReader(b *Buffer, configPath, readerName string) *otea.Shell {
	cfg := loadConfig(configPath)
	cfg.Cwd, _ = os.Getwd()
	cfg.ConfigDir = configPath
	cfg.Reader = readerName
	if cfg.Settings.Bool("clipboard", true) {
		cfg.Copy = systemCopier()
	}
	return newApp(cfg, b).shell()
}

var modes = []core.Mode{{Name: "normal"}, {Name: "vim"}, {Name: "prompt"}, {Name: "pick"}, {Name: "files"}, {Name: "help"}}

func newApp(cfg Config, b *Buffer) *App {
	a := &App{ring: &BufferRing{}, uis: map[int]*bufUI{}, copy: cfg.Copy, fs: cfg.FS, cwd: cfg.Cwd, cfgDir: cfg.ConfigDir, notes: cfg.Notes, theme: ui.DefaultDarkTheme()}
	if a.fs == nil {
		a.fs = pathpicker.OSFS{}
	}
	if cfg.Theme != nil {
		a.theme = *cfg.Theme
	}
	set := cfg.Settings
	if set == nil {
		set, _ = config.LoadSettings(nil, defaultSettings)
	}
	a.match = set.String("matchMode", "substring")
	if !slices.Contains(matchModes, a.match) {
		a.match = "substring"
		a.notes = append(a.notes, "settings: matchMode must be substring, fuzzy or regex")
	}
	a.inner = set.Bool("innerJoin", false)
	a.specs = commandSpecs()
	for _, d := range config.CheckBindings(modes, cfg.Bindings, a.specs...) {
		if d.Severity == config.SevError {
			a.notes = append(a.notes, fmt.Sprintf("bindings.json #%d: %s", d.Index, d.Message))
		}
	}
	a.bindings = config.MergeBindings(defaultBindings(), cfg.Bindings)
	km, err := core.NewKeymap(modes, a.bindings, a.specs...)
	if err != nil {
		a.notes = append(a.notes, "bindings: "+strings.ReplaceAll(err.Error(), "\n", "; "))
	}
	a.km, a.help = km, bindings.New(modes, a.bindings, a.specs...)
	a.ring.Add(b)
	a.wins = []*window{newWindow(b.ID, viewNested)}
	if _, arr := b.Value.([]any); arr {
		a.active().view = viewTable
	}
	if cfg.Reader != "" {
		a.activateReader(cfg.Reader)
	}
	if b.Editable && b.Path == "" && strings.TrimSpace(b.RawText) == "" {
		a.startScratch(b)
	}
	if len(a.notes) > 0 {
		a.warn(strings.Join(a.notes, " · "))
	}
	return a
}

// startScratch shows b's raw text, focused and already inserting, so typing
// { or [ starts a document.
func (a *App) startScratch(b *Buffer) {
	w := a.active()
	w.buf, w.view, w.ed, a.pane = b.ID, viewRaw, nil, 1
	a.ensureEditor(w, b)
	w.ed.HandleKey("i")
}

// --- commands and bindings ------------------------------------------------------------

// cmdTable lists the user commands: id, normal-mode keys, raw-editor keys
// (both "|"-separated sequences) and the description.
var cmdTable = [...][4]string{
	{"view.table", "1", "alt+1", "Table view of the focused array (the column under the cursor shows in full)"},
	{"view.nested", "2", "alt+2", "Nested data browser (h/l fold, v selects rows to fold)"},
	{"view.pretty", "3", "alt+3", "Pretty JSON at the focused path (again: key: value lines)"},
	{"view.raw", "4", "alt+4", "Raw text in a vim editor (again: the focused subtree only)"},
	{"view.reader", "5", "alt+5", "Declarative reader / timeline view (switch back to raw JSON with 1-4)"},
	{"reader.select", "g r", "", "Select a declarative reader (e.g. ai-session) or turn off"},
	{"reader.toggle.thinking", "T", "", "Reader: toggle thinking blocks"},
	{"reader.toggle.tools", "O", "", "Reader: toggle tool call/result blocks"},
	{"reader.filter.tools", "P", "", "Reader: filter tool calls by regex/substring pattern"},
	{"recipe.save", "", "", "Save the current pipeline as a reusable JSON recipe file"},
	{"pane.toggle", "tab", "", "Switch between the structure tree and the data window"},
	{"pane.enter", "enter", "", "Open the data window from the tree, or fold the nested row"},
	{"select.toggle", "space", "", "Select or unselect the field under the tree cursor (any depth)"},
	{"select.clear", "esc", "", "Clear the visual rows, then the field selection"},
	{"select.project", "p", "", "Project the selected fields as a pipeline step"},
	{"flatten", "F", "", "Flatten the selected fields (or the subtree) to one row per deepest array element"},
	{"flatten.inner", "I", "", "Toggle inner join for flatten: drop parents whose arrays are empty"},
	{"shape.keyby", "K", "", "Array to object, keyed by the focused field"},
	{"shape.entries", "E", "", "Object to array of entries, its keys in a column you name (key by default)"},
	{"shape.convert", "C", "", "Convert shape: key by field, wrap in object/array, or object to entries/values/keys"},
	{"shape.group", "g b", "", "Group rows by the focused column"},
	{"shape.hoist", "<", "", "Hoist focused/marked fields up one level, or splat an object into its parent"},
	{"shape.nest", ">", "", "Move focused/marked fields into an existing sibling object or a new nested object"},
	{"shape.transfer", "M", "", "Transfer focused/marked fields or subtree into the other split window"},
	{"column.rename", "r", "", "Rename the focused column"},
	{"column.drop", "x", "", "Drop the focused column"},
	{"sort.cycle", "s", "", "Sort rows by the focused column: ascending, descending, off"},
	{"filter.all", "/", "", "Filter rows across all columns"},
	{"filter.field", "f", "", "Filter rows by the focused column (==, !=, >, <, ~regex, or contains)"},
	{"filter.mode", "ctrl+m|alt+m", "", "Cycle the filter match mode: substring, fuzzy, regex"},
	{"jq.open", "J", "", "Open the jq bar with the pipeline's jq (live, cancellable preview)"},
	{"yank.jq", "y j", "", "Copy the jq command for the pipeline"},
	{"yank.path", "y p", "", "Copy the focused path"},
	{"yank.value", "y y", "", "Copy the focused value as JSON"},
	{"history.undo", "u", "", "Undo the last pipeline step or edit"},
	{"history.redo", "ctrl+r", "", "Redo"},
	{"edit.toggle", "ctrl+e", "ctrl+e", "Toggle edit mode (files open read-only)"},
	{"file.save", "ctrl+s", "ctrl+s", "Save the buffer (asks for a path when it has none)"},
	{"file.saveas", "W", "", "Save or export: pick pretty, minified, JSONL, CSV or TSV (a table saves as shown), then a folder and name"},
	{"file.open", "ctrl+o", "ctrl+o", "Open a file into a new buffer"},
	{"buffer.new", "ctrl+n", "ctrl+n", "New scratch buffer"},
	{"buffer.next", "] b", "", "Next buffer"},
	{"buffer.prev", "[ b", "", "Previous buffer"},
	{"buffer.alt", "ctrl+6|ctrl+^", "ctrl+6|ctrl+^", "Alternate buffer"},
	{"buffer.pick", "g B", "", "Pick a buffer"},
	{"window.split", "ctrl+w v", "ctrl+w v", "Split: a second window side by side"},
	{"window.next", "ctrl+w w|ctrl+w l|ctrl+w h", "ctrl+w w|ctrl+w l|ctrl+w h", "Next window"},
	{"window.close", "ctrl+w q|ctrl+w c", "ctrl+w q|ctrl+w c", "Close the window"},
	{"window.only", "ctrl+w o", "ctrl+w o", "Close the other window"},
	{"raw.format", "=", "=", "Raw view: pretty, minified, as typed"},
	{"raw.scope", "ctrl+t", "ctrl+t", "Raw view: whole document or focused subtree"},
	{"raw.fold.toggle", "", "z a", "Raw view: toggle fold on enclosing JSON block"},
	{"raw.fold.close", "", "z c", "Raw view: fold enclosing JSON block"},
	{"raw.fold.open", "", "z o", "Raw view: unfold enclosing JSON block"},
	{"raw.fold.all", "", "z M", "Raw view: fold all nested JSON blocks"},
	{"raw.fold.none", "", "z R", "Raw view: unfold all JSON blocks"},
	{"nested.visual", "v", "", "Nested view: start or stop selecting rows"},
	{"palette.open", ":", ":", "Commands (also :w :q :bn :bp :b :e PATH :enew :vs)"},
	{"help.open", "?", "", "Keys active here"},
	{"theme.cycle", "t|g t", "", "Next theme preset"},
	{"density.cycle", "d|g d", "", "Next density: compact, balanced, spacious"},
	{"app.close", "q|ctrl+c", "ctrl+c", "Quit (asks again with unsaved edits)"},
}

var internalCmds = []string{"editor.key", "prompt.key", "prompt.accept", "prompt.cancel", "jq.chip", "jq.builder", "jq.result", "clipboard.done",
	"pick.insert", "pick.backspace", "pick.close", picker.CmdConfirm, picker.CmdModeCycle,
	"files.insert", "files.backspace", "files.close", pathpicker.CmdOpen, pathpicker.CmdParent, pathpicker.CmdHiddenToggle,
	pathpicker.CmdSelectCurrent, pathpicker.CmdModeCycle, "help.close", "file.write", "file.load", "buffer.switch", "app.quit", "recipe.load", "reader.set"}

func commandSpecs() []core.CommandSpec {
	var out []core.CommandSpec
	for _, c := range cmdTable {
		out = append(out, core.CommandSpec{ID: c[0], Description: c[3], Tags: []string{"user"}})
	}
	for _, id := range internalCmds {
		out = append(out, core.CommandSpec{ID: id, Description: id})
	}
	out = append(out, input.ReadlineSpecs()...)
	return out
}

func keyBind(mode, keys, cmd, when string, args map[string]any) core.Binding {
	return core.Binding{Mode: mode, Keys: strings.Fields(keys), Command: cmd, When: when, Args: args}
}

func classBind(mode, cls, cmd string) core.Binding {
	return core.Binding{Mode: mode, Class: cls, Command: cmd, Args: map[string]any{"key": "$key", "text": "$text"}, Hidden: true}
}

// defaultBindings is the built-in keymap; help is generated from whatever of
// it (plus user overrides) resolves.
func defaultBindings() []core.Binding {
	var out []core.Binding
	vimNormal := `facts.mode == "normal"`
	for _, c := range cmdTable {
		when := ""
		if strings.HasPrefix(c[0], "view.") {
			when = "count == null"
		}
		for _, k := range strings.Split(c[1], "|") {
			if k != "" {
				bd := keyBind("normal", k, c[0], when, nil)
				bd.Hint = c[3]
				out = append(out, bd)
			}
		}
		for _, k := range strings.Split(c[2], "|") {
			if k != "" {
				bd := keyBind("vim", k, c[0], when, nil)
				if k == ":" || k == "=" || strings.HasPrefix(k, "z ") {
					bd.When = vimNormal
				}
				bd.Hint = c[3]
				out = append(out, bd)
			}
		}
	}
	for _, sb := range core.StandardBindings("normal") {
		if !slices.Contains([]string{"motion.word.next", "motion.word.prev", "motion.word.end", "motion.group.next", "motion.group.prev", "motion.block.next", "motion.block.prev"}, sb.Command) {
			out = append(out, sb)
		}
	}
	out = append(out, keyBind("normal", "end", "motion.bottom", "", nil), keyBind("normal", "home", "motion.top", "", nil),
		keyBind("normal", "down", "motion.down", "", nil), keyBind("normal", "up", "motion.up", "", nil),
		keyBind("normal", "left", "motion.left", "", nil), keyBind("normal", "right", "motion.right", "", nil),
		keyBind("vim", "tab", "pane.toggle", vimNormal, nil), keyBind("vim", "esc", "pane.toggle", vimNormal, nil),
		classBind("vim", core.ClassAny, "editor.key"),

		keyBind("prompt", "enter", "prompt.accept", "", nil), keyBind("prompt", "esc", "prompt.cancel", "", nil), keyBind("prompt", "ctrl+c", "prompt.cancel", "", nil),
		keyBind("prompt", "ctrl+m", "filter.mode", "", nil), keyBind("prompt", "alt+m", "filter.mode", "", nil),
		keyBind("prompt", "tab", "jq.builder", "", nil), keyBind("prompt", "ctrl+b", "jq.builder", "", nil))
	for _, ch := range jqChips {
		out = append(out, keyBind("prompt", ch.key, "jq.chip", "", map[string]any{"chip": ch.name}))
	}
	out = append(out, classBind("prompt", core.ClassAny, "prompt.key"))
	for _, m := range []string{"pick", "files"} {
		out = append(out, classBind(m, core.ClassPrintable, m+".insert"), classBind(m, core.ClassPaste, m+".insert"),
			keyBind(m, "backspace", m+".backspace", "", nil), keyBind(m, "esc", m+".close", "", nil), keyBind(m, "ctrl+c", m+".close", "", nil),
			keyBind(m, "down", "motion.down", "", nil), keyBind(m, "ctrl+n", "motion.down", "", nil), keyBind(m, "up", "motion.up", "", nil),
			keyBind(m, "ctrl+p", "motion.up", "", nil), keyBind(m, "pgdown", "motion.page.down", "", nil), keyBind(m, "pgup", "motion.page.up", "", nil))
	}
	out = append(out, keyBind("pick", "enter", picker.CmdConfirm, "", nil), keyBind("pick", "tab", picker.CmdModeCycle, "", nil),
		keyBind("files", "enter", pathpicker.CmdOpen, "", nil), keyBind("files", "right", pathpicker.CmdOpen, "", nil),
		keyBind("files", "left", pathpicker.CmdParent, "", nil), keyBind("files", "ctrl+s", pathpicker.CmdSelectCurrent, "", nil),
		keyBind("files", "ctrl+t", pathpicker.CmdHiddenToggle, "", nil), keyBind("files", "tab", pathpicker.CmdModeCycle, "", nil),
		keyBind("help", "esc", "help.close", "", nil), keyBind("help", "q", "help.close", "", nil), keyBind("help", "?", "help.close", "", nil))
	for _, m := range []string{"pick", "files"} {
		taken := map[string]bool{}
		for _, b := range out {
			if b.Mode == m && len(b.Keys) > 0 {
				taken[strings.Join(b.Keys, " ")] = true
			}
		}
		for _, rb := range input.ReadlineBindings(m, "") {
			k := strings.Join(rb.Keys, " ")
			if taken[k] {
				continue
			}
			taken[k] = true
			out = append(out, rb)
		}
	}
	return append(out, core.StandardBindings("help")...)
}

// --- engine -------------------------------------------------------------------------------

type hfn func(s core.State, c core.Command) (core.State, []core.Effect)

// wrap clears the last status on user commands, runs f and syncs focus and
// mode.
func (a *App) wrap(f hfn) core.Handler {
	return func(s core.State, c core.Command) core.StepResult {
		a.w, a.h = s.Width, s.Height
		if c.Actor.Kind == "user" {
			a.prevConfirm, a.confirm = a.confirm, ""
			a.status, a.tone = "", ""
		}
		s, eff := f(s, c)
		a.prevConfirm = ""
		return core.Next(a.sync(s), eff...)
	}
}

func openModal(s core.State, id, mode string, data any) core.State {
	s = closeModal(s, id)
	s.Modals = append(slices.Clone(s.Modals), core.Frame{ID: id, Kind: core.FrameModal, Mode: mode, FocusID: id, Data: data})
	return s
}

func closeModal(s core.State, id string) core.State {
	if i := slices.IndexFunc(s.Modals, func(f core.Frame) bool { return f.ID == id }); i >= 0 {
		s.Modals = slices.Delete(slices.Clone(s.Modals), i, i+1)
	}
	return s
}

func modalData(s core.State, id string) any {
	if i := slices.IndexFunc(s.Modals, func(f core.Frame) bool { return f.ID == id }); i >= 0 {
		return s.Modals[i].Data
	}
	return nil
}

// dispatch runs c through the engine after this step, with the same guards
// as a key.
func dispatch(id string, args map[string]any) core.Effect {
	return core.CustomEffect{Kind: "dispatch", Data: core.Command{ID: id, Args: args, Source: "palette", Actor: core.Actor{Kind: "user"}}}
}

var motionIDs = []string{"motion.down", "motion.up", "motion.left", "motion.right", "motion.top", "motion.bottom", "motion.start", "motion.end",
	"motion.page.down", "motion.page.up", "motion.half.down", "motion.half.up"}

func (a *App) engine() *core.Engine {
	e := core.NewEngine(a.km)
	h := func(id string, f hfn) { e.Handle(id, a.wrap(f)) }
	for _, id := range motionIDs {
		for scope, f := range map[string]hfn{"tree": a.treeMotion, "table": a.tableMotion, "nested": a.nestedMotion, "pretty": a.prettyMotion, "reader": a.readerMotion, "pick": a.pickCmd, "files": a.filesCmd, "help": a.helpScroll} {
			e.HandleScope(scope, id, a.wrap(f))
		}
	}
	for i, id := range []string{"view.table", "view.nested", "view.pretty", "view.raw", "view.reader"} {
		h(id, a.setView(i+1))
	}
	for id, f := range map[string]hfn{
		"reader.select": a.readerSelect, "reader.toggle.thinking": a.readerToggleBool("thinking"),
		"reader.toggle.tools": a.readerToggleBool("tools"), "reader.filter.tools": a.readerFilterOpen,
		"reader.set": func(s core.State, c core.Command) (core.State, []core.Effect) {
			a.activateReader(c.Arg("name"))
			return s, nil
		},
		"recipe.save": a.recipeSave, "recipe.load": a.recipeLoad,
		"pane.toggle": a.paneToggle, "pane.enter": a.paneEnter, "select.toggle": a.selectToggle, "select.clear": a.selectClear,
		"select.project": a.project, "flatten": a.flatten, "flatten.inner": a.innerToggle, "shape.keyby": a.shape, "shape.entries": a.shape,
		"shape.convert": a.shapeConvert, "shape.group": a.shape, "shape.hoist": a.shapeHoist, "shape.nest": a.shapeNest, "shape.transfer": a.shapeTransfer,
		"column.rename": a.shape, "column.drop": a.shape, "sort.cycle": a.sortCycle,
		"filter.all": a.filterOpen, "filter.field": a.filterOpen, "filter.mode": a.modeCycle, "jq.open": a.jqOpen, "jq.chip": a.jqChip, "jq.builder": a.jqBuilder,
		"jq.result": a.jqResult, "yank.jq": a.yank, "yank.path": a.yank, "yank.value": a.yank, "clipboard.done": a.clipDone,
		"history.undo": a.history, "history.redo": a.history, "edit.toggle": a.editToggle, "file.save": a.save, "file.saveas": a.save,
		"file.write": a.write, "file.open": a.filesOpen, "file.load": a.load, "buffer.new": a.bufferCmd, "buffer.next": a.bufferCmd,
		"buffer.prev": a.bufferCmd, "buffer.alt": a.bufferCmd, "buffer.switch": a.bufferCmd, "buffer.pick": a.bufferPick,
		"window.split": a.windowCmd, "window.next": a.windowCmd, "window.close": a.windowCmd, "window.only": a.windowCmd,
		"raw.format": a.rawCmd, "raw.scope": a.rawCmd,
		"raw.fold.toggle": a.rawFoldCmd, "raw.fold.close": a.rawFoldCmd, "raw.fold.open": a.rawFoldCmd,
		"raw.fold.all": a.rawFoldCmd, "raw.fold.none": a.rawFoldCmd,
		"nested.visual": a.visual, "palette.open": a.paletteOpen, "help.open": a.helpOpen,
		"help.close":  func(s core.State, c core.Command) (core.State, []core.Effect) { return closeModal(s, modalHelp), nil },
		"theme.cycle": a.themeCycle, "density.cycle": a.themeCycle, "app.close": a.quit,
		"app.quit": func(s core.State, c core.Command) (core.State, []core.Effect) {
			return s, []core.Effect{core.QuitEffect{}}
		},
		"editor.key": a.editorKey, "prompt.key": a.promptKey, "prompt.accept": a.promptAccept, "prompt.cancel": a.promptCancel,
	} {
		h(id, f)
	}
	for _, id := range append([]string{"pick.insert", "pick.backspace", "pick.close", picker.CmdConfirm, picker.CmdModeCycle}, input.ReadlineCommandIDs()...) {
		e.HandleScope("pick", id, a.wrap(a.pickCmd))
	}
	for _, id := range append([]string{"files.insert", "files.backspace", "files.close", pathpicker.CmdOpen, pathpicker.CmdParent, pathpicker.CmdHiddenToggle, pathpicker.CmdSelectCurrent, pathpicker.CmdModeCycle}, input.ReadlineCommandIDs()...) {
		e.HandleScope("files", id, a.wrap(a.filesCmd))
	}
	return e
}

// shell builds the tea shell. Palette picks come back as commands; theme
// changes reach the renderer.
func (a *App) shell() *otea.Shell {
	s := a.sync(core.NewState(core.Frame{ID: pageID, FocusID: nodeTree, Mode: "normal"}))
	sh := otea.New(a.engine(), s, a.view, a.theme)
	sh.OnCustom = func(e core.CustomEffect) tea.Cmd {
		switch d := e.Data.(type) {
		case core.Command:
			return func() tea.Msg { return core.CommandEvent{Command: d} }
		case ui.Theme:
			sh.Theme = d
		}
		return nil
	}
	return sh
}

// --- navigation ------------------------------------------------------------------------

// moveIdx applies a vertical motion to a cursor over total rows.
func moveIdx(cur, total, page int, c core.Command) int {
	n := c.Times()
	page = max(page, 1)
	switch c.ID {
	case "motion.down":
		cur += n
	case "motion.up":
		cur -= n
	case "motion.top", "motion.bottom":
		cur = total - 1
		if c.ID == "motion.top" {
			cur = 0
		}
		if c.Count > 0 {
			cur = c.Count - 1
		}
	case "motion.page.down":
		cur += n * page
	case "motion.page.up":
		cur -= n * page
	case "motion.half.down":
		cur += n * max(page/2, 1)
	case "motion.half.up":
		cur -= n * max(page/2, 1)
	}
	return min(max(cur, 0), max(total-1, 0))
}

func (a *App) treeMotion(s core.State, c core.Command) (core.State, []core.Effect) {
	b := a.cur()
	u := a.bufUI(b)
	rows := a.structRows(b)
	if len(rows) == 0 {
		return s, nil
	}
	i := min(u.cursor, len(rows)-1)
	r := rows[i]
	switch c.ID {
	case "motion.left":
		if r.open && r.depth > 0 {
			u.open[r.s.Path.String()] = false
		} else {
			for j := i - 1; j >= 0; j-- {
				if rows[j].depth < r.depth {
					i = j
					break
				}
			}
		}
	case "motion.right":
		if len(r.s.Children) > 0 && !r.open {
			u.open[r.s.Path.String()] = true
		} else if r.open {
			i++
		}
	case "motion.start", "motion.end":
	default:
		i = moveIdx(i, len(rows), a.geometry().rows(), c)
	}
	rows = a.structRows(b)
	u.cursor = min(i, len(rows)-1)
	u.focus = resolvePath(a.display(b), rows[u.cursor].s.Path)
	return s, nil
}

func (a *App) nestedMotion(s core.State, c core.Command) (core.State, []core.Effect) {
	w := a.active()
	b := a.buf(w.buf)
	u := a.bufUI(b)
	v := a.display(b)
	rows, _ := a.nestedRows(w, v)
	i := a.nestedCursor(w, b, rows)
	switch c.ID {
	case "motion.left", "motion.right":
		open := c.ID == "motion.right"
		if w.anchor >= 0 {
			lo, hi, n := min(i, w.anchor), min(max(i, w.anchor), len(rows)-1), 0
			for _, p := range rows[lo : hi+1] {
				if x, _ := AtPath(v, p); childCount(x) > 0 && len(p) > 0 {
					w.open[p.String()], n = open, n+1
				}
			}
			w.anchor = -1
			a.info(fmt.Sprintf("%s %d of the %d selected rows", map[bool]string{true: "unfolded", false: "folded"}[open], n, hi-lo+1))
			return s, nil
		}
		p := rows[i]
		x, _ := AtPath(v, p)
		isOpen := childCount(x) > 0 && w.nestedOpen(p.String(), len(p))
		switch {
		case open && childCount(x) > 0 && !isOpen:
			w.open[p.String()] = true
		case open && isOpen:
			i++
		case !open && isOpen && len(p) > 0:
			w.open[p.String()] = false
		case !open && len(p) > 0:
			u.focus = p[:len(p)-1]
			return s, nil
		}
	case "motion.start", "motion.end":
	default:
		i = moveIdx(i, len(rows), a.geometry().rows(), c)
	}
	rows, _ = a.nestedRows(w, v)
	u.focus = rows[min(i, len(rows)-1)]
	w.seen = u.focus.String()
	return s, nil
}

func (a *App) prettyMotion(s core.State, c core.Command) (core.State, []core.Effect) {
	w := a.active()
	b := a.buf(w.buf)
	u := a.bufUI(b)
	switch c.ID {
	case "motion.left":
		if len(u.focus) > 0 {
			u.focus = u.focus[:len(u.focus)-1]
		}
	case "motion.right":
		x, _ := AtPath(a.display(b), u.focus)
		done := false
		eachChild(x, func(part PathPart, _ string, _ any) {
			if !done {
				u.focus, done = pathJoin(u.focus, part), true
			}
		})
	case "motion.start", "motion.end":
	default:
		d := a.textDoc(b, w.kv)
		w.line = moveIdx(w.line, len(d.lines), a.geometry().rows(), c)
		if p, ok := d.linePath(w.line); ok {
			u.focus = p
		}
		w.seen = u.focus.String()
	}
	return s, nil
}

func (a *App) tableMotion(s core.State, c core.Command) (core.State, []core.Effect) {
	w := a.active()
	b := a.buf(w.buf)
	u := a.bufUI(b)
	rel, root := a.relation(w, b)
	v := a.display(b)
	row, col := a.tablePosFor(w, b, root, rel.Columns)
	col = max(col, 0)
	switch c.ID {
	case "motion.left":
		col -= c.Times()
	case "motion.right":
		col += c.Times()
	case "motion.start":
		col = 0
	case "motion.end":
		col = len(rel.Columns) - 1
	default:
		row = moveIdx(row, len(rel.Rows), a.geometry().rows()-2, c)
	}
	name := ""
	if n := len(rel.Columns); n > 0 {
		name = rel.Columns[min(max(col, 0), n-1)]
	}
	u.focus = a.cellPathFor(w, v, root, row, name)
	return s, nil
}

func (a *App) setView(n int) hfn {
	return func(s core.State, c core.Command) (core.State, []core.Effect) {
		w := a.active()
		switch {
		case w.view != n:
			w.view = n
		case n == viewPretty:
			w.kv = !w.kv
		case n == viewRaw:
			w.sub = !w.sub
		}
		w.ed, w.seen, w.anchor = nil, "", -1
		a.info(viewNames[n] + " view · focus " + a.bufUI(a.buf(w.buf)).focus.String())
		return s, nil
	}
}

func (a *App) paneToggle(s core.State, c core.Command) (core.State, []core.Effect) {
	a.pane = 1 - a.pane
	return s, nil
}

func (a *App) paneEnter(s core.State, c core.Command) (core.State, []core.Effect) {
	if w := a.active(); a.pane == 1 && w.view == viewNested {
		return a.nestedMotion(s, core.Command{ID: "motion.right"})
	}
	a.pane = 1
	return s, nil
}

func (a *App) visual(s core.State, c core.Command) (core.State, []core.Effect) {
	w := a.active()
	if a.pane != 1 || w.view != viewNested {
		a.info("v selects rows in the nested view (2, then tab)")
		return s, nil
	}
	if w.anchor >= 0 {
		w.anchor = -1
		return s, nil
	}
	b := a.buf(w.buf)
	rows, _ := a.nestedRows(w, a.display(b))
	w.anchor = a.nestedCursor(w, b, rows)
	a.info("VISUAL · j/k extend · h folds · l unfolds · esc cancels")
	return s, nil
}

// --- selection and pipeline ----------------------------------------------------------

func (a *App) treePath() (Path, bool) {
	rows := a.structRows(a.cur())
	if len(rows) == 0 {
		return nil, false
	}
	return rows[min(a.curUI().cursor, len(rows)-1)].s.Path, true
}

func (a *App) selectToggle(s core.State, c core.Command) (core.State, []core.Effect) {
	p, ok := a.treePath()
	if a.pane != 0 || !ok {
		a.info("space selects fields in the structure tree (tab goes there)")
		return s, nil
	}
	u := a.curUI()
	if i := slices.IndexFunc(u.sel, func(q Path) bool { return q.String() == p.String() }); i >= 0 {
		u.sel = slices.Delete(u.sel, i, i+1)
	} else {
		u.sel = append(u.sel, p)
	}
	a.info(fmt.Sprintf("%d field(s) selected · the data view shows the projection · F flattens · p keeps it", len(u.sel)))
	return s, nil
}

func (a *App) selectClear(s core.State, c core.Command) (core.State, []core.Effect) {
	u := a.curUI()
	switch w := a.active(); {
	case w.anchor >= 0:
		w.anchor = -1
	case len(u.sel) > 0:
		u.sel = nil
		a.info("selection cleared")
	}
	return s, nil
}

// openScope is the path the steps after ops would update in place, nil
// when they apply to the whole document.
func openScope(ops []Operation) Path {
	var scope Path
	for _, op := range ops {
		if op.Kind == "scope" {
			scope = nil
			if op.Mode == "" {
				scope = op.Path
			}
		}
	}
	return scope
}

// scopedSteps are the steps that run op on the values at root only: a
// temporary scope, op, and a scope back to the document, so later steps
// are not narrowed. after is the pipeline the steps follow.
func scopedSteps(after []Operation, root Path, op Operation) []Operation {
	switch {
	case len(root) > 0:
		return []Operation{{Kind: "scope", Path: slices.Clone(root)}, op, {Kind: "scope", Path: Path{}}}
	case len(openScope(after)) > 0:
		return []Operation{{Kind: "scope", Path: Path{}}, op}
	}
	return []Operation{op}
}

// lastStep is the pipeline's last step and the root scopedSteps ran it at.
func lastStep(ops []Operation) (Operation, Path, bool) {
	n := len(ops)
	if n >= 3 && ops[n-1].Kind == "scope" && len(ops[n-1].Path) == 0 && ops[n-3].Kind == "scope" && ops[n-3].Mode == "" && ops[n-2].Kind != "scope" {
		return ops[n-2], ops[n-3].Path, true
	}
	if n >= 1 && ops[n-1].Kind != "scope" && len(openScope(ops[:n-1])) == 0 {
		return ops[n-1], Path{}, true
	}
	return Operation{}, nil, false
}

// applyAt appends op to the current buffer's pipeline, run on the values at
// root only, as one undo step. Shape changes put the focus on root, where
// the new shape is; other steps keep it (sync trims it to what remains).
func (a *App) applyAt(root Path, op Operation) bool {
	b := a.cur()
	u := a.bufUI(b)
	if err := b.ApplySteps(scopedSteps(b.Pipeline, root, op)...); err != nil {
		a.fail(op.Kind + ": " + err.Error())
		return false
	}
	switch op.Kind {
	case "array-object", "object-array", "group", "flatten", "project":
		u.focus = slices.Clone(root)
	}
	a.touch(b)
	a.follow = true
	a.note(op.Kind + " added · u undoes · jq: " + SynthesizeJQ(b.Pipeline))
	return true
}

// visualSteps are the steps the data views show on top of the pipeline:
// the live field selection as a project step and, while a filter is being
// typed (live), that filter at its root. J and y j use the same steps
// (without the prompt), so the jq matches what is on screen.
func (a *App) visualSteps(b *Buffer, live bool) []Operation {
	u := a.bufUI(b)
	var out []Operation
	if len(u.sel) > 0 {
		v := a.preview(b)
		if op, err := ResolveOperation(v, Operation{Kind: "project", Paths: u.sel}); err == nil {
			out = append(out, scopedSteps(b.Pipeline, nil, op)...)
		}
	}
	if p := a.prompt; live && p != nil && b.ID == a.cur().ID {
		if op, ok := a.filterOp(p); ok {
			out = append(out, scopedSteps(append(slices.Clone(b.Pipeline), out...), p.root, op)...)
		}
	}
	return out
}

// visualPipeline is the pipeline plus the visual steps: what y j copies
// and J starts from.
func (a *App) visualPipeline(b *Buffer) []Operation {
	return append(slices.Clone(b.Pipeline), a.visualSteps(b, false)...)
}

func (a *App) project(s core.State, c core.Command) (core.State, []core.Effect) {
	u := a.curUI()
	if len(u.sel) == 0 {
		a.warn("select fields first: space in the structure tree")
		return s, nil
	}
	paths := u.sel
	u.sel = nil
	a.applyAt(nil, Operation{Kind: "project", Paths: paths})
	return s, nil
}

func leafPaths(s *Schema) []Path {
	if len(s.Children) == 0 {
		return []Path{s.Path}
	}
	var out []Path
	for _, c := range s.Children {
		out = append(out, leafPaths(c)...)
	}
	return out
}

// flatten turns the selected fields (or every leaf under the tree cursor)
// into rows at the deepest array. Unrelated sibling arrays ask which one is
// the row grain.
func (a *App) flatten(s core.State, c core.Command) (core.State, []core.Effect) {
	b := a.cur()
	u := a.bufUI(b)
	paths := slices.Clone(u.sel)
	if len(paths) == 0 {
		rows := a.structRows(b)
		if len(rows) == 0 {
			a.warn("nothing to flatten")
			return s, nil
		}
		paths = leafPaths(rows[min(u.cursor, len(rows)-1)].s)
	}
	op := Operation{Kind: "flatten", Paths: paths, Inner: a.inner}
	if sib := SiblingArrays(a.preview(b), paths); len(sib) > 0 {
		var items []picker.Item
		for i, p := range sib {
			items = append(items, picker.Item{ID: strconv.Itoa(i), Title: p.String(), Subtitle: "one row per element of " + p.String()})
		}
		items = append(items, picker.Item{ID: "cross", Title: "cross product", Subtitle: "one row per combination of every array"})
		m := picker.New("Row grain: these arrays are unrelated", items)
		m.Scope = "pick"
		return openModal(s, modalPick, "pick", &pickModal{kind: "grain", m: m, paths: sib, op: op}), nil
	}
	a.doFlatten(op)
	return s, nil
}

func (a *App) doFlatten(op Operation) {
	u := a.curUI()
	u.sel, u.focus = nil, Path{}
	if a.applyAt(nil, op) {
		a.active().view = viewTable
	}
}

func (a *App) innerToggle(s core.State, c core.Command) (core.State, []core.Effect) {
	a.inner = !a.inner
	a.info(map[bool]string{true: "flatten: inner join (parents with empty arrays are dropped)", false: "flatten: left join (empty arrays keep the parent row)"}[a.inner])
	return s, nil
}

// parentScope strips a trailing wildcard array step so scoped operations on
// elements of an array (orders[]) target the array itself (orders), keeping
// synthesized jq clean and matching eachRow semantics.
func parentScope(parent Path) Path {
	if len(parent) > 0 && isWildPart(parent[len(parent)-1]) {
		return slices.Clone(parent[:len(parent)-1])
	}
	return slices.Clone(parent)
}

// fieldCtx names the focused field: the array (or object) holding it and
// its name relative to the elements.
func (a *App) fieldCtx() (Path, string, bool) {
	b := a.cur()
	p := a.curUI().focus
	if a.pane == 0 {
		var ok bool
		if p, ok = a.treePath(); !ok {
			return nil, "", false
		}
	} else if w := a.active(); w.view == viewTable {
		rel, root := a.relation(w, b)
		_, col := a.tablePosFor(w, b, root, rel.Columns)
		if col < 0 {
			return root, "", false
		}
		return root, rel.Columns[col], true
	}
	for k := len(p) - 1; k >= 0; k-- {
		if p[k].Array || p[k].Indexed {
			return p[:k], columnName(p[k+1:]), k+1 < len(p)
		}
	}
	if len(p) == 0 {
		return Path{}, "", false
	}
	return p[:len(p)-1], columnName(p[len(p)-1:]), true
}

// ownerFieldCtx names the immediate owning object (or array of objects) and
// literal key of the focused field, so rename and drop work at any nesting
// depth (e.g. orders[].customer.email -> scope orders[].customer, field email).
func (a *App) ownerFieldCtx() (Path, string, bool) {
	if a.pane == 1 && a.active().view == viewTable {
		return a.fieldCtx()
	}
	var p Path
	if a.pane == 0 {
		var ok bool
		if p, ok = a.treePath(); !ok {
			return nil, "", false
		}
	} else {
		p = a.curUI().focus
	}
	if len(p) == 0 || !isKeyPart(p[len(p)-1]) {
		return nil, "", false
	}
	return parentScope(p[:len(p)-1]), p[len(p)-1].Key, true
}

func (a *App) shape(s core.State, c core.Command) (core.State, []core.Effect) {
	if c.ID == "shape.entries" {
		target := a.curUI().focus
		if a.pane == 0 {
			target, _ = a.treePath()
		}
		if x, _ := AtPath(a.display(a.cur()), target); childCount(x) == 0 || isList(x) {
			if len(target) > 0 {
				target = target[:len(target)-1]
			}
		}
		if x, _ := AtPath(a.display(a.cur()), target); func() bool { _, obj := x.(Object); return !obj }() {
			a.warn("E turns an object into rows: focus an object (" + target.String() + " is not one)")
			return s, nil
		}
		// The key column is named in a prompt, prefilled with "key".
		a.openPrompt("entries", "entries of "+target.String()+" · key column name", "key", target, "")
		return s, nil
	}
	if c.ID == "column.drop" || c.ID == "column.rename" {
		root, field, ok := a.ownerFieldCtx()
		if !ok {
			a.warn("focus a field first (a column in the table, a field in the tree)")
			return s, nil
		}
		if c.ID == "column.drop" {
			a.applyAt(root, Operation{Kind: "drop", Field: field})
		} else {
			a.openPrompt("rename", "rename "+field+" to", field, root, field)
		}
		return s, nil
	}
	root, field, ok := a.fieldCtx()
	if !ok {
		a.warn("focus a field first (a column in the table, a field in the tree)")
		return s, nil
	}
	switch c.ID {
	case "shape.keyby":
		a.applyAt(root, Operation{Kind: "array-object", Field: field})
	case "shape.group":
		a.applyAt(root, Operation{Kind: "group", Field: field, Name: "rows"})
	}
	return s, nil
}

// activeTargets returns the Space-marked schema paths if any, else the single
// focused schema path from either the structure tree or the active data view.
func (a *App) activeTargets() []Path {
	u := a.curUI()
	if len(u.sel) > 0 {
		return slices.Clone(u.sel)
	}
	if a.pane == 0 {
		if p, ok := a.treePath(); ok {
			return []Path{p}
		}
		return nil
	}
	return []Path{wildPath(u.focus)}
}

// shapeHoist (<) lifts focused/marked fields up one level into their parent
// object, or splats an object node's keys directly into its parent object.
func (a *App) shapeHoist(s core.State, c core.Command) (core.State, []core.Effect) {
	b := a.cur()
	u := a.bufUI(b)
	v := a.preview(b)
	sch := InferSchema(v)
	targets := a.activeTargets()
	if len(targets) == 0 {
		a.warn("focus a nested field or object to hoist")
		return s, nil
	}
	type hoistGroup struct {
		scope    Path
		childKey string
		fields   []string
		splat    bool
	}
	var groups []hoistGroup
	byKey := map[string]int{}
	for _, p := range targets {
		if len(p) == 0 || !isKeyPart(p[len(p)-1]) {
			a.warn("hoist needs a field inside a nested object (or an object node to splat)")
			return s, nil
		}
		node := sch.Find(p)
		isObj := node != nil && node.Type == "object"
		if !isObj {
			if val, ok := AtPath(v, p); ok {
				_, isObj = val.(Object)
			}
		}
		if isObj {
			parent := p[:len(p)-1]
			pn := sch.Find(parent)
			if pn == nil || (pn.Type != "object" && pn.Types["object"] == 0) {
				a.warn("cannot splat " + p.String() + ": parent is not an object")
				return s, nil
			}
			sc := parentScope(parent)
			ck := p[len(p)-1].Key
			gk := sc.String() + "\x00" + ck + "\x00splat"
			if _, dup := byKey[gk]; !dup {
				byKey[gk] = len(groups)
				groups = append(groups, hoistGroup{scope: sc, childKey: ck, splat: true})
			}
			continue
		}
		if len(p) < 2 || !isKeyPart(p[len(p)-2]) {
			a.warn("cannot hoist " + p.String() + ": already at top level of its parent object")
			return s, nil
		}
		grand := p[:len(p)-2]
		gn := sch.Find(grand)
		if gn == nil || (gn.Type != "object" && gn.Types["object"] == 0) {
			a.warn("cannot hoist " + p.String() + ": grandparent is not an object")
			return s, nil
		}
		sc := parentScope(grand)
		ck := p[len(p)-2].Key
		fk := p[len(p)-1].Key
		gk := sc.String() + "\x00" + ck
		if idx, exists := byKey[gk]; exists {
			if !slices.Contains(groups[idx].fields, fk) {
				groups[idx].fields = append(groups[idx].fields, fk)
			}
		} else {
			byKey[gk] = len(groups)
			groups = append(groups, hoistGroup{scope: sc, childKey: ck, fields: []string{fk}})
		}
	}
	var steps []Operation
	sim := slices.Clone(b.Pipeline)
	for _, g := range groups {
		op := Operation{Kind: "hoist", Field: g.childKey, Fields: g.fields}
		st := scopedSteps(sim, g.scope, op)
		steps = append(steps, st...)
		sim = append(sim, st...)
	}
	if err := b.ApplySteps(steps...); err != nil {
		a.fail("hoist: " + err.Error())
		return s, nil
	}
	u.sel = nil
	a.touch(b)
	a.follow = true
	a.note("hoist added · u undoes · jq: " + SynthesizeJQ(b.Pipeline))
	return s, nil
}

// shapeNest (>) opens a picker listing existing sibling object keys at the
// same parent level plus "New nested object..." to move focused/marked fields
// into an existing or brand-new child object.
func (a *App) shapeNest(s core.State, c core.Command) (core.State, []core.Effect) {
	b := a.cur()
	v := a.preview(b)
	sch := InferSchema(v)
	targets := a.activeTargets()
	if len(targets) == 0 {
		a.warn("focus or mark fields to nest")
		return s, nil
	}
	var parent Path
	var fields []string
	for i, p := range targets {
		if len(p) == 0 || !isKeyPart(p[len(p)-1]) {
			a.warn("nest needs object field(s) (focus a field in the tree or table)")
			return s, nil
		}
		par := p[:len(p)-1]
		if i == 0 {
			parent = par
		} else if par.String() != parent.String() {
			a.warn("select sibling fields at the same level to nest together")
			return s, nil
		}
		k := p[len(p)-1].Key
		if !slices.Contains(fields, k) {
			fields = append(fields, k)
		}
	}
	pn := sch.Find(parent)
	if pn == nil || (pn.Type != "object" && pn.Types["object"] == 0) {
		a.warn("nest needs fields inside an object")
		return s, nil
	}
	var items []picker.Item
	for _, sib := range pn.Children {
		if (sib.Type == "object" || sib.Types["object"] > 0) && !slices.Contains(fields, sib.Name) {
			items = append(items, picker.Item{
				ID:       "into:" + sib.Name,
				Title:    sib.Name,
				Subtitle: "move " + strings.Join(fields, ", ") + " into existing object " + sib.Name + " {...}",
			})
		}
	}
	items = append(items, picker.Item{
		ID:       "new",
		Title:    "New nested object...",
		Subtitle: "wrap " + strings.Join(fields, ", ") + " under a new key",
	})
	m := picker.New("Nest "+strings.Join(fields, ", ")+" into", items)
	m.Scope, m.Placeholder = "pick", "pick sibling object or type a new key name"
	return openModal(s, modalPick, "pick", &pickModal{kind: "nest", m: m, target: parentScope(parent), fields: fields}), nil
}

// shapeTransfer (M) copies the focused field, Space-marked fields, or focused
// subtree into the other split window's buffer (opening a right-hand scratch
// split if only one window is open), with full u undo in the target buffer.
func (a *App) shapeTransfer(s core.State, c core.Command) (core.State, []core.Effect) {
	srcBuf := a.cur()
	srcUI := a.bufUI(srcBuf)
	srcVal := a.display(srcBuf)
	if srcVal == nil {
		a.warn("nothing to transfer: buffer is empty")
		return s, nil
	}

	var transferred []Field
	var unnamed any
	hasUnnamed := false
	var summary string

	if len(srcUI.sel) > 0 {
		cols := columnNames(srcUI.sel)
		sch := InferSchema(srcVal)
		for i, p := range srcUI.sel {
			var val any
			if hasWild(p) {
				ep := p
				if n := sch.Find(p); n != nil && (n.Type == "array" || n.Types["array"] > 0) {
					ep = p.Child(wildPart)
				}
				val = selectPath(srcVal, ep)
			} else {
				val, _ = AtPath(srcVal, p)
			}
			transferred = append(transferred, Field{Key: cols[i], Value: val})
		}
		summary = fmt.Sprintf("%d field(s) (%s)", len(transferred), strings.Join(cols, ", "))
		srcUI.sel = nil
	} else {
		var p Path
		if a.pane == 0 {
			p, _ = a.treePath()
		} else {
			p = srcUI.focus
		}
		sch := InferSchema(srcVal)
		var val any
		if hasWild(p) {
			ep := p
			if n := sch.Find(p); n != nil && (n.Type == "array" || n.Types["array"] > 0) {
				ep = p.Child(wildPart)
			}
			val = selectPath(srcVal, ep)
		} else {
			val, _ = AtPath(srcVal, p)
		}
		key := ""
		for k := len(p) - 1; k >= 0; k-- {
			if isKeyPart(p[k]) {
				key = p[k].Key
				break
			}
		}
		if key != "" && (len(p) == 0 || !p[len(p)-1].Indexed) {
			transferred = []Field{{Key: key, Value: val}}
			summary = key
		} else {
			unnamed, hasUnnamed = val, true
			summary = p.String()
		}
	}

	// Ensure a second window with a distinct target buffer exists.
	var dstWin *window
	var dstBuf *Buffer
	if len(a.wins) < 2 {
		dstBuf = NewScratch()
		dstBuf.Name = "reshaped.json"
		a.ring.Add(dstBuf)
		a.ring.Switch(srcBuf.ID)
		dstWin = newWindow(dstBuf.ID, viewPretty)
		a.wins = append(a.wins, dstWin)
	} else {
		dstWin = a.wins[1-a.win]
		if dstWin.buf == srcBuf.ID {
			dstBuf = NewScratch()
			dstBuf.Name = "reshaped.json"
			a.ring.Add(dstBuf)
			a.ring.Switch(srcBuf.ID)
			dstWin.buf = dstBuf.ID
		} else {
			dstBuf = a.buf(dstWin.buf)
		}
	}

	dstVal := a.preview(dstBuf)
	var merged any
	switch cur := dstVal.(type) {
	case nil:
		if hasUnnamed {
			merged = unnamed
		} else {
			merged = Object(transferred)
		}
	case Object:
		out := append(Object{}, cur...)
		if hasUnnamed {
			if uo, ok := unnamed.(Object); ok {
				for _, f := range uo {
					out = out.set(f.Key, f.Value)
				}
			} else {
				out = out.set("value", unnamed)
			}
		} else {
			for _, f := range transferred {
				out = out.set(f.Key, f.Value)
			}
		}
		merged = out
	case []any:
		out := append([]any{}, cur...)
		if hasUnnamed {
			out = append(out, unnamed)
		} else {
			out = append(out, Object(transferred))
		}
		merged = out
	default:
		out := Object{{"value", cur}}
		if hasUnnamed {
			out = out.set("transferred", unnamed)
		} else {
			for _, f := range transferred {
				out = out.set(f.Key, f.Value)
			}
		}
		merged = out
	}

	formatted, err := FormatJSON(merged, true)
	if err != nil {
		a.fail("transfer: " + err.Error())
		return s, nil
	}
	dstBuf.BreakUndo()
	if len(dstBuf.Pipeline) > 0 {
		dstBuf.push()
		dstBuf.Redo = nil
		dstBuf.Pipeline = nil
		dstBuf.parse(string(formatted))
	} else {
		dstBuf.SetRaw(string(formatted))
	}
	dstBuf.BreakUndo()
	dstWin.ed, dstWin.relKey, dstWin.seen = nil, "", ""
	a.touch(dstBuf)
	a.note(fmt.Sprintf("transferred %s to %s · ctrl+w w switches · u undoes", summary, bufName(dstBuf)))
	return s, nil
}

func (a *App) shapeConvert(s core.State, c core.Command) (core.State, []core.Effect) {
	target := a.curUI().focus
	if a.pane == 0 {
		target, _ = a.treePath()
	}
	root, field, hasField := a.fieldCtx()
	val, ok := AtPath(a.display(a.cur()), target)
	if !ok {
		target = nil
		val = a.display(a.cur())
	}
	var items []picker.Item
	switch val.(type) {
	case []any:
		if hasField && root.String() == target.String() {
			items = append(items, picker.Item{ID: "keyby", Title: "Key array by " + field, Subtitle: "Array of objects -> object keyed by ." + field})
		} else if hasField {
			items = append(items, picker.Item{ID: "keyby", Title: "Key parent array by " + field, Subtitle: "Array at " + root.String() + " -> object keyed by ." + field})
		}
		items = append(items,
			picker.Item{ID: "wrap-object", Title: "Wrap under object key", Subtitle: `{ "<key>": [...] }`},
			picker.Item{ID: "wrap-array", Title: "Wrap in array", Subtitle: `[ [...] ]`},
		)
	case Object:
		items = append(items,
			picker.Item{ID: "entries", Title: "Entries array", Subtitle: `[{"key": k, ...}] · keep keys as a column`},
			picker.Item{ID: "object-values", Title: "Values array", Subtitle: `[v1, v2, ...] · drop keys`},
			picker.Item{ID: "object-keys", Title: "Keys array", Subtitle: `["k1", "k2", ...] · keys only`},
			picker.Item{ID: "wrap-object", Title: "Wrap under object key", Subtitle: `{ "<key>": {...} }`},
			picker.Item{ID: "wrap-array", Title: "Wrap in array", Subtitle: `[ {...} ]`},
		)
	default:
		if hasField {
			items = append(items, picker.Item{ID: "keyby", Title: "Key array by " + field, Subtitle: "Array at " + root.String() + " -> object keyed by ." + field})
		}
		items = append(items,
			picker.Item{ID: "wrap-object", Title: "Wrap under object key", Subtitle: `{ "<key>": value }`},
			picker.Item{ID: "wrap-array", Title: "Wrap in array", Subtitle: `[ value ]`},
		)
	}
	m := picker.New("Convert "+target.String(), items)
	m.Scope, m.Placeholder = "pick", "choose shape conversion"
	pm := &pickModal{kind: "convert", m: m, target: target}
	if hasField {
		pm.field = field
		pm.paths = []Path{root}
	}
	return openModal(s, modalPick, "pick", pm), nil
}

func isList(v any) bool { _, ok := v.([]any); return ok }

func (a *App) sortCycle(s core.State, c core.Command) (core.State, []core.Effect) {
	root, field, ok := a.fieldCtx()
	if !ok {
		a.warn("focus a column to sort by")
		return s, nil
	}
	b := a.cur()
	// Pressing s again on the column the last step sorted (at the same
	// root) takes that step back: ascending turns descending, then off.
	if last, at, ok := lastStep(b.Pipeline); ok && last.Kind == "sort" && last.Field == field && at.String() == root.String() && len(b.Undo) > 0 && b.Undo[len(b.Undo)-1].Raw == b.RawText {
		b.UndoStep()
		a.touch(b)
		if last.Desc {
			a.info("sort by " + field + " removed")
			return s, nil
		}
		a.applyAt(root, Operation{Kind: "sort", Field: field, Desc: true})
		return s, nil
	}
	a.applyAt(root, Operation{Kind: "sort", Field: field})
	return s, nil
}

func (a *App) history(s core.State, c core.Command) (core.State, []core.Effect) {
	b := a.cur()
	n := 0
	for range c.Times() {
		if (c.ID == "history.undo" && !b.UndoStep()) || (c.ID == "history.redo" && !b.RedoStep()) {
			break
		}
		n++
	}
	a.touch(b)
	a.follow = true
	if n == 0 {
		a.info("nothing to " + strings.TrimPrefix(c.ID, "history."))
	} else {
		a.note(fmt.Sprintf("%s %d step(s) · pipeline has %d", strings.TrimPrefix(c.ID, "history."), n, len(b.Pipeline)))
	}
	return s, nil
}

// --- prompts: filter, rename, jq, save path -----------------------------------------------

var matchModes = []string{"substring", "fuzzy", "regex"}

func (a *App) openPrompt(kind, label, text string, root Path, field string) {
	ed := input.New(text, false)
	ed.HandleKey("A")
	a.prompt = &prompt{kind: kind, label: label, ed: ed, root: root, field: field}
}

func (a *App) filterOpen(s core.State, c core.Command) (core.State, []core.Effect) {
	if c.ID == "filter.all" {
		b := a.cur()
		v := a.display(b)
		var root Path
		if a.pane == 0 {
			if tp, ok := a.treePath(); ok {
				if ar, _, deep := deepArrayFor(InferSchema(v), tp); deep {
					root = ar
				} else if n := InferSchema(v).Find(tp); n != nil && (n.Type == "array" || n.Types["array"] > 0) {
					root = slices.Clone(tp)
				} else {
					for k := len(tp) - 1; k >= 0; k-- {
						if isWildPart(tp[k]) {
							root = slices.Clone(tp[:k])
							break
						}
					}
				}
			}
		} else if w := a.active(); w.view == viewTable {
			_, root = a.relation(w, b)
		}
		if root == nil {
			root = tableRoot(v, a.curUI().focus)
		}
		a.openPrompt("filter", "/ ["+a.match+"]", "", root, "")
		return s, nil
	}
	root, field, ok := a.fieldCtx()
	if !ok {
		a.warn("focus a column to filter by")
		return s, nil
	}
	a.openPrompt("field", "f "+field+" (==, !=, >, <, ~re, or contains)", "", root, field)
	return s, nil
}

// filterOp reads the prompt as a filter step; ok is false for other prompts
// and an empty query.
func (a *App) filterOp(p *prompt) (Operation, bool) {
	q := p.ed.Value()
	switch p.kind {
	case "filter":
		return Operation{Kind: "filter", Mode: a.match, Query: q}, q != ""
	case "field":
		mode := "contains"
		for _, pre := range [...][2]string{{"==", "eq"}, {"!=", "ne"}, {">", "gt"}, {"<", "lt"}, {"~", "regex"}, {"=", "eq"}} {
			if rest, ok := strings.CutPrefix(q, pre[0]); ok {
				mode, q = pre[1], strings.TrimSpace(rest)
				break
			}
		}
		return Operation{Kind: "filter", Field: p.field, Mode: mode, Query: q}, q != ""
	}
	return Operation{}, false
}

func (a *App) modeCycle(s core.State, c core.Command) (core.State, []core.Effect) {
	a.match = matchModes[(slices.Index(matchModes, a.match)+1)%len(matchModes)]
	if p := a.prompt; p != nil && p.kind == "filter" {
		p.label = "/ [" + a.match + "]"
	}
	a.info("match mode: " + a.match)
	return s, nil
}

func (a *App) promptKey(s core.State, c core.Command) (core.State, []core.Effect) {
	p := a.prompt
	if p == nil {
		return s, nil
	}
	before := p.ed.Value()
	p.ed.HandleInput(c.Arg("key"), c.Arg("text"))
	if p.kind == "jq" && p.ed.Value() != before {
		return s, []core.Effect{a.jqEffect(p.ed.Value())}
	}
	return s, nil
}

func (a *App) promptCancel(s core.State, c core.Command) (core.State, []core.Effect) {
	p := a.prompt
	a.prompt = nil
	running := a.jq.running
	a.jq.ok, a.jq.ready, a.jq.running, a.jq.err = false, false, false, ""
	if p != nil && p.kind == "jq" && running {
		// A no-op run in the jq slot makes the shell cancel the running
		// query (its result is dropped); this one dispatches nothing.
		a.info("jq query cancelled")
		return s, []core.Effect{core.AsyncEffect{Slot: "jq", Run: func(context.Context) (any, error) { return nil, nil }}}
	}
	return s, nil
}

func (a *App) promptAccept(s core.State, c core.Command) (core.State, []core.Effect) {
	p := a.prompt
	if p == nil {
		return s, nil
	}
	a.prompt = nil
	text := strings.TrimSpace(p.ed.Value())
	switch p.kind {
	case "filter", "field":
		if op, ok := a.filterOp(p); ok {
			a.applyAt(p.root, op)
		}
	case "rename":
		if text != "" && text != p.field {
			a.applyAt(p.root, Operation{Kind: "rename", Field: p.field, Name: text})
		}
	case "entries":
		if text == "" {
			text = "key"
		}
		if !a.applyAt(p.root, Operation{Kind: "object-array", Name: text}) {
			a.prompt = p // keep the form open to pick another name
		}
	case "wrap-object":
		if text == "" {
			text = "value"
		}
		a.applyAt(p.root, Operation{Kind: "wrap-object", Name: text})
	case "nest":
		if text == "" {
			text = "group"
		}
		a.curUI().sel = nil
		a.applyAt(p.root, Operation{Kind: "nest", Name: text, Fields: p.fields})
	case "saveas":
		return a.write(s, core.Command{Args: map[string]any{"path": text, "format": a.saveFormat}})
	case "reader-tool-filter":
		if a.reader != nil {
			a.readerState.Patterns[p.field] = text
			w := a.active()
			w.readerIdx, w.readerOff = 0, 0
			a.syncReaderFocus(w, a.buf(w.buf), a.readerEntries(a.buf(w.buf)))
			if text == "" {
				a.info("tool filter cleared")
			} else {
				a.info("tool filter: /" + text + "/")
			}
		}
	case "recipe-save":
		if text != "" {
			return a.recipeSave(s, core.Command{Args: map[string]any{"path": text}})
		}
	case "jq":
		if !a.jq.ready || !a.jq.ok {
			a.prompt = p
			switch {
			case a.jq.running:
				a.info("jq is still running · enter again when it finishes · esc cancels")
			case a.jq.err != "":
				a.warn("the jq query has no result: " + a.jq.err)
			default:
				a.warn("the jq query has no result yet")
			}
			return s, nil
		}
		out, err := FormatJSON(a.jq.val, true)
		if err != nil {
			a.fail("jq result: " + err.Error())
			return s, nil
		}
		nb := NewScratch()
		nb.SetRaw(string(out))
		nb.Name = "jq: " + clipText(text, 30)
		a.ring.Add(nb)
		a.attach(nb)
		a.jq.ok = false
		a.note("opened the jq result in buffer " + strconv.Itoa(nb.ID) + " · ctrl+6 goes back")
	}
	return s, nil
}

// jqChips are the interactive builder stages the jq bar can append via shortcut
// or the Tab/ctrl+b stage picker. %[1]s is the focused field's jq path, %[2]s
// its name as a jq string.
type jqChipDef struct {
	key, name, short, label, desc, tmpl string
}

var jqChips = []jqChipDef{
	{"alt+f", "filter", "filter", "filter", "Keep elements where the focused field is non-null", " | map(select(%[1]s != null))"},
	{"alt+s", "select", "select", "project/select", "Keep only the focused or marked fields on each element", " | map({%[2]s: %[1]s})"},
	{"alt+l", "flatten", "flatten", "flatten", "Extract and flatten the focused nested array across elements", " | [.[] | %[1]s] | flatten"},
	{"alt+o", "sort_by", "sort_by", "sort_by", "Sort elements ascending by the focused field", " | sort_by(%[1]s)"},
	{"alt+g", "group_by", "group_by", "group_by", "Group elements sharing the same focused field value", " | group_by(%[1]s)"},
	{"alt+k", "keys", "keys", "keys", "List the object keys of the current result", " | keys"},
	{"alt+u", "unique_by", "unique_by", "unique_by", "Keep one element per distinct value of the focused field", " | unique_by(%[1]s)"},
	{"alt+c", "count", "count", "count/length", "Count the elements or keys in the current result", " | length"},
}

// jqStageSnippet builds the jq snippet for stage ch using the focused field
// (or all Space-marked fields for project/select).
func (a *App) jqStageSnippet(ch jqChipDef) string {
	switch ch.name {
	case "keys", "count":
		return ch.tmpl
	case "select":
		u := a.curUI()
		if len(u.sel) > 0 {
			cols := columnNames(u.sel)
			parts := make([]string, len(u.sel))
			for i, p := range u.sel {
				rel := p
				for k := len(p) - 1; k >= 0; k-- {
					if isWildPart(p[k]) {
						rel = p[k+1:]
						break
					}
				}
				if len(rel) == 0 {
					parts[i] = jqKey(cols[i]) + ": ."
				} else {
					parts[i] = jqKey(cols[i]) + ": " + jqPath(rel)
				}
			}
			return " | map({" + strings.Join(parts, ", ") + "})"
		}
	}
	field, jp := a.chipField()
	return fmt.Sprintf(ch.tmpl, jp, jqStr(field))
}

// chipPath is the jq path of a field fieldCtx named: a table column is one
// literal key (flattened names hold dots), any other field a nested path.
func (a *App) chipPath(field string) string {
	if a.pane == 1 && a.active().view == viewTable {
		return jqPath(Path{pathKey(field)})
	}
	if p, err := ParsePath(field); err == nil && len(p) > 0 {
		return jqPath(p)
	}
	return jqPath(Path{pathKey(field)})
}

func (a *App) jqOpen(s core.State, c core.Command) (core.State, []core.Effect) {
	// The bar starts from what the data view shows: the pipeline plus a
	// field selection not yet kept with p.
	expr := SynthesizeJQ(a.visualPipeline(a.cur()))
	if expr == "" {
		expr = "."
	}
	a.openPrompt("jq", "jq", expr, nil, "")
	a.jq.ok, a.jq.err = false, ""
	return s, []core.Effect{a.jqEffect(expr)}
}

// jqInput is the text jq reads: the buffer text, or the last valid value
// when the text is JSONL or does not parse (the same input the pipeline
// and its synthesized jq see).
func jqInput(b *Buffer) string {
	if b.ParseErr == nil && !b.JSONL {
		return b.RawText
	}
	out, err := FormatJSON(b.Value, false)
	if err != nil {
		return b.RawText
	}
	return string(out)
}

// jqEffect starts jq in slot "jq"; a newer query cancels the running one.
// Until its result arrives the bar is not ready, so enter never takes the
// result of an older query; the last result stays on screen meanwhile.
func (a *App) jqEffect(expr string) core.Effect {
	a.jq.ready, a.jq.running, a.jq.expr = false, true, expr
	a.info("jq running · " + clipText(singleLine(expr), 60))
	e := JQEffect("jq", jqInput(a.cur()), expr)
	if e.Command == "" {
		e.Command = JQResultCommand
	}
	return e
}

func (a *App) jqChip(s core.State, c core.Command) (core.State, []core.Effect) {
	p := a.prompt
	if p == nil || p.kind != "jq" {
		return s, nil
	}
	for _, ch := range jqChips {
		if ch.name == c.Arg("chip") {
			p.ed = input.New(p.ed.Value()+a.jqStageSnippet(ch), false)
			p.ed.HandleKey("A")
			break
		}
	}
	return s, []core.Effect{a.jqEffect(p.ed.Value())}
}

func (a *App) jqBuilder(s core.State, c core.Command) (core.State, []core.Effect) {
	p := a.prompt
	if p == nil || p.kind != "jq" {
		return s, nil
	}
	var items []picker.Item
	for _, ch := range jqChips {
		snip := a.jqStageSnippet(ch)
		items = append(items, picker.Item{
			ID:       ch.name,
			Title:    fmt.Sprintf("%s (%s)  %s", ch.label, ch.key, strings.TrimSpace(snip)),
			Subtitle: ch.desc,
			Keywords: []string{ch.name, ch.label, ch.key, ch.desc, snip},
		})
	}
	m := picker.New("jq Builder · append stage", items)
	m.Scope, m.Placeholder = "pick", "filter stages by name or jq snippet"
	return openModal(s, modalPick, "pick", &pickModal{kind: "jqbuilder", m: m}), nil
}

// chipField is the focused field's name and jq path, read with the prompt
// closed (fieldCtx looks at the pane and view), "id" when none is focused.
func (a *App) chipField() (string, string) {
	p := a.prompt
	a.prompt = nil
	defer func() { a.prompt = p }()
	_, field, _ := a.fieldCtx()
	if field == "" {
		field = "id"
	}
	return field, a.chipPath(field)
}

func (a *App) jqResult(s core.State, c core.Command) (core.State, []core.Effect) {
	p := a.prompt
	if p == nil || p.kind != "jq" {
		return s, nil
	}
	v, err := c.Args["value"], c.Args["error"]
	expr := a.jq.expr
	switch r := v.(type) {
	case JQResult:
		v, err, expr = r.Value, errOr(r.Err, err), r.Expression
	case *JQResult:
		if r != nil {
			v, err, expr = r.Value, errOr(r.Err, err), r.Expression
		}
	}
	if expr != a.jq.expr {
		return s, nil // an older query's result
	}
	a.jq.seq++
	a.jq.running = false
	if e, _ := err.(error); e != nil {
		a.jq.ok, a.jq.err = false, singleLine(e.Error())
		a.warn(a.jq.err)
		return s, nil
	}
	a.jq.ok, a.jq.ready, a.jq.val, a.jq.err = true, true, v, ""
	a.info("jq result" + nodeSummary(v) + " · enter opens it as a buffer · esc closes")
	return s, nil
}

func errOr(e error, other any) any {
	if e != nil {
		return e
	}
	return other
}

// --- clipboard -------------------------------------------------------------------------------

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (a *App) yank(s core.State, c core.Command) (core.State, []core.Effect) {
	b := a.cur()
	u := a.bufUI(b)
	var text, what string
	switch c.ID {
	case "yank.jq":
		expr := SynthesizeJQ(a.visualPipeline(b))
		if expr == "" {
			expr = "."
		}
		cmd := "jq "
		if b.JSONL {
			cmd = "jq -s " // the pipeline sees JSONL as one array
		}
		text, what = cmd+shQuote(expr), "jq command"
		if b.Path != "" {
			text += " " + shQuote(b.Path)
		}
	case "yank.path":
		text, what = u.focus.String(), "path"
	default:
		x, _ := AtPath(a.display(b), u.focus)
		out, err := FormatJSON(x, true)
		if err != nil {
			a.fail(err.Error())
			return s, nil
		}
		text, what = strings.TrimRight(string(out), "\n"), "value at "+u.focus.String()
	}
	a.register = text
	if a.copy == nil {
		a.info("yanked the " + what + " to the register (no clipboard tool): " + clipText(singleLine(text), 60))
		return s, nil
	}
	a.info("yanked the " + what + ": " + clipText(singleLine(text), 60))
	cp := a.copy
	return s, []core.Effect{core.AsyncEffect{Slot: "clipboard", Command: "clipboard.done", Run: func(context.Context) (any, error) { return nil, cp(text) }}}
}

func (a *App) clipDone(s core.State, c core.Command) (core.State, []core.Effect) {
	if err, _ := c.Args["error"].(error); err != nil {
		a.warn("clipboard: " + err.Error() + " (kept in the register)")
	}
	return s, nil
}

// --- edit, save, open ----------------------------------------------------------------------

func (a *App) editToggle(s core.State, c core.Command) (core.State, []core.Effect) {
	b := a.cur()
	b.Editable = !b.Editable
	if b.Editable {
		a.note("EDIT · raw view (4) edits the text · ctrl+s saves")
	} else {
		a.info("READ-ONLY · unsaved edits are kept")
	}
	return s, nil
}

// save is ctrl+s and W. ctrl+s writes the buffer where it came from (a
// folder and name are asked when it has none); W asks for a format first,
// then a folder and a file name.
func (a *App) save(s core.State, c core.Command) (core.State, []core.Effect) {
	b := a.cur()
	if c.ID == "file.saveas" {
		return a.formatPick(s)
	}
	a.saveFormat = ""
	if b.Path != "" {
		return a.write(s, core.Command{Args: map[string]any{"path": b.Path}})
	}
	return a.saveDir(s)
}

func (a *App) saveDir(s core.State) (core.State, []core.Effect) {
	b := a.cur()
	dir := a.cwd
	if b.Path != "" {
		dir = filepath.Dir(b.Path)
	}
	a.filesFor = "save"
	return a.openFiles(s, dir, pathpicker.PickDir)
}

// tableShown is the relation the active window shows for the current
// buffer, when it is a table.
func (a *App) tableShown() (Relation, Path, bool) {
	w := a.active()
	b := a.cur()
	if w.view != viewTable || w.buf != b.ID {
		return Relation{}, nil, false
	}
	rel, root := a.relation(w, b)
	return rel, root, true
}

// formatPick offers every save format; the pick goes on to the folder.
func (a *App) formatPick(s core.State) (core.State, []core.Effect) {
	rows := "the rows of the result (1 shows the focused table)"
	rel, root, table := a.tableShown()
	if table {
		rows = fmt.Sprintf("the table as shown: %s, %d rows × %d columns", root.String(), len(rel.Rows), len(rel.Columns))
	}
	subs := map[string]string{
		"pretty":   "indented JSON (.json)",
		"minified": "one-line JSON (.json)",
		"jsonl":    "one JSON value per line (.jsonl)",
		"csv":      "comma-separated, " + rows,
		"tsv":      "tab-separated, " + rows,
		"yaml":     "YAML document (.yaml)",
		"toml":     "TOML document (.toml)",
		"md":       "Markdown table or timeline (.md)",
	}
	var items []picker.Item
	for _, f := range SaveFormats {
		items = append(items, picker.Item{ID: f, Title: f, Subtitle: subs[f]})
	}
	m := picker.New("Save as", items)
	m.Scope = "pick"
	if table {
		m.SelectedIdx = max(slices.Index(SaveFormats, "csv"), 0)
	}
	return openModal(s, modalPick, "pick", &pickModal{kind: "format", m: m}), nil
}

// formatExt is the file extension a save format writes.
func formatExt(format string) string {
	switch format {
	case "jsonl", "csv", "tsv", "yaml", "toml", "md":
		return "." + format
	}
	return ".json"
}

// write saves the current buffer to path. The format is the "format" arg
// (W's pick), else what the extension implies. CSV and TSV from a table
// view write exactly the table on screen; everything else goes through
// Buffer.Save (the text as typed, or the pipeline's result).
func (a *App) write(s core.State, c core.Command) (core.State, []core.Effect) {
	b := a.cur()
	path, format := c.Arg("path"), c.Arg("format")
	a.saveFormat = ""
	if path == "" {
		path = b.Path
	}
	if path == "" {
		return a.saveDir(s)
	}
	if !b.Editable && b.Path != "" && samePath(path, b.Path) {
		a.warn("READ-ONLY: ctrl+e enables saving over " + b.Name + " · W saves a copy")
		return s, nil
	}
	if format == "" {
		format = FormatForPath(path)
	}
	var err error
	what := ""
	if rel, root, ok := a.tableShown(); ok && (format == "csv" || format == "tsv") {
		err = b.ExportRelation(path, format, rel)
		what = fmt.Sprintf(" · the table at %s, %d rows", root.String(), len(rel.Rows))
	} else if format == "md" && a.active().view == viewReader && a.reader != nil {
		entries := a.readerEntries(b)
		var buf bytes.Buffer
		err = ExportMarkdownTimeline(&buf, entries, &a.reader.Timeline)
		if err == nil {
			err = atomicWrite(path, buf.Bytes())
		}
		what = fmt.Sprintf(" · %s timeline, %d items", a.reader.Name, len(entries))
	} else {
		err = b.Save(path, format)
	}
	if err != nil {
		a.fail("not saved: " + err.Error())
		return s, nil
	}
	if ext := strings.ToLower(filepath.Ext(path)); b.Path == "" && (ext == ".json" || ext == ".jsonl") {
		b.Path, b.Name, b.Dirty = path, filepath.Base(path), false
	}
	a.note("saved " + path + what)
	return s, nil
}

func (a *App) openFiles(s core.State, dir string, mode pathpicker.PickMode) (core.State, []core.Effect) {
	m, err := pathpicker.New(a.fs, dir, mode)
	if err != nil && dir != a.cwd {
		m, err = pathpicker.New(a.fs, a.cwd, mode)
	}
	if err != nil {
		a.fail("cannot browse " + dir + ": " + err.Error())
		return s, nil
	}
	m.MatchMode = picker.MatchMode(a.match)
	return openModal(s, modalFiles, "files", m), nil
}

func (a *App) filesOpen(s core.State, c core.Command) (core.State, []core.Effect) {
	dir := a.cwd
	if p := a.cur().Path; p != "" {
		dir = filepath.Dir(p)
	}
	a.filesFor = "open"
	return a.openFiles(s, dir, pathpicker.PickFile)
}

func (a *App) filesCmd(s core.State, c core.Command) (core.State, []core.Effect) {
	m, ok := modalData(s, modalFiles).(*pathpicker.Model)
	if !ok {
		return s, nil
	}
	var picked *pathpicker.DirEntry
	switch c.ID {
	case "files.insert":
		m.AppendQuery(c.Arg("text"))
	case "files.backspace":
		if m.Query == "" {
			m.Parent()
		} else {
			m.BackspaceQuery()
		}
	case "files.close":
		return closeModal(s, modalFiles), nil
	default:
		m.List().PageSize = max(a.h*80/100-6, 1)
		picked, _ = m.HandleCommand(c)
	}
	if m.Err != "" {
		a.fail(m.Err)
	}
	if picked == nil {
		return s, nil
	}
	s = closeModal(s, modalFiles)
	if a.filesFor == "open" {
		return a.load(s, core.Command{Args: map[string]any{"path": picked.Path}})
	}
	b := a.cur()
	stem := "untitled"
	if b.Path != "" {
		stem = strings.TrimSuffix(filepath.Base(b.Path), filepath.Ext(b.Path)) + ".copy"
	}
	ext, label := formatExt(a.saveFormat), "save as "+a.saveFormat
	switch {
	case a.saveFormat == "" && a.active().view == viewTable:
		ext, label = ".csv", "save as (.json .jsonl .csv .tsv)"
	case a.saveFormat == "":
		label = "save as (.json .jsonl .csv .tsv)"
	}
	a.openPrompt("saveas", label, filepath.Join(picked.Path, stem+ext), nil, "")
	return s, nil
}

// load opens path into a new read-only buffer.
func (a *App) load(s core.State, c core.Command) (core.State, []core.Effect) {
	path := c.Arg("path")
	if path == "" {
		return a.filesOpen(s, c)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		a.fail("cannot open " + path + ": " + err.Error())
		return s, nil
	}
	nb, err := NewBuffer(path, string(raw), false)
	if nb == nil {
		a.fail(fmt.Sprintf("cannot open %s: %v", path, err))
		return s, nil
	}
	a.ring.Add(nb)
	a.attach(nb)
	if w := a.active(); w.view == viewRaw {
		w.view = viewNested
	}
	a.pane = 0
	a.note("opened " + nb.Name + " READ-ONLY · ctrl+e edits · ctrl+6 goes back")
	return s, nil
}

// --- buffers and windows ----------------------------------------------------------------

// attach shows the ring's current buffer b in the active window.
func (a *App) attach(b *Buffer) {
	w := a.active()
	w.buf, w.ed, w.seen, w.anchor = b.ID, nil, "", -1
}

func (a *App) bufferCmd(s core.State, c core.Command) (core.State, []core.Effect) {
	ok := true
	switch c.ID {
	case "buffer.new":
		nb := NewScratch()
		a.ring.Add(nb)
		a.attach(nb)
		a.startScratch(nb)
		a.note("new scratch buffer · type JSON · ctrl+s asks for a path")
		return s, nil
	case "buffer.next":
		ok = a.ring.Cycle(c.Times())
	case "buffer.prev":
		ok = a.ring.Cycle(-c.Times())
	case "buffer.alt":
		ok = a.ring.Alternate()
	case "buffer.switch":
		id, _ := strconv.Atoi(fmt.Sprint(c.Args["id"]))
		ok = a.ring.Switch(id)
	}
	if !ok {
		a.info("no other buffer")
		return s, nil
	}
	a.attach(a.cur())
	a.info(fmt.Sprintf("buffer %d of %d · %s", a.bufPos(a.cur()), len(a.ring.List()), bufName(a.cur())))
	return s, nil
}

func (a *App) bufferPick(s core.State, c core.Command) (core.State, []core.Effect) {
	var items []picker.Item
	for _, b := range a.ring.List() {
		flags := ""
		if b.Dirty {
			flags += " [+]"
		}
		if !b.Editable {
			flags += " read-only"
		}
		items = append(items, picker.Item{ID: strconv.Itoa(b.ID), Title: fmt.Sprintf("%d %s%s", b.ID, bufName(b), flags), Subtitle: b.Path})
	}
	m := picker.New("Buffers", items)
	m.Scope = "pick"
	return openModal(s, modalPick, "pick", &pickModal{kind: "buffers", m: m}), nil
}

func (a *App) windowCmd(s core.State, c core.Command) (core.State, []core.Effect) {
	switch c.ID {
	case "window.split":
		if len(a.wins) > 1 {
			a.info("already split · ctrl+w w switches · ctrl+w q closes")
			return s, nil
		}
		w := a.active()
		nw := newWindow(w.buf, viewTable)
		if w.view == viewTable {
			nw.view = viewNested
		}
		if list := a.ring.List(); len(list) > 1 {
			i := slices.IndexFunc(list, func(b *Buffer) bool { return b.ID == w.buf })
			nw.buf, nw.view = list[(i+1)%len(list)].ID, w.view
		}
		a.wins, a.win, a.pane = append(a.wins, nw), 1, 1
	case "window.next":
		a.win, a.pane = (a.win+1)%len(a.wins), 1
	case "window.close", "window.only":
		if len(a.wins) == 1 {
			a.info("only one window")
			return s, nil
		}
		keep := a.wins[a.win]
		if c.ID == "window.close" {
			keep = a.wins[1-a.win]
		}
		a.wins, a.win = []*window{keep}, 0
	}
	a.ring.Switch(a.active().buf)
	return s, nil
}

func (a *App) rawCmd(s core.State, c core.Command) (core.State, []core.Effect) {
	w := a.active()
	if w.view != viewRaw {
		a.info("raw view only (4)")
		return s, nil
	}
	b := a.buf(w.buf)
	if c.ID == "raw.scope" {
		w.sub, w.ed = !w.sub, nil
		return s, nil
	}
	w.format = map[string]string{"": "pretty", "pretty": "minified", "minified": ""}[w.format]
	w.ed = nil
	a.ensureEditor(w, b)
	if t := w.ed.Value(); b.Editable && w.format != "" && t != b.RawText[w.edAt:w.edAt+w.edLen] {
		a.writeBack(w, b, t)
		a.note("raw text " + w.format)
	} else if w.format != "" {
		a.info("showing " + w.format + " · READ-ONLY keeps the file as is")
	}
	return s, nil
}

func (a *App) rawFoldCmd(s core.State, c core.Command) (core.State, []core.Effect) {
	w := a.active()
	if w.view != viewRaw {
		a.info("raw view only (4)")
		return s, nil
	}
	b := a.buf(w.buf)
	a.ensureEditor(w, b)
	if w.rawFolds == nil {
		w.rawFolds = map[int]bool{}
	}
	text := w.ed.Value()
	blocks := findJSONBlocks(text)
	if len(blocks) == 0 {
		a.info("no multi-line JSON blocks to fold (press = to pretty-print)")
		return s, nil
	}
	switch c.ID {
	case "raw.fold.all":
		n := 0
		for _, blk := range blocks {
			if blk.depth >= 1 {
				w.rawFolds[blk.openLine] = true
				n++
			}
		}
		if n == 0 {
			for _, blk := range blocks {
				w.rawFolds[blk.openLine] = true
				n++
			}
		}
		a.info(fmt.Sprintf("folded %d JSON blocks · zR unfolds all", n))
	case "raw.fold.none":
		clear(w.rawFolds)
		a.info("unfolded all JSON blocks")
	default:
		line := cursorLine(text, w.ed.Cursor())
		blk, ok := enclosingBlock(blocks, line)
		if !ok {
			a.info("no enclosing JSON block on this line")
			return s, nil
		}
		switch c.ID {
		case "raw.fold.close":
			w.rawFolds[blk.openLine] = true
		case "raw.fold.open":
			delete(w.rawFolds, blk.openLine)
		case "raw.fold.toggle":
			if w.rawFolds[blk.openLine] {
				delete(w.rawFolds, blk.openLine)
			} else {
				w.rawFolds[blk.openLine] = true
			}
		}
	}
	return s, nil
}

// --- palette, pickers, help ----------------------------------------------------------------

// exCommand reads a palette query as a vim command line.
func exCommand(q string) (string, map[string]any, bool) {
	f := strings.Fields(q)
	if len(f) == 0 {
		return "", nil, false
	}
	arg := strings.Join(f[1:], " ")
	switch f[0] {
	case "w":
		return "file.write", map[string]any{"path": arg}, true
	case "q":
		return "app.close", nil, true
	case "q!":
		return "app.quit", nil, true
	case "bn":
		return "buffer.next", nil, true
	case "bp", "bN":
		return "buffer.prev", nil, true
	case "b":
		if arg == "" {
			return "buffer.pick", nil, true
		}
		return "buffer.switch", map[string]any{"id": arg}, true
	case "e":
		return "file.load", map[string]any{"path": arg}, true
	case "enew", "new":
		return "buffer.new", nil, true
	case "vs", "vsplit":
		return "window.split", nil, true
	case "reader":
		if arg == "" {
			return "reader.select", nil, true
		}
		return "reader.set", map[string]any{"name": arg}, true
	case "recipe":
		if len(f) >= 2 && f[1] == "save" {
			return "recipe.save", map[string]any{"path": strings.Join(f[2:], " ")}, true
		}
		if len(f) >= 2 && f[1] == "load" {
			return "recipe.load", map[string]any{"path": strings.Join(f[2:], " ")}, true
		}
		return "recipe.save", map[string]any{"path": arg}, true
	}
	return "", nil, false
}

func (a *App) paletteOpen(s core.State, c core.Command) (core.State, []core.Effect) {
	var items []picker.Item
	for _, spec := range a.specs {
		if spec.HasTag("user") {
			items = append(items, picker.Item{ID: spec.ID, Title: spec.ID, Subtitle: spec.Description, Keywords: []string{spec.Description}})
		}
	}
	m := picker.New("Commands", items)
	m.Scope, m.Placeholder, m.MatchMode = "pick", "filter commands, or :w :q :bn :bp :b N :e PATH :enew :vs", picker.MatchMode(a.match)
	return openModal(s, modalPick, "pick", &pickModal{kind: "palette", m: m}), nil
}

func (a *App) pickCmd(s core.State, c core.Command) (core.State, []core.Effect) {
	pm, _ := modalData(s, modalPick).(*pickModal)
	if pm == nil {
		return s, nil
	}
	switch c.ID {
	case "pick.insert":
		pm.m.AppendQuery(c.Arg("text"))
	case "pick.backspace":
		pm.m.BackspaceQuery()
	case "pick.close":
		return closeModal(s, modalPick), nil
	default:
		if id, args, ok := exCommand(pm.m.Query); ok && c.ID == picker.CmdConfirm && pm.kind == "palette" {
			return closeModal(s, modalPick), []core.Effect{dispatch(id, args)}
		}
		pm.m.PageSize = max(a.h*70/100-4, 1)
		picked, _ := pm.m.HandleCommand(c)
		if picked == nil {
			if c.ID == picker.CmdConfirm && pm.kind == "nest" {
				if name := strings.TrimSpace(pm.m.Query); name != "" {
					s = closeModal(s, modalPick)
					a.curUI().sel = nil
					a.applyAt(pm.target, Operation{Kind: "nest", Name: name, Fields: pm.fields})
				}
			}
			return s, nil
		}
		s = closeModal(s, modalPick)
		switch pm.kind {
		case "palette":
			return s, []core.Effect{dispatch(picked.ID, nil)}
		case "buffers":
			return s, []core.Effect{dispatch("buffer.switch", map[string]any{"id": picked.ID})}
		case "format":
			a.saveFormat = picked.ID
			return a.saveDir(s)
		case "reader":
			a.activateReader(picked.ID)
		case "grain":
			op := pm.op
			if picked.ID == "cross" {
				op.Cross = true
			} else if i, err := strconv.Atoi(picked.ID); err == nil {
				op.Grain = pm.paths[i]
			}
			a.doFlatten(op)
		case "nest":
			if name, ok := strings.CutPrefix(picked.ID, "into:"); ok {
				a.curUI().sel = nil
				a.applyAt(pm.target, Operation{Kind: "nest", Name: name, Fields: pm.fields})
			} else {
				a.openPrompt("nest", "new nested object key for "+strings.Join(pm.fields, ", "), "", pm.target, "")
				a.prompt.fields = pm.fields
			}
		case "jqbuilder":
			if p := a.prompt; p != nil && p.kind == "jq" {
				for _, ch := range jqChips {
					if ch.name == picked.ID {
						p.ed = input.New(p.ed.Value()+a.jqStageSnippet(ch), false)
						p.ed.HandleKey("A")
						return s, []core.Effect{a.jqEffect(p.ed.Value())}
					}
				}
			}
		case "convert":
			switch picked.ID {
			case "keyby":
				root := pm.target
				if len(pm.paths) > 0 {
					root = pm.paths[0]
				}
				a.applyAt(root, Operation{Kind: "array-object", Field: pm.field})
			case "entries":
				a.openPrompt("entries", "entries of "+pm.target.String()+" · key column name", "key", pm.target, "")
			case "object-values":
				a.applyAt(pm.target, Operation{Kind: "object-values"})
			case "object-keys":
				a.applyAt(pm.target, Operation{Kind: "object-keys"})
			case "wrap-object":
				def := "data"
				if v, ok := AtPath(a.display(a.cur()), pm.target); ok {
					if _, isArr := v.([]any); isArr {
						def = "items"
					} else if _, isObj := v.(Object); !isObj {
						def = "value"
					}
				}
				a.openPrompt("wrap-object", "wrap "+pm.target.String()+" under object key", def, pm.target, "")
			case "wrap-array":
				a.applyAt(pm.target, Operation{Kind: "wrap-array"})
			}
		}
	}
	return s, nil
}

func (a *App) helpOpen(s core.State, c core.Command) (core.State, []core.Effect) {
	f := s.ActiveFrame()
	mode := f.Mode
	if mode == "" {
		mode = "normal"
	}
	q := core.KeyQuery{Chain: ui.ContextChain(a.view(s), f.FocusID, nil), Mode: mode}
	return openModal(s, modalHelp, "help", &helpModal{q: q}), nil
}

func (a *App) helpScroll(s core.State, c core.Command) (core.State, []core.Effect) {
	m, _ := modalData(s, modalHelp).(*helpModal)
	if m == nil {
		return s, nil
	}
	rows, _ := a.help.HelpView(modalHelp, m.q).Props["rows"].([]any)
	m.off = moveIdx(m.off, len(rows), max(a.h*85/100-4, 1), c)
	return s, nil
}

func (a *App) themeCycle(s core.State, c core.Command) (core.State, []core.Effect) {
	if c.ID == "density.cycle" {
		ds := []string{ui.DensityCompact, ui.DensityBalanced, ui.DensitySpacious}
		a.theme.Density = ds[(slices.Index(ds, a.theme.EffectiveDensity())+1)%len(ds)]
		a.info("density " + a.theme.Density)
	} else {
		ps := ui.ThemePresets()
		i := slices.IndexFunc(ps, func(t ui.Theme) bool { return t.Name == a.theme.Name })
		density := a.theme.Density
		a.theme = ps[(i+1)%len(ps)]
		a.theme.Density = density
		a.info("theme " + a.theme.Name)
	}
	return s, []core.Effect{core.CustomEffect{Kind: "theme", Data: a.theme}}
}

func (a *App) quit(s core.State, c core.Command) (core.State, []core.Effect) {
	for _, b := range a.ring.List() {
		if b.Dirty && a.prevConfirm != "quit" {
			a.confirm = "quit"
			a.warn(bufName(b) + " has unsaved edits · press q again to quit without saving (ctrl+s saves)")
			return s, nil
		}
	}
	return s, []core.Effect{core.QuitEffect{}}
}
