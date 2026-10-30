package main

import (
	"fmt"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	// qc is the query line's path completion (querycomplete.go).
	qc completion

	// The structure tree's filters: Query keeps shapes whose key or path
	// contains it (case-insensitive) with their ancestors; Kind keeps
	// shapes that hold values of that type.
	Query string
	Kind  Kind
	// Raw shows the selected shape's values as indented JSON instead of a
	// table or fields.
	Raw bool
	// viewByPath remembers the data lens independently for each shape path.
	viewByPath map[string]string
	viewName   string
	// folds is the JSON view's folded containers (document paths) per
	// shown structure path.
	folds map[string]map[string]bool
	// sr is the search line's type and hits, overlay the view over the
	// structure's own (search.go): results, jq or "".
	sr searchState
	// editing is EDIT (edit.go), on the structure pane when editTree.
	editing, editTree bool
	// raw is the open RAW edit (raw.go), nil when none.
	raw *rawSession
	// sort is s ic: the data pane's table sorted (operate.go).
	sort *tableSort
	// tuios runs the tuios command and getenv reads the environment
	// (RAW's pane, rawpane.go); tests replace both. later is commands a
	// verb leaves for the loop (takeLater).
	tuios   tuiosRunner
	getenv  func(string) string
	later   []tea.Cmd
	overlay string

	// Clipboard copies text to the system clipboard; nil when there is
	// none (ochazuke then sends OSC 52). Only ochazuke's registers call it
	// (Registers.Copy, registerClipboard), so the clipboard setting and the
	// "+ register decide which yanks reach it.
	Clipboard func(string) error
	// Paste reads the system clipboard (viewer.paste); nil when there is
	// no tool for it.
	Paste func() (string, error)
	// Stdin is what a "-" path reads.
	Stdin interface{ Read([]byte) (int, error) }

	treeWidth int // the structure tree's natural width

	shown *Shape // the shape the data panel shows
	lit   *Shape // the plain-value shape highlighted in it (follow.go), nil for none
	// litPath is the path last highlighted (kept across a show of the
	// same table), so lighting it again does not move the cursor.
	litPath string
	data    *rowsView
	tab     *Table     // the data panel's table, when it shows one
	fields  *Node      // the data panel's object, when it shows fields
	lines   []dataLine // the data panel's FLAT or JSON lines (lines.go)
	scalar  *Node      // the data panel's single scalar

	detail *rowsView
	// detailTab is the detail's one-row table of an object (object.view
	// row), nil while it shows labeled rows.
	detailTab *Table
	// objectView is the object.view the data pane last showed with.
	objectView string
	// flatDrawn is the FLAT look the lines were last built with (flat.go).
	flatDrawn flatLook
	trail     []*Node // the detail modal's drill path, innermost last

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
	rb       *rebuilder   // rebuilds it off the loop once a rebuild is slow
	carry    *rowCarry    // the data pane's row and column the next show keeps (T)

	vedit *valueEdit // the data pane's open inline input (values.go)
	// followKey is the column the next show puts the cursor on: the key a
	// rename or an add on a table row made (columns.go).
	followKey string
	// queued are the value edits made while a rebuild ran off the loop,
	// under rebuild.value-edit queue: applied when it lands (values.go).
	queued []queuedEdit

	q queryState // the live jq query (query.go)

	hover hoverState // the walk into the data pane's sub-tables (hover.go)

	jqp jqPanel // the live jq panel (jqpanel.go)

	mapv mappingCache // the Mapping view's values (views.go)
}

