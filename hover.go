package main

// Nested data in tables (setting table.nested, default hover): with the
// data pane's cursor on a cell holding an object or an array, a small
// sub-table of its data opens directly under the row, with no key; moving
// away closes it. Each sub-table is indented a fixed step from the level
// above and laid out with its own column widths across the rest of the
// pane, never aligned to the parent's columns:
//
//	object            a line of its keys, a line of its values
//	array of objects  a line of the columns (NewTable), a line per item
//	array of scalars  one line of the values, joined with " · "
//	other arrays      a line per item: # and the value
//
// The sub-table's data lines are cursor stops: rows.move (j/k, counts)
// walks from the parent row down into them and out the bottom to the next
// row, and rows.column (h/l, bound ahead of the list's column verb while
// the cursor is in a sub-table) moves along one. A nested cell under the
// cursor in a sub-table opens its own sub-table one step deeper, so only
// the chain under the cursor is open; anything deeper shows as {N keys}
// or [N]. The sub-tables are drawn into the pane: the data pane's table
// keeps its rows (the windowed list is untouched), and the rows above
// scroll up under the header to keep the cursor's line in view.
//
// table.nested summary is the view as it was: the cell shows a summary
// and the detail (enter) drills.
//
// From the throwaway prototype branch proto/nested-tables (proto_hover.go
// and the cell-to-node mapping of proto_state.go), re-cut onto the
// setting.

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

const (
	settingNested = "table.nested"
	nestedHover   = "hover"
	nestedSummary = "summary"

	settingHoverRows = "table.nested-rows"
)

const (
	hoverIndent = 4  // the first level's columns from the row's left edge
	hoverStep   = 2  // each deeper level's further indent
	hoverColMax = 24 // a column's widest cell before it is truncated
	hoverGap    = 2  // spaces between columns
)

func declareHoverSettings(r *ochazuke.Runtime) error {
	return declareOwn(r, ochazuke.Setting{
		Name: settingNested, Type: ochazuke.SettingString, Default: nestedHover, Enum: []string{nestedHover, nestedSummary},
		Description: "A data pane cell holding an object or an array: hover opens a sub-table of it under the row while the cursor rests on it (j/k walk into its lines, h/l along them); summary shows only its summary ({N keys}, [N]) and enter drills in the detail",
	}, ochazuke.Setting{
		Name: settingHoverRows, Type: ochazuke.SettingInteger, Default: 8,
		Description: "How many data lines the first hover sub-table shows before it scrolls (each deeper level two fewer, at least 3)",
	})
}

// hoverPos is the cursor in one sub-table level: a data line and a column.
type hoverPos struct{ row, col int }

// hoverState is the walk into the open sub-tables and the data pane cell
// it started from: a move there (G, a search, a click, a new shape) drops
// it.
type hoverState struct {
	path     []hoverPos
	root     string // the path of the cell's value (nodes are rebuilt by an edit)
	sel, col int
	cursorY  int // the cursor's line in the pane as last drawn, -1 on the row
}

// hoverGrid is one sub-table: a header (nil for a scalar array's single
// line), its data lines, per cell the node it shows (nil when the cell is
// not a value of its own: #, a joined scalar list), and per line the value
// the line stands for (an item, the object, an element, the joined array).
type hoverGrid struct {
	header []string
	rows   [][]string
	nodes  [][]*Node
	lines  []*Node
	keys   []string // an array of objects: each column's field ("" for # and the item's own value)
}

// hoverText is a value in a sub-table: the data for a scalar, a
// placeholder for a nested value.
func hoverText(n *Node) string {
	switch {
	case n == nil:
		return ""
	case n.Kind == KObject:
		if len(n.Children) == 1 {
			return "{1 key}"
		}
		return "{" + strconv.Itoa(len(n.Children)) + " keys}"
	case n.Kind == KArray:
		return "[" + strconv.Itoa(len(n.Children)) + "]"
	}
	return oneLine(Text(n, 200))
}

