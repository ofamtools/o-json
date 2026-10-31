package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestKeyOrderIdentity(t *testing.T) {
	for _, src := range []string{
		`{"z":1,"a":2}`,
		`[{"odd key":1,"":2,"a":3},{"a":4,"":5,"odd key":6}]`,
		`[[{"z":1,"a":2},{"a":3,"z":4}],[]]`,
		`[{"a":{"z":1,"a":2}},{"a":{}},{"a":{"a":3,"z":4}}]`,
		`[{"z":1,"optional":true,"a":{"z":2,"a":3}},{"a":{"a":4,"z":5},"z":6}]`,
		`{"rows":[{"x":0,"b":1},{"b":2,"x":3}],"empty":{},"mixed":[{},null,{"z":1,"a":2}]}`,
		`{"z":0,"a":1,"z":{"b":2,"a":3}}`,
	} {
		t.Run(src, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.json")
			if err := os.WriteFile(path, []byte(src), 0600); err != nil {
				t.Fatal(err)
			}
			a := open(t, path, 120, 30)
			want := Compact(a.Doc.Root, 0)
			got, err := Build([]byte(output(t, a)))
			if err != nil {
				t.Fatal(err)
			}
			if Compact(got, 0) != want {
				t.Fatalf("identity reordered keys:\n got %s\nwant %s", Compact(got, 0), want)
			}
		})
	}
}

func TestKeyOrderParseRoundTripLarge(t *testing.T) {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < 10000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		if i%2 == 0 {
			b.WriteString(`{"z":1,"a":{"y":2,"b":3}}`)
		} else {
			b.WriteString(`{"a":{"b":3,"y":2},"z":1}`)
		}
	}
	b.WriteByte(']')
	n, err := Build([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte(b.String())) {
		t.Fatal("marshal lost per-object key order")
	}
	path := filepath.Join(t.TempDir(), "large.json")
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	a := open(t, path, 120, 30)
	if got := output(t, a); got != b.String() {
		t.Fatal("large identity lost per-item order")
	}
	t.Logf("round-tripped %d objects (%d bytes)", len(n.Children), b.Len())
}

// Exercise the exported mapping, both execution engines, and the actual
// asynchronous write. Order is compared as JSON text, never as a Go map.
func checkKeyOrderMapping(t *testing.T, a *App, want string) {
	t.Helper()
	spec, err := FormatMapping(a.Mapping())
	if err != nil {
		t.Fatal(err)
	}
	m, err := ReadMapping(spec)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewDocPlan(m, a.Doc.Root, a.Doc.Shape)
	if err != nil {
		t.Fatal(err)
	}
	if got := Compact(plan.Apply(a.Doc.Root), 0); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	t.Run("jq", func(t *testing.T) {
		if got := runJQ(t, plan.JQ(), Compact(a.Doc.Root, 0)); got != want {
			t.Fatalf("jq got %s\nwant %s\n%s", got, want, plan.JQ())
		}
	})
	out := filepath.Join(t.TempDir(), "copy.json")
	if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "write", Args: ochazuke.Arguments{"path": out}}); err != nil {
		t.Fatal(err)
	}
	settle(a)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	n, err := Build(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := Compact(n, 0); got != want {
		t.Fatalf("write got %s\nwant %s", got, want)
	}
}

func TestKeyOrderEditWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		patches []Patch
		want    string
	}{
		{"unchanged", nil, `{"records":[{"z":1,"optional":true,"a":{"z":2,"a":3}},{"a":{"a":4,"z":5},"z":6}],"version":"1"}`},
		{"value edits", []Patch{{Op: opSet, Path: ".records[1].a.z", Value: json.RawMessage(`99`)}, {Op: opRename, Path: ".records[0].z", Key: "renamed"}, {Op: opAdd, Path: ".records[1].new", Value: json.RawMessage(`true`), After: "a"}}, `{"records":[{"renamed":1,"optional":true,"a":{"z":2,"a":3}},{"a":{"a":4,"z":99},"new":true,"z":6}],"version":"1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := open(t, "testdata/key-order.json", 120, 30)
			before, err := os.ReadFile(a.Path)
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.patches) > 0 {
				if err := a.commitWith("edit values", rsRootID, a.current(), tc.patches); err != nil {
					t.Fatal(err)
				}
			}
			checkKeyOrderMapping(t, a, tc.want)
			after, err := os.ReadFile(a.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("write modified source")
			}
			if len(tc.patches) > 0 {
				press(t, a, "u")
				if got := output(t, a); got != compactJSON(before) {
					t.Fatalf("undo lost original order: %s", got)
				}
			}
		})
	}
}

func TestKeyOrderStructureEdits(t *testing.T) {
	t.Run("rename", func(t *testing.T) {
		a := open(t, "testdata/key-order.json", 120, 30)
		e := a.current()
		for i, n := range e.nodes {
			if e.meta[n.ID].from == ".records[].z" {
				e.nodes[i].Label = "renamed"
			}
		}
		if err := a.commit("rename", rsRootID, e); err != nil {
			t.Fatal(err)
		}
		checkKeyOrderMapping(t, a, `{"records":[{"renamed":1,"optional":true,"a":{"z":2,"a":3}},{"a":{"a":4,"z":5},"renamed":6}],"version":"1"}`)
	})
	t.Run("explicit reorder", func(t *testing.T) {
		a := open(t, "testdata/key-order.json", 120, 30)
		a.on(t, ".records[].optional")
		press(t, a, "K")
		checkKeyOrderMapping(t, a, `{"records":[{"optional":true,"z":1,"a":{"z":2,"a":3}},{"z":6,"a":{"a":4,"z":5}}],"version":"1"}`)
		press(t, a, "u")
		if got := output(t, a); got != Compact(a.Doc.Root, 0) {
			t.Fatalf("undo order %s", got)
		}
	})
}

func TestKeyOrderFrequencyOptOut(t *testing.T) {
	a := open(t, "testdata/key-order.json", 120, 30)
	if err := a.r.Settings().Set(settingStructureOrder, "frequency"); err != nil {
		t.Fatal(err)
	}
	if err := a.startState(); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range a.Mapping().Map {
		if e.Order != "" {
			t.Fatalf("frequency has document order: %+v", e)
		}
		got = append(got, e.From)
	}
	if slices.Index(got, ".records[].optional") < slices.Index(got, ".records[].a") {
		t.Fatal("frequency failed to put optional last")
	}
	checkKeyOrderMapping(t, a, `{"records":[{"z":1,"a":{"z":2,"a":3},"optional":true},{"z":6,"a":{"z":5,"a":4}}],"version":"1"}`)
}

func TestKeyOrderDuplicateKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicates.json")
	if err := os.WriteFile(path, []byte(`[{"z":0,"a":1,"z":{"b":2,"a":3}},{"a":4,"z":5,"a":6}]`), 0600); err != nil {
		t.Fatal(err)
	}
	a := open(t, path, 120, 30)
	if n := a.Doc.Root.Children[0].Field("z"); n.Repeats != 2 || n.Index != 0 {
		t.Fatalf("duplicate metadata %+v", n)
	}
	checkKeyOrderMapping(t, a, `[{"z":{"b":2,"a":3},"a":1},{"a":6,"z":5}]`)
}

func TestKeyOrderMappingValidation(t *testing.T) {
	n, _ := Build([]byte(`{"a":1}`))
	for _, spec := range []string{
		`{"map":[{"from":".","to":".","order":"sorted"}]}`,
		`{"map":[{"to":".","order":"document"}]}`,
		`{"map":[{"from":".a","to":".a","order":"document"}]}`,
		`{"map":[{"from":".","to":".","order":"document"},{"to":".new"}]}`,
	} {
		m, err := ReadMapping([]byte(spec))
		if err == nil {
			_, err = NewPlan(m, Infer(n))
		}
		if err == nil {
			t.Fatalf("accepted %s", spec)
		}
	}
}

func TestKeyOrderYankPut(t *testing.T) {
	a := open(t, "testdata/key-order.json", 120, 30)
	a.on(t, ".records")
	press(t, a, "y", "y")
	a.on(t, ".version")
	press(t, a, "p")
	n, err := Build([]byte(output(t, a)))
	if err != nil {
		t.Fatal(err)
	}
	copy := n.Field("records-2")
	if copy == nil || Compact(copy, 0) != Compact(a.Doc.Root.Field("records"), 0) {
		t.Fatalf("copied subtree lost order: %s", Compact(n, 0))
	}
	checkKeyOrderMapping(t, a, Compact(n, 0))
	press(t, a, "u")
	a.on(t, ".records")
	press(t, a, "d", "d")
	a.on(t, ".version")
	press(t, a, "p")
	n, err = Build([]byte(output(t, a)))
	if err != nil {
		t.Fatal(err)
	}
	moved := n.Field("records")
	if moved == nil || Compact(moved, 0) != Compact(a.Doc.Root.Field("records"), 0) {
		t.Fatalf("moved subtree lost order: %s", Compact(n, 0))
	}
	checkKeyOrderMapping(t, a, Compact(n, 0))
}
