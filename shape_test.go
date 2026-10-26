package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func load(t testing.TB, path string) *Node {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := Build(data)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInferMarksOptionalFieldsLast(t *testing.T) {
	s := Infer(load(t, "testdata/sample.json"))
	items := s.Find(".items[]")
	if items == nil || items.Count != 4 || items.Kinds[KObject] != 4 {
		t.Fatalf("items shape %+v", items)
	}
	var names []string
	for _, c := range items.Children {
		names = append(names, fmt.Sprintf("%s:%v", c.Key, c.Optional))
	}
	want := "id:false name:false owner:false tags:false archived:true note:true"
	if strings.Join(names, " ") != want {
		t.Fatalf("fields %s\nwant %s", strings.Join(names, " "), want)
	}
	email := s.Find(".items[].owner.email")
	if email == nil || !email.Optional || email.Count != 2 || email.Of != 4 || email.Types() != "string|null" {
		t.Fatalf("email %+v", email)
	}
	tags := s.Find(".items[].tags")
	if tags.MinItems != 0 || tags.MaxItems != 2 || tags.Note() != "0..2 items" {
		t.Errorf("tags range %d..%d %q", tags.MinItems, tags.MaxItems, tags.Note())
	}
	if s.Find(".items[].tags[]").Count != 4 {
		t.Errorf("tag items %d", s.Find(".items[].tags[]").Count)
	}
	if got := len(s.Find(".items[].owner.name").Values()); got != 4 {
		t.Errorf("owner name values %d", got)
	}
}

func TestInferMixedTypes(t *testing.T) {
	root, _ := Build([]byte(`[1, "two", null, {"a": 1}, {"a": "x", "b": true}, [1, 2]]`))
	s := Infer(root)
	items := s.Find("[]")
	if items.Types() != "object|array|string|number|null" {
		t.Errorf("types %s", items.Types())
	}
	a, b := s.Find("[].a"), s.Find("[].b")
	if a.Optional || a.Of != 2 || a.Types() != "string|number" || !b.Optional {
		t.Errorf("a %+v b %+v", a, b)
	}
	if s.Find("[][]").Count != 2 {
		t.Errorf("nested items %+v", s.Find("[][]"))
	}
}

func TestWriteStructure(t *testing.T) {
	var b strings.Builder
	if err := WriteStructure(&b, Infer(load(t, "testdata/sample.json"))); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/sample.structure")
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != string(want) {
		t.Errorf("structure\n%s\nwant\n%s", b.String(), want)
	}
}

func TestTableColumnOrder(t *testing.T) {
	root := load(t, "testdata/sample.json")
	values, fanned, _ := Select(root, ".items")
	tab := NewTable(".items", values, fanned)
	var titles []string
	for _, c := range tab.Columns {
		title := c.Title()
		if c.Optional {
			title += "?"
		}
		titles = append(titles, title)
	}
	if got := strings.Join(titles, " "); got != "id name owner tags archived? note?" {
		t.Fatalf("columns %s", got)
	}
	if tab.CellText(tab.Rows[0], 4) != "" || tab.CellText(tab.Rows[1], 4) != "true" || tab.CellText(tab.Rows[0], 2) != `{"name":"ann","email":"ann@example.com"}` {
		t.Errorf("cells %q %q %q", tab.CellText(tab.Rows[0], 4), tab.CellText(tab.Rows[1], 4), tab.CellText(tab.Rows[0], 2))
	}
	// A fanned path: the owners as rows, email optional.
	values, fanned, _ = Select(root, ".items[].owner")
	owners := NewTable(".items[].owner", values, fanned)
	if len(owners.Rows) != 4 || owners.Columns[1].Name != "email" || !owners.Columns[1].Optional {
		t.Errorf("owners %+v", owners.Columns)
	}
	// Scalars and mixed items take the (value) column, last.
	mixed, _ := Build([]byte(`[{"a":1}, 2, "x"]`))
	m := NewTable(".", []*Node{mixed}, false)
	if len(m.Columns) != 2 || !m.Columns[1].Self || m.CellText(m.Rows[1], 1) != "2" || m.CellText(m.Rows[0], 1) != "" {
		t.Errorf("mixed %+v", m.Columns)
	}
}

func TestTableFormats(t *testing.T) {
	root := load(t, "testdata/sample.json")
	values, fanned, _ := Select(root, ".items")
	tab := NewTable(".items", values, fanned)
	var text, md strings.Builder
	_ = tab.WriteText(&text)
	_ = tab.WriteMarkdown(&md)
	lines := strings.Split(text.String(), "\n")
	if !strings.HasPrefix(lines[0], "id  name   owner") || !strings.HasSuffix(lines[0], "archived?  note?") {
		t.Errorf("text header %q", lines[0])
	}
	if !strings.HasPrefix(md.String(), "| id | name | owner | tags | archived? | note? |\n| --- |") {
		t.Errorf("markdown\n%s", md.String())
	}
	var js strings.Builder
	_ = tab.WriteJSON(&js)
	if !strings.Contains(js.String(), `"rows": [`) || !strings.Contains(js.String(), "\"id\": 1,\n      \"name\": \"alpha\"") {
		t.Errorf("json\n%s", js.String())
	}
}

// bigDocument is an array of n objects of about 500 bytes, every third one
// with an extra optional field.
func bigDocument(n int) []byte {
	var b strings.Builder
	b.WriteString("[")
	for i := range n {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, `{"id":%d,"name":"item %d","score":%d.5,"active":%v,"owner":{"name":"user %d","team":"team %d"},"tags":["a","b","c"],"body":"%s"`,
			i, i, i%100, i%2 == 0, i%37, i%5, strings.Repeat("x", 300))
		if i%3 == 0 {
			fmt.Fprintf(&b, `,"extra":"e%d"`, i)
		}
		b.WriteString("}")
	}
	b.WriteString("]")
	return []byte(b.String())
}

func TestLargeDocument(t *testing.T) {
	n := 100_000 // about 50 MB
	if raceOn || testing.Short() {
		n = 10_000
	}
	data := bigDocument(n)
	start := time.Now()
	root, err := Build(data)
	if err != nil {
		t.Fatal(err)
	}
	s := Infer(root)
	t.Logf("%d MB: build and infer in %v", len(data)>>20, time.Since(start))
	items := s.Find("[]")
	if items.Count != n {
		t.Fatalf("items %d", items.Count)
	}
	last := items.Children[len(items.Children)-1]
	if last.Key != "extra" || !last.Optional || last.Count != (n+2)/3 {
		t.Errorf("extra %+v", last)
	}
	values, fanned, _ := Select(root, ".")
	tab := NewTable(".", values, fanned)
	if len(tab.Rows) != n || tab.Columns[len(tab.Columns)-1].Name != "extra" {
		t.Errorf("table %d rows, columns %+v", len(tab.Rows), tab.Columns)
	}
}

func BenchmarkBuild(b *testing.B) {
	data := bigDocument(20_000)
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := Build(data); err != nil {
			b.Fatal(err)
		}
	}
}
