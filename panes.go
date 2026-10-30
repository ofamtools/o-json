package main

// Window keys in Vim's form: ctrl+w h / l move to the pane on that side
// and stop at the edge (viewer.pane). ctrl+w o is ochazuke's pane.only
// (v0.61.0, UX-RULES R3.16): the focused pane alone; ctrl+w o again or
// ctrl+w = (pane.restore) brings the other back, and so does moving the
// focus to the hidden pane. Each mode's own row carries the footer hint
// (only structure, only data). viewer.only stays as a verb over them.

import (
	"errors"

	"github.com/unixsuperhero/ochazuke"
)

const (
	paneStructure = "structure"
	paneData      = "data"
)

// focusStructure focuses the structure pane the viewer shows: the edited
// tree, or the original.
func (a *App) focusStructure() error {
	if a.editedShown() {
		return a.rs.tree.Focus()
	}
	return a.tree.Focus()
}

// structureFocused reports the focus in the viewer's structure pane.
func (a *App) structureFocused() bool {
	return a.tree.List().Focused() || a.rs.tree.List().Focused() || a.r.Focus == treeID || a.r.Focus == rsTreeID
}

// onlyPane is viewer.only, kept as a verb over ochazuke's pane.only and
// pane.restore (v0.61.0; the keys run those): pane is structure, data,
// focused or none. With one pane already alone, it restores both.
func (a *App) onlyPane(c ochazuke.Ctx, pane string) error {
	page := a.r.Page
	if a.r.PaneOnly(page) != "" || pane == "none" {
		c.Info("both panes shown")
		return a.r.RestorePanes(page)
	}
	keep := ""
	switch pane {
	case "focused", "":
	case paneStructure:
		keep = treeID
		if a.editedShown() {
			keep = rsTreeID
		}
	case paneData:
		keep = a.data.id
	default:
		return errors.New("pane is structure, data, focused or none")
	}
	c.Info("one pane · ctrl+w o again or ctrl+w = restores")
	return a.r.ShowPaneOnly(page, keep)
}

// movePane is viewer.pane: to is left or right; at the edge it stays.
func (a *App) movePane(to string) error {
	if a.r.Page == rsPageID {
		if to == "left" {
			return a.rs.orig.Focus()
		}
		return a.rs.tree.Focus()
	}
	if to == "left" {
		if a.structureFocused() {
			return nil
		}
		return a.focusStructure()
	}
	if a.inData(a.r.Focus) {
		return nil
	}
	return a.data.list.Focus()
}

func (a *App) registerPaneVerbs() {
	r := a.r
	done := func(err error) ochazuke.Result {
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(nil)
	}
	r.Define("viewer").
		Verb("only", func(c ochazuke.Ctx) ochazuke.Result {
			return done(a.onlyPane(c, ochazuke.StringArg(c.Args, "pane", "focused")))
		}, "Show only one pane (ctrl+w o): structure, data or the focused one, as the mode's row says; again, or with pane none, both come back").
		Verb("restore", func(c ochazuke.Ctx) ochazuke.Result {
			return done(a.onlyPane(c, "none"))
		}, "Show both panes again after ctrl+w o (ctrl+w =)").When("pane-only").
		Verb("pane", func(c ochazuke.Ctx) ochazuke.Result {
			return done(a.movePane(ochazuke.StringArg(c.Args, "to", "right")))
		}, "Move the focus to the pane on the left or right (ctrl+w h, ctrl+w l); at the edge it stays, as in Vim")
	setParams(r, "viewer.only", map[string]ochazuke.Parameter{"pane": {Type: "string", Enum: []string{paneStructure, paneData, "focused", "none"}, Description: "default focused"}})
	setParams(r, "viewer.pane", map[string]ochazuke.Parameter{"to": {Type: "string", Enum: []string{"left", "right"}, Description: "default right"}})
	setHistory(r, ochazuke.HistoryAction, "viewer.only", "viewer.restore", "viewer.pane")
}