// gridOf lays a nested value out as a sub-table, by kind.
func gridOf(n *Node) hoverGrid {
	var g hoverGrid
	switch {
	case n.Kind == KArray && slices.ContainsFunc(n.Children, func(c *Node) bool { return c.Kind == KObject }):
		t := NewTable(n.Path(), []*Node{n}, false)
		g.header, g.keys = []string{"#"}, []string{""}
		for _, c := range t.Columns {
			g.header = append(g.header, c.Title())
			key := c.Name
			if c.Self {
				key = ""
			}
			g.keys = append(g.keys, key)
		}
		for i, row := range t.Rows {
			cells := []string{strconv.Itoa(i)}
			ns := []*Node{nil}
			for j := range t.Columns {
				c := t.Cell(row, j)
				cells, ns = append(cells, hoverText(c)), append(ns, c)
			}
			g.rows, g.nodes, g.lines = append(g.rows, cells), append(g.nodes, ns), append(g.lines, row)
		}
	case n.Kind == KObject:
		var cells []string
		var ns []*Node
		for _, c := range n.Children {
			g.header = append(g.header, c.Key)
			cells, ns = append(cells, hoverText(c)), append(ns, c)
		}
		g.rows, g.nodes, g.lines = [][]string{cells}, [][]*Node{ns}, []*Node{n}
	case !slices.ContainsFunc(n.Children, func(c *Node) bool { return c.IsContainer() }):
		var vals []string
		for _, c := range n.Children {
			vals = append(vals, hoverText(c))
		}
		g.rows, g.nodes, g.lines = [][]string{{strings.Join(vals, " · ")}}, [][]*Node{{nil}}, []*Node{n}
	default:
		g.header = []string{"#", "value"}
		for i, c := range n.Children {
			g.rows = append(g.rows, []string{strconv.Itoa(i), hoverText(c)})
			g.nodes = append(g.nodes, []*Node{nil, c})
			g.lines = append(g.lines, c)
		}
	}
	return g
}

// cell is the node at p, nil off the grid or on a cell that is no value.
func (g hoverGrid) cell(p hoverPos) *Node {
	if p.row < 0 || p.row >= len(g.nodes) || p.col < 0 || p.col >= len(g.nodes[p.row]) {
		return nil
	}
	return g.nodes[p.row][p.col]
}

// node is the nested value at p, the one a deeper level opens on.
func (g hoverGrid) node(p hoverPos) *Node {
	if n := g.cell(p); nested(n) {
		return n
	}
	return nil
}

// value is what the cursor at p stands for: the cell's value, else the
// line's (the item on #, the array on a joined list).
func (g hoverGrid) value(p hoverPos) *Node {
	if n := g.cell(p); n != nil {
		return n
	}
	if p.row >= 0 && p.row < len(g.lines) {
		return g.lines[p.row]
	}
	return nil
}

// at is the value the cursor at p edits, as valueOf answers on a table
// row: the cell's value, a cell the item lacks (its parent and key), the
// line's value on # or a joined list.
func (g hoverGrid) at(p hoverPos) (valueAt, bool) {
	if n := g.cell(p); n != nil {
		return valueAt{node: n, parent: n.Parent, key: n.Key}, true
	}
	if p.row < 0 || p.row >= len(g.lines) {
		return valueAt{}, false
	}
	line := g.lines[p.row]
	if p.col >= 0 && p.col < len(g.keys) && g.keys[p.col] != "" && line.Kind == KObject {
		return valueAt{parent: line, key: g.keys[p.col]}, true
	}
	return valueAt{node: line, parent: line.Parent, key: line.Key}, true
}

func nested(n *Node) bool { return n != nil && n.IsContainer() && len(n.Children) > 0 }

// hoverOn reports the hover sub-tables in use for the data pane: the
// setting, a table or fields shown (not the raw JSON).
func (a *App) hoverOn() bool {
	return a.r.Settings().String(settingNested) == nestedHover && !a.Raw && (a.tab != nil || a.fields != nil)
}

// inData reports a focus on the data pane's table (the list or a row).
func (a *App) inData(focus string) bool {
	inst := a.r.Instances[focus]
	return inst != nil && a.data != nil && (inst.ID == a.data.id || inst.Parent == a.data.id)
}

// dataCell is the node under the data pane's cursor: the cell's value
// (the row on #), the field on a fields view.
func (a *App) dataCell() *Node {
	sel := a.data.Selected()
	if sel < 0 {
		return nil
	}
	col := a.data.list.Column()
	switch {
	case a.tab != nil:
		if sel >= len(a.tab.Rows) {
			return nil
		}
		if col <= 0 {
			return a.tab.Rows[sel]
		}
		if col-1 < len(a.tab.Columns) {
			return a.tab.Cell(a.tab.Rows[sel], col-1)
		}
	case a.fields != nil:
		if sel < len(a.fields.Children) {
			return a.fields.Children[sel]
		}
	}
	return nil
}

