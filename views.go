package main

// The data pane's views: what one shape's values can be shown as. Each
// view says which values it applies to and builds the rows; show picks the
// view the settings prefer among those that apply (object.view for one
// object), else the first that applies. The detail has its own list, since
// it shows one object or array at a time. A new view is a row here, not a
// branch in show.

import (
	"slices"

	"charm.land/lipgloss/v2"
)

// cycleDataView steps the data pane's view (D, tab; shift+tab back): the
// structure path's own views that apply, then the overlays that are live
// (the search's Results, the jq result). A count walks several. A path's
// own view is remembered per path; an overlay is shown over every path
// until esc clears it or the cycle steps off it.
func (a *App) cycleDataView(steps int) error {
	s := a.selectedShape()
	if s == nil || steps == 0 {
		return nil
	}
	values := s.Values()
	available := make([]string, 0, len(dataViewOrder)+2)
	for _, name := range dataViewOrder {
		if viewApplies(name, values) {
			available = append(available, name)
		}
	}
	if a.sr.view != nil {
		available = append(available, overlayResults)
	}
	if a.searchJQ() && a.q.goodView != nil {
		available = append(available, overlayJQ)
	}
	current := a.overlay
	if current == "" {
		current = a.viewByPath[s.Path]
		if !viewApplies(current, values) {
			current = defaultDataView(values, a.r.Settings().String(settingObjectView))
		}
	}
	i := slices.Index(available, current)
	if i < 0 {
		i = 0
	}
	i = ((i+steps)%len(available) + len(available)) % len(available)
	switch next := available[i]; next {
	case overlayResults, overlayJQ:
		a.overlay = next
	default:
		a.overlay = ""
		a.viewByPath[s.Path] = next
	}
	if err := a.reshow(); err != nil {
		return err
	}
	if err := a.sync(); err != nil {
		return err
	}
	a.syncDeclaredMode()
	return nil
}

const (
	viewFlat    = "flat"
	viewJSON    = "json"
	viewResults = "results"
)

// dataViewOrder is the trial's view cycle. Fields is available for an
// object; the other views work for every selected path.
var dataViewOrder = []string{viewTable, objectFields, viewFlat, viewJSON}

// syncDeclaredMode uses the focus stop and the active data view as the
// local mode declaration. Ochazuke currently pins modes by noun only;
// this app-side declaration is the trial equivalent of an instance pin.
func (a *App) syncDeclaredMode() {
	defer a.syncModeAccent()
	if a.r.Settings().String(settingDeclaredModes) == "off" {
		switch a.r.BaseMode() {
		case "shape", "table", "nested", "flat", "json", "jq", "split", "search", "edit":
			_ = a.r.SetMode("normal")
		}
		return
	}
	if len(a.r.Modals()) > 0 || a.r.Mode.Payload.Prefix != "" || a.r.Mode.Name != a.r.BaseMode() {
		return // a modal, or a prefix or count waiting for its next key
	}
	if a.r.Focus == jqFieldID {
		// The search line: the type's mode, while it is in insert.
		if base := a.r.BaseMode(); (base == "insert" || slices.Contains(lineModes(), base)) && base != a.lineMode() {
			_ = a.r.SetMode(a.lineMode())
		}
		return
	}
	want := ""
	switch {
	case a.tree.List().Focused() || a.rs.tree.List().Focused() || a.rs.orig.List().Focused() || a.r.Focus == treeID || a.r.Focus == rsTreeID || a.r.Focus == rsOrigID:
		want = "shape"
		if a.r.Page == rsPageID {
			want = "split"
		}
	case a.data.list.Focused() || a.r.Focus == dataID:
		want = a.viewName
		if a.editing {
			want = "edit"
			break
		}
		if want == "" || want == objectFields || want == objectRow {
			want = "table"
		}
		if a.queryShowing() {
			want = "jq"
		} else if a.overlay == overlayResults && a.shown == a.sr.view {
			want = "search"
		} else if a.hoverIn() {
			want = "nested"
		}
	}
	if want != "edit" && a.vedit == nil && a.r.Focus != valueInputID {
		a.editing = false // EDIT is per pane: leaving the data pane leaves it
	}
	if want == "" {
		return
	}
	base := a.r.BaseMode()
	if base == queryMode && want == "shape" {
		return // m explicitly lifted the structure pane into JQ
	}
	if base != "normal" && base != "shape" && base != "table" && base != "nested" && base != "flat" && base != "json" && base != "jq" && base != "split" && base != "search" && base != "edit" {
		return // an explicit edit, query or selection mode is on top
	}
	if base != want {
		_ = a.r.SetMode(want)
	}
}

