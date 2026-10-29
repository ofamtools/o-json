package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// exportOf exports a document's text in a format with options.
func exportOf(t *testing.T, doc, format string, opts ExportOptions) string {
	t.Helper()
	root, err := Build([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	f, err := formatNamed(format)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Join == "" {
		opts.Join = ";"
	}
	data, err := exportBytes(root, f, opts)
	if err != nil {
		t.Fatalf("export %s: %v", format, err)
	}
	return string(data)
}

func TestExportEachFormat(t *testing.T) {
	doc := sampleText(t)
	opts := ExportOptions{Of: ".items", Columns: []string{"id", "name", "tags"}}
	for format, want := range map[string]string{
		"csv":   "id,name,tags\n1,alpha,red;blue\n2,beta,\n3,gamma,green\n4,delta,red\n",
		"tsv":   "id\tname\ttags\n1\talpha\tred;blue\n2\tbeta\t\n3\tgamma\tgreen\n4\tdelta\tred\n",
		"md":    "| id | name | tags |\n| --- | --- | --- |\n| 1 | alpha | red;blue |\n| 2 | beta |  |\n| 3 | gamma | green |\n| 4 | delta | red |\n",
		"jsonl": `{"id":1,"name":"alpha","tags":["red","blue"]}` + "\n" + `{"id":2,"name":"beta","tags":[]}` + "\n" + `{"id":3,"name":"gamma","tags":["green"]}` + "\n" + `{"id":4,"name":"delta","tags":["red"]}` + "\n",
	} {
		if got := exportOf(t, doc, format, opts); got != want {
			t.Errorf("%s\n%s\nwant\n%s", format, got, want)
		}
	}
	// jsonl does not flatten: every item as it is.
	got := exportOf(t, doc, "jsonl", ExportOptions{Of: ".items"})
	if lines := strings.Split(strings.TrimSpace(got), "\n"); len(lines) != 4 || !strings.Contains(lines[0], `"owner":{"name":"ann","email":"ann@example.com"}`) {
		t.Fatalf("jsonl\n%s", got)
	}
	// The extension picks the format; .json and anything else is JSON.
	for path, want := range map[string]string{"a.csv": "csv", "A.TSV": "tsv", "a.jsonl": "jsonl", "a.md": "md", "a.json": "", "a.txt": ""} {
		f := formatFor(path)
		if (f == nil && want != "") || (f != nil && f.name != want) {
			t.Errorf("%s: %v", path, f)
		}
	}
}

func TestExportDottedColumns(t *testing.T) {
	x, err := Export(mustBuild(t, sampleText(t)), ExportOptions{Of: ".items", Join: ";"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"id", "name", "owner.name", "owner.email", "tags", "archived", "note"}
	if !slices.Equal(x.Columns, want) {
		t.Fatalf("columns %v", x.Columns)
	}
	got := exportOf(t, `[{"a": {"b": {"c": 1}, "d": 2}}, {"a": "flat"}, {"a": {}}]`, "csv", ExportOptions{})
	// a's own column (a scalar, an empty object) first, then its fields.
	if want := "a,a.b.c,a.d\n,1,2\nflat,,\n{},,\n"; got != want {
		t.Fatalf("mixed object and scalar\n%s", got)
	}
}

func TestExportArraysModes(t *testing.T) {
	doc := `[{"id": 1, "tags": ["a", "b"], "n": [1, null]}, {"id": 2, "tags": [], "n": [{"x": 1}]}]`
	for mode, want := range map[string]string{
		arraysJoin: "id,tags,n\n1,a|b,1|\n2,,\"{\"\"x\"\":1}\"\n",
		arraysJSON: "id,tags,n\n1,\"[\"\"a\"\",\"\"b\"\"]\",\"[1,null]\"\n2,[],\"[{\"\"x\"\":1}]\"\n",
		// One row per item, every combination; an empty array keeps its
		// row; object items flatten to dotted columns.
		arraysRows: "id,tags,n,n.x\n1,a,1,\n1,a,,\n1,b,1,\n1,b,,\n2,,,1\n",
	} {
		got := exportOf(t, doc, "csv", ExportOptions{Arrays: mode, Join: "|"})
		if got != want {
			t.Errorf("%s\n%s\nwant\n%s", mode, got, want)
		}
	}
	if _, err := Export(mustBuild(t, doc), ExportOptions{Arrays: "nope"}); err == nil {
		t.Fatal("an unknown arrays mode exported")
	}
}

func TestExportMissingAndNull(t *testing.T) {
	doc := `[{"a": 1, "b": null}, {"a": null}, {"b": "x"}]`
	if got, want := exportOf(t, doc, "csv", ExportOptions{}), "a,b\n1,\n,\n,x\n"; got != want {
		t.Errorf("empty\n%s", got)
	}
	// null mode: null is the text null, a missing field still empty.
	if got, want := exportOf(t, doc, "csv", ExportOptions{Null: nullNull}), "a,b\n1,null\nnull,\n,x\n"; got != want {
		t.Errorf("null\n%s", got)
	}
}

func TestExportColumnsAndShapes(t *testing.T) {
	doc := sampleText(t)
	if got := exportOf(t, doc, "csv", ExportOptions{Of: ".items", Columns: []string{"owner.name", "id"}}); got != "owner.name,id\nann,1\nbob,2\ncy,3\ndee,4\n" {
		t.Errorf("columns\n%s", got)
	}
	if _, err := Export(mustBuild(t, doc), ExportOptions{Of: ".items", Columns: []string{"owner"}}); err == nil || !strings.Contains(err.Error(), "owner.name") {
		t.Errorf("an unknown column: %v", err)
	}
	// A [] path fans out: every item's owner is a row.
	if got := exportOf(t, doc, "csv", ExportOptions{Of: ".items[].owner"}); got != "name,email\nann,ann@example.com\nbob,\ncy,\ndee,\n" {
		t.Errorf("fanned\n%s", got)
	}
	// Not an array: one row, one column value; an array of scalars too.
	if got := exportOf(t, doc, "csv", ExportOptions{Of: ".meta"}); got != "value\n\"{\"\"generated\"\":\"\"2026-10-25\"\",\"\"count\"\":4,\"\"source\"\":null}\"\n" {
		t.Errorf("object\n%s", got)
	}
	if got := exportOf(t, doc, "csv", ExportOptions{Of: ".items[0].tags"}); got != "value\nred\nblue\n" {
		t.Errorf("scalars\n%s", got)
	}
	if _, err := Export(mustBuild(t, doc), ExportOptions{Of: ".nope"}); err == nil {
		t.Error("nothing at a path exported")
	}
}

// w to a .csv (.tsv, .md, .jsonl) path writes the export of the array
// under the cursor, with the same rules as a JSON write; the edits stay
// unsaved, since an export is not the document.
func TestWriteInfersTheFormat(t *testing.T) {
	dir := t.TempDir()
	a := open(t, sample, 120, 30)
	a.on(t, ".items[].owner.name")
	write := func(noun string, args ochazuke.Arguments) error {
		_, err := a.r.Execute(ochazuke.Request{Noun: noun, Verb: "write", Args: args})
		settle(a)
		return err
	}
	out := filepath.Join(dir, "items.csv")
	if err := write("restructure", ochazuke.Arguments{"path": out}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if !strings.HasPrefix(string(got), "id,name,owner.name,owner.email,tags,archived,note\n1,alpha,ann,") {
		t.Fatalf("wrote %q (info %q, error %q)", got, a.m.Info, a.m.Status)
	}
	// An existing file asks, as a JSON write does.
	if err := write("restructure", ochazuke.Arguments{"path": out}); err != nil || a.r.TopModal() == "" {
		t.Fatalf("no confirm: %v", err)
	}
	press(t, a, "n")
	// The original (T): viewer.write infers too.
	press(t, a, "T")
	md := filepath.Join(dir, "items.md")
	if err := write("viewer", ochazuke.Arguments{"path": md}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(md); !strings.HasPrefix(string(got), "| id | name | owner.name |") {
		t.Fatalf("md %q (error %q)", got, a.m.Status)
	}
	// .json is the document, as before.
	js := filepath.Join(dir, "doc.json")
	if err := write("viewer", ochazuke.Arguments{"path": js}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(js); !strings.Contains(string(got), `"name": "o-json sample"`) {
		t.Fatalf("json %q", got)
	}
}

// The export reads the edited document: the restructure and the value
// patches.
func TestExportReflectsEditsAndPatches(t *testing.T) {
	dir := t.TempDir()
	a := open(t, sample, 120, 30)
	onData(t, a, ".items", ".items[1]")
	press(t, a, "d", "d")
	onData(t, a, ".items", ".items[0]")
	// Rename the name key of every item (a structure edit).
	a.on(t, ".items[].name")
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "rename", Args: ochazuke.Arguments{"key": "title"}}); err != nil {
		t.Fatal(err)
	}
	view(a)
	out := filepath.Join(dir, "x.csv")
	viewerDo(t, a, "export", ochazuke.Arguments{"path": out, "of": ".items", "columns": "id,title"})
	got, _ := os.ReadFile(out)
	if string(got) != "id,title\n1,alpha\n3,gamma\n4,delta\n" {
		t.Fatalf("export %q (error %q)", got, a.m.Status)
	}
	// The file is there: viewer.export asks, keeping its arguments.
	viewerDo(t, a, "export", ochazuke.Arguments{"path": out, "of": ".items", "columns": "id"})
	if a.r.TopModal() == "" {
		t.Fatal("no confirm")
	}
	finish(a, Press(a.m, "y"))
	if got, _ := os.ReadFile(out); string(got) != "id\n1\n3\n4\n" {
		t.Fatalf("replaced %q (error %q)", got, a.m.Status)
	}
	if !a.unsaved() {
		t.Fatal("an export saved the edits")
	}
}

// o-json export with the viewer's mapping writes what viewer.export
// writes.
func TestExportCLIAgreesWithTheTUI(t *testing.T) {
	dir := t.TempDir()
	a := open(t, sample, 120, 30)
	onData(t, a, ".items", ".items[2]")
	press(t, a, "d", "d")
	spec := writeMap(t, mustJSON(t, a.Mapping()))
	for _, format := range exportFormatNames() {
		out := filepath.Join(dir, "tui."+format)
		viewerDo(t, a, "export", ochazuke.Arguments{"path": out, "of": ".items"})
		tui, _ := os.ReadFile(out)
		var b strings.Builder
		if err := Main([]string{"export", sample, "--map", spec, "--of", ".items", "--format", format}, nil, &b, &b); err != nil {
			t.Fatal(err)
		}
		if b.String() != string(tui) || len(tui) == 0 {
			t.Errorf("%s: cli\n%s\ntui\n%s", format, b.String(), tui)
		}
	}
}

// The jq program makes the same records as the Go export (jq quotes
// every string in @csv, so the comparison is by record).
func TestExportJQAgreesWithGo(t *testing.T) {
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is not on PATH")
	}
	run := func(program, input string) string {
		cmd := exec.Command(jq, "-r", program)
		cmd.Stdin = strings.NewReader(input)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			t.Fatalf("jq: %v %s\n%s", err, errb.String(), program)
		}
		return out.String()
	}
	records := func(text string, comma rune) [][]string {
		r := csv.NewReader(strings.NewReader(text))
		r.Comma, r.FieldsPerRecord, r.LazyQuotes = comma, -1, true
		out, err := r.ReadAll()
		if err != nil {
			t.Fatalf("%v\n%s", err, text)
		}
		return out
	}
	odd := `[[1,2],[],{"a":{}},{"a":[{"b":1},{"b":2,"c":[3,4]}]},5,null,{"a":"x","odd key":true}]`
	docs := map[string]string{"sample": sampleText(t), "odd": odd}
	for name, doc := range docs {
		for _, of := range []string{".", ".items", ".items[]", ".items[].owner", ".meta"} {
			for _, arrays := range []string{arraysJoin, arraysJSON, arraysRows} {
				for _, null := range []string{nullEmpty, nullNull} {
					opts := ExportOptions{Of: of, Arrays: arrays, Join: ";", Null: null}
					x, err := Export(mustBuild(t, doc), opts)
					if err != nil {
						continue // nothing at that path in this document
					}
					for _, format := range []string{"csv", "tsv"} {
						f, _ := formatNamed(format)
						program, err := exportJQ(x, f)
						if err != nil {
							t.Fatal(err)
						}
						var b strings.Builder
						if err := f.write(&b, x); err != nil {
							t.Fatal(err)
						}
						comma := ','
						if format == "tsv" {
							if strings.ContainsAny(b.String(), `"\`) {
								continue // @tsv escapes where encoding/csv quotes
							}
							comma = '\t'
						}
						got := run(program, doc)
						if !slices.EqualFunc(records(got, comma), records(b.String(), comma), slices.Equal) {
							t.Errorf("%s %s %s %s %s\njq\n%s\ngo\n%s\nprogram %s", name, of, arrays, null, format, got, b.String(), program)
						}
					}
				}
			}
		}
	}
	// With a restructure: the --jq program reads the source document.
	a := open(t, sample, 120, 30)
	onData(t, a, ".items", ".items[2]")
	press(t, a, "d", "d")
	v, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "jq", Args: ochazuke.Arguments{"format": "csv", "of": ".items"}})
	if err != nil {
		t.Fatal(err)
	}
	y, _ := v.(ochazuke.Yank)
	root, _ := a.shownRoot()
	want, _ := exportBytes(root, &exportFormats[0], a.exportOptions(".items", nil))
	if got := run(y.Text, sampleText(t)); !slices.EqualFunc(records(got, ','), records(string(want), ','), slices.Equal) {
		t.Fatalf("restructure.jq {format: csv}\n%s\nwant\n%s\nprogram %s", got, want, y.Text)
	}
	var b strings.Builder
	spec := writeMap(t, mustJSON(t, a.Mapping()))
	if err := Main([]string{"export", sample, "--map", spec, "--of", ".items", "--jq"}, nil, &b, &b); err != nil {
		t.Fatal(err)
	}
	if got := run(b.String(), sampleText(t)); !slices.EqualFunc(records(got, ','), records(string(want), ','), slices.Equal) {
		t.Fatalf("export --jq\n%s\nwant\n%s", got, want)
	}
}

// Without of, the export takes the array under the cursor: the selected
// array's items, or the nearest array holding the selection.
func TestExportTakesTheArrayUnderTheCursor(t *testing.T) {
	a := open(t, sample, 120, 30)
	for path, want := range map[string]string{".items": ".items[]", ".items[].owner.name": ".items[]", ".items[].tags": ".items[].tags[]"} {
		a.on(t, path)
		if got := a.exportPath(); got != want {
			t.Errorf("on %s: %s, want %s", path, got, want)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
