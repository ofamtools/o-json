package main

// The data pane's views: what one shape's values can be shown as. Each
// view says which values it applies to and builds the rows; show picks the
// view the settings prefer among those that apply (object.view for one
// object), else the first that applies. The detail has its own list, since
// it shows one object or array at a time. A new view is a row here, not a
// branch in show.

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// cycleDataView steps the data pane's view (D, tab; shift+tab back): the
// structure path's own views that apply, then the overlays that are live
// (the restructure's Mapping, the search's Results, the jq result). A
// count walks several. A path's own view is remembered per path; an
// overlay is shown over every path until esc clears it or the cycle
// steps off it.
func (a *App) cycleDataView(steps int) error {
	available, current := a.viewCycle()
	if len(available) == 0 || steps == 0 {
		return nil
	}
	i := slices.Index(available, current)
	if i < 0 {
		i = 0
	}
	i = ((i+steps)%len(available) + len(available)) % len(available)
	switch next := available[i]; next {
	case overlayMapping, overlayResults, overlayJQ:
		a.overlay = next
	default:
		a.overlay = ""
		a.viewByPath[a.selectedShape().Path] = next
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

// viewCycle is the views D steps through for the selected path, in
// order, and the one shown; none without a selected path.
func (a *App) viewCycle() (available []string, current string) {
	s := a.selectedShape()
	if s == nil {
		return nil, ""
	}
	values := s.Values()
	available = make([]string, 0, len(dataViewOrder)+3)
	for _, name := range dataViewOrder {
		if viewApplies(name, values) {
			available = append(available, name)
		}
	}
	if a.editedShown() {
		available = append(available, overlayMapping)
	}
	if a.sr.view != nil {
		available = append(available, overlayResults)
	}
	if a.searchJQ() && a.q.goodView != nil {
		available = append(available, overlayJQ)
	}
	current = a.overlay
	if current == "" || !slices.Contains(available, current) {
		current = a.viewByPath[s.Path]
		if !viewApplies(current, values) {
			current = defaultDataView(values, a.r.Settings().String(settingObjectView))
		}
	}
	return available, current
}

// viewStrip is the data pane title's strip of the cycle: the views D
// steps through, the shown one in brackets ("table [flat] json mapping").
// Views that do not apply to the path are left out: a panel title is
// plain text (ochazuke strips styles from it), so they cannot be dimmed.
func (a *App) viewStrip() string {
	available, current := a.viewCycle()
	if len(available) < 2 {
		return ""
	}
	parts := make([]string, len(available))
	for i, name := range available {
		parts[i] = name
		if name == current {
			parts[i] = "[" + name + "]"
		}
	}
	return strings.Join(parts, " ")
}

// overlayMapping is the Mapping view: the restructure's mapping (the
// --mapping form restructure.export-map writes) as JSON lines, over every
// path, while the restructured document is shown (the viewer's edited
// view, the restructure page's preview).
const overlayMapping = "mapping"

// mappingShowing reports the data pane on the Mapping view.
func (a *App) mappingShowing() bool { return a.overlay == overlayMapping && a.editedShown() }

// mappingCache is the Mapping view's values, made again when the mapping
// changes.
type mappingCache struct {
	key  string
	view *Shape
}

// mappingView is the mapping as one JSON value to show (a mapping that
// does not check: why, as a string), shown as JSON.
func (a *App) mappingView() *Shape {
	key := mappingKey(a.Mapping())
	if a.mapv.view != nil && a.mapv.key == key {
		return a.mapv.view
	}
	text, err := a.mappingText()
	var root *Node
	if err == nil {
		root, err = Build([]byte(text))
	}
	if err != nil {
		quoted, _ := json.Marshal("the mapping does not check: " + err.Error())
		root, _ = Build(quoted)
	}
	s := &Shape{Path: overlayMapping, Kinds: map[Kind]int{}}
	s.add(root)
	s.finish()
	a.viewByPath[s.Path] = viewJSON
	a.mapv = mappingCache{key: key, view: s}
	return s
}

const (
	viewFlat    = "flat"
	viewJSON    = "json"
	viewResults = "results"
)

// dataViewOrder is the trial's view cycle. Fields is available for an
// object; the other views work for every selected path.
var dataViewOrder = []string{viewTable, objectFields, viewFlat, viewJSON}

// syncDeclaredMode declares each pane's mode as an ochazuke instance pin
// (Runtime.PinInstance, v0.61.0): the structure trees declare SHAPE (SPLIT
// on the restructure page), the data pane the mode of its view (TABLE,
// NESTED while a sub-table is open under the cursor, FLAT, JSON, SEARCH
// on the Results, JQ on the jq result). The library applies a pin when
// the focus lands on the pane, and a re-pin of the focused pane at once
// unless the user lifted into another mode (EDIT, VISUAL, a pending
// mode); esc rows return with mode.set {mode: declared}. It runs after
// each event (the jq runner's PendingCommand) and after a view change;
// it sets no mode itself. modes.declared off drops the pins: every pane
// declares normal.
func (a *App) syncDeclaredMode() {
	off := a.r.Settings().String(settingDeclaredModes) == "off"
	pin := func(id, mode string) {
		if off {
			mode = ""
		}
		if inst := a.r.Instances[id]; inst != nil && inst.Pin != mode {
			_ = a.r.PinInstance(id, mode)
		}
	}
	tree := "shape"
	if a.r.Page == rsPageID {
		tree = "split"
	}
	pin(treeID, tree)
	pin(rsTreeID, tree)
	pin(rsOrigID, "split")
	if a.data != nil {
		pin(a.data.id, a.dataMode())
	}
	if a.editing && (!a.onEditPane() || a.r.Mode.Name == a.r.DeclaredMode(a.r.Focus) && a.r.Input(a.r.Focus) == nil) {
		// EDIT is per pane: leaving the pane leaves it, and so does the
		// esc row back to the declared mode (mode.set {mode: declared}).
		a.editing = false
	}
	// o-jj workaround: ochazuke's own rows that end a lifted mode (the
	// list's visual end, the outline's input) set normal, not the
	// declared mode; a frame in normal on a pane that declares another
	// goes to it.
	if a.r.Mode.Name == "normal" && len(a.r.Modals()) == 0 && a.r.Input(a.r.Focus) == nil && a.r.DeclaredMode(a.r.Focus) != "normal" {
		_ = a.r.SetMode(a.backMode())
	}
}

// dataMode is the mode the data pane's view declares.
func (a *App) dataMode() string {
	switch {
	case a.queryShowing():
		return "jq"
	case a.mappingShowing():
		return "json"
	case a.overlay == overlayResults && a.shown == a.sr.view:
		return "search"
	case a.hoverIn():
		return "nested"
	case a.viewName == viewFlat || a.viewName == viewJSON:
		return a.viewName
	}
	return "table"
}

// onEditPane reports the focus still on the pane EDIT was entered on (or
// in what it opened there: the value editor, a rename, a modal).
func (a *App) onEditPane() bool {
	switch {
	case len(a.r.Modals()) > 0, a.vedit != nil, a.r.Focus == valueInputID, a.renaming(), a.r.Focus == jqFieldID:
		return true
	case a.editTree:
		return a.structureFocused()
	}
	return a.inData(a.r.Focus)
}

// backMode is the mode a lifted mode returns to: EDIT while it is on,
// else the focus's declared mode.
func (a *App) backMode() string {
	if a.editing {
		return "edit"
	}
	return ochazuke.ModeDeclared
}

// trialModeAccents are the badge hues of o-jj's modes (light, dark),
// declared as ochazuke mode tones (Appearance.SetModeTone: the tone row
// mode.<name>, which a themes.json theme overrides). insert and the
// visual modes keep ochazuke's own hues.
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
	"raw":             {"#516B36", "#9DD077"},
	"search":          {"#886A1E", "#E9CD6A"},
	"search_·_substr": {"#886A1E", "#E9CD6A"},
	"search_·_fuzzy":  {"#886A1E", "#E9CD6A"},
	"search_·_regex":  {"#886A1E", "#E9CD6A"},
	"jq_·_line":       {"#A45916", "#F2AD64"},
}

// declareModeTones declares each o-jj mode's badge hue.
func (a *App) declareModeTones() {
	for mode, c := range trialModeAccents {
		a.m.Appearance.SetModeTone(mode, ochazuke.SchemeColor{Light: c[0], Dark: c[1]})
	}
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
