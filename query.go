package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
)

// The live jq query. One single-line input (jqFieldID) holds the query;
// its text is the only query state, and everything else follows it: the
// results in the data pane, the chips on the query line (querylinego),
// the path the structure pane shows under the cursor.
//
// Runs are off the loop. After every event the runner (an ochazuke
// Component, as the library's completion list is) compares the query and
// the document it runs on with the last run; a change waits a pause
// (debounce), then runs as a job in one slot, so a newer run cancels the
// older (gojq stops on the cancelled context) and a late answer is
// dropped. The answer's results replace the data pane's; an error keeps
// the last good results on screen and shows on the query line with its
// position. An empty query shows the document as the tree's selection
// does, as it always has.
//
// The query runs on the document the viewer shows: the edited
// (restructured) one, or the original after T.

const (
	jqFieldID = "jq-field" // the query line's input
	jqRunID   = "jq-run"   // the runner

	settingQueryLine = "query.line"
	settingChipFocus = "query.chip-focus"

	// jqDebounce is the pause after the last change before a run.
	jqDebounce = 150 * time.Millisecond
)

func declareQuerySettings(r *ochazuke.Runtime) error {
	return declareOwn(r, ochazuke.Setting{
		Name: settingQueryLine, Type: ochazuke.SettingBoolean, Default: true,
		Description: "The jq query line above the structure pane (/ focuses it) and the structure pane's query mode; off, / is the path search (viewer.search) and the data pane follows the tree alone",
	}, ochazuke.Setting{
		Name: settingChipFocus, Type: ochazuke.SettingString, Default: "cursor", Enum: []string{"cursor", "line"},
		Description: "The query line's field chips sit on a dim background; while the line is focused, cursor brightens the chip under the cursor, line brightens every chip",
	})
}

// queryState is the live query: the input, the runner and the answers.
type queryState struct {
	in       *ochazuke.Input
	run      *jqRunner
	last     *jqOut // the latest answer: results or an error
	good     *jqOut // the latest answer with results, shown while the query is not empty
	goodView *Shape // good's results as the data pane shows them
	running  bool   // a run of the current query has not answered

	toks     []jqTok // the query's tokens (queryTokens), for toksFor
	toksFor  string
	from     string // the focus / came from, where esc returns
	fromMode string // and its mode (query mode stays on)
}

// queryOn reports the query line (the query.line setting).
func (a *App) queryOn() bool { return a.r.Settings().Bool(settingQueryLine) }

// queryText is the query as typed.
func (a *App) queryText() string { return string(a.q.in.Text) }

// queryShowing reports the data pane showing the query's results.
func (a *App) queryShowing() bool {
	return a.queryOn() && a.Doc != nil && strings.TrimSpace(a.queryText()) != "" && a.q.goodView != nil
}

// queryRoot is the document the query runs on: the shown one.
func (a *App) queryRoot() *Node {
	if a.Doc == nil {
		return nil
	}
	if a.editedShown() {
		return a.shownDoc().root // the last built while a slow rebuild runs; the query runs again on the new one
	}
	return a.Doc.Root
}

// setQuery replaces the query's text, the cursor at its end, as one
// undoable edit.
func (a *App) setQuery(text string) error {
	in := a.q.in
	if err := in.Replace(0, len(in.Text), text); err != nil {
		return err
	}
	in.Cursor = len(in.Text)
	return nil
}

// answered takes a run's answer.
func (a *App) answered(out jqOut) {
	a.q.last, a.q.running = &out, false
	if out.Err != nil {
		return
	}
	a.q.good = &out
	// The results panel's title says when the order is gojq's, not the
	// document's (as the breadcrumb's note does).
	label := "jq " + strings.TrimSpace(out.Query)
	switch {
	case out.Sorted:
		label += "  keys sorted"
	case out.Reordered:
		label += "  not in document order"
	}
	s := &Shape{Path: label, Kinds: map[Kind]int{}}
	for _, v := range out.Values {
		s.add(v)
	}
	s.finish()
	a.q.goodView = s
}

// forgetQuery drops the answers (a new document, an emptied query).
func (a *App) forgetQuery() {
	a.q.last, a.q.good, a.q.goodView, a.q.running = nil, nil, nil, false
}

// jqRunner runs the query off the loop (see the file comment).
type jqRunner struct {
	a        *App
	Debounce time.Duration
	wanted   string // the query and document the latest request is for
	seq      uint64 // the latest request
	due      uint64 // a request whose pause is over, to launch at the next drain
	ticket   ochazuke.Ticket
}

type jqTick struct{ seq uint64 }

