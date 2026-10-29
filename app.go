package main

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
	"strconv"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// The viewer: the state o-json owns. The runtime holds the interaction
// state; the structure tree (ochazuke's outline, read-only) holds the
// shapes and their collapse state; the data panel and the detail modal are
// windowed tables (rows.go). Nothing here writes a file.

const (
	pageID   = "viewer"
	treeID   = "structure"
	dataID   = "data"
	detailID = "detail"
	rowsID   = "detail/rows"
)

// App is the viewer's state.
type App struct {
	Doc     *Doc
	Path    string // the file being shown or read ("-" is stdin)
	Loading string // the file being read off the loop, "" when idle
	LoadErr error
	// recent is the recent list the path chooser offers (viewer.open's
	// recent source): read before the loop starts and after each read, on
	// the read's goroutine, so the chooser does no I/O.
	recent []string

	// The structure tree's filters: Query keeps shapes whose key or path
	// contains it (case-insensitive) with their ancestors; Kind keeps
	// shapes that hold values of that type.
	Query string
	Kind  Kind
	// Raw shows the selected shape's values as indented JSON instead of a
	// table or fields.
	Raw bool

	// Clipboard copies text to the system clipboard; nil when there is none.
	Clipboard func(string) error
	// Paste reads the system clipboard (viewer.paste); nil when there is
	// no tool for it.
	Paste func() (string, error)
	// Stdin is what a "-" path reads.
	Stdin interface{ Read([]byte) (int, error) }

	treeWidth int // the structure tree's natural width

	shown  *Shape // the shape the data panel shows
	data   *rowsView
	tab    *Table   // the data panel's table, when it shows one
	fields *Node    // the data panel's object, when it shows fields
	lines  []string // the data panel's raw lines, when Raw
	scalar *Node    // the data panel's single scalar

	detail *rowsView
	trail  []*Node // the detail modal's drill path, innermost last

	r    *ochazuke.Runtime
	m    *ochazuke.Model
	tree *ochazuke.Tree
	ask  *question
	in   *ochazuke.Input

	rs restructureState // the restructure state: the page and the edited structure (page.go, inplace.go)

	// original shows the original document in the viewer (T); otherwise,
	// with structure.editable, the edited one (inplace.go).
	original bool
	note     string       // why the data panel is empty in the edited view
	ed       *editedCache // the restructured document the edited view shows

	vedit *valueEdit // the data pane's open inline input (values.go)

	q queryState // the live jq query (query.go)
}

// New builds the viewer on a fresh runtime: the page with the structure
// tree and the data panel, the detail modal, the search prompt, the nouns,
// verbs and bindings. It starts empty ("[no file]"); Load,
// viewer.open or viewer.paste fills it.
func New() (*App, error) {
	r := ochazuke.NewRuntime()
	a := &App{r: r}
	a.m = ochazuke.NewModel(r)
	a.m.Title = "o-json"
	r.Instance(pageID, ochazuke.Kind("viewer"))
	a.registerNouns()
	tree, err := r.AddTree(treeID, nil, ochazuke.Parent(pageID), ochazuke.NoSpill(), ochazuke.NoMarks(),
		ochazuke.Columns(ochazuke.Column{Title: "path", Min: 8}, ochazuke.Column{Title: "type", Priority: 2}, ochazuke.Column{Title: "count", Align: ochazuke.AlignRight, Priority: 1}))
	if err != nil {
		return nil, err
	}
	a.tree = tree
	if a.data, err = newRowsView(r, dataID, pageID); err != nil {
		return nil, err
	}
	a.registerPrompt()
	a.registerDetail()
	a.registerVerbs()
	if err := a.registerQuery(); err != nil {
		return nil, err
	}
	if err := a.registerQueryMode(); err != nil {
		return nil, err
	}
	a.registerQueryFunctions()
	if err := declareSettings(r); err != nil {
		return nil, err
	}
	if err := declareOpenSettings(r); err != nil {
		return nil, err
	}
	if err := declareEditSettings(r); err != nil {
		return nil, err
	}
	if err := declareValueSettings(r); err != nil {
		return nil, err
	}
	if err := declareExportSettings(r); err != nil {
		return nil, err
	}
	page, err := r.AddPage(pageID, "o-json", a.render, ochazuke.Focus(treeID))
	if err != nil {
		return nil, err
	}
	page.OnOpen = func(*ochazuke.Runtime, string) { a.openedViewer() }
	if err := a.registerRestructure(); err != nil {
		return nil, err
	}
	a.registerValueVerbs()
	a.placeTree(pageID)
	if err := a.bind(); err != nil {
		return nil, err
	}
	if err := r.OpenPage(pageID); err != nil {
		return nil, err
	}
	return a, nil
}