// Ochazuke exposes one application accent but no app-defined mode tone
// table. In this trial the accent follows the mode, colouring the badge and
// focused chrome together. The library can own per-mode badge tones later.
var trialModeAccents = map[string][2]string{
	"shape":           {"#266B9A", "#73B6ED"},
	"table":           {"#237A56", "#72D5A7"},
	"nested":          {"#596E2E", "#BDD568"},
	"flat":            {"#8B5A26", "#E7B673"},
	"json":            {"#7952A3", "#C7A2ED"},
	"jq":              {"#A45916", "#F2AD64"},
	"query":           {"#A45916", "#F2AD64"},
	"split":           {"#166C78", "#74CDD4"},
	"edit":            {"#A44049", "#F2949A"},
	"insert":          {"#A44049", "#F2949A"},
	"visual":          {"#8D4A91", "#E59CE2"},
	"visual-line":     {"#8D4A91", "#E59CE2"},
	"raw":             {"#516B36", "#9DD077"},
	"search":          {"#886A1E", "#E9CD6A"},
	"search_·_substr": {"#886A1E", "#E9CD6A"},
	"search_·_fuzzy":  {"#886A1E", "#E9CD6A"},
	"search_·_regex":  {"#886A1E", "#E9CD6A"},
	"jq_·_line":       {"#A45916", "#F2AD64"},
}

func (a *App) syncModeAccent() {
	colours, ok := trialModeAccents[a.r.BaseMode()]
	if !ok || a.r.Settings().String(settingDeclaredModes) == "off" {
		a.m.Appearance.Accent = nil
		return
	}
	which := 0
	if a.m.Appearance.Dark {
		which = 1
	}
	a.m.Appearance.Accent = lipgloss.Color(colours[which])
}

func viewApplies(name string, values []*Node) bool {
	switch name {
	case viewTable, viewFlat, viewJSON:
		return true
	case objectFields:
		return oneObject(values)
	}
	return false
}

func defaultDataView(values []*Node, objectPreference string) string {
	if oneObject(values) && objectPreference == objectFields {
		return objectFields
	}
	return viewTable
}

// dataView is one way of showing values: a name (the setting's value that
// prefers it), the values it applies to, and the rows it builds.
type dataView struct {
	name    string
	applies func(values []*Node) bool
	build   func(path string, values []*Node) viewRows
}

// viewRows is what a view builds: a table (tab), or labeled rows of an
// object (fields) with their source.
type viewRows struct {
	tab    *Table
	fields *Node
	src    rowSource
}

const viewTable = "table"

func oneObject(values []*Node) bool { return len(values) == 1 && values[0].Kind == KObject }

// tableOf is any values as a table: one array its items, several values
// the rows, one object its one row (no "#": Table.Items).
func tableOf(path string, values []*Node) viewRows {
	return viewRows{tab: NewTable(path, values, len(values) != 1)}
}

// dataViews are the data pane's views, in the order a value without a
// preference takes them. One plain value and the raw JSON (v) are shown
// before any view is picked.
var dataViews = []dataView{
	{name: viewTable, applies: func(values []*Node) bool { return !oneObject(values) }, build: tableOf},
	{name: objectRow, applies: oneObject, build: tableOf},
	{name: objectFields, applies: oneObject, build: func(_ string, values []*Node) viewRows {
		return viewRows{fields: values[0], src: fieldsSource(values[0], "field")}
	}},
}

// detailViews are the detail's views of the object or array it shows: an
// object's one row is the trail's value, not an item, so it has no "#".
var detailViews = []dataView{
	{name: objectRow, applies: oneObject, build: func(path string, values []*Node) viewRows {
		t := NewTable(path, values, false)
		t.Items = false
		return viewRows{tab: t}
	}},
	{name: objectFields, applies: func([]*Node) bool { return true }, build: func(_ string, values []*Node) viewRows {
		return viewRows{src: fieldsSource(values[0], "entry")}
	}},
}

// pickView is the view of views named prefer when it applies to values,
// else the first that applies.
func pickView(views []dataView, values []*Node, prefer string) dataView {
	i := slices.IndexFunc(views, func(v dataView) bool { return v.name == prefer && v.applies(values) })
	if i < 0 {
		i = slices.IndexFunc(views, func(v dataView) bool { return v.applies(values) })
	}
	return views[i]
}