// New builds the viewer on a fresh runtime: the page with the structure
// tree and the data panel, the detail modal, the search prompt, the nouns,
// verbs and bindings. It starts empty ("[no file]"); Load,
// viewer.open or viewer.paste fills it.
func New() (*App, error) {
	r := ochazuke.NewRuntime()
	a := &App{r: r, viewByPath: make(map[string]string), folds: make(map[string]map[string]bool)}
	a.m = ochazuke.NewModel(r)
	a.declareModeTones()
	a.declareFlatTones()
	r.Settings().Observe(settingDeclaredModes, func(string, any) { a.syncDeclaredMode() })
	a.m.Title = appName()
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
	a.registerClipboard()
	a.registerVerbs()
	a.registerLineVerbs()
	a.registerPaneVerbs()
	if err := a.registerQuery(); err != nil {
		return nil, err
	}
	if err := a.registerSearch(); err != nil {
		return nil, err
	}
	if err := a.registerEdit(); err != nil {
		return nil, err
	}
	if err := a.registerRaw(); err != nil {
		return nil, err
	}
	a.registerOperate()
	if err := a.registerQueryMode(); err != nil {
		return nil, err
	}
	a.registerQueryFunctions()
	// The rebuilder's jobs are its instance's (a noun of its own, with no
	// verbs, rows or stop).
	a.rb = &rebuilder{a: a}
	r.Define(rebuildID)
	r.Instance(rebuildID, ochazuke.Kind(rebuildID)).Component = a.rb
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
	if err := declareHoverSettings(r); err != nil {
		return nil, err
	}
	if err := declareReshapeSettings(r); err != nil {
		return nil, err
	}
	if err := declareJQPanelSettings(r); err != nil {
		return nil, err
	}
	if err := declareFlatSettings(r); err != nil {
		return nil, err
	}
	if err := declareFollowSettings(r); err != nil {
		return nil, err
	}
	page, err := r.AddPage(pageID, appName(), a.render, ochazuke.Focus(treeID))
	if err != nil {
		return nil, err
	}
	page.OnOpen = func(*ochazuke.Runtime, string) { a.openedViewer() }
	if err := a.registerRestructure(); err != nil {
		return nil, err
	}
	a.registerValueVerbs()
	a.registerReshapeVerbs()
	a.registerJQPanel()
	a.registerHover()
	a.placeTree(pageID)
	if err := a.bind(); err != nil {
		return nil, err
	}
	if err := r.OpenPage(pageID); err != nil {
		return nil, err
	}
	a.syncDeclaredMode()
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
	a.viewByPath = make(map[string]string)
	a.folds = make(map[string]map[string]bool)
	a.viewName = ""
	a.Raw = false
	a.forgetQuery()
	a.overlay, a.sr.key = "", ""
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
	if err := a.sync(); err != nil {
		return err
	}
	a.syncDeclaredMode()
	return nil
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
	return declareOwn(r, ochazuke.Setting{
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
		// ochazuke's search.case: smart ignores case unless the query
		// holds a capital.
		m := a.r.CaseFor(a.Query)
		return m.Contains(s.Key, a.Query) || m.Contains(s.Path, a.Query)
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
	if a.viewName == viewFlat && a.shown != nil && a.lines != nil {
		if look := a.flatLook(); look != a.flatDrawn {
			// flat.depth-colors, flat.pad-indexes or the theme changed:
			// draw the same lines again, the cursor kept.
			a.flatDrawn = look
			a.lines = flatLines(a.shown.Values(), look)
			if err := a.data.set(linesSource(a.lines), a.data.Selected()); err != nil {
				return err
			}
		}
	}
	if v := a.r.Settings().String(settingObjectView); v != a.objectView {
		// object.view changed (the Settings page, the palette): show the
		// same path again in the other form, the cursor kept.
		a.objectView = v
		if a.shown != nil && (a.fields != nil || a.tab != nil && !a.tab.Items) {
			a.carry = &rowCarry{sel: max(0, a.data.Selected())}
			a.shown = nil
		}
	}
	if a.overlay == overlayResults && a.sr.view != nil {
		a.note = ""
		if a.shown == a.sr.view {
			return nil
		}
		return a.show(a.sr.view)
	}
	if a.mappingShowing() {
		a.note = ""
		if v := a.mappingView(); v != a.shown {
			return a.show(v)
		}
		return nil
	}
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
			a.shown, a.tab, a.fields, a.lines, a.scalar, a.lit, a.note = nil, nil, nil, nil, nil, nil, note
			return a.data.set(rowSource{}, 0)
		}
		a.note = ""
		return a.display(s, false)
	}
	a.note = ""
	return a.display(a.selectedShape(), false)
}

