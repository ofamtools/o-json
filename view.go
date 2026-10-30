package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// The page: a breadcrumb line, then two panels side by side (stacked
// below stackWidth columns): the structure tree on the left, the selected
// shape's data on the right. The panel holding the focus has the accent
// border. The divider follows the page's pane keys (> and < move it, =
// resets it, remembered per page) through PaneLayout and Divide: the
// default split below is the first argument, and the framework moves it.
//
// Layout.SplitFocusBlock is not used: it sizes the split from the whole
// viewport (BodySize less the page chrome) and stacks below 72 columns,
// while this renderer gets the page body, draws a breadcrumb line above
// the panels and gives the tree its natural width.

// stackWidth is the page width below which the panels stack.
const stackWidth = 60

func (a *App) render(r *ochazuke.Runtime, width, height int) ochazuke.Block {
	a.decorate()
	a.highlight()
	a.syncMarks()
	if !a.editedShown() && a.sideOf(r.Focus) == rsTreeID {
		_ = a.tree.Focus() // the edited structure is not shown (structure.editable went off)
	}
	if ve := a.vedit; ve != nil && ve.modal != nil && r.Instances[valueEditorID] == nil {
		a.vedit = nil // the value editor was closed another way: nothing changed
	}
	if a.vedit != nil && a.vedit.modal == nil && a.vedit.view != a.detail && r.Focus != valueInputID {
		// The focus left the inline input (a click, a page): its text is
		// the edit, as enter would make it.
		if err := a.accept(); err != nil {
			a.LoadErr = err
		}
	}
	if err := a.sync(); err != nil {
		a.LoadErr = err
	}
	a.syncDeclaredMode()
	s := ochazuke.NewStyles(*a.m.Appearance)
	l := a.r.PaneLayout(pageID, ochazuke.Layout{Width: width, Height: height, Styles: s})
	crumb := s.Muted.Render(ansi.Truncate(a.breadcrumb(), width, "…"))
	a.syncJQPanel()
	ph := a.splitHeight(height - 1)
	bh := max(0, height-1-ph)
	if bh < 3 || width < 8 {
		return ochazuke.Text(crumb)
	}
	// An empty tree (no document) holds the focus itself, not its list.
	treeFocused := a.tree.List().Focused() || a.rs.tree.List().Focused() || r.Focus == treeID || r.Focus == rsTreeID
	// Under a modal the page is drawn with the focus it opened over, so
	// the panel the modal came from keeps its border.
	dataFocused := a.data.list.Focused() || r.Focus == dataID || a.vedit != nil && (a.vedit.view == nil || a.vedit.view == a.data)
	var left, right ochazuke.Block
	var lw, lh, rw, rh int
	stacked := width < stackWidth
	only := a.paneOnly(treeFocused || a.r.Focus == jqFieldID, dataFocused)
	if only != "" {
		// ctrl+w o (viewer.only): one pane takes the page.
		stacked = false
		lw, lh, rw, rh = width, bh, width, bh
	} else if stacked {
		lw, lh = width, l.Divide(bh*2/5, bh, ochazuke.PaneMinHeight)
		rw, rh = width, bh-lh
	} else {
		// The tree takes its natural width (plus the panel's border and
		// padding), between 24 cells and two fifths of the page.
		natural := a.treeWidth
		if a.editedShown() {
			natural = a.editedWidth()
		}
		lw = l.Divide(min(max(24, natural+4), width*2/5), width-1, ochazuke.PaneMinWidth)
		lh, rw, rh = bh, width-lw-1, bh
	}
	// The query line is the structure pane's first row, above its tree.
	cw, ch := ochazuke.PanelContentSize(lw, lh)
	q, qh := a.queryLines(s, cw)
	if ch-qh < 2 {
		q, qh = ochazuke.Text(""), 0
	}
	inner := ochazuke.Text(a.treeBody(cw, ch-qh))
	if qh > 0 {
		inner = ochazuke.Block{Text: q.Text + "\n" + inner.Text, Cursor: q.Cursor}
	}
	left = l.PanelBlock(a.treeTitle(), inner, lw, lh, treeFocused || a.r.Focus == jqFieldID)
	cw, ch = ochazuke.PanelContentSize(rw, rh)
	right = l.PanelBlock(a.dataTitle(), a.dataBlock(cw, ch), rw, rh, dataFocused)
	var body string
	cursor := right.Offset(lw+1, 1)
	if only == paneData {
		body, cursor = right.Text, right.Offset(0, 1)
	} else if only == paneStructure {
		body = left.Text
	} else if stacked {
		body = left.Text + "\n" + right.Text
		cursor = right.Offset(0, 1+lh)
	} else {
		lr, rr := strings.Split(left.Text, "\n"), strings.Split(right.Text, "\n")
		rows := make([]string, bh)
		for i := range rows {
			rows[i] = pad(at(lr, i), lw) + " " + at(rr, i)
		}
		body = strings.Join(rows, "\n")
	}
	if left.Cursor != nil {
		cursor = left.Offset(0, 1)
	}
	if ph > 0 {
		panel := a.jqPanelBlock(l, width, ph)
		body += "\n" + panel.Text
		if panel.Cursor != nil {
			cursor = panel.Offset(0, 1+bh)
		}
	}
	return cursor.WithText(crumb + "\n" + body)
}

