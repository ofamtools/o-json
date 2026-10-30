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
	ready                bool
}{
	{"SHAPE", "read and walk the structure", "structure pane · ctrl+w h", true},
	{"TABLE", "rows and columns; objects have one row", "data pane · ctrl+w l", true},
	{"NESTED", "inside a nested cell's sub-table", "enter on a nested cell", false},
	{"FLAT", "path = value lines", "D / tab to Flat", true},
	{"JSON", "pretty JSON text and folds", "D / tab to JSON", true},
	{"SEARCH", "find keys and values", "/", false},
	{"JQ", "build and run a jq query", "m on structure", true},
	{"EDIT", "change values, keys and rows", "i / r", false},
	{"VISUAL", "select cells, rows, lines or paths", "v / V", false},
	{"RAW", "edit JSON in a foreground editor", "g r", false},
	{"SPLIT", "original and edited structures", "ctrl+w v", true},
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
		if !choice.ready {
			row.Refuse = choice.name + " entry is still being built in this trial"
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
		Choose: func(row ochazuke.ChooserRow) ochazuke.Result {
			if err := a.gotoMode(row.ID); err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(a.state())
		},
	})
	return err
}

func (a *App) gotoMode(name string) error {
	if a.Doc == nil && name != "SHAPE" {
		return errors.New("open a document before choosing this mode")
	}
	switch name {
	case "SHAPE":
		if a.r.Page == rsPageID {
			return a.rs.orig.Focus()
		}
		return a.focusStructure()
	case "TABLE", "FLAT", "JSON":
		if a.r.Page == rsPageID {
			if err := a.r.OpenPage(pageID); err != nil {
				return err
			}
		}
		view := strings.ToLower(name)
		if a.shown != nil {
			a.viewByPath[a.shown.Path] = view
			if err := a.reshow(); err != nil {
				return err
			}
		}
		return a.data.list.Focus()
	case "JQ":
		if a.r.Page == rsPageID {
			if err := a.r.OpenPage(pageID); err != nil {
				return err
			}
		}
		if err := a.focusStructure(); err != nil {
			return err
		}
		return a.r.SetMode(queryMode)
	case "SPLIT":
		if err := a.r.OpenPage(rsPageID); err != nil {
			return err
		}
		return a.rs.orig.Focus()
	case "NESTED", "SEARCH", "EDIT", "VISUAL", "RAW":
		return fmt.Errorf("%s mode is listed for the trial; its entry action is still being built", name)
	}
	return fmt.Errorf("unknown mode %q", name)
}