// Load reads a file on the calling goroutine (tests, --smoke). The TUI
// reads through viewer.load, off the loop.
func (a *App) Load(path string) error {
	doc, err := ReadDoc(path, a.Stdin)
	if err != nil {
		return err
	}
	return a.setDoc(path, doc)
}

// setDoc shows a parsed document: the structure tree from its shapes, the
// root selected.
func (a *App) setDoc(path string, doc *Doc) error {
	a.Doc, a.Path, a.Loading, a.LoadErr = doc, path, "", nil
	a.shown = nil
	a.forgetQuery()
	if page := a.r.PageOf(pageID); page != nil {
		page.Title = doc.Name
	}
	if err := a.rebuild(); err != nil {
		return err
	}
	if err := a.setOriginal(); err != nil {
		return err
	}
	a.rs.dirty = true
	if err := a.startState(); err != nil {
		return err
	}
	if err := a.collapseDeep(); err != nil {
		return err
	}
	if err := a.tree.Select(shapeID(doc.Shape)); err != nil {
		return err
	}
	if err := a.rs.tree.Select(rsRootID); err != nil {
		return err
	}
	if a.r.Page == pageID {
		t := a.tree
		if a.editedShown() {
			t = a.rs.tree
		}
		if err := t.Focus(); err != nil {
			return err
		}
	}
	return a.sync()
}

// maxFields is how many fields of one shape the tree lists; an object used
// as a map (a key per record) would otherwise list every key.
const maxFields = 200

// bigStructure is the shape count above which the tree opens collapsed
// below the second level.
const bigStructure = 150

func shapeID(s *Shape) string { return "s:" + s.Path }

// A field some objects repeat has its label toned (json.duplicate-tone):
// background, the built-in error tone (bold on the error background), or
// foreground, the errored tone (the scheme's error role as the colour).
const settingDuplicateTone = "json.duplicate-tone"

func declareToneSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(ochazuke.Setting{
		Name: settingDuplicateTone, Type: ochazuke.SettingString, Default: "background", Enum: []string{"background", "foreground"},
		Description: "How a key some objects repeat (jq keeps the last value) is marked in the structure trees: background puts its label on the error background; foreground colours it in the error colour",
	})
}

// duplicateTone is the tone of a repeated key's label cell.
func (a *App) duplicateTone() ochazuke.ToneSpan {
	if a.r.Settings().String(settingDuplicateTone) == "foreground" {
		return ochazuke.CellTone(0, ochazuke.ToneErrored)
	}
	return ochazuke.CellTone(0, ochazuke.ToneError)
}

// rebuild sets the tree's nodes from the shapes the filters keep.
func (a *App) rebuild() error {
	if a.Doc == nil {
		return a.tree.SetNodes(nil)
	}
	nodes, width := shapeNodes(a.Doc.Shape, a.keep(), "", a.duplicateTone())
	a.treeWidth = width
	return a.tree.SetNodes(nodes)
}

// shapeNodes is a structure tree's nodes (ids prefix+"s:"+path, noun
// shape, cells path, type, count) for the shapes keep holds (nil: all),
// and the tree's natural width.
func shapeNodes(root *Shape, keep map[*Shape]bool, prefix string, dup ochazuke.ToneSpan) ([]ochazuke.TreeNode, int) {
	var nodes []ochazuke.TreeNode
	var add func(s *Shape, parent string)
	add = func(s *Shape, parent string) {
		if keep != nil && !keep[s] {
			return
		}
		name := s.Label()
		if s.Optional {
			name += "?"
		}
		id := prefix + shapeID(s)
		var tones []ochazuke.ToneSpan
		if s.Duplicates != nil {
			tones = []ochazuke.ToneSpan{dup}
		}
		nodes = append(nodes, ochazuke.TreeNode{ID: id, Parent: parent, Label: name, Noun: "shape", Data: s,
			Cells: []string{name, s.Types(), s.Presence()}, Tones: tones})
		shown := 0
		for _, c := range s.Children {
			if c.IsField() && shown >= maxFields {
				continue
			}
			if c.IsField() {
				shown++
			}
			add(c, id)
		}
		if hidden := len(s.fields) - shown; hidden > 0 && keep == nil {
			label := fmt.Sprintf("… %d more fields", hidden)
			nodes = append(nodes, ochazuke.TreeNode{ID: id + "/more", Parent: id, Label: label, Noun: "shape", Cells: []string{label, "", ""}})
		}
	}
	add(root, "")
	// The tree's natural width: indent and marker, then the three cells.
	cols := [3]int{4, 4, 5}
	for _, n := range nodes {
		s, _ := n.Data.(*Shape)
		depth := 0
		if s != nil {
			for p := s.parent; p != nil; p = p.parent {
				depth++
			}
		}
		cols[0] = max(cols[0], 2*depth+2+ansi.StringWidth(n.Cells[0]))
		cols[1] = max(cols[1], len(n.Cells[1]))
		cols[2] = max(cols[2], len(n.Cells[2]))
	}
	return nodes, cols[0] + cols[1] + cols[2] + 4
}

