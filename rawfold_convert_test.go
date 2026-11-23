package main

import (
	"context"
	"strings"
	"testing"
)

func TestNewShapeOperationsAndJQParity(t *testing.T) {
	src := `{"a":{"x":1},"b":{"x":2}}`
	val, err := ParseJSON(src)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		ops  []Operation
		want string
	}{
		{"wrap-object", []Operation{{Kind: "wrap-object", Name: "data"}}, `{"data":{"a":{"x":1},"b":{"x":2}}}`},
		{"wrap-array", []Operation{{Kind: "wrap-array"}}, `[{"a":{"x":1},"b":{"x":2}}]`},
		{"object-values", []Operation{{Kind: "object-values"}}, `[{"x":1},{"x":2}]`},
		{"object-keys", []Operation{{Kind: "object-keys"}}, `["a","b"]`},
	}
	for _, tc := range cases {
		got, err := ApplyPipeline(val, tc.ops)
		if err != nil {
			t.Fatalf("%s: ApplyPipeline: %v", tc.name, err)
		}
		b, err := FormatJSON(got, false)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tc.want {
			t.Fatalf("%s: got %s, want %s", tc.name, b, tc.want)
		}
		prog := SynthesizeJQ(tc.ops)
		if jqVal, err := RunJQ(context.Background(), src, prog); err == nil {
			jb, _ := FormatJSON(jqVal, false)
			if string(jb) != string(b) {
				t.Fatalf("%s: jq (%s) returned %s, Go returned %s", tc.name, prog, jb, b)
			}
		}
	}
}

func TestJSONClosureFolding(t *testing.T) {
	sample := "{\n  \"msg\": \"ignore { and } in string\",\n  \"orders\": [\n    {\n      \"id\": 1\n    },\n    {\n      \"id\": 2\n    }\n  ],\n  \"ok\": true\n}"
	blocks := findJSONBlocks(sample)
	if len(blocks) != 4 {
		t.Fatalf("expected 4 multi-line blocks (root, orders, 2 objects), got %d: %+v", len(blocks), blocks)
	}
	// Line 4 is inside first order object (lines 3..5).
	blk, ok := enclosingBlock(blocks, 4)
	if !ok || blk.openLine != 3 || blk.closeLine != 5 {
		t.Fatalf("enclosingBlock(4) = %+v, ok=%v", blk, ok)
	}
	// Fold the orders array (lines 2..9).
	folded, cur := foldRawView(sample, 0, map[int]bool{2: true})
	if !strings.Contains(folded, `"orders": [ … 7 lines ],`) {
		t.Fatalf("folded output unexpected:\n%s", folded)
	}
	if cur != 0 {
		t.Fatalf("mapped cursor = %d, want 0", cur)
	}
	// Folding the orders array reduces total lines from 12 to 5.
	if n := len(strings.Split(folded, "\n")); n != 5 {
		t.Fatalf("folded line count = %d, want 5:\n%s", n, folded)
	}
}

func TestConvertPickerAndRawFoldsInteractive(t *testing.T) {
	b, err := NewBuffer("sample.json", `{"a":{"x":1},"b":{"x":2}}`, true)
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(Config{}, b)
	d := newDriver(app.shell(), 120, 30)

	// Open Convert Shape picker with C on root object -> pick "Values array".
	d.keys("C")
	if modalData(d.sh.State, modalPick) == nil {
		t.Fatal("C did not open convert shape picker")
	}
	d.typeText("values")
	d.keys("enter")
	got, _ := FormatJSON(app.display(app.cur()), false)
	if string(got) != `[{"x":1},{"x":2}]` {
		t.Fatalf("C -> Values array gave %s", got)
	}

	// Undo back to object, switch to Raw view, focus editor (tab), pretty-print (=), fold with z a, unfold with z R.
	d.keys("u", "4", "tab", "=")
	if len(app.active().rawFolds) != 0 {
		t.Fatalf("expected 0 folds initially, got %d", len(app.active().rawFolds))
	}
	d.keys("z", "a")
	if len(app.active().rawFolds) != 1 {
		t.Fatalf("z a did not fold enclosing JSON block: %+v (status %q)", app.active().rawFolds, app.status)
	}
	d.keys("z", "R")
	if len(app.active().rawFolds) != 0 {
		t.Fatalf("z R did not unfold all blocks: %+v", app.active().rawFolds)
	}
}
