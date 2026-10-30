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

// cycleDataView changes only the selected path's preferred view. A count
// walks the applicable views, so a scalar skips Fields.
func (a *App) cycleDataView(steps int) error {
	if a.shown == nil || steps == 0 {
		return nil
	}
	values := a.shown.Values()
	available := make([]string, 0, len(dataViewOrder))
	for _, name := range dataViewOrder {
		if viewApplies(name, values) {
			available = append(available, name)
		}
	}
	i := slices.Index(available, a.viewName)
	if i < 0 {
		i = 0
	}
	i = ((i+steps)%len(available) + len(available)) % len(available)
	a.viewByPath[a.shown.Path] = available[i]
	if err := a.reshow(); err != nil {
		return err
	}
	a.syncDeclaredMode()
	return nil
}

const (
	viewFlat = "flat"
	viewJSON = "json"
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
		case "shape", "table", "nested", "flat", "json", "jq", "split":
			_ = a.r.SetMode("normal")
		}
		return
	}
	if len(a.r.Modals()) > 0 {
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
		if want == "" || want == objectFields || want == objectRow {
			want = "table"
		}
		if a.queryShowing() {
			want = "jq"
		} else if a.hoverIn() {
			want = "nested"
		}
	}
	if want == "" {
		return
	}
	base := a.r.BaseMode()
	if base == queryMode && want == "shape" {
		return // m explicitly lifted the structure pane into JQ
	}
	if base != "normal" && base != "shape" && base != "table" && base != "nested" && base != "flat" && base != "json" && base != "jq" && base != "split" {
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
	"shape":       {"#266B9A", "#73B6ED"},
	"table":       {"#237A56", "#72D5A7"},
	"nested":      {"#596E2E", "#BDD568"},
	"flat":        {"#8B5A26", "#E7B673"},
	"json":        {"#7952A3", "#C7A2ED"},
	"jq":          {"#A45916", "#F2AD64"},
	"query":       {"#A45916", "#F2AD64"},
	"split":       {"#166C78", "#74CDD4"},
	"edit":        {"#A44049", "#F2949A"},
	"insert":      {"#A44049", "#F2949A"},
	"visual":      {"#8D4A91", "#E59CE2"},
	"visual-line": {"#8D4A91", "#E59CE2"},
	"raw":         {"#516B36", "#9DD077"},
	"search":      {"#886A1E", "#E9CD6A"},
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