func (j *jqRunner) key() (string, string, *Node) {
	a := j.a
	if !a.queryOn() || a.Doc == nil {
		return "", "", nil
	}
	q := a.queryText()
	root := a.queryRoot()
	return fmt.Sprintf("%p\x00%s", root, q), q, root
}

// PendingCommand starts the pause of a changed query, or the run whose
// pause is over.
func (j *jqRunner) PendingCommand() tea.Cmd {
	key, q, root := j.key()
	a := j.a
	if key != j.wanted {
		j.wanted = key
		j.seq++
		j.due = 0
		if strings.TrimSpace(q) == "" || root == nil {
			// Nothing to run: the document shows as the tree's selection
			// does; a run still going is cancelled.
			a.forgetQuery()
			j.cancel()
			return nil
		}
		a.q.running = true
		if j.Debounce > 0 {
			seq := j.seq
			return tea.Tick(j.Debounce, func(time.Time) tea.Msg { return jqTick{seq: seq} })
		}
		j.due = j.seq
	}
	if j.due == 0 || j.due != j.seq || root == nil {
		return nil
	}
	j.due = 0
	ticket, ctx, err := a.r.Issue(jqRunID, "jq.run", "run", ochazuke.Ctx{Runtime: a.r})
	if err != nil {
		a.answered(jqOut{Query: q, Err: err, Pos: -1})
		return nil
	}
	j.ticket = ticket
	return func() tea.Msg {
		return ochazuke.Completion{Ticket: ticket, Value: queryDoc(ctx, root, q)}
	}
}

// cancel stops the running job, if any.
func (j *jqRunner) cancel() {
	if j.a.r.Accepts(j.ticket) {
		j.a.r.Settle(j.ticket, errors.New("superseded"))
	}
}

func (j *jqRunner) HandleMessage(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case jqTick:
		if msg.seq == j.seq {
			j.due = msg.seq
		}
		return true
	case ochazuke.Completion:
		if msg.Ticket.Owner != jqRunID {
			return false
		}
		if out, ok := msg.Value.(jqOut); ok && msg.Ticket == j.ticket {
			j.a.answered(out)
		}
		return true
	}
	return false
}

func (j *jqRunner) Close() { j.cancel() }

// registerQuery adds the query's input, its runner, the jq noun and the
// setting.
func (a *App) registerQuery() error {
	r := a.r
	if err := declareQuerySettings(r); err != nil {
		return err
	}
	r.Define("jq")
	r.Define("jq-input")
	a.q.in = ochazuke.NewInput(jqFieldID, "", false)
	if err := declareCompleteSettings(r); err != nil {
		return err
	}
	if err := r.AddInput(a.q.in, ochazuke.Parent(pageID), ochazuke.Kind("jq-input")); err != nil {
		return err
	}
	a.applyCompletePaths()
	r.Settings().Observe(settingCompletePaths, func(string, any) { a.applyCompletePaths() })
	// The query line is focused only by / (jq.focus): it is no focus stop
	// of the page's tab ring.
	r.Instances[jqFieldID].Unfocusable = true
	a.q.run = &jqRunner{a: a, Debounce: jqDebounce}
	r.Instance(jqRunID, ochazuke.Kind("jq")).Component = a.q.run
	r.DefineGuard("query-line", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.queryOn() && a.Doc != nil })
	r.DefineGuard("query-focused", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool { return ev.Focus == jqFieldID })
	r.DefineGuard("query-selecting", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return ev.Focus == jqFieldID && strings.HasPrefix(a.q.in.Mode, "visual")
	})
	r.Define("jq").
		Verb("set", func(c ochazuke.Ctx) ochazuke.Result {
			if err := a.setQuery(ochazuke.StringArg(c.Args, "query", "")); err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(a.queryState())
		}, "Replace the jq query (the data pane shows its results as they come; empty shows the document)").When("query-line").
		Verb("clear", func(c ochazuke.Ctx) ochazuke.Result {
			if err := a.setQuery(""); err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(a.queryState())
		}, "Empty the jq query: the data pane follows the structure again").When("query-line").
		Verb("yank", func(c ochazuke.Ctx) ochazuke.Result {
			q := a.queryText()
			if strings.TrimSpace(q) == "" {
				return ochazuke.Failed(errors.New("no query to copy"))
			}
			return a.copyText(c, q, "the query")
		}, "Copy the jq query into the registers and the system clipboard").When("query-line").
		Verb("focus", func(c ochazuke.Ctx) ochazuke.Result {
			return a.queryDone(a.focusQuery())
		}, "Focus the jq query line, typing at its end (/); esc goes back where it came from, the query kept").When("query-line").
		Verb("leave", func(c ochazuke.Ctx) ochazuke.Result {
			return a.queryDone(a.leaveQuery())
		}, "Leave the jq query line for where / came from (the structure pane), the query kept").When("query-focused").
		Verb("wrap", func(c ochazuke.Ctx) ochazuke.Result {
			return a.queryDone(a.wrapSelection(ochazuke.StringArg(c.Args, "open", "[")))
		}, "Wrap the query line's visual selection in brackets: [ ] (collect), { } (an object) or ( ) (group), and go on typing after it").When("query-selecting").
		Verb("state", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(a.queryState()) }, "The jq query, its results' count, the error and where it points").Fetch()
	params := func(key string, p map[string]ochazuke.Parameter) {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = p
		r.Operations[r.CanonicalKey(key)] = op
	}
	params("jq.set", map[string]ochazuke.Parameter{"query": {Type: "string", Description: "the jq query; empty clears it"}})
	params("jq.wrap", map[string]ochazuke.Parameter{"open": {Type: "string", Enum: []string{"[", "{", "("}, Description: "the opening bracket; its pair closes"}})
	setHistory(r, ochazuke.HistoryAction, "jq.set", "jq.clear", "jq.focus", "jq.leave", "jq.wrap")
	setHistory(r, ochazuke.HistoryChange, "jq.yank")
	return nil
}

