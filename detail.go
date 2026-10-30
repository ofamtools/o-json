package main

import (
	"errors"
	"fmt"

	"github.com/unixsuperhero/ochazuke"
)

// The detail: enter on a table row (or on a field holding an object or an
// array) opens that value in a modal as labeled rows, field (or [index]),
// type and value. enter on a row holding a container drills into it, and
// h or backspace steps back out; esc (the modal noun) or q closes it. The
// rows are a windowed table (rows.go) of the entry noun, so a huge array
// opens as fast as a small one.

// registerDetail declares the modal and its view; its keys are in bind.
func (a *App) registerDetail() {
	a.m.ModalViews[detailID] = ochazuke.ModalView{Title: "Detail", Render: a.detailView}
}

// openDetail opens the modal on a container.
func (a *App) openDetail(n *Node) error {
	if n == nil || !n.IsContainer() {
		return errors.New("only an object or an array opens in the detail")
	}
	if a.r.Instances[detailID] != nil && a.detail != nil && a.r.Instances[rowsID] != nil {
		// Open already (item.open from the palette over it): re-target it
		// to n, from the top, as a second open of any view does (R4.8).
		a.trail = []*Node{n}
		if err := a.showDetail(0); err != nil {
			return err
		}
		if a.r.TopModal() != detailID {
			return nil // the palette over it gives the focus back as it closes
		}
		return a.detail.list.Focus()
	}
	if _, err := a.r.OpenModal(detailID, ochazuke.Payload{Prompt: n.Path()}); err != nil {
		return err
	}
	if a.r.Instances[rowsID] == nil || a.detail == nil {
		v, err := newRowsView(a.r, rowsID, detailID)
		if err != nil {
			_ = a.r.Pop()
			return err
		}
		a.detail = v
	}
	a.trail = []*Node{n}
	if err := a.showDetail(0); err != nil {
		_ = a.r.Pop()
		return err
	}
	return a.detail.list.Focus()
}

// showDetail shows the innermost node of the trail with row sel selected.
func (a *App) showDetail(sel int) error {
	n := a.trail[len(a.trail)-1]
	view := a.m.ModalViews[detailID]
	view.Title = fmt.Sprintf("%s  %s", n.Path(), n.TypeLabel())
	a.m.ModalViews[detailID] = view
	return a.detail.set(fieldsSource(n, "entry"), sel)
}

// drill steps into the selected entry when it holds a container.
func (a *App) drill(n *Node) error {
	if n == nil || !n.IsContainer() {
		return errors.New("not an object or an array")
	}
	a.trail = append(a.trail, n)
	return a.showDetail(0)
}

// up steps back out to the container the trail came from, its entry
// selected; at the top it closes the modal.
func (a *App) up() error {
	if len(a.trail) <= 1 {
		return a.r.Pop()
	}
	from := a.trail[len(a.trail)-1]
	a.trail = a.trail[:len(a.trail)-1]
	return a.showDetail(from.Index)
}

func (a *App) detailView(r *ochazuke.Runtime, _ *ochazuke.Instance, w, h int) ochazuke.Block {
	if a.detail == nil || r.Instances[rowsID] == nil {
		return ochazuke.Text("")
	}
	if a.vedit != nil && a.vedit.modal == nil && a.vedit.view == a.detail && r.Focus != valueInputID {
		// The focus left the detail's inline input: its text is the edit.
		if err := a.accept(); err != nil {
			a.LoadErr = err
		}
	}
	a.refreshDetail()
	if a.detail.src.n == 0 {
		r.Placed(rowsID)
		return ochazuke.Text("(empty)")
	}
	body := a.detail.view(w, h)
	if a.vedit != nil && a.vedit.modal == nil && a.vedit.view == a.detail && r.Instances[valueInputID] != nil {
		return a.inputBlock(body, w)
	}
	return ochazuke.Text(body)
}

// detailEditable reports a detail whose rows are the edited document's
// (opened from the edited view): the value verbs patch them as they do
// the data pane's.
func (a *App) detailEditable() bool {
	if len(a.trail) == 0 || !a.editedShown() {
		return false
	}
	d := a.shownDoc()
	return d.err == nil && rootOf(a.trail[0]) == d.root
}

// rootOf is the document node n is in.
func rootOf(n *Node) *Node {
	for n != nil && n.Parent != nil {
		n = n.Parent
	}
	return n
}

// refreshDetail follows a value edit: the edited document is built
// again, so the trail is found again by path in the new one (a level
// that went is dropped), the selected row kept.
func (a *App) refreshDetail() {
	if len(a.trail) == 0 || !a.editedShown() {
		return
	}
	d := a.shownDoc()
	if d.err != nil || d.root == nil || rootOf(a.trail[0]) == d.root || rootOf(a.trail[0]) == a.Doc.Root {
		return
	}
	var trail []*Node
	for _, n := range a.trail {
		m, err := Get(d.root, n.Path())
		if err != nil || !m.IsContainer() {
			break
		}
		trail = append(trail, m)
	}
	if len(trail) == 0 {
		_ = a.r.Pop() // the value it showed is gone
		a.trail = nil
		return
	}
	a.trail = trail
	_ = a.showDetail(max(0, a.detail.Selected()))
}

// declareDetailSettings gives ochazuke's modal-highlights o-json's
// default, plain: beneath the detail (and every other modal) the page is
// drawn without reverse video or underline, so the data pane's cursor bar
// does not peek out beside the detail, which shows that row. The row keeps
// ochazuke's owner, section, enum and description; only the default is
// o-json's, and keep brings the dimmed highlights back.
func declareDetailSettings(r *ochazuke.Runtime) error {
	for _, s := range r.Settings().Declared() {
		if s.Name == ochazuke.ModalBackdropHighlightsSetting {
			s.Default = "plain"
			return r.DeclareSetting(s)
		}
	}
	return fmt.Errorf("ochazuke has no %s setting", ochazuke.ModalBackdropHighlightsSetting)
}
