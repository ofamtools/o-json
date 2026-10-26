package main

import (
	"errors"

	"github.com/unixsuperhero/ochazuke"
)

// The prompt (from o-csv): one single-line input of the query noun
// (insert pinned) in one modal, used by the search. enter commits, esc
// cancels. The label rides in the body's payload; the commit is
// application state.

const (
	promptFrame = "o-json-prompt"
	fieldID     = "query-field"
)

type question struct {
	commit func(text string) error
}

func (a *App) registerPrompt() {
	r := a.r
	r.Define("query").Pin("insert").
		Verb("commit", func(r *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			q := a.ask
			if q == nil || r.Focus != fieldID {
				return nil, errors.New("no prompt is open")
			}
			text := string(a.in.Text)
			a.ask = nil
			if err := r.Pop(); err != nil {
				return nil, err
			}
			return text, q.commit(text)
		}, "Close the prompt and run the question with the field's text").
		Verb("cancel", func(r *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			if a.ask == nil {
				return nil, errors.New("no prompt is open")
			}
			a.ask = nil
			return nil, r.Pop()
		}, "Close the prompt without running the question").
		Mode("insert").
		Bind("enter", ochazuke.Do("query.commit")).
		Bind("esc", ochazuke.Do("query.cancel"))
	// The prompt asks only for a search, so committing it is a view move.
	setHistory(r, ochazuke.HistoryAction, "query.commit", "query.cancel")
	a.in = ochazuke.NewInput(fieldID, "", false)
	a.m.ModalViews[promptFrame] = ochazuke.ModalView{Title: "Prompt", Render: func(r *ochazuke.Runtime, body *ochazuke.Instance, w, h int) ochazuke.Block {
		field := ochazuke.InputBlock(a.in, max(1, w), 1)
		return field.Offset(0, 2).WithText(body.Payload.Prompt + "\n\n" + field.Text)
	}}
}

// prompt opens the modal with a label and initial text; commit runs with
// the text when enter is pressed.
func (a *App) prompt(label, text string, commit func(string) error) error {
	if a.ask != nil {
		return errors.New("a prompt is already open")
	}
	if err := a.in.Replace(0, len(a.in.Text), text); err != nil {
		return err
	}
	a.in.Cursor = len(a.in.Text)
	view := a.m.ModalViews[promptFrame]
	view.Title = label
	a.m.ModalViews[promptFrame] = view
	a.ask = &question{commit: commit}
	if _, err := a.r.OpenModal(promptFrame, ochazuke.Payload{Prompt: label}, ochazuke.WithInput(a.in, ochazuke.Kind("query"))); err != nil {
		a.ask = nil
		return err
	}
	return nil
}