// keep is the set of shapes the filters keep (matches and their
// ancestors), nil when no filter is on.
func (a *App) keep() map[*Shape]bool {
	if a.Query == "" && a.Kind == "" {
		return nil
	}
	keep := map[*Shape]bool{a.Doc.Shape: true}
	a.Doc.Shape.Walk(func(s *Shape, _ int) {
		if a.matches(s) {
			for at := s; at != nil; at = at.parent {
				if keep[at] && at != s {
					break
				}
				keep[at] = true
			}
		}
	})
	return keep
}

// matches reports a shape the filters select.
func (a *App) matches(s *Shape) bool {
	if a.Kind != "" && s.Kinds[a.Kind] == 0 {
		return false
	}
	if a.Query != "" {
		q := strings.ToLower(a.Query)
		return strings.Contains(strings.ToLower(s.Key), q) || strings.Contains(strings.ToLower(s.Path), q)
	}
	return true
}

// collapseDeep collapses a big structure below the second level.
func (a *App) collapseDeep() error {
	count := 0
	a.Doc.Shape.Walk(func(*Shape, int) { count++ })
	if count <= bigStructure {
		return nil
	}
	var err error
	a.Doc.Shape.Walk(func(s *Shape, depth int) {
		if depth >= 2 && len(s.Children) > 0 && err == nil {
			err = a.tree.SetCollapsed(shapeID(s), true)
		}
	})
	return err
}

// selectedShape is the shape the data panel follows: the original tree's
// selected shape, or in the edited view the restructured document's shape
// at the selected node's path; nil for none.
func (a *App) selectedShape() *Shape {
	if a.editedShown() {
		s, _ := a.editedShape()
		return s
	}
	s, _ := a.selectedOriginal()
	return s
}

// sync points the data panel at the selected shape when it moved, or
// when an edit made the restructured document again.
func (a *App) sync() error {
	if a.queryShowing() {
		a.note = ""
		if a.q.goodView == a.shown {
			return nil
		}
		return a.show(a.q.goodView)
	}
	if a.editedShown() {
		s, note := a.editedShape()
		if s == nil {
			if a.shown == nil && note == a.note {
				return nil
			}
			a.shown, a.tab, a.fields, a.lines, a.scalar, a.note = nil, nil, nil, nil, nil, note
			return a.data.set(rowSource{}, 0)
		}
		a.note = ""
		if s == a.shown {
			return nil
		}
		return a.show(s)
	}
	a.note = ""
	s := a.selectedShape()
	if s == nil || s == a.shown {
		return nil
	}
	return a.show(s)
}

// show fills the data panel with a shape's values: one object as labeled
// fields, one array or many values as a table, one scalar as is; with Raw,
// the values as indented JSON lines.
//
// The same path shown again (a value edit, an undo) keeps the selected
// row and column.
func (a *App) show(s *Shape) error {
	sel, col := 0, 0
	if prev := a.shown; prev != nil && prev.Path == s.Path && a.lines == nil && !a.Raw {
		sel, col = a.data.Selected(), a.data.list.Column()
	}
	a.shown, a.tab, a.fields, a.lines, a.scalar = s, nil, nil, nil, nil
	values := s.Values()
	set := func(src rowSource) error {
		if err := a.data.set(src, sel); err != nil {
			return err
		}
		if col > 0 {
			a.data.list.SetColumn(col)
		}
		return nil
	}
	switch {
	case a.Raw:
		a.lines = rawLines(values, len(values) != 1)
		return a.data.set(linesSource(a.lines), 0)
	case len(values) == 1 && values[0].Kind == KObject:
		a.fields = values[0]
		return set(fieldsSource(values[0], "field"))
	case len(values) == 1 && !values[0].IsContainer():
		a.scalar = values[0]
		return a.data.set(rowSource{}, 0)
	}
	a.tab = NewTable(s.Path, values, len(values) != 1)
	return set(tableSource(a.tab))
}