// hoverCell is the nested value under the data pane's cursor that opens
// a sub-table, or nil. The # column of a table is the row itself, which
// the row already shows.
func (a *App) hoverCell() *Node {
	if !a.hoverOn() || a.tab != nil && a.data.list.Column() == 0 {
		return nil
	}
	if n := a.dataCell(); nested(n) {
		return n
	}
	return nil
}

// hoverCheck drops the walk when the data pane's cursor moved under it
// (G, a search, a mouse click, another shape), so the path always belongs
// to the cell.
func (a *App) hoverCheck() {
	h := &a.hover
	if len(h.path) > 0 && (a.data.Selected() != h.sel || a.data.list.Column() != h.col || pathOf(a.hoverCell()) != h.root) {
		h.path = nil
	}
}

func pathOf(n *Node) string {
	if n == nil {
		return ""
	}
	return n.Path()
}

func (a *App) hoverMark() {
	a.hover.sel, a.hover.col, a.hover.root = a.data.Selected(), a.data.list.Column(), pathOf(a.hoverCell())
}

// hoverIn reports the cursor on a sub-table's line (not the row).
func (a *App) hoverIn() bool {
	a.hoverCheck()
	return len(a.hover.path) > 0
}

// hoverLevels are the open sub-tables: level 0 opens under the data
// pane's row, level k under level k-1's cursor line when its cell nests.
func (a *App) hoverLevels() []hoverGrid {
	a.hoverCheck()
	n := a.hoverCell()
	if n == nil {
		return nil
	}
	levels := []hoverGrid{gridOf(n)}
	for k := range a.hover.path {
		// An edit can shorten a level (a delete): keep the cursor on it.
		g, pos := levels[k], &a.hover.path[k]
		pos.row = min(max(pos.row, 0), len(g.rows)-1)
		pos.col = min(max(pos.col, 0), len(g.rows[pos.row])-1)
		next := g.node(*pos)
		if next == nil {
			a.hover.path = a.hover.path[:k+1]
			break
		}
		levels = append(levels, gridOf(next))
	}
	return levels
}

// hoverValueAt is the value under the cursor in a sub-table of the data
// pane (valueOf's answer there), false on a row.
func (a *App) hoverValueAt() (valueAt, bool) {
	if !a.hoverIn() {
		return valueAt{}, false
	}
	levels := a.hoverLevels()
	d := len(a.hover.path)
	if d == 0 || d > len(levels) {
		return valueAt{}, false
	}
	return levels[d-1].at(a.hover.path[d-1])
}

// hoverValue is the value under the cursor in a sub-table of the data
// pane, nil when the cursor is on a row (or inst is not the data pane's).
func (a *App) hoverValue(inst *ochazuke.Instance) *Node {
	if inst == nil || a.rowsOf(inst) != a.data || !a.hoverIn() {
		return nil
	}
	levels := a.hoverLevels()
	d := len(a.hover.path)
	if d == 0 || d > len(levels) {
		return nil
	}
	return levels[d-1].value(a.hover.path[d-1])
}

// hoverDown walks steps lines down: into an open sub-table, along its
// lines, out its bottom to the next line above, and from a row to the
// next row.
func (a *App) hoverDown(steps int) error {
	for range steps {
		levels := a.hoverLevels()
		h := &a.hover
		d := len(h.path)
		// The cell under the cursor has a sub-table open below: step in.
		if d < len(levels) {
			h.path = append(h.path, hoverPos{})
			a.hoverMark()
			continue
		}
		// Else the next line, out the bottom of each level in turn.
		for {
			d = len(h.path)
			if d == 0 {
				if err := a.data.move(1); err != nil {
					return err
				}
				break
			}
			cur := &h.path[d-1]
			if cur.row < len(levels[d-1].rows)-1 {
				cur.row++
				break
			}
			h.path = h.path[:d-1]
		}
		a.hoverMark()
	}
	return nil
}

// hoverUp walks steps lines up: along a sub-table, out its top to the
// line it opened from, and from a row to the row above.
func (a *App) hoverUp(steps int) error {
	for range steps {
		a.hoverCheck()
		h := &a.hover
		d := len(h.path)
		switch {
		case d == 0:
			if err := a.data.move(-1); err != nil {
				return err
			}
		case h.path[d-1].row > 0:
			h.path[d-1].row--
		default:
			h.path = h.path[:d-1]
		}
		a.hoverMark()
	}
	return nil
}

