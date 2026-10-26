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
	if a.detail.src.n == 0 {
		r.Placed(rowsID)
		return ochazuke.Text("(empty)")
	}
	return ochazuke.Text(a.detail.view(w, h))
}
