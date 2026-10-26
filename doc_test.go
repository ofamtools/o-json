package main

import (
	"errors"
	"testing"
)

const small = `{"name":"mtm","n":3,"ok":true,"none":null,"tags":["a","b"],"sub":{"deep":{"k":"v"}},"odd key":"<&>"}`

// From gmtm's jsonexplorer tests, with jq-style paths.
func TestBuildTree(t *testing.T) {
	root, err := Build([]byte(small))
	if err != nil {
		t.Fatal(err)
	}
	if root.TypeLabel() != "object{7}" || root.Children[4].TypeLabel() != "array[2]" {
		t.Fatalf("labels %s %s", root.TypeLabel(), root.Children[4].TypeLabel())
	}
	kinds := []Kind{KString, KNumber, KBool, KNull, KArray, KObject}
	for i, k := range kinds {
		if root.Children[i].Kind != k {
			t.Errorf("child %d kind %s want %s", i, root.Children[i].Kind, k)
		}
	}
	if p := root.Children[5].Children[0].Children[0].Path(); p != ".sub.deep.k" {
		t.Errorf("path %s", p)
	}
	if p := root.Children[4].Children[1].Path(); p != ".tags[1]" {
		t.Errorf("path %s", p)
	}
	if p := root.Children[6].Path(); p != `["odd key"]` {
		t.Errorf("path %s", p)
	}
	if root.Path() != "." {
		t.Errorf("root path %s", root.Path())
	}
	if len(Flatten(root)) != 12 {
		t.Errorf("flatten %d", len(Flatten(root)))
	}
}

func TestMarshalKeepsKeyOrder(t *testing.T) {
	in := `{"z":1,"a":{"y":[1,"two",null],"b":false},"m":"<&>"}`
	root, err := Build([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := root.MarshalJSON()
	if string(data) != in {
		t.Fatalf("marshal\n got %s\nwant %s", data, in)
	}
	if got := Compact(root, 10); got != `{"z":1,"a":…` {
		t.Errorf("compact cut %q", got)
	}
	want := "{\n  \"z\": 1,\n  \"a\": {\n    \"y\": [\n      1,\n      \"two\",\n      null\n    ],\n    \"b\": false\n  },\n  \"m\": \"<&>\"\n}"
	if got := Pretty(root); got != want {
		t.Errorf("pretty\n%s", got)
	}
}

func TestInvalidJSON(t *testing.T) {
	_, err := Build([]byte("{\n  \"a\": 1,\n  \"b\" 2\n}"))
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("err %v", err)
	}
	if pe.Line != 3 {
		t.Errorf("line %d col %d", pe.Line, pe.Col)
	}
	if _, err := Build([]byte(`{"a":`)); err == nil {
		t.Error("truncated should fail")
	}
	if _, err := Build([]byte(`{} x`)); err == nil {
		t.Error("trailing should fail")
	}
	if _, err := Build([]byte("  ")); err == nil {
		t.Error("empty should fail")
	}
}

func TestSelect(t *testing.T) {
	root, _ := Build([]byte(`{"items":[{"o":{"n":"a"}},{"o":{"n":"b"}},{"x":1}],"odd key":[10,20]}`))
	cases := []struct {
		path   string
		want   []string
		fanned bool
	}{
		{".", []string{`{"items":[{"o":{"n":"a"}},{"o":{"n":"b"}},{"x":1}],"odd key":[10,20]}`}, false},
		{".items[1].o.n", []string{`"b"`}, false},
		{"items[-1].x", []string{"1"}, false},
		{"$.items[].o.n", []string{`"a"`, `"b"`}, true},
		{`["odd key"][0]`, []string{"10"}, false},
		{`.["odd key"][]`, []string{"10", "20"}, true},
		{".nope", nil, false},
	}
	for _, c := range cases {
		values, fanned, err := Select(root, c.path)
		if err != nil {
			t.Errorf("%s: %v", c.path, err)
			continue
		}
		var got []string
		for _, v := range values {
			data, _ := v.MarshalJSON()
			got = append(got, string(data))
		}
		if fanned != c.fanned || len(got) != len(c.want) {
			t.Errorf("%s: got %v fanned %v", c.path, got, fanned)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v", c.path, got)
			}
		}
	}
	for _, bad := range []string{".a[", ".a[x]", "..", `.["unterminated]`} {
		if _, _, err := Select(root, bad); err == nil {
			t.Errorf("%q should be a bad path", bad)
		}
	}
	if _, err := Get(root, ".items[].o"); err == nil {
		t.Error("Get of a fanned path should fail")
	}
}
