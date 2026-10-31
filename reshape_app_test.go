package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

const reshapeFile = "testdata/reshape.json"

// onNode selects the restructured tree's node at a path of the
// restructured document.
func (a *App) onNode(t *testing.T, path string) string {
	t.Helper()
	selectIn(t, a, a.rs.tree, func(n ochazuke.TreeNode) bool {
		return nodePaths(a.rs.tree.Nodes(), a.rs.meta)[n.ID] == path
	})
	n, _ := a.rs.tree.Selected()
	return n.ID
}

// markPaths marks the nodes at paths with space.
func markPaths(t *testing.T, a *App, paths ...string) {
	t.Helper()
	for _, p := range paths {
		a.onNode(t, p)
		press(t, a, "space")
	}
}

func openReshape(t *testing.T, width, height int) *App {
	t.Helper()
	a := open(t, reshapeFile, width, height)
	if !a.editedShown() {
		t.Fatal("not the edited view")
	}
	return a
}

func TestMarksSurviveCollapseMovesAndUndo(t *testing.T) {
	a := openReshape(t, 120, 30)
	markPaths(t, a, ".people[].name", ".people[].address.city")
	if got := a.nodePathsOf(a.markedIDs()); strings.Join(got, " ") != ".people[].name .people[].address.city" {
		t.Fatalf("marks %v", got)
	}
	if !strings.Contains(view(a), "2 marked") {
		t.Fatalf("no count in the title\n%s", view(a))
	}
	// Collapsing the array hides the marked rows; the marks stay.
	a.onNode(t, ".people")
	press(t, a, "h")
	if len(a.markedIDs()) != 2 || !strings.Contains(view(a), "2 marked") {
		t.Fatalf("collapse lost marks: %v", a.markedIDs())
	}
	press(t, a, "l")
	// A move to another level keeps the mark (the node keeps its id).
	city := a.onNode(t, ".people[].address.city")
	press(t, a, "<", "<")
	if !a.rs.marks[city] || len(a.markedIDs()) != 2 {
		t.Fatalf("dedent lost the mark")
	}
	press(t, a, "u")
	if !a.rs.marks[city] {
		t.Fatal("undo lost the mark")
	}
	// space again unmarks; g u clears.
	a.onNode(t, ".people[].name")
	press(t, a, "space")
	if len(a.markedIDs()) != 1 {
		t.Fatalf("unmark: %v", a.markedIDs())
	}
	press(t, a, "g", "u")
	if len(a.markedIDs()) != 0 || strings.Contains(view(a), "marked") {
		t.Fatalf("clear: %v", a.markedIDs())
	}
	if err := a.r.Settings().Set(settingMarksCount, false); err != nil {
		t.Fatal(err)
	}
	markPaths(t, a, ".title")
	if strings.Contains(view(a), "1 marked") {
		t.Fatal("marks.count off still counts")
	}
}

func TestFlattenUnderEachArraysMode(t *testing.T) {
	for mode, want := range map[string]string{
		arraysJoin: `[{"name":"Ann","city":"Oslo","tags":"a;b"},{"name":"Bo","city":"Rome","tags":""},{"name":"Cy","tags":"c"},{"name":"Dee"}]`,
		arraysJSON: `[{"name":"Ann","city":"Oslo","tags":"[\"a\",\"b\"]"},{"name":"Bo","city":"Rome","tags":"[]"},{"name":"Cy","tags":"[\"c\"]"},{"name":"Dee"}]`,
		arraysRows: `[{"name":"Ann","city":"Oslo","tags":"a"},{"name":"Ann","city":"Oslo","tags":"b"},{"name":"Bo","city":"Rome"},{"name":"Cy","tags":"c"},{"name":"Dee"}]`,
	} {
		t.Run(mode, func(t *testing.T) {
			a := openReshape(t, 120, 30)
			if err := a.r.Settings().Set(settingFlattenArrays, mode); err != nil {
				t.Fatal(err)
			}
			before := output(t, a)
			markPaths(t, a, ".people[].name", ".people[].address.city", ".people[].tags")
			press(t, a, "F")
			if got := agreesWith(t, a, reshapeFile); !sameJSON(t, got, want) {
				t.Fatalf("flatten\n got %s\nwant %s", got, want)
			}
			m := a.Mapping()
			if len(m.Then) != 1 || m.Then[0].Op != opFlatten || m.Then[0].Arrays != mode || m.Then[0].Map != nil {
				t.Fatalf("mapping %+v", m.Then)
			}
			// The tree is the output's copy: the data pane shows it.
			if a.rs.in == nil || len(a.rs.tree.Nodes()) < 3 {
				t.Fatalf("tree %+v", a.rs.tree.Nodes())
			}
			if !strings.Contains(view(a), "Oslo") {
				t.Fatalf("data pane\n%s", view(a))
			}
			// One undo step brings the tree and its marks back.
			press(t, a, "u")
			if got := output(t, a); got != before || len(a.Mapping().Then) != 0 || len(a.markedIDs()) != 3 {
				t.Fatalf("undo: %s, %d marks", got, len(a.markedIDs()))
			}
			press(t, a, "ctrl+r")
			if got := output(t, a); !sameJSON(t, got, want) {
				t.Fatalf("redo %s", got)
			}
		})
	}
}

