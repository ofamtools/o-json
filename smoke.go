package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/core"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/pathpicker"
)

// driver feeds a Shell synchronously, without a terminal: every command a
// step returns runs at once and its message is fed back, so async effects
// (jq, the clipboard) finish before the next key. It sets PrefixTimeout and
// MaxFPS to 0, so no timer commands are produced.
type driver struct{ sh *otea.Shell }

func newDriver(sh *otea.Shell, w, h int) *driver {
	sh.Engine.PrefixTimeout, sh.MaxFPS = 0, 0
	d := &driver{sh: sh}
	d.send(tea.WindowSizeMsg{Width: w, Height: h})
	return d
}

func (d *driver) send(msg tea.Msg) {
	_, cmd := d.sh.Update(msg)
	d.pump(cmd)
}

func (d *driver) pump(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var m tea.Msg
	select {
	case m = <-done:
	case <-time.After(10 * time.Second):
		return
	}
	switch m := m.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			d.pump(c)
		}
	case tea.QuitMsg, nil:
	default:
		d.send(m)
	}
}

// keys sends key presses by core name: "j", "G", "ctrl+w", "enter", "esc".
func (d *driver) keys(names ...string) {
	for _, n := range names {
		d.send(keyMsg(n))
	}
}

// typeText sends each rune of s as a key press.
func (d *driver) typeText(s string) {
	for _, r := range s {
		if r == ' ' {
			d.send(keyMsg("space"))
		} else {
			d.send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
}

var namedKeys = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
}

func keyMsg(n string) tea.KeyPressMsg {
	var mod tea.KeyMod
	for {
		if rest, ok := strings.CutPrefix(n, "ctrl+"); ok && rest != "" {
			mod, n = mod|tea.ModCtrl, rest
		} else if rest, ok := strings.CutPrefix(n, "alt+"); ok && rest != "" {
			mod, n = mod|tea.ModAlt, rest
		} else {
			break
		}
	}
	if r, ok := namedKeys[n]; ok {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}
	if n == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " ", Mod: mod}
	}
	r := []rune(n)[0]
	if mod != 0 {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}
	return tea.KeyPressMsg{Code: r, Text: n}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:?<=>]*[ -/]*[@-~]|\x1b\][^\x07]*\x07`)

func (d *driver) frame() string { return ansiRe.ReplaceAllString(d.sh.View().Content, "") }

const smokeDoc = `{
  "customer": {"name": "Ann", "tier": "gold"},
  "orders": [
    {"order_id": "A1", "total": 50, "memo": "deliver to the side door after five in the evening please",
     "items": [{"sku": "x1", "price": 20}, {"sku": "x2", "price": 30}], "notes": [{"text": "gift"}]},
    {"order_id": "A2", "total": 5, "memo": "none", "items": [], "notes": []}
  ]
}
`

type smoke struct {
	w     io.Writer
	d     *driver
	a     *App
	fails int
	mu    sync.Mutex
	clips []string
}

func (k *smoke) check(name string, ok bool, detail ...any) {
	if ok {
		fmt.Fprintln(k.w, "ok  ", name)
		return
	}
	k.fails++
	fmt.Fprintln(k.w, "FAIL", name, "·", fmt.Sprint(detail...), "· status:", k.a.status)
}

func (k *smoke) mode() string { return k.d.sh.State.Pages[pageID].Mode }

func (k *smoke) focus() string { return k.a.curUI().focus.String() }

func (k *smoke) pipeline() []string {
	var out []string
	for _, op := range k.a.cur().Pipeline {
		out = append(out, op.Kind)
	}
	return out
}

// treeTo walks the structure tree with real keys (j and l) to the first
// row named name.
func (k *smoke) treeTo(name string) bool {
	if k.a.pane != 0 {
		k.d.keys("tab")
	}
	k.d.keys("g", "g")
	for range 80 {
		rows := k.a.structRows(k.a.cur())
		i := k.a.curUI().cursor
		if i < 0 || i >= len(rows) {
			return false
		}
		switch r := rows[i]; {
		case r.s.Name == name:
			return true
		case len(r.s.Children) > 0 && !r.open:
			k.d.keys("l")
		case i == len(rows)-1:
			return false
		default:
			k.d.keys("j")
		}
	}
	return false
}

// runeAt is the rune under the editor cursor, or 0.
func runeAt(ed *input.Model) rune {
	if ed == nil {
		return 0
	}
	r := []rune(ed.Value())
	if i := ed.Cursor(); i >= 0 && i < len(r) {
		return r[i]
	}
	return 0
}

// toTree leaves the raw editor (insert, then normal) for the tree pane.
func (k *smoke) toTree() {
	for range 3 {
		if k.mode() != "vim" {
			return
		}
		k.d.keys("esc")
	}
}

func (k *smoke) toBuffer(id int) {
	for range len(k.a.ring.List()) {
		if k.a.cur().ID == id {
			return
		}
		k.d.keys("]", "b")
	}
}

func (k *smoke) undoAll() {
	for range 10 {
		if len(k.a.cur().Pipeline) == 0 {
			return
		}
		k.d.keys("u")
	}
}

func (k *smoke) at(path string) (any, bool) {
	p, err := ParsePath(path)
	if err != nil {
		return nil, false
	}
	return AtPath(k.a.display(k.a.cur()), p)
}

// givenFile opens a copy of path in dir (so an export never lands next to
// the original), flattens the whole document with F from the tree root and
// exports the table with W. It reports the row and column counts so a
// caller can hold a fixture to its oracle.
func (k *smoke) givenFile(path, dir string) {
	a, d := k.a, k.d
	orig, err := os.ReadFile(path)
	if err != nil {
		k.check("the given file reads", false, err)
		return
	}
	cp := filepath.Join(dir, filepath.Base(path))
	if err := os.WriteFile(cp, orig, 0o644); err != nil {
		k.check("the given file copies", false, err)
		return
	}
	d.keys(":")
	d.typeText("e " + cp)
	d.keys("enter")
	g := a.cur()
	k.check("the given file opens read-only", g.Path == cp && !g.Editable && a.tone != "error", a.status)
	if g.Path != cp {
		return
	}
	k.toTree()
	if a.pane != 0 {
		d.keys("tab")
	}
	d.keys("g", "g", "F")
	if pm, _ := modalData(d.sh.State, modalPick).(*pickModal); pm != nil && pm.kind == "grain" {
		fmt.Fprintln(k.w, "note  the given file asks for a grain; taking the first of", len(pm.paths))
		d.keys("enter")
	}
	rel, _ := a.relation(a.active(), g)
	k.check(fmt.Sprintf("F flattens the given file to %d rows x %d columns", len(rel.Rows), len(rel.Columns)),
		len(rel.Rows) > 0 && a.active().view == viewTable, k.pipeline())
	// The synthesized jq, run by real jq over the file, gives the same rows.
	if _, err := exec.LookPath("jq"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		v, err := RunJQ(ctx, string(orig), SynthesizeJQ(g.Pipeline))
		cancel()
		rows, _ := v.([]any)
		k.check("real jq agrees on the given file's row count", err == nil && len(rows) == len(rel.Rows), err, " jq rows ", len(rows))
	}
	// A table preselects CSV in W's format picker.
	d.keys("W", "enter", "ctrl+s", "enter")
	out := filepath.Join(dir, strings.TrimSuffix(filepath.Base(cp), filepath.Ext(cp))+".copy.csv")
	f, err := os.Open(out)
	var recs [][]string
	if err == nil {
		recs, err = csv.NewReader(f).ReadAll()
		f.Close()
	}
	header := ""
	if len(recs) > 0 {
		header = strings.Join(recs[0], ",")
	}
	k.check(fmt.Sprintf("W exports the flattened table as CSV with header %s", header),
		err == nil && len(recs) == len(rel.Rows)+1 && header == strings.Join(rel.Columns, ","), err, " records ", len(recs))
	if len(recs) > 1 {
		fmt.Fprintln(k.w, "csv   row 1:", strings.Join(recs[1], ","))
	}
	now, _ := os.ReadFile(path)
	k.check("the given file is untouched", string(now) == string(orig))
}

// runSmoke replays a scripted session through the real bindings, engine,
// shell and renderer without a terminal, one line per check. path, when
// given, is opened as one more buffer at the end.
func runSmoke(w io.Writer, path string) error {
	dir, err := os.MkdirTemp("", "o-json-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	sample := filepath.Join(dir, "orders.json")
	if err := os.WriteFile(sample, []byte(smokeDoc), 0o644); err != nil {
		return err
	}
	k := &smoke{w: w}
	cfg := Config{FS: pathpicker.OSFS{}, Cwd: dir, Copy: func(s string) error {
		k.mu.Lock()
		defer k.mu.Unlock()
		k.clips = append(k.clips, s)
		return nil
	}}
	k.a = newApp(cfg, NewScratch())
	k.d = newDriver(k.a.shell(), 140, 36)
	a, d := k.a, k.d

	// Scratch: editable at once, quiet while incomplete, last valid tree kept.
	k.check("zero-arg scratch is editable and inserting", a.cur().Editable && k.mode() == "vim" && a.active().ed.Mode() == input.ModeInsert, k.mode())
	d.typeText(`{"a": `)
	k.check("incomplete JSON is a quiet status", strings.Contains(d.frame(), "incomplete JSON: line 1, col"), a.cur().ParseErr)
	d.typeText(`[1, 2]}`)
	root := a.schema(a.cur())
	k.check("scratch parses and shows its structure", a.cur().ParseErr == nil && root != nil && len(root.Children) == 1 && root.Children[0].Name == "a")
	d.typeText(`,`)
	k.check("the last valid structure stays while typing", a.cur().ParseErr != nil && a.schema(a.cur()) != nil && len(a.schema(a.cur()).Children) == 1)
	d.keys("backspace", "esc", ":")
	d.typeText("e " + sample)
	d.keys("enter")
	b := a.cur()
	k.check(":e opens a file read-only in a new buffer", b.Path == sample && !b.Editable && len(a.ring.List()) == 2 && strings.Contains(d.frame(), "READ-ONLY"), b.Path)

	// Structure tree walking and folding.
	d.keys("g", "g", "j")
	k.check("j moves the tree cursor and the focus", a.curUI().cursor == 1 && k.focus() != "$", k.focus())
	k.check("tree reaches orders", k.treeTo("orders"))
	d.keys("h")
	rows := a.structRows(b)
	k.check("h folds the node", !rows[a.curUI().cursor].open)
	d.keys("l")
	rows = a.structRows(b)
	k.check("l unfolds the node", rows[a.curUI().cursor].open)
	at := a.curUI().cursor
	d.keys("l")
	k.check("l on an open node walks into it", a.curUI().cursor == at+1)

	// Selection at any depth, live projection, flatten with a grain picker.
	k.treeTo("order_id")
	d.keys("space")
	k.treeTo("sku")
	d.keys("space")
	k.treeTo("text")
	d.keys("space")
	_, hasCustomer := k.at("customer")
	k.check("space selects nested fields and the view shows the projection", len(a.curUI().sel) == 3 && !hasCustomer)
	d.keys("F")
	pm, _ := modalData(d.sh.State, modalPick).(*pickModal)
	k.check("F asks for the row grain between sibling arrays", pm != nil && pm.kind == "grain" && len(pm.paths) >= 2)
	d.keys("enter")
	rel, _ := a.relation(a.active(), b)
	k.check("flatten makes one row per item, left join keeps the empty order", len(rel.Rows) == 3 && a.active().view == viewTable, len(rel.Rows), rel.Columns)
	k.undoAll()
	d.keys("I")
	k.treeTo("order_id")
	d.keys("space")
	k.treeTo("sku")
	d.keys("space")
	d.keys("F")
	rel, _ = a.relation(a.active(), b)
	k.check("inner join drops the order without items", a.inner && len(rel.Rows) == 2, len(rel.Rows))
	k.undoAll()
	d.keys("I", "esc")

	// Table: the column under the cursor expands; views keep the focus.
	k.treeTo("memo")
	d.keys("1", "tab", "h")
	long := "deliver to the side door after five in the evening please"
	k.check("a column away from the cursor is clipped", !strings.Contains(d.frame(), long))
	d.keys("l")
	k.check("the column under the cursor shows in full", strings.Contains(d.frame(), long) && k.focus() == "orders[0].memo", k.focus())
	d.keys("2")
	nrows, _ := a.nestedRows(a.active(), a.display(b))
	nfocus := ""
	if i := a.nestedCursor(a.active(), b, nrows); i >= 0 && i < len(nrows) {
		nfocus = nrows[i].String()
	}
	k.check("nested view lands on the focused path", nfocus == "orders[0].memo", nfocus)
	d.keys("3")
	doc := a.textDoc(b, false)
	lo, _, ok := doc.focusLines(a.curUI().focus)
	k.check("pretty view highlights the focused field", ok && strings.Contains(doc.lines[lo], `"memo"`) && strings.Contains(d.frame(), `"memo"`))
	d.keys("3")
	k.check("3 again shows key: value lines at the focus", a.active().kv && strings.Contains(d.frame(), "memo: deliver"))
	d.keys("h", "4")
	ed := a.active().ed
	k.check("raw view puts the cursor on the focused value", k.mode() == "vim" && runeAt(ed) == '{' && k.focus() == "orders[0]", k.focus())
	// In the raw editor a bare 4 is a vim count, so view.raw is alt+4 there.
	d.keys("alt+4")
	ed = a.active().ed
	k.check("alt+4 in the editor edits only the focused subtree", ed != nil && strings.HasPrefix(ed.Value(), "{") && strings.Contains(ed.Value(), "A1") && !strings.Contains(ed.Value(), "customer"), edText(ed), " focus ", k.focus())
	d.keys("=")
	k.check("= formats the subtree", strings.Contains(a.active().ed.Value(), "\n") && strings.Contains(a.active().ed.Value(), `"order_id"`) && b.RawText == smokeDoc)
	d.keys("=")
	k.check("= again minifies it", !strings.Contains(a.active().ed.Value(), "\n") && b.RawText == smokeDoc)
	d.keys("i")
	k.check("READ-ONLY refuses edits until ctrl+e", b.RawText == smokeDoc && a.active().ed.Mode() != input.ModeInsert && strings.Contains(a.status, "READ-ONLY"))
	d.keys("ctrl+s")
	k.check("READ-ONLY never saves over the file", strings.Contains(a.status, "READ-ONLY") && !b.Dirty)
	d.keys("ctrl+e", "=", "i")
	d.typeText("[")
	k.check("an edit after ctrl+e updates the buffer live, keeping the last valid tree", b.Editable && b.Dirty && b.ParseErr != nil && a.schema(b) != nil && len(a.schema(b).Children) == 2)
	d.keys("backspace")
	k.check("fixing the text parses again", b.ParseErr == nil && b.RawText == smokeDoc)
	d.keys("esc", "esc")

	// Pipeline steps, undo and redo, jq synthesis.
	k.treeTo("orders")
	d.keys("/")
	d.typeText("A2")
	n, _ := k.at("orders")
	k.check("/ filters live while typing", isList(n) && len(n.([]any)) == 1)
	d.keys("enter")
	k.check("enter keeps the filter as a scoped step", strings.Join(k.pipeline(), ",") == "scope,filter,scope" && strings.Contains(SynthesizeJQ(b.Pipeline), "select"), k.pipeline(), SynthesizeJQ(b.Pipeline))
	d.keys("y", "j")
	k.check("y j copies the jq command", len(k.clips) > 0 && strings.HasPrefix(k.clips[len(k.clips)-1], "jq '") && strings.HasSuffix(k.clips[len(k.clips)-1], shQuote(sample)), k.clips)
	d.keys("u")
	undone := len(b.Pipeline) == 0
	d.keys("ctrl+r")
	k.check("u undoes the scoped filter as one step and ctrl+r redoes it", undone && strings.Join(k.pipeline(), ",") == "scope,filter,scope", k.pipeline())
	k.undoAll()
	// Steps on a field under orders[] run scoped to orders and then scope
	// back to the document: the enclosing object stays, the step's result
	// lives at orders, and one u takes the whole step back.
	for _, tc := range []struct{ key, field, path, want, gone, kind string }{
		{"K", "order_id", "orders.A1.total", "50", "orders[0]", "array-object"},
		{"x", "total", "orders[0].memo", "", "orders[0].total", "drop"},
		{"G", "order_id", "orders[0].rows[0].memo", "", "", "group"},
		{"s", "total", "orders[0].order_id", "A2", "", "sort"},
	} {
		k.treeTo(tc.field)
		d.keys(tc.key)
		v, found := k.at(tc.path)
		_, stays := k.at(tc.gone)
		ops := k.pipeline()
		ok := strings.Join(ops, ",") == "scope,"+tc.kind+",scope" && found && (tc.want == "" || valueText(v) == tc.want) && (tc.gone == "" || !stays)
		_, kept := k.at("customer.name")
		d.keys("u")
		k.check(tc.key+" adds a "+tc.kind+" step scoped to orders", ok && kept && len(b.Pipeline) == 0, ops, " ", tc.path, "=", valueText(v))
		k.undoAll()
	}
	// In the table G groups by the column under the cursor; g g goes to the top.
	k.treeTo("order_id")
	d.keys("1", "tab", "G")
	k.check("G in the table groups by the column under the cursor", strings.Join(k.pipeline(), ",") == "scope,group,scope", k.pipeline())
	k.undoAll()
	d.keys("j", "g", "g")
	k.check("g g in the table still goes to the top", len(b.Pipeline) == 0 && k.focus() == "orders[0].order_id", k.focus())
	k.treeTo("total")
	d.keys("r")
	d.keys("backspace", "backspace", "backspace", "backspace", "backspace")
	d.typeText("amount")
	d.keys("enter")
	_, renamed := k.at("orders[0].amount")
	_, old := k.at("orders[0].total")
	k.check("r renames a column", renamed && !old, k.pipeline())
	k.undoAll()
	k.treeTo("customer")
	d.keys("E")
	k.check("E asks for the key column, prefilled with key", a.prompt != nil && a.prompt.kind == "entries" && a.prompt.ed.Value() == "key")
	d.keys("enter")
	e, _ := k.at("customer")
	key, _ := k.at("customer[0].key")
	_, kept := k.at("orders[1].order_id")
	k.check("E turns an object into entries", isList(e) && valueText(key) == "name" && kept, k.pipeline())
	k.undoAll()
	k.treeTo("customer")
	d.keys("E", "backspace", "backspace", "backspace")
	d.typeText("field")
	d.keys("enter")
	named, _ := k.at("customer[1].field")
	k.check("E names the key column", valueText(named) == "tier", k.pipeline())
	k.undoAll()

	// jq bar: real jq, live and cancellable, chips and errors.
	if _, err := exec.LookPath("jq"); err != nil {
		fmt.Fprintln(w, "skip  jq bar (jq is not on PATH)")
	} else {
		d.keys("J")
		k.check("J opens the jq bar with the pipeline's jq", a.prompt != nil && a.prompt.kind == "jq" && a.jq.ok)
		d.typeText(" | .orders | length")
		k.check("the jq bar previews real jq results live", a.jq.ok && valueText(a.jq.val) == "2", a.jq.err)
		d.keys("alt+o")
		k.check("a chip appends a step and errors show inline", strings.Contains(a.prompt.ed.Value(), "sort_by") && a.jq.err != "" && strings.Contains(d.frame(), "jq:"), a.jq.err)
		d.keys("esc", "J", "enter")
		k.check("enter opens the jq result as a new buffer", strings.HasPrefix(a.cur().Name, "jq") && a.cur().Editable && len(a.ring.List()) == 3)
		d.keys("ctrl+6")
		k.check("ctrl+6 returns to the alternate buffer", a.cur().ID == b.ID)
	}

	// Buffers and windows.
	d.keys("ctrl+n")
	scratch := a.cur()
	d.typeText("[1]")
	k.toTree()
	d.keys("[", "b")
	k.check("[ b goes to the previous buffer", a.cur().ID != scratch.ID)
	d.keys("]", "b")
	k.check("] b goes to the next buffer", a.cur().ID == scratch.ID)
	d.keys(":", "b", "enter")
	k.check(":b opens the buffer picker", modalData(d.sh.State, modalPick) != nil)
	d.keys("esc", "ctrl+w", "v")
	k.check("ctrl+w v splits side by side", len(a.wins) == 2 && strings.Count(d.frame(), "· [No Name]")+strings.Count(d.frame(), "orders.json") >= 2)
	d.keys("ctrl+w", "w")
	k.check("ctrl+w w switches windows", a.win == 0)
	d.keys("ctrl+w", "o")
	k.check("ctrl+w o keeps one window", len(a.wins) == 1)

	// Save a scratch buffer through the path picker, export CSV.
	k.toTree()
	k.toBuffer(scratch.ID)
	d.keys("ctrl+s")
	k.check("ctrl+s on a scratch buffer asks for a folder", modalData(d.sh.State, modalFiles) != nil)
	d.keys("ctrl+s", "enter")
	saved, err := os.ReadFile(filepath.Join(dir, "untitled.json"))
	k.check("the scratch buffer is saved", err == nil && strings.Contains(string(saved), "1") && scratch.Path != "", err)
	k.toTree()
	k.toBuffer(b.ID)
	k.treeTo("orders")
	d.keys("1", "W")
	d.typeText("csv")
	d.keys("enter", "ctrl+s", "enter")
	csv, err := os.ReadFile(filepath.Join(dir, "orders.copy.csv"))
	k.check("W exports the table as CSV", err == nil && strings.Contains(string(csv), "order_id"), err, a.status)

	// Help, density, quit.
	d.keys("?")
	k.check("? shows the keys active here", strings.Contains(d.frame(), "select.toggle"))
	d.keys("esc", "g", "d")
	density := a.theme.Density
	k.check("g d changes the density", d.sh.Theme.Density == density && density != "")
	d.keys("d")
	k.check("d changes the density", d.sh.Theme.Density == a.theme.Density && a.theme.Density != density, a.theme.Density)
	theme := a.theme.Name
	d.keys("t")
	k.check("t changes the theme and keeps the density", d.sh.Theme.Name == a.theme.Name && a.theme.Name != theme && a.theme.Density == d.sh.Theme.Density && a.theme.Density != density)
	if path != "" {
		k.givenFile(path, dir)
	}
	d.keys("q")
	k.check("q asks again with unsaved edits", strings.Contains(a.status, "unsaved"))
	d.keys("q")
	quit := false
	for _, e := range d.sh.LastEffects {
		_, q := e.(core.QuitEffect)
		quit = quit || q
	}
	k.check("q again quits", quit)
	if k.fails > 0 {
		return fmt.Errorf("smoke: %d check(s) failed", k.fails)
	}
	return nil
}

// edText is the editor text for a failure detail, cut to 120 bytes.
func edText(ed *input.Model) string {
	if ed == nil {
		return "<no editor>"
	}
	s := ed.Value()
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
