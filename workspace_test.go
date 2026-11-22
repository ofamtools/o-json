package main

import "testing"

// scanJSON ends on malformed text and every span it returns is a whole
// value inside the text.
func TestScanJSONMalformed(t *testing.T) {
	for _, s := range []string{`[}`, `[,`, `[,,]`, `{"a":1,}`, `{"a"`, `{"a":[1,2`, `["x`, `{"a":1} {"b":`, `[1 2]`} {
		for _, sp := range scanJSON(s) {
			if sp.start < 0 || sp.end <= sp.start || sp.end > len(s) {
				t.Errorf("scanJSON(%q): bad span %v [%d,%d)", s, sp.path, sp.start, sp.end)
			}
		}
	}
	spans := scanJSON(`{"a":1,"b":[2,`)
	if len(spans) != 2 || spans[0].path.String() != MustPath("a").String() || spans[1].path.String() != MustPath("b[0]").String() {
		t.Errorf("finished prefix spans: %v", spans)
	}
}

// A scoped step is one transaction: scope, step and a scope back to the
// document undo together, and later steps run on the whole document.
func TestApplyStepsScoped(t *testing.T) {
	b, err := NewBuffer("", `{"orders":[{"id":2},{"id":1}],"n":[3,1]}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ApplySteps(scopedSteps(b.Pipeline, MustPath("orders"), Operation{Kind: "sort", Field: "id"})...); err != nil {
		t.Fatal(err)
	}
	if len(b.Undo) != 1 || len(b.Pipeline) != 3 || len(openScope(b.Pipeline)) != 0 {
		t.Fatalf("undo %d, pipeline %v", len(b.Undo), b.Pipeline)
	}
	if err := b.ApplySteps(Operation{Kind: "bogus"}); err == nil || len(b.Pipeline) != 3 || len(b.Undo) != 1 {
		t.Fatalf("a failing step changed the buffer: %v %v", err, b.Pipeline)
	}
	v, _ := b.Preview()
	if x, _ := AtPath(v, MustPath("orders[0].id")); valueText(x) != "1" {
		t.Errorf("orders not sorted: %v", valueText(v))
	}
	if x, _ := AtPath(v, MustPath("n[0]")); valueText(x) != "3" {
		t.Errorf("a value outside the scope changed: %v", valueText(v))
	}
	if !b.UndoStep() || len(b.Pipeline) != 0 {
		t.Errorf("one undo should take back the whole step: %v", b.Pipeline)
	}
}
