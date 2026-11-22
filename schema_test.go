package main

import "testing"

func TestSchemaNullMixedAndPresence(t *testing.T) {
	v, err := ParseJSON(`[{"a":1,"b":null},null,"x",{"a":null,"b":[]}]`)
	if err != nil {
		t.Fatal(err)
	}
	s := InferSchema(v)
	el := s.Find(MustPath("[]"))
	if el.Type != "mixed" || el.Count != 4 {
		t.Fatalf("element: %s %d", el.Type, el.Count)
	}
	a := s.Find(MustPath("[].a"))
	if a.Type != "mixed" || a.Count != 2 || a.Total != 4 {
		t.Fatalf("a: %s %d/%d", a.Type, a.Count, a.Total)
	}
	if b := s.Find(MustPath("[].b")); b.Type != "mixed" {
		t.Fatalf("b: %s", b.Type)
	}
	if e := s.Find(MustPath("[].b[]")); e.Type != "empty" || e.Total != 1 {
		t.Fatalf("b[]: %s total %d", e.Type, e.Total)
	}
	if n := InferSchema(nil); n.Type != "null" {
		t.Fatalf("null: %s", n.Type)
	}
}

func TestSchemaKeyOrder(t *testing.T) {
	v, _ := ParseJSON(`[{"z":1,"a":2},{"m":1,"a":1}]`)
	var got string
	for _, c := range InferSchema(v).Find(MustPath("[]")).Children {
		got += c.Name
	}
	if got != "zam" {
		t.Fatalf("order %q", got)
	}
}

func TestNegativeIndexNoPanic(t *testing.T) {
	v := []any{"x"}
	p := Path{{Index: -1, Indexed: true}}
	if _, ok := AtPath(v, p); ok {
		t.Fatal("found")
	}
	if _, err := ReplaceAt(v, p, "y"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseDocumentLinesAndConcatenation(t *testing.T) {
	if v, err := ParseDocument("{\n \"a\": 1\n}\n"); err != nil || len(v.(Object)) != 1 {
		t.Fatalf("multiline: %v %v", v, err)
	}
	if v, err := ParseDocument("{}\n\n[1]\n"); err != nil || len(v.([]any)) != 2 {
		t.Fatalf("jsonl: %v %v", v, err)
	}
	for _, bad := range []string{"{}{}", "01", "{}\n{}{}", "{\n\"a\":\n}"} {
		if v, err := ParseDocument(bad); err == nil {
			t.Fatalf("%q accepted: %v", bad, v)
		}
	}
	_, err := ParseDocument("{}\n{\"a\":1}{}")
	if pe, ok := err.(*ParseError); !ok || pe.Line != 2 {
		t.Fatalf("line: %v", err)
	}
}
