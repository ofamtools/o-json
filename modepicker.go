package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// modeChoices is the trial's mode catalog. The chooser keeps the key and
// description alongside the mode so they can be found with one fuzzy query.
var modeChoices = []struct {
	name, purpose, enter string
}{
	{"SHAPE", "read and walk the structure", "structure pane · ctrl+w h"},
	{"TABLE", "rows and columns; objects have one row", "data pane · ctrl+w l"},
	{"NESTED", "inside a nested cell's sub-table", "enter on a nested cell"},
	{"FLAT", "path = value lines", "D / tab to Flat"},
	{"JSON", "pretty JSON text and folds", "D / tab to JSON"},
	{"SEARCH", "find keys and values; the Results view", "/"},
	{"JQ", "a jq query and its result as a table", "/ then shift+tab · m on the structure"},
	{"EDIT", "change values, keys and rows", "i · r (EDIT and the value editor)"},
	{"VISUAL", "select rows, lines or paths", "v · V"},
	{"RAW", "the document in nvim (raw.editor), in the foreground", "g r"},
	{"SPLIT", "original and edited structures", "ctrl+w v"},
}

const modeChooser = "mode-picker"

func modeFuzzy(query, label string) bool {
	needle := []rune(strings.ToLower(query))
	label = strings.ToLower(label)
	for _, ch := range label {
		if len(needle) > 0 && ch == needle[0] {
			needle = needle[1:]
		}
	}
	return len(needle) == 0
}

func (a *App) openModePicker() error {
	current := strings.ToUpper(a.r.BaseMode())
	if current == "QUERY" {
		current = "JQ"
	}
	rows := make([]ochazuke.ChooserRow, 0, len(modeChoices))
	for _, choice := range modeChoices {
		colour := trialModeAccents[strings.ToLower(choice.name)][1]
		row := ochazuke.ChooserRow{
			ID: choice.name, Label: choice.name, Data: choice,
			Cells: []string{choice.name, colour, choice.purpose, choice.enter},
		}
		if choice.name == current {
			rows = append([]ochazuke.ChooserRow{row}, rows...)
		} else {
			rows = append(rows, row)
		}
	}
	_, err := a.r.OpenChooser(ochazuke.ChooserSpec{
		Kind: modeChooser, Title: "Modes", Rows: rows,
		Filter: func(q string, row ochazuke.ChooserRow) bool { return modeFuzzy(q, row.Label) },
		Choose: func(row ochazuke.ChooserRow) ochazuke.Result { return a.gotoMode(row.ID) },
	})
	return err
}

// gotoMode enters a mode as the prototype's picker does: SHAPE the
// structure, TABLE / FLAT / JSON that view of the data pane, NESTED the
// first nested cell's sub-table, SEARCH and JQ the search line in that
// type, EDIT and VISUAL on the data pane's table, RAW the editor, SPLIT
// the restructure split.
func (a *App) gotoMode(name string) ochazuke.Result {
	fail := func(err error) ochazuke.Result {
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(a.state())
	}
	if a.Doc == nil && name != "SHAPE" {
		return fail(errors.New("open a document before choosing this mode"))
	}
	toViewer := func() error {
		if a.r.Page == rsPageID {
			return a.r.OpenPage(pageID)
		}
		return nil
	}
	// dataView focuses the data pane on view (a structure path's own).
	dataView := func(view string) error {
		if err := toViewer(); err != nil {
			return err
		}
		a.overlay = ""
		if s := a.selectedShape(); s != nil {
			a.viewByPath[s.Path] = view
			if err := a.reshow(); err != nil {
				return err
			}
		}
		return a.data.list.Focus()
	}
	switch name {
	case "SHAPE":
		if a.r.Page == rsPageID {
			return fail(a.rs.orig.Focus())
		}
		return fail(a.focusStructure())
	case "TABLE", "FLAT", "JSON":
		return fail(dataView(strings.ToLower(name)))
	case "NESTED":
		if err := dataView(viewTable); err != nil {
			return fail(err)
		}
		return fail(a.enterNested())
	case "SEARCH", "JQ":
		if err := toViewer(); err != nil {
			return fail(err)
		}
		if name == "JQ" {
			a.sr.typ = "jq"
		} else if a.searchJQ() {
			a.sr.typ = a.r.Settings().String(settingSearchType)
			if a.searchJQ() {
				a.sr.typ = searchTypes[0].name
			}
		}
		return fail(a.focusQuery())
	case "EDIT":
		if err := dataView(viewTable); err != nil {
			return fail(err)
		}
		if !a.r.Available(ochazuke.Request{Noun: "viewer", Verb: "edit"}, a.r.Focus, a.r.Mode) {
			return fail(errors.New("EDIT needs the edited document's table (T shows it; modes.edit-gate on)"))
		}
		return a.enterEdit(ochazuke.Ctx{Runtime: a.r}, false)
	case "VISUAL":
		if err := dataView(viewTable); err != nil {
			return fail(err)
		}
		_, err := a.r.Execute(ochazuke.Request{Noun: "ochazuke.list", Verb: "visual", Target: a.r.Focus})
		return fail(err)
	case "RAW":
		if err := toViewer(); err != nil {
			return fail(err)
		}
		return a.startRaw(ochazuke.Ctx{Runtime: a.r})
	case "SPLIT":
		if err := a.r.OpenPage(rsPageID); err != nil {
			return fail(err)
		}
		return fail(a.rs.orig.Focus())
	}
	return fail(fmt.Errorf("unknown mode %q", name))
}

// enterNested puts the data pane's cursor in the first nested cell's
// sub-table (table.nested hover), searching the table's rows from the
// cursor's.
func (a *App) enterNested() error {
	if a.tab == nil {
		return errors.New("NESTED needs a table with {…} or […] cells")
	}
	for row := max(0, a.data.Selected()); row < a.data.src.n; row++ {
		if err := a.data.to(row); err != nil {
			return err
		}
		for j, col := range a.tab.Columns {
			if col.Self {
				continue // the row itself (#), not a cell
			}
			a.data.list.SetColumn(a.tab.ListColumn(j))
			if n := a.dataCell(); n != nil && n.IsContainer() && len(n.Children) > 0 {
				return a.hoverDown(1)
			}
		}
	}
	return errors.New("no nested cell in this table: pick one with {…} or […] cells")
}