// queryNote is what the query's answer says beside its results: the
// count and the order caveat, or the error and the column it points at.
func (a *App) queryNote() string {
	if !a.queryOn() || a.Doc == nil || strings.TrimSpace(a.queryText()) == "" {
		return ""
	}
	if l := a.q.last; l != nil && l.Err != nil {
		if l.Pos >= 0 {
			return fmt.Sprintf("jq error at column %d: %v", l.Pos+1, l.Err)
		}
		return "jq error: " + l.Err.Error()
	}
	g := a.q.good
	if g == nil || !a.queryShowing() {
		return ""
	}
	n := len(g.Values)
	note := fmt.Sprintf("%d result%s", n, plural(n))
	if g.Cut {
		note = fmt.Sprintf("first %d results", n)
	}
	switch {
	case g.Sorted:
		note += ", keys sorted by jq"
	case g.Reordered:
		note += ", not in document order"
	}
	if a.q.running {
		note += " (running…)"
	}
	return note
}

func (a *App) queryDone(err error) ochazuke.Result {
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(a.queryState())
}

// focusQuery focuses the query line in insert, the cursor at the end,
// remembering where the focus and the mode were for leaveQuery.
func (a *App) focusQuery() error {
	if a.r.Focus != jqFieldID {
		a.q.from, a.q.fromMode = a.r.Focus, a.r.BaseMode()
	}
	if err := a.r.SetFocus(jqFieldID); err != nil {
		return err
	}
	if err := a.r.SetMode("insert"); err != nil {
		return err
	}
	a.q.in.Cursor = len(a.q.in.Text)
	return nil
}

// leaveQuery goes back where / came from (in the mode it was in), else to
// the structure pane shown.
func (a *App) leaveQuery() error {
	if a.r.SetFocus(a.q.from) != nil {
		t := a.tree
		if a.editedShown() {
			t = a.rs.tree
		}
		a.q.fromMode = ""
		if err := t.Focus(); err != nil {
			return err
		}
	}
	if m := a.q.fromMode; m != "" && m != "normal" && a.r.Modes[m] {
		return a.r.SetMode(m)
	}
	return nil
}

// wrapSelection puts the visual selection between open and its pair, as
// one edit, and goes on typing after it.
func (a *App) wrapSelection(open string) error {
	pairs := map[string]string{"[": "]", "{": "}", "(": ")"}
	cls, ok := pairs[open]
	if !ok {
		return fmt.Errorf("no bracket %q: [, { or (", open)
	}
	in := a.q.in
	lo, hi, ok := in.Selection()
	if !ok {
		return errors.New("no selection to wrap: ctrl+v selects")
	}
	inner := string(in.Text[lo:hi])
	if err := in.Replace(lo, hi, open+inner+cls); err != nil {
		return err
	}
	if err := a.r.SetMode("insert"); err != nil {
		return err
	}
	in.Cursor = lo + len([]rune(open+inner+cls))
	return nil
}

// queryState is the query as jq.state answers it.
func (a *App) queryState() map[string]any {
	out := map[string]any{"query": a.queryText(), "running": a.q.running}
	if l := a.q.last; l != nil {
		if l.Err != nil {
			out["error"] = l.Err.Error()
			if l.Pos >= 0 {
				out["position"] = l.Pos
			}
		}
	}
	if g := a.q.good; g != nil && a.queryShowing() {
		out["results"] = len(g.Values)
		out["paths"] = g.Paths
		if g.Sorted {
			out["sorted"] = true
		}
		if g.Cut {
			out["cut"] = true
		}
	}
	return out
}
