package main

import (
	"slices"
	"strconv"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// A rowsView shows n rows of a source through an ochazuke list (a table)
// that holds only the rows on screen. ochazuke's List keeps one instance
// per row and measures every row on every frame, so a 100,000-row array
// as list items would stall each key (and replacing rows costs a scan of
// the instances per removed row). Here the list's items are the window:
// row IDs are positions in the window (<list>/0, <list>/1, ...), so a
// scroll replaces labels, not instances, and a frame costs a screen. The
// view owns the selected row and the scroll offset over the whole source;
// the motions are its verbs (rows.move), not list.move.

// rowSource is what a rowsView shows.
type rowSource struct {
	n       int
	columns []ochazuke.Column
	natural []int // each column's width over the first rows (columnWidth)
	cells   func(i int) []string
	noun    string          // the row instances' noun
	data    func(i int) any // a row instance's Data
	label   func(i int) string
}

// rowsView is a windowed table over a rowSource.
type rowsView struct {
	r      *ochazuke.Runtime
	list   *ochazuke.List
	id     string
	src    rowSource
	sel    int // the selected row of the source, -1 for none
	off    int // the first row on screen
	height int // rows on screen, from the last frame
}

// defaultRowsHeight is the window before the first frame measures it.
const defaultRowsHeight = 20

// scrolloff is the margin kept above and below the selected row.
const scrolloff = 2

func newRowsView(r *ochazuke.Runtime, id, parent string) (*rowsView, error) {
	list, err := r.AddList(id, nil, ochazuke.Parent(parent), ochazuke.NoSpill(), ochazuke.NoMarks(), ochazuke.Kind("rows"))
	if err != nil {
		return nil, err
	}
	return &rowsView{r: r, list: list, id: id, sel: -1, height: defaultRowsHeight}, nil
}

// set shows a new source with row sel selected (clamped).
func (v *rowsView) set(src rowSource, sel int) error {
	v.src = src
	v.off = 0
	v.list.SetColumns(src.columns...)
	v.sel = -1
	if src.n > 0 {
		v.sel = min(max(sel, 0), src.n-1)
	}
	v.follow()
	return v.refresh()
}

// Selected is the selected row of the source, -1 for none.
func (v *rowsView) Selected() int {
	v.pull()
	return v.sel
}

// pull takes a selection made through the list itself (list.select, a
// list.move that reached it) into the view.
func (v *rowsView) pull() {
	item, ok := v.list.Selected()
	if !ok || v.sel < 0 {
		return
	}
	if k, err := strconv.Atoi(item.ID[len(v.id)+1:]); err == nil && v.off+k < v.src.n {
		v.sel = v.off + k
	}
}

// move moves the selection by delta rows, clamped.
func (v *rowsView) move(delta int) error {
	v.pull()
	return v.to(v.sel + delta)
}

// to selects row i, clamped, scrolling to keep it on screen.
func (v *rowsView) to(i int) error {
	if v.src.n == 0 {
		return nil
	}
	v.sel = min(max(i, 0), v.src.n-1)
	v.follow()
	return v.refresh()
}

// follow scrolls so the selected row keeps the margin (shrunk in a short
// window, relaxed at the ends).
func (v *rowsView) follow() {
	h := max(1, v.height)
	margin := min(scrolloff, (h-1)/2)
	if v.sel >= 0 {
		if v.sel < v.off+margin {
			v.off = v.sel - margin
		}
		if v.sel > v.off+h-1-margin {
			v.off = v.sel - h + 1 + margin
		}
	}
	v.off = min(max(v.off, 0), max(0, v.src.n-h))
}

// refresh puts the window's rows in the list and selects the row.
func (v *rowsView) refresh() error {
	end := min(v.src.n, v.off+max(1, v.height))
	items := make([]ochazuke.ListItem, 0, end-v.off)
	for i := v.off; i < end; i++ {
		item := ochazuke.ListItem{ID: v.id + "/" + strconv.Itoa(i-v.off), Noun: v.src.noun, Cells: v.src.cells(i)}
		if v.src.label != nil {
			item.Label = v.src.label(i)
		}
		if v.src.data != nil {
			item.Data = v.src.data(i)
		}
		items = append(items, item)
	}
	if err := v.list.SetItems(items); err != nil {
		return err
	}
	if v.sel >= 0 {
		return v.list.Select(v.id + "/" + strconv.Itoa(v.sel-v.off))
	}
	return nil
}

// page is half a screen (half) or a screen less one row.
func (v *rowsView) page(half bool) int {
	if half {
		return max(1, v.height/2)
	}
	return max(1, v.height-1)
}

// view draws the table at width x height: the header, then the window.
// A new height re-windows the rows first.
func (v *rowsView) view(width, height int) string {
	body := height
	if len(v.src.columns) > 0 && height > 1 {
		body--
	}
	if body != v.height && body > 0 {
		v.pull()
		v.height = body
		v.follow()
		_ = v.refresh()
	}
	if len(v.src.natural) == len(v.src.columns) && len(v.src.columns) > 0 {
		v.list.SetColumns(fitColumns(v.src.columns, v.src.natural, width)...)
	}
	return v.list.View(width, height)
}

// tableGap is the space ochazuke's table puts between columns.
const tableGap = 2

// fitColumns fixes each column's width for this frame: its natural width
// when they all fit, else the widest columns narrow one cell at a time
// down to a floor (the title, at least minCellWidth), so every column
// shows before any drops; the table drops from the right after that.
// Widths come from the first rows, not the window, so they hold still
// while the rows scroll.
func fitColumns(cols []ochazuke.Column, natural []int, width int) []ochazuke.Column {
	out := slices.Clone(cols)
	widths := slices.Clone(natural)
	floor := make([]int, len(cols))
	for j, c := range cols {
		floor[j] = min(widths[j], max(ansi.StringWidth(c.Title), minCellWidth))
	}
	need := func() int {
		n := tableGap * (len(widths) - 1)
		for _, w := range widths {
			n += w
		}
		return n
	}
	for need() > width {
		widest := -1
		for j := range widths {
			if widths[j] > floor[j] && (widest < 0 || widths[j] > widths[widest]) {
				widest = j
			}
		}
		if widest < 0 {
			break
		}
		widths[widest]--
	}
	for j := range out {
		out[j].Width = widths[j]
	}
	// The last column takes the room left (at least its fitted width).
	last := len(out) - 1
	out[last].Width, out[last].Min = 0, widths[last]
	return out
}

// columnWidth is a fixed column width measured over the first rows, so
// the columns keep their widths while the window scrolls.
func columnWidth(title string, n int, cell func(i int) string) int {
	w := ansi.StringWidth(title)
	for i := 0; i < n && i < widthSample; i++ {
		w = max(w, ansi.StringWidth(cell(i)))
	}
	return min(max(w, 1), maxCellWidth)
}

// widthSample rows measure a column; a column is at most maxCellWidth.
const (
	widthSample  = 2000
	maxCellWidth = 32
	minCellWidth = 6
)