// Edits after a step are the step's map: a rename in the flat result.
func TestEditAfterFlattenIsTheStepsMap(t *testing.T) {
	a := openReshape(t, 120, 30)
	markPaths(t, a, ".people[].name", ".people[].address.city")
	press(t, a, "F")
	id := a.onNode(t, ".[].city")
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "rename", Target: id, Args: ochazuke.Arguments{"key": "town"}}); err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"Ann","town":"Oslo"},{"name":"Bo","town":"Rome"},{"name":"Cy"},{"name":"Dee"}]`
	if got := agreesWith(t, a, reshapeFile); !sameJSON(t, got, want) {
		t.Fatalf("rename\n got %s\nwant %s", got, want)
	}
	if m := a.Mapping(); len(m.Then[0].Map) == 0 {
		t.Fatalf("the step's map is empty: %+v", m.Then[0])
	}
	// The original tree's s sends nothing into a later tree.
	if a.available("shape", "send") {
		t.Fatal("send available after a reshape step")
	}
	// A value edit after the step is the step's patch.
	press(t, a, "u", "u")
	if len(a.Mapping().Then) != 0 {
		t.Fatalf("two undos: %+v", a.Mapping().Then)
	}
}

func TestPickKeepsMarkedFieldsAtTheirPaths(t *testing.T) {
	a := openReshape(t, 120, 30)
	markPaths(t, a, ".people[].name", ".people[].address.city")
	press(t, a, "g", "p")
	want := `{"people":[{"name":"Ann","address":{"city":"Oslo"}},{"name":"Bo","address":{"city":"Rome"}},{"name":"Cy"},{"name":"Dee"}]}`
	if got := agreesWith(t, a, reshapeFile); !sameJSON(t, got, want) {
		t.Fatalf("pick\n got %s\nwant %s", got, want)
	}
	if len(a.Mapping().Then) != 0 {
		t.Fatal("pick is a tree edit, not a step")
	}
	press(t, a, "u")
	if got := output(t, a); !strings.Contains(got, `"zip"`) {
		t.Fatalf("undo %s", got)
	}
}

func keyPeople(t *testing.T, a *App, args ochazuke.Arguments) {
	t.Helper()
	id := a.onNode(t, ".people")
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "key-by", Target: id, Args: args}); err != nil {
		t.Fatal(err)
	}
}

func TestKeyByUnderEachSetting(t *testing.T) {
	ann := `{"id":"p1","name":"Ann","address":{"city":"Oslo","zip":"0150"},"tags":["a","b"]}`
	bo := `{"id":"p2","name":"Bo","address":{"city":"Rome"},"tags":[]}`
	cy := `{"id":"p1","name":"Cy","tags":["c"]}`
	for _, tc := range []struct {
		name     string
		settings map[string]any
		want     string
	}{
		{"rename", nil, `{"title":"people","people":{"p1":` + ann + `,"p2":` + bo + `,"p1-2":` + cy + `,"_missing":[{"name":"Dee"}]}}`},
		{"last", map[string]any{settingKeyByDup: dupLast}, `{"title":"people","people":{"p1":` + cy + `,"p2":` + bo + `,"_missing":[{"name":"Dee"}]}}`},
		{"group", map[string]any{settingKeyByDup: dupGroup}, `{"title":"people","people":{"p1":[` + ann + `,` + cy + `],"p2":[` + bo + `],"_missing":[{"name":"Dee"}]}}`},
		{"drop the field, name the missing key", map[string]any{settingKeyByKeep: false, settingKeyByMissing: "none"},
			`{"title":"people","people":{"p1":` + strings.Replace(ann, `"id":"p1",`, "", 1) + `,"p2":` + strings.Replace(bo, `"id":"p2",`, "", 1) + `,"p1-2":` + strings.Replace(cy, `"id":"p1",`, "", 1) + `,"none":[{"name":"Dee"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := openReshape(t, 120, 30)
			for k, v := range tc.settings {
				if err := a.r.Settings().Set(k, v); err != nil {
					t.Fatal(err)
				}
			}
			keyPeople(t, a, ochazuke.Arguments{"field": "id"})
			if got := agreesWith(t, a, reshapeFile); !sameJSON(t, got, tc.want) {
				t.Fatalf("key-by\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestKeyByFieldChooserOffersScalarFields(t *testing.T) {
	a := openReshape(t, 120, 30)
	id := a.onNode(t, ".people")
	if got := strings.Join(a.keyFields(id), " "); got != "name id" && got != "id name" {
		t.Fatalf("fields %q (address and tags are not scalars)", got)
	}
	if a.onNode(t, ".title"); a.available("node", "key-by") {
		t.Fatal("key-by on a string")
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "key-by", Target: id, Args: ochazuke.Arguments{"field": "address"}}); err == nil {
		t.Fatal("key-by took an object field")
	}
}

func TestValuesAndEntriesReverseKeyBy(t *testing.T) {
	a := openReshape(t, 120, 30)
	keyPeople(t, a, ochazuke.Arguments{"field": "name", "keep": true})
	keyed := output(t, a)
	a.onNode(t, ".people")
	press(t, a, "g", "v")
	if got := agreesWith(t, a, reshapeFile); !strings.HasPrefix(got, `{"title":"people","people":[{"name":"Ann","id":"p1"`) || strings.Count(got, `"name"`) != 4 {
		t.Fatalf("values %s", got)
	}
	if m := a.Mapping(); len(m.Then) != 2 || m.Then[1].Op != opValues {
		t.Fatalf("steps %+v", m.Then)
	}
	press(t, a, "u")
	if output(t, a) != keyed {
		t.Fatal("undo of values")
	}
	a.onNode(t, ".people")
	press(t, a, "g", "e")
	got := agreesWith(t, a, reshapeFile)
	if !strings.Contains(got, `[{"key":"Ann","value":{"name":"Ann","id":"p1"`) {
		t.Fatalf("entries %s", got)
	}
}

// w writes the reshaped document; the mapping file it exports runs the
// same through o-json restructure --map (agreesWith).
func TestWriteAfterAReshapeStep(t *testing.T) {
	a := openReshape(t, 120, 30)
	markPaths(t, a, ".people[].id", ".people[].name")
	press(t, a, "F")
	out := filepath.Join(t.TempDir(), "flat.json")
	if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "write", Args: ochazuke.Arguments{"path": out}}); err != nil {
		t.Fatal(err)
	}
	settle(a)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	root, err := Build(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Compact(root, 0), agreesWith(t, a, reshapeFile); got != want {
		t.Fatalf("written %s\nwant %s", got, want)
	}
}

// The frames at 80 and 120 columns: a marked tree, and a key-by result.
func TestReshapeGolden(t *testing.T) {
	for _, width := range []int{80, 120} {
		for name, setup := range map[string]func(*App){
			"marked": func(a *App) {
				markPaths(t, a, ".people[].name", ".people[].address.city")
			},
			"key-by": func(a *App) {
				keyPeople(t, a, ochazuke.Arguments{"field": "id"})
				a.onNode(t, ".people")
			},
		} {
			a := openReshape(t, width, 24)
			setup(a)
			got := view(a)
			golden := filepath.Join("testdata", "reshape-"+name+"-"+itoa(width)+".golden")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				_ = os.WriteFile(golden, []byte(got), 0o644)
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("%s at %d columns\n%s\nwant\n%s", name, width, got, want)
			}
		}
	}
}

// Persistent marks take priority over the cursor and a visual range, including
// the modes whose own tables shadow normal's rows. Draw between keys, as the
// real loop does (especially after Escape).
func TestFlattenMarksAcrossContexts(t *testing.T) {
	for name, keys := range map[string][]string{
		"shape":         nil,
		"edit":          {"i"},
		"split-edited":  {"ctrl+w", "v", "ctrl+a", "o"},
		"single-edited": {"ctrl+w", "v", "L", "ctrl+a", "o"},
		"visual":        {"v"},
		"after-visual":  {"v", "esc"},
		"jq":            {"m"},
		"original":      {"T"},
	} {
		t.Run(name, func(t *testing.T) {
			a := openReshape(t, 120, 30)
			gated(t, a)
			before := output(t, a)
			markPaths(t, a, ".people[].name", ".people[].address.city")
			a.onNode(t, ".title") // neither the cursor nor this V range is marked
			for _, key := range keys {
				press(t, a, key)
			}
			if hints := ochazuke.BindingHints(a.r, a.r.Focus); !strings.Contains(hints, "F flatten") {
				t.Fatalf("missing flatten hint: %s", hints)
			}
			press(t, a, "F")
			want := `[{"name":"Ann","city":"Oslo"},{"name":"Bo","city":"Rome"},{"name":"Cy"},{"name":"Dee"}]`
			if got := agreesWith(t, a, reshapeFile); !sameJSON(t, got, want) {
				t.Fatalf("flatten: %s", got)
			}
			if !a.editedShown() || !strings.Contains(view(a), "Oslo") {
				t.Fatalf("result not shown\n%s", view(a))
			}
			if name == "jq" {
				press(t, a, "m") // u in JQ still undoes a query edit
			}
			press(t, a, "u")
			if got := output(t, a); got != before || len(a.markedIDs()) != 2 {
				t.Fatalf("undo: %s marks %v", got, a.markedIDs())
			}
		})
	}
}

// Source marks are a new extraction from the source, even after a reshape and
// value edits. The previous mapping, patches and steps must all return on u.
func TestFlattenOriginalMarksReplacesAndUndoes(t *testing.T) {
	for _, layout := range []string{"split", "single"} {
		t.Run(layout, func(t *testing.T) {
			a := openReshape(t, 120, 30)
			gated(t, a)
			markPaths(t, a, ".people[].name")
			press(t, a, "F")
			id := a.onNode(t, ".[].name")
			if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "rename", Target: id, Args: ochazuke.Arguments{"key": "person"}}); err != nil {
				t.Fatal(err)
			}
			if err := a.commitWith("patch", id, a.current(), []Patch{{Op: opSet, Path: ".[0].person", Value: []byte(`"changed"`)}}); err != nil {
				t.Fatal(err)
			}
			before, mapping := output(t, a), mappingKey(a.Mapping())
			if err := a.r.Settings().Set(settingLayout, layout); err != nil {
				t.Fatal(err)
			}
			press(t, a, "ctrl+w", "v")
			selectIn(t, a, a.rs.orig, func(n ochazuke.TreeNode) bool {
				s, _ := n.Data.(*Shape)
				return s != nil && s.Path == ".people[].id"
			})
			press(t, a, "space")
			press(t, a, "F")
			want := `[{"id":"p1"},{"id":"p2"},{"id":"p1"},{}]`
			if got := agreesWith(t, a, reshapeFile); !sameJSON(t, got, want) {
				t.Fatalf("source flatten: %s", got)
			}
			if a.sideOf(a.r.Focus) != rsTreeID || len(a.Mapping().Then) != 1 {
				t.Fatalf("not the new result: focus %s mapping %+v", a.r.Focus, a.Mapping())
			}
			press(t, a, "u")
			if got := output(t, a); got != before || mappingKey(a.Mapping()) != mapping {
				t.Fatalf("undo: %s mapping %+v", got, a.Mapping())
			}
			if len(a.rs.orig.List().Marked()) != 1 {
				t.Fatal("source mark lost")
			}
		})
	}
}

