package main

// One search line (Design D): / opens the query line in the type
// search.type starts at; tab and shift+tab cycle the type through
// searchTypes (substring, fuzzy, regex: ochazuke's match modes; jq: the
// live jq query of query.go). The type is data: each row names its
// matcher and the mode the line is in (the badge: SEARCH · SUBSTR, ...,
// INSERT for jq, whose path completion needs insert), so no key
// compares a type name.
//
// A search type's hits are every key and every plain value the matcher
// accepts, as `path │ value` lines: the Results view of the data pane
// (mode search). The jq type's answer is the jq result view (mode jq),
// drawn as the document views draw values. Both are overlays over the
// structure's own view: they join the D / tab cycle while they exist,
// and esc (search.clear) drops them, back to the remembered view.

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

const (
	overlayResults = "results"
	overlayJQ      = "jq"

	settingSearchType = "search.type"

	// maxHits caps the Results view.
	maxHits = 5000
)

// searchType is one row of the line's type cycle: its name (the
// setting's value), ochazuke's match mode ("" for jq) and the mode the
// line is in while it has this type.
type searchType struct {
	name, match, mode string
}

var searchTypes = []searchType{
	{"substring", ochazuke.MatchSubstring, "search_·_substr"},
	{"fuzzy", ochazuke.MatchFuzzy, "search_·_fuzzy"},
	{"regex", ochazuke.MatchRegex, "search_·_regex"},
	// insert, not a JQ line mode: ochazuke's completion list (the path
	// completion after ".") opens only in insert.
	{"jq", "", "insert"},
}

// searchState is the search line's type and the hits its text gives.
type searchState struct {
	typ  string
	key  string // the type, text and document the hits are for
	what string // the type and text, to tell a new search from a new document
	hits []dataLine
	err  error
	cut  bool
	view *Shape // the Results view's shape (its title)
	cur  int    // the hit n / N stand on
}

func (a *App) searchTypeRow() searchType {
	i := slices.IndexFunc(searchTypes, func(t searchType) bool { return t.name == a.sr.typ })
	if i < 0 {
		return searchTypes[0]
	}
	return searchTypes[i]
}

// searchJQ reports the line's type is jq: the query runs.
func (a *App) searchJQ() bool { return a.searchTypeRow().match == "" }

// lineMode is the mode the line is in for its type.
func (a *App) lineMode() string { return a.searchTypeRow().mode }

// lineModes is every type's line mode.
func lineModes() []string {
	out := make([]string, len(searchTypes))
	for i, t := range searchTypes {
		out[i] = t.mode
	}
	return out
}

// refreshSearch recomputes the hits when the type, the text or the
// document changed; a new type or text shows its view (the overlay).
func (a *App) refreshSearch() {
	text := strings.TrimSpace(a.queryText())
	root := a.queryRoot()
	what := a.sr.typ + "\x00" + text
	key := fmt.Sprintf("%s\x00%p", what, root)
	if key == a.sr.key {
		return
	}
	fresh := what != a.sr.what
	a.sr.key, a.sr.what = key, what
	a.sr.hits, a.sr.err, a.sr.cut, a.sr.view = nil, nil, false, nil
	if a.searchJQ() {
		switch {
		case text == "" && a.overlay == overlayJQ:
			a.overlay = ""
		case text != "" && fresh:
			a.overlay = overlayJQ
		case text != "" && a.overlay == overlayResults:
			a.overlay = overlayJQ
		}
		return
	}
	if text == "" || root == nil {
		if a.overlay != "" {
			a.overlay = ""
		}
		return
	}
	m := a.r.Matcher(a.searchTypeRow().match, text)
	if err := m.Err(); err != nil {
		a.sr.err = err
		return
	}
	a.sr.hits, a.sr.cut = searchHits(root, m)
	a.sr.view = &Shape{Path: "search /" + text, Kinds: map[Kind]int{}}
	a.sr.cur = 0
	if fresh || a.overlay == overlayJQ {
		a.overlay = overlayResults
	}
}

