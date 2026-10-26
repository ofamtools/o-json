package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
)

// The doc noun is the command line a host mounts: o-json doc VERB FILE
// [PATH] reads the document in process and prints JSON, and o-json
// catalog describes it. The top-level structure, table and get commands
// (commands.go) print the same answers as text.

// newShell is the NOUN VERB shell over a runtime holding the doc noun.
func newShell(stdin io.Reader, stdout, stderr io.Writer) *ocli.Shell {
	r := ochazuke.NewRuntime()
	// Each verb reads the file as pending work: reading and parsing a
	// document is I/O, which an application verb keeps off the loop. (Key
	// order is not the reason: the shell keeps an immediate result's keys
	// in order. The JSON is the same either way except that the shell
	// prints an immediate array one compact element per line, so doc get
	// of an array would change shape, and an immediate failure has no
	// "job #N doc.VERB failed" stderr line.)
	pending := func(c ochazuke.Ctx, fn func(*Doc, string) (any, error)) ochazuke.Result {
		file := ochazuke.StringArg(c.Args, "file", "")
		path := ochazuke.StringArg(c.Args, "path", ".")
		return ochazuke.Pending(func(context.Context) (any, error) {
			if file == "" {
				return nil, fmt.Errorf("a file is required (- reads stdin)")
			}
			doc, err := ReadDoc(file, stdin)
			if err != nil {
				return nil, err
			}
			return fn(doc, path)
		})
	}
	r.Define("item") // doc rows' rows; their columns are set per call
	r.Define("doc").
		Verb("structure", func(c ochazuke.Ctx) ochazuke.Result {
			return pending(c, func(d *Doc, _ string) (any, error) { return d.Shape, nil })
		}, "the document's shape as a tree of {key, path, count, of, types, min_items, max_items, optional, children}; fields most common first, optional ones last").Quiet().Positional("file").
		Verb("table", func(c ochazuke.Ctx) ochazuke.Result {
			return pending(c, func(d *Doc, path string) (any, error) { return tableAt(d, path) })
		}, "an array (or the values a [] path names) as {path, columns: [{name, count, optional, types, self}], rows}; columns most common first, optional ones last, rows as in the document").Quiet().Positional("file", "path").
		Verb("get", func(c ochazuke.Ctx) ochazuke.Result {
			return pending(c, func(d *Doc, path string) (any, error) {
				values, fanned, err := Select(d.Root, path)
				switch {
				case err != nil:
					return nil, err
				case fanned:
					return values, nil
				case len(values) == 0:
					return nil, fmt.Errorf("nothing at %s", path)
				}
				return values[0], nil
			})
		}, "the value at a path, keys in document order; a [] path answers an array of every value it names").Quiet().Positional("file", "path").
		Verb("rows", func(c ochazuke.Ctx) ochazuke.Result {
			// The item noun's columns are the table's, known only once the
			// file is read: the work keeps them for Then, on the loop,
			// before the shell prints (--format draws them over the cells).
			var cols []ochazuke.Column
			return pending(c, func(d *Doc, path string) (any, error) {
				t, err := tableAt(d, path)
				if err != nil {
					return nil, err
				}
				cols = rowColumns(t)
				return tableRows(t), nil
			}).Then(func(ochazuke.Ctx, any, error) { r.Define("item").Columns(cols...) })
		}, "doc table's rows as JSON Lines {noun: item, id: PATH, data: ITEM, cells}, one cell per doc table column; --format table, markdown and toon print them as a table").Quiet().Positional("file", "path").Rows("item")
	file := ochazuke.Parameter{Type: "string", Required: true, Description: "the JSON file; - reads stdin"}
	params := map[string]map[string]ochazuke.Parameter{
		"doc.structure": {"file": file},
		"doc.table":     {"file": file, "path": {Type: "string", Description: "the array: .items, or a [] path such as .items[].owner (default . , the root)"}},
		"doc.rows":      {"file": file, "path": {Type: "string", Description: "the array: .items, or a [] path such as .items[].owner (default . , the root)"}},
		"doc.get":       {"file": file, "path": {Type: "string", Required: true, Description: "jq-style path: . .key [\"odd key\"] [0] [-1] [] (every item)"}},
	}
	results := map[string]string{"doc.structure": "shape", "doc.table": "{path, columns, rows}", "doc.get": "value", "doc.rows": "row lines"}
	for key, p := range params {
		op := r.Operations[key]
		op.Parameters = p
		op.Result = results[key]
		r.Operations[key] = op
	}
	sh := ocli.New(r, "o-json", stdin, stdout, stderr)
	sh.Usage = ocli.DefaultUsage("o-json")
	return sh
}

// tableRows is a table's rows as the shell's rows: one per item, its path
// the id, the item itself the data, its cells as the text table shows them.
func tableRows(t *Table) ochazuke.Lines {
	out := make(ochazuke.Lines, len(t.Rows))
	for i, row := range t.Rows {
		cells := make([]string, len(t.Columns))
		for j := range t.Columns {
			cells[j] = oneLine(t.CellText(row, j))
		}
		out[i] = ocli.Row{Noun: "item", ID: row.Path(), Data: row, Cells: cells}
	}
	return out
}

// rowColumns are the columns --format draws a table's rows under: titles
// as the text table writes them (an optional one ends in "?"), numbers
// right-aligned, cut at the text table's width, optional columns dropped
// first on a narrow terminal.
func rowColumns(t *Table) []ochazuke.Column {
	cols := make([]ochazuke.Column, len(t.Columns))
	for j, c := range t.Columns {
		col := ochazuke.Column{Title: c.Title(), Max: textCellWidth}
		if c.Optional {
			col.Title += "?"
			col.Priority = 1
		}
		if c.Types == string(KNumber) {
			col.Align = ochazuke.AlignRight
		}
		cols[j] = col
	}
	return cols
}

// writeIndented prints a value as indented JSON.
func writeIndented(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}