func at(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return ""
}

func pad(line string, width int) string {
	return line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
}

// breadcrumb is the line above the panels: the file, the selected path
// (the selected row's path when the data panel holds the focus), its
// types and presence, and the filters.
func (a *App) breadcrumb() string {
	switch {
	case a.Loading != "":
		return "loading " + a.Loading + "…"
	case a.LoadErr != nil && a.Doc == nil:
		return "error: " + a.LoadErr.Error()
	case a.Doc == nil:
		return a.emptyCrumb()
	}
	parts := []string{a.Doc.Name + a.dirtyMark()}
	// The path the tree's cursor is on: the shape shown, or the plain
	// values highlighted in it (follow.go).
	s := a.shown
	if a.lit != nil {
		s = a.lit
	}
	if s != nil {
		info := s.Types() + " ×" + fmt.Sprint(s.Count)
		if s.IsField() {
			info = s.Types() + " " + s.Presence()
		}
		if s.Optional {
			info += " optional"
		}
		part := s.Path + " (" + info + ")"
		if n, ok := a.rs.tree.Selected(); ok && a.editedShown() && !a.queryShowing() && a.rs.meta[n.ID].from != "" && a.rs.meta[n.ID].from != s.Path {
			part += " ← " + a.rs.meta[n.ID].from
		}
		parts = append(parts, part)
		if note := a.origin(s).DuplicateNote(); note != "" && !a.queryShowing() {
			parts = append(parts, note)
		}
		if n := a.dataNode(); n != nil && a.data.list.Focused() {
			parts = append(parts, "row "+n.Path())
		}
	}
	if note := a.queryNote(); note != "" {
		parts = append(parts, note)
	}
	if a.Query != "" {
		parts = append(parts, fmt.Sprintf("search %q", a.Query))
	}
	if a.Kind != "" {
		parts = append(parts, "type "+string(a.Kind))
	}
	if a.LoadErr != nil {
		parts = append(parts, "error: "+a.LoadErr.Error())
	}
	if a.note != "" && a.editedShown() {
		parts = append(parts, a.note)
	}
	if len(a.rs.patches) > 0 && a.editable() {
		n := len(a.rs.patches)
		part := fmt.Sprintf("%d value edit%s", n, plural(n))
		if o := a.shownOrphans(); len(o) > 0 {
			part += fmt.Sprintf(" (%d orphaned)", len(o))
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "  ·  ")
}

// treeTitle is the structure pane's title: "Structure · unchanged" as a
// document opens, "Structure · edited [+]" once edited (viewLabel); in
// query mode the highlighted field's term first, then "query" and the
// view (".email · query · unchanged"), so the path survives a narrow pane.
func (a *App) treeTitle() string {
	title, view := "Structure", ""
	if a.editable() && a.Doc != nil {
		view = a.viewLabel() + a.dirtyMark()
	}
	if a.queryModeOn() {
		title = "query"
		if term := a.queryModeTerm(); term != "" {
			title = term + " · query"
		}
	}
	if note := a.marksNote(); note != "" && a.editedShown() {
		title += " · " + note
	}
	if view != "" {
		title += " · " + view
	}
	if (a.Query != "" || a.Kind != "") && !a.editedShown() {
		title += " (filtered)"
	}
	return title
}

func (a *App) treeBody(w, h int) string {
	switch {
	case a.Doc == nil:
		a.r.Placed(treeID)
		return ""
	case a.editedShown():
		return a.rs.tree.View(w, h)
	}
	return a.tree.View(w, h)
}

// dataNode is the node of the data panel's selected row, or nil.
func (a *App) dataNode() *Node {
	item, ok := a.data.list.Selected()
	if !ok {
		return nil
	}
	return nodeAt(a.r.Instances[item.ID])
}

func (a *App) dataTitle() string {
	s := a.shown
	switch {
	case s == nil:
		return "Data"
	case a.Raw:
		return "JSON " + s.Path
	case a.viewName == viewFlat:
		return "Flat " + s.Path
	case a.tab != nil && !a.tab.Items:
		return "Object " + s.Path
	case a.tab != nil:
		return fmt.Sprintf("Table %s  %d/%d", s.Path, a.data.Selected()+1, len(a.tab.Rows))
	case a.fields != nil:
		return "Fields " + s.Path
	}
	return "Value " + s.Path
}

// dataBlock is the data panel's body, with the inline input of a value
// edit (values.go) drawn on its row and carrying the cursor.
func (a *App) dataBlock(w, h int) ochazuke.Block {
	body := a.dataBody(w, h)
	if a.vedit == nil || a.vedit.modal != nil || a.r.Instances[valueInputID] == nil || a.vedit.view != nil && a.vedit.view != a.data {
		return ochazuke.Text(body)
	}
	return a.inputBlock(body, w)
}

func (a *App) dataBody(w, h int) string {
	switch {
	case a.Loading != "":
		a.r.Placed(dataID)
		return "loading " + a.Loading + "…"
	case a.Doc == nil && a.LoadErr != nil:
		a.r.Placed(dataID)
		return wrap(a.LoadErr.Error(), w)
	case a.shown == nil && a.note != "" && a.editedShown():
		a.r.Placed(dataID)
		return wrap(a.note, w)
	case a.Doc == nil && a.shown == nil && a.scalar == nil && a.r.Settings().Bool(settingEmptyHint):
		a.r.Placed(dataID)
		return wrap(a.emptyHint(), w)
	case a.scalar != nil:
		a.r.Placed(dataID)
		return wrap(Pretty(a.scalar), w)
	case a.tab != nil && len(a.tab.Rows) == 0:
		a.r.Placed(dataID)
		return "(empty array)"
	}
	return a.hoverView(w, h)
}

// wrap breaks text at the width.
func wrap(text string, width int) string {
	return ansi.Hardwrap(text, max(1, width), true)
}

// emptyHint is the data pane of the empty document: that no document is
// open, and the keys that fill it, from the bindings (a verb with no key
// is named by its action, which the palette runs).
func (a *App) emptyHint() string {
	lines := []string{"No document is open.", ""}
	for _, v := range []struct{ verb, what string }{{"doc.open", "open a JSON file"}, {"doc.paste", "paste JSON from the clipboard"}, {"restructure.open", "build a structure from nothing"}} {
		key := a.keyFor(v.verb)
		if key == "" {
			key = ":" + strings.Replace(v.verb, ".", " ", 1)
		}
		lines = append(lines, fmt.Sprintf("  %-5s %s (%s)", key, v.what, v.verb))
	}
	return strings.Join(lines, "\n")
}

// emptyCrumb is the breadcrumb of the empty document: "[no file]" and the
// keys that fill it, from the bindings (a verb with no key is left out).
func (a *App) emptyCrumb() string {
	parts := []string{noFile}
	for _, v := range []struct{ verb, what string }{{"viewer.open", "open a file"}, {"viewer.paste", "paste JSON"}, {"restructure.open", "restructure"}} {
		if key := a.keyFor(v.verb); key != "" {
			parts = append(parts, key+" "+v.what)
		}
	}
	return strings.Join(parts, "  ·  ")
}