// hoverColumn moves along the sub-table line under the cursor: by delta
// cells, to the first or last, or to cell index (1 the first) when
// index > 0.
func (a *App) hoverColumn(delta int, to string, index int) error {
	levels := a.hoverLevels()
	h := &a.hover
	d := len(h.path)
	if d == 0 || d > len(levels) {
		return errors.New("not in a sub-table")
	}
	g := levels[d-1]
	cur := &h.path[d-1]
	last := len(g.rows[cur.row]) - 1
	switch {
	case index > 0:
		cur.col = min(index-1, last)
	case to == "first":
		cur.col = 0
	case to == "last":
		cur.col = last
	case to == "":
		cur.col = min(max(cur.col+delta, 0), last)
	default:
		return errors.New("to is first or last")
	}
	return nil
}

// hoverLimit is how many data lines level k shows before it scrolls.
func (a *App) hoverLimit(k int) int {
	return max(a.r.Settings().Int(settingHoverRows)-2*k, 3)
}

// hoverBlock draws level k (and the levels open below it) at width w.
// cursorAt is the index of the cursor's line in the block, -1 for none.
func (a *App) hoverBlock(s ochazuke.Styles, levels []hoverGrid, k, w int) (lines []string, cursorAt int) {
	g := levels[k]
	path := a.hover.path
	indent := hoverIndent + k*hoverStep
	ind := strings.Repeat(" ", indent)
	inner := max(10, w-indent)
	var pos *hoverPos
	if k < len(path) {
		pos = &path[k]
	}
	deepest := pos != nil && k == len(path)-1
	cursorAt = -1

	ncol := len(g.header)
	for _, r := range g.rows {
		ncol = max(ncol, len(r))
	}
	widths := make([]int, ncol)
	for j := range ncol {
		if j < len(g.header) {
			widths[j] = min(ansi.StringWidth(g.header[j]), hoverColMax)
		}
		for _, r := range g.rows {
			if j < len(r) {
				widths[j] = max(widths[j], min(ansi.StringWidth(r[j]), hoverColMax))
			}
		}
	}
	if g.header == nil {
		widths[0] = min(inner, ansi.StringWidth(g.rows[0][0])) // a joined scalar list is one cell
	}
	// Scroll the columns so the cursor's column shows.
	first := 0
	if pos != nil {
		for first < pos.col {
			used := 0
			for j := first; j <= pos.col; j++ {
				used += widths[j] + hoverGap
			}
			if used <= inner {
				break
			}
			first++
		}
	}
	line := func(cells []string, sel int, style func(string) string) string {
		var b strings.Builder
		if first > 0 {
			b.WriteString(s.Muted.Render("‹ "))
		}
		for j := first; j < len(cells); j++ {
			if j > first {
				b.WriteString(strings.Repeat(" ", hoverGap))
			}
			c := ansi.Truncate(cells[j], widths[j], "…")
			c += strings.Repeat(" ", widths[j]-ansi.StringWidth(c))
			if j == sel {
				c = s.Accent.Reverse(true).Render(c)
			} else {
				c = style(c)
			}
			b.WriteString(c)
		}
		return ind + ansi.Truncate(b.String(), inner, "…")
	}
	plain := func(c string) string { return c }
	muted := func(c string) string { return s.Muted.Render(c) }
	accent := func(c string) string { return s.Accent.Render(c) }

	if g.header != nil {
		lines = append(lines, line(g.header, -1, muted))
	}
	limit := a.hoverLimit(k)
	off := 0
	if pos != nil && pos.row >= limit-1 {
		off = pos.row - (limit - 2)
	}
	if off > 0 {
		lines = append(lines, s.Muted.Render(ind+fmt.Sprintf("… %d above", off)))
	}
	end := min(len(g.rows), off+limit)
	if end < len(g.rows) && end-off > 1 {
		end--
	}
	for i := off; i < end; i++ {
		sel, style := -1, plain
		if pos != nil && pos.row == i {
			if deepest {
				sel, cursorAt = pos.col, len(lines)
			} else {
				style = accent // on the way to the cursor
			}
		}
		lines = append(lines, line(g.rows[i], sel, style))
		if pos != nil && pos.row == i && k+1 < len(levels) {
			sub, at := a.hoverBlock(s, levels, k+1, w)
			if at >= 0 {
				cursorAt = len(lines) + at
			}
			lines = append(lines, sub...)
		}
	}
	if more := len(g.rows) - end; more > 0 {
		lines = append(lines, s.Muted.Render(ind+fmt.Sprintf("… %d more", more)))
	}
	return lines, cursorAt
}

