package main

// The data pane's views: what one shape's values can be shown as. Each
// view says which values it applies to and builds the rows; show picks the
// view the settings prefer among those that apply (object.view for one
// object), else the first that applies. The detail has its own list, since
// it shows one object or array at a time. A new view is a row here, not a
// branch in show.

import "slices"

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
