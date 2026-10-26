package main

import "github.com/unixsuperhero/ochazuke"

// bind installs the key rows. On the structure tree the stack is [shape
// row, the tree's list, the tree (outline), viewer, app]: the outline
// answers l/h (expand, collapse) and enter folds; the viewer owns the
// search, the type filter and the raw toggle. In the data panel it is
// [item|field|line row, rows, viewer, app], and in the detail [entry row,
// rows, detail modal, modal, app]. tab and shift+tab (the app's focus
// rows) move between the panels. Counts reach rows.move through
// count_pending, which is exclusive, so the rows noun's count_pending
// extends its normal table.
func (a *App) bind() {
	r := a.r
	move := func(motion string) ochazuke.Binding {
		return ochazuke.Do("rows.move", ochazuke.Arguments{"motion": motion}).FromPayload("steps", ochazuke.SourceCount)
	}
	line := func(motion string) ochazuke.Binding {
		return ochazuke.Do("rows.move", ochazuke.Arguments{"motion": motion}).FromPayload("line", ochazuke.SourceLine)
	}
	r.Define("rows").Mode("normal").
		Bind("j", move("down")).Bind("down", move("down")).
		Bind("k", move("up")).Bind("up", move("up")).
		Bind("ctrl+d", move("half-down")).Bind("ctrl+u", move("half-up")).
		Bind("ctrl+f", move("page-down")).Bind("pgdown", move("page-down")).
		Bind("ctrl+b", move("page-up")).Bind("pgup", move("page-up")).
		Bind("g g", line("top")).Bind("home", line("top")).
		Bind("G", line("bottom")).Bind("end", line("bottom"))
	r.Define("rows").Mode("count_pending").Extends("normal")

	r.Define("viewer").Mode("normal").
		Bind("/", ochazuke.Do("viewer.search")).
		Bind("n", ochazuke.Do("viewer.next")).
		Bind("N", ochazuke.Do("viewer.previous")).
		Bind("t", ochazuke.Do("viewer.type")).
		Bind("v", ochazuke.Do("viewer.raw")).
		Bind("esc", ochazuke.Do("viewer.clear")).
		Bind("R", ochazuke.Do("viewer.load")).
		Bind("q", ochazuke.Do("app.quit"))

	r.Define("shape").Mode("normal").
		Bind("enter", ochazuke.Do("outline.fold")).
		Bind("y", ochazuke.Do("shape.yank")).
		// The tree is read-only and has nothing to mark: space would run
		// the list's list.mark.
		Bind("space", ochazuke.Do("key.ignore").Hide())
	r.Define("item").Mode("normal").
		Bind("enter", ochazuke.Do("item.open")).
		Bind("y", ochazuke.Do("item.yank"))
	r.Define("field").Mode("normal").
		Bind("enter", ochazuke.Do("field.open")).
		Bind("y", ochazuke.Do("field.yank"))
	r.Define("line").Mode("normal").
		Bind("y", ochazuke.Do("line.yank"))
	r.Define("entry").Mode("normal").
		Bind("enter", ochazuke.Do("entry.open")).Bind("l", ochazuke.Do("entry.open")).
		Bind("h", ochazuke.Do("entry.up")).Bind("backspace", ochazuke.Do("entry.up")).
		Bind("y", ochazuke.Do("entry.yank"))
	r.Define(detailID).Mode("normal").
		Bind("q", ochazuke.Do("modal.pop"))
}