// hoverView draws the table with the hovered cell's sub-tables under the
// cursor's row. The rows above scroll up under the header to keep the
// cursor's line and two lines below it on screen.
func (a *App) hoverView(w, h int) string {
	body := a.data.view(w, h)
	a.hover.cursorY = -1
	if !a.inData(a.r.Focus) { // the inline value input is the pane's child too
		return body
	}
	levels := a.hoverLevels()
	if len(levels) == 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	y := 1 + a.data.Selected() - a.data.off // below the header line
	if y < 1 || y >= h {
		return body
	}
	s := ochazuke.NewStyles(*a.m.Appearance)
	sub, at := a.hoverBlock(s, levels, 0, w)
	cursor := y
	if at >= 0 {
		// The cursor is in a sub-table: the row is an ancestor now.
		lines[y] = s.Accent.Render(ansi.Strip(lines[y]))
		cursor = y + 1 + at
	}
	out := append(slices.Clone(lines[:y+1]), sub...)
	out = append(out, lines[y+1:]...)
	// Keep the header, and the cursor's line with two lines below it.
	if over := min(cursor+3, len(out)) - h; over > 0 {
		drop := min(over, cursor-1)
		out = append(out[:1], out[1+drop:]...)
		cursor -= drop
	}
	if len(out) > h {
		out = out[:h]
	}
	if at >= 0 {
		a.hover.cursorY = cursor // where a value input on the line is drawn
	}
	return strings.Join(out, "\n")
}

// registerHover declares rows.column, the walk along a sub-table's line:
// available on the data pane while the cursor is in a sub-table, so its
// keys (bound ahead of the list's column verb) pass on to that verb
// elsewhere. The walk down and up is rows.move's.
func (a *App) registerHover() {
	r := a.r
	r.DefineGuard("rows-in-sub-table", func(_ *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.inData(ev.Focus) && a.hoverIn()
	})
	// The list's own column verbs (choose, resize, reset, pin) act on the
	// row's columns, so a sub-table does not offer them: o-json adds this
	// guard to each verb beside its own, and keys, hints, the palette and
	// Execute all drop it there.
	r.DefineGuard("rows-on-row", func(_ *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return !a.inData(ev.Focus) || !a.hoverIn()
	})
	for _, verb := range []string{"ochazuke.list.choose-column", "ochazuke.list.resize-column", "ochazuke.list.reset-columns", "ochazuke.list.toggle-pin-column"} {
		if err := r.AddVerbAvailability(verb, "rows-on-row"); err != nil {
			panic(err) // ochazuke's own verbs: a rename is a bug here
		}
	}
	r.Define("rows").
		Verb("column", func(c ochazuke.Ctx) ochazuke.Result {
			steps := max(1, ochazuke.IntArg(c.Args, "steps", 1))
			if err := a.hoverColumn(ochazuke.IntArg(c.Args, "delta", 1)*steps, ochazuke.StringArg(c.Args, "to", ""), ochazuke.IntArg(c.Args, "index", 0)); err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(a.hover.path[len(a.hover.path)-1].col)
		}, "Move along the line of the data pane's sub-table under the cursor (table.nested hover): by delta cells, to the first or last, or to cell index").Motion().When("rows-in-sub-table")
	key := r.CanonicalKey("rows.column")
	op := r.Operations[key]
	op.Parameters = map[string]ochazuke.Parameter{
		"delta": {Type: "integer", Description: "cells to move, negative to the left (default 1)"},
		"steps": {Type: "integer", Description: "repeat count (default 1)"},
		"to":    {Type: "string", Enum: []string{"first", "last"}, Description: "the first or last cell instead of delta"},
		"index": {Type: "integer", Description: "the cell number, 1 the first (a count: 3|), instead of delta"},
	}
	r.Operations[key] = op
	setHistory(r, ochazuke.HistoryAction, "rows.column")
}
