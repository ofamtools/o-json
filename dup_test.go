package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// A key repeated in one object counts once: .items[].id is 3/3, not 4/3,
// and the shape takes the first value, as Node.Field (and so the table)
// does.
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
	if got := strings.Join(values, " "); got != "1 2 3" {
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
	if at < 0 || !slices.Equal(nodes[at].Tones, []ochazuke.ToneSpan{ochazuke.CellTone(0, toneDuplicateKey)}) {
		t.Fatalf("id tones %+v", nodes)
	}
	if name := nodes[slices.IndexFunc(nodes, func(n ochazuke.TreeNode) bool { return n.ID == "s:.items[].name" })]; len(name.Tones) != 0 {
		t.Errorf("name tones %+v", name.Tones)
	}
	selectPath(t, a, ".items[].name")
	if strings.Contains(view(a), "duplicate key") {
		t.Error("the note follows the selection")
	}
}