// tableSource is a table as rows: "#" then the columns, optional titles
// ending in "?", widths fixed from the first rows.
func tableSource(t *Table) rowSource {
	cols := []ochazuke.Column{{Title: "#", Align: ochazuke.AlignRight}}
	natural := []int{len(strconv.Itoa(max(0, len(t.Rows)-1)))}
	for j, c := range t.Columns {
		title := c.Title()
		if c.Optional {
			title += "?"
		}
		cols = append(cols, ochazuke.Column{Title: title, Priority: 1})
		natural = append(natural, columnWidth(title, len(t.Rows), func(i int) string { return oneLine(t.CellText(t.Rows[i], j)) }))
	}
	return rowSource{
		n: len(t.Rows), columns: cols, natural: natural, noun: "item",
		cells: func(i int) []string {
			out := make([]string, 0, len(t.Columns)+1)
			out = append(out, strconv.Itoa(i))
			for j := range t.Columns {
				out = append(out, oneLine(t.CellText(t.Rows[i], j)))
			}
			return out
		},
		data:  func(i int) any { return t.Rows[i] },
		label: func(i int) string { return t.Rows[i].Path() },
	}
}

// fieldsSource is a container's children as labeled rows: field (or
// [index]), type and value.
func fieldsSource(n *Node, noun string) rowSource {
	key := func(c *Node) string {
		if n.Kind == KArray {
			return "[" + strconv.Itoa(c.Index) + "]"
		}
		return c.Key
	}
	keyW := columnWidth("field", len(n.Children), func(i int) string { return oneLine(key(n.Children[i])) })
	typeW := columnWidth("type", len(n.Children), func(i int) string { return n.Children[i].TypeLabel() })
	return rowSource{
		n:       len(n.Children),
		columns: []ochazuke.Column{{Title: "field"}, {Title: "type", Priority: 1}, {Title: "value"}},
		natural: []int{keyW, typeW, columnWidth("value", len(n.Children), func(i int) string { return oneLine(Text(n.Children[i], cellLimit)) })},
		noun:    noun,
		cells: func(i int) []string {
			c := n.Children[i]
			return []string{oneLine(key(c)), c.TypeLabel(), oneLine(Text(c, cellLimit))}
		},
		data:  func(i int) any { return n.Children[i] },
		label: func(i int) string { return key(n.Children[i]) },
	}
}

// rawLimit caps the indented JSON the raw view builds.
const rawLimit = 4 << 20

// rawLines is the values as indented JSON (many values: a JSON array).
func rawLines(values []*Node, many bool) []string {
	var b strings.Builder
	cut := false
	if many {
		b.WriteString("[\n")
		for i, v := range values {
			if !writePrettyLimit(&b, v, 1, rawLimit) {
				cut = true
				break
			}
			if i < len(values)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		if !cut {
			b.WriteString("]")
		}
	} else if len(values) == 1 {
		cut = !writePrettyLimit(&b, values[0], 0, rawLimit)
	}
	lines := strings.Split(b.String(), "\n")
	if many && !cut {
		for i := 1; i < len(lines)-1; i++ {
			lines[i] = "  " + lines[i]
		}
	}
	if cut {
		lines = append(lines, "… cut at 4 MB: o-json get prints it all")
	}
	return lines
}

func linesSource(lines []string) rowSource {
	return rowSource{
		n: len(lines), noun: "line",
		columns: []ochazuke.Column{{Title: "json"}},
		cells:   func(i int) []string { return []string{lines[i]} },
		data:    func(i int) any { return lines[i] },
		label:   func(i int) string { return lines[i] },
	}
}

// nodeAt is the node a data or detail row instance stands for.
func nodeAt(inst *ochazuke.Instance) *Node {
	if inst == nil {
		return nil
	}
	n, _ := inst.Data.(*Node)
	return n
}

// state is the viewer's state as the viewer.state verb answers it.
func (a *App) state() map[string]any {
	out := map[string]any{"path": a.Path, "loading": a.Loading, "query": a.Query, "kind": a.Kind, "raw": a.Raw}
	if a.LoadErr != nil {
		out["error"] = a.LoadErr.Error()
	}
	if a.shown != nil {
		out["shape"] = a.shown.Path
	}
	if a.Doc != nil {
		out["size"] = a.Doc.Size
	}
	out["row"] = a.data.Selected()
	if a.editable() {
		out["view"] = a.shownName()
		out["unsaved"] = a.unsaved()
	}
	if len(a.rs.patches) > 0 {
		out["valueEdits"] = len(a.rs.patches)
		if o := a.orphans(); len(o) > 0 {
			out["orphaned"] = len(o)
		}
	}
	return out
}
