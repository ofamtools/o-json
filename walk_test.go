package main

import (
	"strings"
	"testing"
)

// walkTheStructure presses j from the top of the structure pane to its
// last node and checks the data pane shows values at every node: the
// document, array items ([]), object fields, nested items and leaves.
// It returns the paths it visited.
func walkTheStructure(t *testing.T, a *App, selected func() string) []string {
	t.Helper()
	var seen []string
	for range 200 {
		id := selected()
		frame := view(a)
		if a.selectedShape() == nil || a.note != "" || strings.Contains(frame, "no values at") {
			t.Fatalf("no data at node %q (note %q)\n%s", id, a.note, frame)
		}
		seen = append(seen, a.selectedShape().Path)
		press(t, a, "j")
		if selected() == id {
			return seen
		}
	}
	t.Fatal("j never reached the last node")
	return nil
}

// Walking down the structure shows data at every node, in the edited
// view (the default) and the original, for a document whose root is an
// array as for one whose root is an object. The edited view's node
// paths are jq's (".[]"), a shape's are canonical ("[]"): a top-level
// array's nodes all showed "no values at .[] in the restructured
// document" until the lookup went by steps.
func TestWalkingTheStructureShowsData(t *testing.T) {
	want := map[string][]string{
		"testdata/walk.json":      {".", "[]", "[].id", "[].tags[]", "[].meta.score", "[].orders[]", "[].orders[].lines[].price"},
		"testdata/top-array.json": {".", "[]", "[].n"},
		sample:                    {"."},
		nestDoc:                   {"."},
	}
	for path, paths := range want {
		for _, original := range []bool{false, true} {
			name := path + " edited"
			if original {
				name = path + " original"
			}
			t.Run(name, func(t *testing.T) {
				a := open(t, path, 120, 40)
				selected := func() string {
					n, _ := a.rs.tree.Selected()
					return n.ID
				}
				if original {
					press(t, a, "T")
					selected = func() string {
						s, _ := a.selectedOriginal()
						if s == nil {
							return ""
						}
						return s.Path
					}
				} else {
					onRoot(t, a)
				}
				for range 200 {
					before := selected()
					press(t, a, "k")
					if selected() == before {
						break
					}
				}
				seen := strings.Join(walkTheStructure(t, a, selected), " ")
				for _, p := range paths {
					if !strings.Contains(" "+seen+" ", " "+p+" ") {
						t.Errorf("the walk never reached %s: %s", p, seen)
					}
				}
			})
		}
	}
}
