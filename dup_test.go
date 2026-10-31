package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// A key repeated in one object counts once: .items[].id is 3/3, not 4/3,
// and the shape takes the last value, as jq does.
func TestDuplicateKeyCountsOncePerObject(t *testing.T) {
	s := Infer(load(t, "testdata/duplicate-keys.json"))
	id := s.Find(".items[].id")
	if id.Count != 3 || id.Of != 3 || id.Presence() != "3/3" || id.Optional {
		t.Fatalf("id %+v presence %s", id, id.Presence())
	}
	var values []string
	for _, v := range id.Values() {
		values = append(values, v.Value)
	}
	if got := strings.Join(values, " "); got != "1 2 33" {
		t.Errorf("values %s", got)
	}
	if id.Duplicates == nil || id.Duplicates.Objects != 1 || id.Duplicates.Most != 2 {
		t.Errorf("duplicates %+v", id.Duplicates)
	}
	if s.Find(".items[].name").Duplicates != nil {
		t.Error("name has no duplicates")
	}
	if got := id.DuplicateNote(); got != `duplicate key "id" (2): jq keeps the last` {
		t.Errorf("note %q", got)
	}
	if !strings.Contains(id.Note(), "duplicate key (2)") {
		t.Errorf("structure note %q", id.Note())
	}
}

// A repeat must not hide a missing field: one item has no id, so id is
// optional (2/3), however many times another item repeats it.
func TestDuplicateKeyDoesNotHideOptional(t *testing.T) {
	s := Infer(load(t, "testdata/duplicate-hides-optional.json"))
	id := s.Find("[].id")
	if id.Count != 2 || id.Of != 3 || !id.Optional {
		t.Fatalf("id %+v", id)
	}
	// Three repeats in one object and two in another: Most is the worst.
	root, _ := Build([]byte(`[{"a":1,"a":2,"a":3},{"a":1,"a":2},{"a":1}]`))
	a := Infer(root).Find("[].a")
	if a.Count != 3 || a.Optional || a.Duplicates.Objects != 2 || a.Duplicates.Most != 3 {
		t.Errorf("a %+v %+v", a, a.Duplicates)
	}
}

func TestStructureJSONIncludesDuplicates(t *testing.T) {
	var b strings.Builder
	if err := Main([]string{"structure", "--format", "json", "testdata/duplicate-keys.json"}, nil, &b, &b); err != nil {
		t.Fatal(err)
	}
	var tree struct {
		Children []struct {
			Children []struct {
				Children []struct {
					Key        string      `json:"key"`
					Count      int         `json:"count"`
					Duplicates *Duplicates `json:"duplicates"`
				} `json:"children"`
			} `json:"children"`
		} `json:"children"`
	}
	if err := json.Unmarshal([]byte(b.String()), &tree); err != nil {
		t.Fatalf("%v\n%s", err, b.String())
	}
	id := tree.Children[0].Children[0].Children[0]
	if id.Key != "id" || id.Count != 3 || id.Duplicates == nil || *id.Duplicates != (Duplicates{Objects: 1, Most: 2}) {
		t.Errorf("id %+v", id)
	}
}

// The viewer tones a duplicated field's label with the error role and its
// breadcrumb says what jq would do.
func TestViewerShowsDuplicateKey(t *testing.T) {
	a := open(t, "testdata/duplicate-keys.json", 120, 30)
	selectPath(t, a, ".items[].id")
	frame := view(a)
	if l := line(frame, ".items[].id ("); !strings.Contains(l, "(number 3/3)") || !strings.Contains(l, `duplicate key "id" (2): jq keeps the last`) {
		t.Errorf("breadcrumb %q", l)
	}
	nodes := a.tree.Nodes()
	at := slices.IndexFunc(nodes, func(n ochazuke.TreeNode) bool { return n.ID == "s:.items[].id" })
	if at < 0 || !slices.Equal(nodes[at].Tones, []ochazuke.ToneSpan{ochazuke.CellTone(0, ochazuke.ToneError)}) {
		t.Fatalf("id tones %+v", nodes)
	}
	// json.duplicate-tone foreground: the error colour, no background.
	if err := a.r.Settings().Set(settingDuplicateTone, "foreground"); err != nil {
		t.Fatal(err)
	}
	if err := a.rebuild(); err != nil {
		t.Fatal(err)
	}
	if got := a.tree.Nodes()[at].Tones; !slices.Equal(got, []ochazuke.ToneSpan{ochazuke.CellTone(0, ochazuke.ToneErrored)}) {
		t.Fatalf("foreground tones %+v", got)
	}
	_ = a.r.Settings().Set(settingDuplicateTone, "background")
	if err := a.rebuild(); err != nil {
		t.Fatal(err)
	}
	nodes = a.tree.Nodes()
	if name := nodes[slices.IndexFunc(nodes, func(n ochazuke.TreeNode) bool { return n.ID == "s:.items[].name" })]; len(name.Tones) != 0 {
		t.Errorf("name tones %+v", name.Tones)
	}
	selectPath(t, a, ".items[].name")
	if strings.Contains(view(a), "duplicate key") {
		t.Error("the note follows the selection")
	}
}

// Every read shows a repeated key's last value, as jq does, at the
// position of the first: the parsed document, get, table, structure's
// values, the viewer's table and fields, and restructure.
func TestEveryReadShowsTheLastValue(t *testing.T) {
	const file = "testdata/duplicate-keys.json"
	root := load(t, file)
	item := root.Field("items").Children[2]
	if len(item.Children) != 2 || item.Children[0].Key != "id" || item.Children[0].Value != "33" || item.Children[0].Repeats != 2 {
		t.Fatalf("item %s", Compact(item, 0))
	}
	run := func(args ...string) string {
		var b strings.Builder
		if err := Main(args, nil, &b, &b); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return b.String()
	}
	if got := run("get", file, ".items[2]"); !strings.Contains(got, `"id": 33`) || strings.Contains(got, `"id": 3,`) {
		t.Errorf("get %s", got)
	}
	if got := run("get", file, ".items[].id"); strings.Fields(got)[2] != "33" {
		t.Errorf("get [] %s", got)
	}
	if got := run("table", file, ".items"); !strings.Contains(got, "33") {
		t.Errorf("table %s", got)
	}
	if got := run("restructure", file, "--map", writeMap(t, `{"map":[{"from":".items[]","to":".r[]"},{"from":".items[].id","to":".r[].id"}]}`)); !strings.Contains(got, `"id": 33`) {
		t.Errorf("restructure %s", got)
	}
	a := open(t, file, 120, 30)
	selectPath(t, a, ".items")
	if row := line(view(a), "gamma"); !strings.Contains(row, "33") {
		t.Errorf("viewer table row %q", row)
	}
	selectPath(t, a, ".items[].id")
	frame := view(a)
	if !strings.Contains(frame, "33") || !strings.Contains(frame, `duplicate key "id" (2): jq keeps the last`) {
		t.Errorf("viewer values\n%s", frame)
	}
}

func writeMap(t *testing.T, mapping string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "map.json")
	if err := os.WriteFile(p, []byte(mapping), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
