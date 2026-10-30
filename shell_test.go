package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const sample = "testdata/sample.json"

// run runs one command line with stdin and fails on an error or stderr.
func run(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if err := Main(args, strings.NewReader(stdin), &out, &errOut); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, errOut.String())
	}
	if errOut.Len() > 0 {
		t.Fatalf("%v wrote to stderr: %q", args, errOut.String())
	}
	return out.String()
}

func sampleText(t *testing.T) string {
	data, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStructureCommand(t *testing.T) {
	want, _ := os.ReadFile("testdata/sample.structure")
	if got := run(t, "", "structure", sample); got != string(want) {
		t.Fatalf("structure\n%s", got)
	}
	// Stdin, as - and with no file word.
	for _, args := range [][]string{{"structure", "-"}, {"structure"}} {
		if got := run(t, sampleText(t), args...); got != string(want) {
			t.Fatalf("%v from stdin\n%s", args, got)
		}
	}
	var shape struct {
		Children []struct {
			Key      string `json:"key"`
			Children []struct {
				Children []struct {
					Key      string `json:"key"`
					Optional bool   `json:"optional"`
				} `json:"children"`
			} `json:"children"`
		} `json:"children"`
	}
	if err := json.Unmarshal([]byte(run(t, "", "structure", "--format", "json", sample)), &shape); err != nil {
		t.Fatal(err)
	}
	fields := shape.Children[2].Children[0].Children
	if shape.Children[2].Key != "items" || fields[4].Key != "archived" || !fields[4].Optional || fields[0].Optional {
		t.Fatalf("json shape %+v", shape)
	}
}

func TestTableCommand(t *testing.T) {
	got := run(t, "", "table", sample, ".items")
	want := `id  name   owner                                     tags            archived?  note?
1   alpha  {"name":"ann","email":"ann@example.com"}  ["red","blue"]
2   beta   {"name":"bob"}                            []              true
3   gamma  {"name":"cy","email":null}                ["green"]                  third
4   delta  {"name":"dee"}                            ["red"]
`
	if got != want {
		t.Fatalf("table text\n%s", got)
	}
	md := run(t, sampleText(t), "table", "--format", "markdown", ".items[].owner")
	if md != "| name | email? |\n| --- | --- |\n| ann | ann@example.com |\n| bob |  |\n| cy | null |\n| dee |  |\n" {
		t.Fatalf("markdown from stdin\n%s", md)
	}
	var tab struct {
		Columns []TableColumn     `json:"columns"`
		Rows    []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal([]byte(run(t, "", "table", sample, ".items", "--format", "json")), &tab); err != nil {
		t.Fatal(err)
	}
	if len(tab.Rows) != 4 || tab.Columns[5].Name != "note" || !tab.Columns[5].Optional {
		t.Fatalf("table json %+v", tab.Columns)
	}
}

func TestGetCommand(t *testing.T) {
	if got := run(t, "", "get", sample, ".items[1].owner"); got != "{\n  \"name\": \"bob\"\n}\n" {
		t.Fatalf("get %q", got)
	}
	if got := run(t, sampleText(t), "get", "-", ".items[].owner.name", "--raw"); got != "ann\nbob\ncy\ndee\n" {
		t.Fatalf("get raw from stdin %q", got)
	}
	if got := run(t, "", "get", sample, ".items[].id", "--format", "json"); got != "[\n  1,\n  2,\n  3,\n  4\n]\n" {
		t.Fatalf("get json %q", got)
	}
	var out, errOut bytes.Buffer
	if err := Main([]string{"get", sample, ".nope"}, strings.NewReader(""), &out, &errOut); err == nil || !strings.Contains(err.Error(), "nothing at .nope") {
		t.Fatalf("missing path: %v", err)
	}
}

func TestDocNounJSON(t *testing.T) {
	// Values keep the document's key order (the verbs are pending work),
	// and a bare --json is accepted.
	got := run(t, "", "doc", "get", sample, ".meta", "--json")
	if got != "{\n  \"generated\": \"2026-10-25\",\n  \"count\": 4,\n  \"source\": null\n}\n" {
		t.Fatalf("doc get %q", got)
	}
	var tab struct {
		Path    string        `json:"path"`
		Columns []TableColumn `json:"columns"`
	}
	if err := json.Unmarshal([]byte(run(t, sampleText(t), "doc", "table", "-", ".items[].owner")), &tab); err != nil {
		t.Fatal(err)
	}
	if tab.Path != ".items[].owner" || len(tab.Columns) != 2 || !tab.Columns[1].Optional {
		t.Fatalf("doc table %+v", tab)
	}
	var shape Shape
	if err := json.Unmarshal([]byte(run(t, "", "doc", "structure", sample)), &shape); err != nil || shape.Path != "." || len(shape.Children) != 4 {
		t.Fatalf("doc structure %v %+v", err, shape)
	}
}

func TestCatalog(t *testing.T) {
	var cat struct {
		Name  string `json:"name"`
		Nouns []struct {
			Name  string `json:"name"`
			Verbs []struct {
				Name string `json:"name"`
			} `json:"verbs"`
		} `json:"nouns"`
	}
	if err := json.Unmarshal([]byte(run(t, "", "catalog")), &cat); err != nil {
		t.Fatal(err)
	}
	var verbs []string
	for _, n := range cat.Nouns {
		if n.Name == "doc" {
			for _, v := range n.Verbs {
				verbs = append(verbs, v.Name)
			}
		}
	}
	if cat.Name != "o-json" || strings.Join(verbs, " ") != "get rows structure table" {
		t.Fatalf("catalog %s %v", cat.Name, verbs)
	}
}

// The viewer reads stdin too: --smoke renders one frame of it.
func TestViewSmokeFromStdin(t *testing.T) {
	for _, args := range [][]string{{"--smoke", "-"}, {"view", "--smoke", "--width", "120"}, {"--smoke", sample}} {
		got := run(t, sampleText(t), args...)
		if !strings.Contains(got, "Structure") || !strings.Contains(got, "Object .") || !strings.Contains(got, "o-json sample") {
			t.Fatalf("%v\n%s", args, got)
		}
	}
}

// doc rows answers the table's rows as JSON Lines whose data is the item
// in document order, and --format prints them under the table's columns.
func TestDocRowsFormats(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(run(t, "", "doc", "rows", sample, ".items")), "\n")
	if len(lines) != 4 || lines[1] != `{"noun":"item","id":".items[1]","data":{"id":2,"name":"beta","owner":{"name":"bob"},"tags":[],"archived":true},"cells":["2","beta","{\"name\":\"bob\"}","[]","true",""]}` {
		t.Fatalf("doc rows JSON Lines\n%s", strings.Join(lines, "\n"))
	}
	want := map[string]string{
		"table":    "id  name   owner                                     tags            archived?  note?\n 1  alpha  {\"name\":\"ann\",\"email\":\"ann@example.com\"}  [\"red\",\"blue\"]\n",
		"markdown": "| id | name | owner | tags | archived? | note? |\n| ---: | --- | --- | --- | --- | --- |\n| 1 | alpha |",
		"toon":     "item[4]{id,name,owner,tags,archived?,note?}:\n  1,alpha,",
	}
	for format, prefix := range want {
		if got := run(t, "", "doc", "rows", sample, ".items", "--format", format); !strings.HasPrefix(got, prefix) {
			t.Errorf("--format %s\n%s", format, got)
		}
	}
	if got := run(t, "", "doc", "rows", sample, ".items[].owner", "--format", "table"); got != "name  email?\nann   ann@example.com\nbob\ncy    null\ndee\n" {
		t.Errorf("a [] path\n%s", got)
	}
}
