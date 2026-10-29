package main

import "embed"

// The key rows are bindings.json, the only place a default key is
// written (loaded into the default layer, below the user's bindings.json).
//
// On the structure tree the stack is [shape row, the tree's list, the tree
// (outline), viewer, app]: the outline answers l/h (expand, collapse) and
// enter folds; the viewer owns the search, the type filter and the raw
// toggle. In the data panel it is [item|field|line row, rows, viewer,
// app], and in the detail [entry row, rows, detail modal, modal, app]. tab
// and shift+tab (the app's focus rows) move between the panels. Counts
// reach rows.move through count_pending, which is exclusive, so the rows
// noun's count_pending extends its normal table.
//
// The rows tables are cell tables (CellSelect), but a list of its own noun
// answers only to that noun's rows, so the framework's cell keys are bound
// on rows, to its list verbs: h/l and the arrows move the current column
// (clamped: the panels are NoSpill), 0/^ and $ go to the first and last,
// N| to column N, g c chooses one, z >/z </z = size them and z p pins one.
// Those verbs are available only on a cell table (their verb guard), so
// the rows carry no guard of their own. home and end stay the rows' top
// and bottom.

//go:embed bindings.json
var bindingsFS embed.FS

func (a *App) bind() error {
	return a.r.LoadDefaultBindingsFS(bindingsFS, "bindings.json")
}