func TestFlattenWithoutMarksKeepsMeaning(t *testing.T) {
	for name, keys := range map[string][]string{
		"shape-gated": nil,
		"edit-node":   {"i"},
		"visual":      {"v", "j"},
		"split-node":  {"ctrl+w", "v", "ctrl+a", "o"},
		"jq-idle":     {"m"},
		"original":    {"T"},
	} {
		t.Run(name, func(t *testing.T) {
			a := openReshape(t, 120, 30)
			gated(t, a)
			a.onNode(t, ".people[].name")
			for _, key := range keys {
				press(t, a, key)
			}
			before := mappingKey(a.Mapping())
			press(t, a, "F")
			switch name {
			case "shape-gated", "jq-idle", "original":
				if mappingKey(a.Mapping()) != before {
					t.Fatal("unmarked F changed the document")
				}
				if name == "shape-gated" && !strings.Contains(a.m.Info, "press i first") {
					t.Fatalf("edit gate lost: %s", a.m.Info)
				}
			default:
				want := []string{".people[].name"}
				if name == "visual" {
					want = append(want, ".people[].id")
				}
				if got := a.Mapping().Then; len(got) != 1 || strings.Join(got[0].Paths, " ") != strings.Join(want, " ") {
					t.Fatalf("flatten targets: %+v", got)
				}
			}
		})
	}
}