// searchHits is every node whose key or plain value m matches, in
// document order, as Results lines.
func searchHits(root *Node, m ochazuke.Matcher) ([]dataLine, bool) {
	var out []dataLine
	cut := false
	var walk func(*Node)
	walk = func(n *Node) {
		if cut {
			return
		}
		onKey := n.Parent != nil && n.Parent.Kind == KObject && m.Match(n.Key)
		onValue := !n.IsContainer() && m.Match(Text(n, 0))
		if onKey || onValue {
			if len(out) == maxHits {
				cut = true
				return
			}
			out = append(out, dataLine{text: n.Path() + " │ " + oneLine(Compact(n, cellLimit)), node: n, partner: -1})
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out, cut
}

// setSearchType steps the line's type (tab, shift+tab), keeping its
// text, and puts the line in the type's mode.
func (a *App) setSearchType(steps int) error {
	i := slices.IndexFunc(searchTypes, func(t searchType) bool { return t.name == a.searchTypeRow().name })
	n := len(searchTypes)
	a.sr.typ = searchTypes[((i+steps)%n+n)%n].name
	a.refreshSearch()
	if a.r.Focus == jqFieldID {
		return a.r.SetMode(a.lineMode())
	}
	return nil
}

// focusSearch is /: the line in the type search.type names (the type
// last used while a search is live).
func (a *App) focusSearch() error {
	if strings.TrimSpace(a.queryText()) == "" {
		a.sr.typ = a.r.Settings().String(settingSearchType)
	}
	return a.focusQuery()
}

// acceptSearch is enter on the line: leave it; with hits or a jq result,
// the data pane takes the focus on that view.
func (a *App) acceptSearch(c ochazuke.Ctx) error {
	if err := a.leaveQuery(); err != nil {
		return err
	}
	switch {
	case a.sr.err != nil:
		return a.sr.err
	case a.overlay == overlayResults:
		n := len(a.sr.hits)
		c.Info(fmt.Sprintf("/%s: %d hit%s · n/N · enter jumps · esc clears", strings.TrimSpace(a.queryText()), n, plural(n)))
		return a.data.list.Focus()
	case a.overlay == overlayJQ && a.q.goodView != nil:
		return a.data.list.Focus()
	}
	return nil
}

// cancelSearch is esc on the line: a search type's text is dropped (the
// search is cancelled, as Vim's esc on /); the jq type keeps its query
// and result, as the query line always has.
func (a *App) cancelSearch() error {
	if !a.searchJQ() {
		if err := a.setQuery(""); err != nil {
			return err
		}
		a.refreshSearch()
	}
	return a.leaveQuery()
}

// clearSearch is esc on a pane while a search or jq result is live: the
// text goes, the overlay with it, back to the remembered view.
func (a *App) clearSearch(c ochazuke.Ctx) error {
	jq := a.searchJQ()
	if err := a.setQuery(""); err != nil {
		return err
	}
	a.refreshSearch()
	a.overlay = ""
	if jq {
		c.Info("jq result cleared")
	} else {
		c.Info("search cleared")
	}
	return nil
}

// stepHit is n / N (and ctrl+n / ctrl+p on the line): the next hit. In
// the Results view the cursor moves; elsewhere the structure cursor
// jumps to the hit's path.
func (a *App) stepHit(c ochazuke.Ctx, steps int) error {
	n := len(a.sr.hits)
	if n == 0 {
		return errors.New("no hits")
	}
	if a.overlay == overlayResults && a.shown == a.sr.view && (a.inData(a.r.Focus) || a.r.Focus == jqFieldID) {
		cur := a.data.Selected()
		if a.r.Focus == jqFieldID {
			cur = a.sr.cur
		}
		a.sr.cur = ((cur+steps)%n + n) % n
		return a.data.to(a.sr.cur)
	}
	a.sr.cur = ((a.sr.cur+steps)%n + n) % n
	hit := a.sr.hits[a.sr.cur]
	c.Info(fmt.Sprintf("hit %d of %d: %s", a.sr.cur+1, n, hit.node.Path()))
	return a.jumpStructure(hit.node.Path())
}

func (a *App) registerSearch() error {
	r := a.r
	if err := declareOwn(r, ochazuke.Setting{
		Name: settingSearchType, Type: ochazuke.SettingString, Default: "substring",
		Enum:        []string{"substring", "fuzzy", "regex", "jq"},
		Description: "The type / opens the search line in (tab and shift+tab cycle it there): substring, fuzzy or regex search keys and values into the Results view; jq runs the query live",
	}); err != nil {
		return err
	}
	a.sr.typ = r.Settings().String(settingSearchType)
	done := func(err error) ochazuke.Result {
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(nil)
	}
	r.DefineGuard("search-jq", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.searchJQ() })
	r.DefineGuard("search-live", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool {
		return a.overlay != "" || len(a.sr.hits) > 0
	})
	r.DefineGuard("search-hits", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return len(a.sr.hits) > 0 })
	r.Define("search").
		Verb("focus", func(ochazuke.Ctx) ochazuke.Result { return done(a.focusSearch()) },
			"Open the search line (/) in the type search.type names; tab and shift+tab cycle substring, fuzzy, regex and jq").When("query-line").
		Verb("type", func(c ochazuke.Ctx) ochazuke.Result {
			return done(a.setSearchType(ochazuke.IntArg(c.Args, "steps", 1)))
		}, "Step the search line's type (tab; shift+tab back): substring, fuzzy, regex, jq").When("query-line").
		Verb("accept", func(c ochazuke.Ctx) ochazuke.Result { return done(a.acceptSearch(c)) },
			"Leave the search line (enter): its hits or jq result show in the data pane, which takes the focus").When("query-focused").
		Verb("cancel", func(ochazuke.Ctx) ochazuke.Result { return done(a.cancelSearch()) },
			"Leave the search line (esc): a search is dropped, a jq query is kept").When("query-focused").
		Verb("clear", func(c ochazuke.Ctx) ochazuke.Result { return done(a.clearSearch(c)) },
			"Clear the search or the jq query (esc): its view goes, the data pane shows the structure's view again").When("search-live").
		Verb("next", func(c ochazuke.Ctx) ochazuke.Result {
			return done(a.stepHit(c, ochazuke.IntArg(c.Args, "steps", 1)))
		}, "The next hit (n; N back): in the Results view the cursor moves, elsewhere the structure cursor jumps to it").Motion().When("search-hits").
		Verb("state", func(ochazuke.Ctx) ochazuke.Result {
			out := map[string]any{"type": a.sr.typ, "text": a.queryText(), "hits": len(a.sr.hits), "view": a.overlay}
			if a.sr.err != nil {
				out["error"] = a.sr.err.Error()
			}
			return ochazuke.Done(out)
		}, "The search line's type, text, hit count and the overlay shown").Fetch()
	setParams(r, "search.type", map[string]ochazuke.Parameter{"steps": {Type: "integer", Description: "types to step; negative goes back"}})
	setParams(r, "search.next", map[string]ochazuke.Parameter{"steps": {Type: "integer", Description: "hits to step; negative goes back"}})
	setHistory(r, ochazuke.HistoryAction, "search.focus", "search.type", "search.accept", "search.cancel", "search.clear", "search.next")
	return nil
}