// show fills the data panel with a shape's values in the view the
// settings prefer (views.go): one object as one row (object.view row) or
// labeled fields, one array or many values as a table; one scalar as is;
// with Raw, the values as indented JSON lines.
//
// The same path shown again (a value edit, an undo) keeps the selected
// row and column.
func (a *App) show(s *Shape) error {
	sel, col := 0, 0
	if prev := a.shown; prev != nil && prev.Path == s.Path && a.lines == nil && !a.Raw {
		sel, col = a.data.Selected(), a.data.list.Column()
	}
	if c := a.carry; c != nil {
		sel, col, a.carry = c.sel, c.col, nil
	}
	var prevCols []TableColumn
	if prev := a.shown; prev != nil && prev.Path == s.Path && a.tab != nil && a.r.Settings().Bool(settingStableColumns) {
		prevCols = a.tab.Columns
	}
	follow := a.followKey
	a.followKey = ""
	a.shown, a.tab, a.fields, a.lines, a.scalar, a.lit = s, nil, nil, nil, nil, nil
	if s == a.sr.view {
		// The Results view (search.go): one line per hit.
		a.viewName, a.Raw, a.lines = viewResults, false, a.sr.hits
		return a.data.set(linesSource(a.lines), a.sr.cur)
	}
	values := s.Values()
	view := a.viewByPath[s.Path]
	if !viewApplies(view, values) {
		view = defaultDataView(values, a.r.Settings().String(settingObjectView))
	}
	a.viewName = view
	a.Raw = view == viewJSON
	set := func(src rowSource) error {
		if err := a.data.set(src, sel); err != nil {
			return err
		}
		// Another table starts at its first column (a highlight moves it
		// on, follow.go); the same one keeps the cursor's column.
		a.data.list.SetColumn(col)
		return nil
	}
	switch {
	case view == viewJSON:
		a.lines = jsonLines(values, len(values) != 1, a.jsonFolded)
		return a.data.set(linesSource(a.lines), 0)
	case view == viewFlat:
		a.flatDrawn = a.flatLook()
		a.lines = flatLines(values, a.flatDrawn)
		return a.data.set(linesSource(a.lines), 0)
	case len(values) == 1 && !values[0].IsContainer():
		a.scalar = values[0]
		return a.data.set(rowSource{}, 0)
	}
	// The view the settings prefer among those that apply (views.go).
	out := pickView(dataViews, values, view).build(s.Path, values)
	if out.tab == nil {
		a.fields = out.fields
		return set(out.src)
	}
	a.tab = out.tab
	a.tab.Rows = a.sortedRows(a.tab) // s ic (operate.go): a view of the rows
	if prevCols != nil {
		// The same table again (a value edit, an undo): its columns
		// keep their order (columns.go).
		a.tab.Columns = stableColumns(prevCols, a.tab.Columns, a.columnAnchors())
	}
	if follow != "" {
		if j := slices.IndexFunc(a.tab.Columns, func(c TableColumn) bool { return !c.Self && c.Name == follow }); j >= 0 {
			col = a.tab.ListColumn(j)
		}
	}
	return set(tableSource(a.tab))
}

// rowCarry is a data pane position carried to the next show: T flips the
// view and keeps the cursor's row and column (the tree keeps its path).
type rowCarry struct{ sel, col int }

// tableSource is a table as rows: "#" (for items, Table.Items) then the
// columns, optional titles ending in "?", widths fixed from the first
// rows. A nested value's cell is its size (Summary).
func tableSource(t *Table) rowSource {
	var cols []ochazuke.Column
	var natural []int
	if t.Items {
		cols = append(cols, ochazuke.Column{Title: "#", Align: ochazuke.AlignRight, Part: ochazuke.PartRowNumber})
		natural = append(natural, len(strconv.Itoa(max(0, len(t.Rows)-1))))
	}
	for j, c := range t.Columns {
		title := c.Title()
		if c.Optional {
			title += "?"
		}
		cols = append(cols, ochazuke.Column{Title: title, Priority: 1})
		natural = append(natural, columnWidth(title, len(t.Rows), func(i int) string { return oneLine(t.SummaryText(t.Rows[i], j)) }))
	}
	return rowSource{
		n: len(t.Rows), columns: cols, natural: natural, noun: "item",
		cells: func(i int) []string {
			out := make([]string, 0, len(t.Columns)+1)
			if t.Items {
				n := i
				if r := t.Rows[i]; r.Parent != nil && r.Parent.Kind == KArray {
					n = r.Index // the item's own index, also in a sorted view
				}
				out = append(out, strconv.Itoa(n))
			}
			for j := range t.Columns {
				out = append(out, oneLine(t.SummaryText(t.Rows[i], j)))
			}
			return out
		},
		data:  func(i int) any { return t.Rows[i] },
		label: func(i int) string { return t.Rows[i].Path() },
		full: func(i, col int) string {
			if j := t.ColumnOf(col); j >= 0 {
				return oneLine(Full(t.Cell(t.Rows[i], j)))
			}
			return ""
		},
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

// linesSource is the raw JSON as rows of the line noun. It has no
// columns, so no header: a list of lines whose labels are padded to the
// widest line, so the cursor is one even bar on every line.
func linesSource(lines []dataLine) rowSource {
	w := 0
	for i := range lines {
		w = max(w, ansi.StringWidth(lines[i].drawn()))
	}
	return rowSource{
		n: len(lines), noun: "line",
		cells: func(i int) []string { return []string{lines[i].text} },
		data:  func(i int) any { return &lines[i] },
		label: func(i int) string { return pad(lines[i].drawn(), w) },
		tones: func(i int) []ochazuke.ToneSpan { return lines[i].tones },
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