func TestFlattenSourceFocusOptOut(t *testing.T) {
	a := openReshape(t, 120, 30)
	if err := a.r.Settings().Set(settingReshapeFocus, false); err != nil {
		t.Fatal(err)
	}
	press(t, a, "ctrl+w", "v")
	selectIn(t, a, a.rs.orig, func(n ochazuke.TreeNode) bool {
		s, _ := n.Data.(*Shape)
		return s != nil && s.Path == ".people[].name"
	})
	press(t, a, "space")
	press(t, a, "F")
	if a.sideOf(a.r.Focus) != rsOrigID || len(a.Mapping().Then) != 1 {
		t.Fatalf("focus %s, mapping %+v", a.r.Focus, a.Mapping())
	}
	if got := agreesWith(t, a, reshapeFile); !sameJSON(t, got, `[{"name":"Ann"},{"name":"Bo"},{"name":"Cy"},{"name":"Dee"}]`) {
		t.Fatalf("flatten: %s", got)
	}
}

func TestFlattenOriginalVisualPrefersMarks(t *testing.T) {
	a := openReshape(t, 120, 30)
	before := mappingKey(a.Mapping())
	press(t, a, "ctrl+w", "v")
	selectIn(t, a, a.rs.orig, func(n ochazuke.TreeNode) bool {
		s, _ := n.Data.(*Shape)
		return s != nil && s.Path == ".people[].name"
	})
	press(t, a, "space")
	selectIn(t, a, a.rs.orig, func(n ochazuke.TreeNode) bool {
		s, _ := n.Data.(*Shape)
		return s != nil && s.Path == ".title"
	})
	press(t, a, "v")
	press(t, a, "F")
	if got := a.Mapping().Then; len(got) != 1 || strings.Join(got[0].Paths, " ") != ".people[].name" {
		t.Fatalf("source flatten targets: %+v", got)
	}
	press(t, a, "u")
	if mappingKey(a.Mapping()) != before || len(a.rs.orig.List().Marked()) != 1 {
		t.Fatalf("not one undo step: %+v", a.Mapping())
	}
}

func TestFlattenMarksMadeInJQ(t *testing.T) {
	a := openReshape(t, 120, 30)
	gated(t, a)
	a.onNode(t, ".people[].name")
	press(t, a, "m")
	press(t, a, "space")
	if len(a.markedIDs()) != 1 {
		t.Fatal("JQ space did not persist the mark")
	}
	press(t, a, "F")
	if got := agreesWith(t, a, reshapeFile); !sameJSON(t, got, `[{"name":"Ann"},{"name":"Bo"},{"name":"Cy"},{"name":"Dee"}]`) {
		t.Fatalf("flatten: %s", got)
	}
}
