package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

// The doc noun is the command line a host mounts: o-json doc VERB FILE
// [PATH] reads the document in process and prints JSON, and o-json
// catalog describes it. The top-level structure, table and get commands
// (commands.go) print the same answers as text.

// newShell is the NOUN VERB shell over a runtime holding the doc noun.
func newShell(stdin io.Reader, stdout, stderr io.Writer, dataDir ...string) *ocli.Shell {
	r := ochazuke.NewRuntime()
	if len(dataDir) > 0 && dataDir[0] != "" {
		r.Config().StateDir = dataDir[0]
	}
	// The settings the viewer reads, so settings list|set know them.
	if err := declareSettings(r); err != nil {
		panic(err)
	}
	var documents *documentStore
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
		}, "the document's shape as a tree of {key, path, count, of, types, min_items, max_items, optional, children}; fields most common first, optional ones last").Quiet().Positional("file").Fetch().
		Verb("tabulate", func(c ochazuke.Ctx) ochazuke.Result {
			return pending(c, func(d *Doc, path string) (any, error) { return tableAt(d, path) })
		}, "an array (or the values a [] path names) as {path, columns: [{name, count, optional, types, self}], rows}; columns most common first, optional ones last, rows as in the document").Quiet().Positional("file", "path").Fetch().
		Verb("get", func(c ochazuke.Ctx) ochazuke.Result {
			if id := ochazuke.StringArg(c.Args, "id", ""); id != "" {
				return ochazuke.Pending(func(context.Context) (any, error) { return documents.get(c, id) })
			}
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
		}, "the value at a path, keys in document order; a [] path answers an array of every value it names").Quiet().Positional("file", "path").Fetch().
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
		}, "doc tabulate's rows as JSON Lines {noun: item, id: PATH, data: ITEM, cells}, one cell per doc tabulate column; --format table, markdown and toon print them as a table").Quiet().Positional("file", "path").Rows("item").Fetch().Table()
	// The rest of the command line as verbs: query, remap and export are
	// the top-level commands query, restructure and export (commands.go,
	// export.go), which print text; these answer JSON and take every file
	// as a typed path, so the palette, a host and MCP reach them. (The verb
	// is remap: restructure is a noun, the viewer's page.)
	r.Define("doc").
		Verb("query", func(c ochazuke.Ctx) ochazuke.Result {
			query := ochazuke.StringArg(c.Args, "query", ".")
			return pending(c, func(d *Doc, _ string) (any, error) {
				out := queryDoc(context.Background(), d.Root, query)
				switch {
				case out.Err != nil && out.Pos >= 0:
					return nil, fmt.Errorf("jq: %v (at column %d)", out.Err, out.Pos+1)
				case out.Err != nil:
					return nil, fmt.Errorf("jq: %v", out.Err)
				}
				return out.Values, nil
			})
		}, "run a jq query (gojq) and answer its results as one array, keys in document order where the query is a path").Quiet().Positional("file", "query").Fetch().
		Verb("remap", func(c ochazuke.Ctx) ochazuke.Result {
			spec, as := ochazuke.StringArg(c.Args, "mapping", ""), ochazuke.StringArg(c.Args, "as", "document")
			out, overwrite := ochazuke.StringArg(c.Args, "out", ""), c.Args["overwrite"] == true
			return pending(c, func(d *Doc, _ string) (any, error) {
				if spec == "" {
					return nil, fmt.Errorf("a mapping file is required (mapping)")
				}
				m, err := readMappingFile(spec)
				if err != nil {
					return nil, err
				}
				isJSON := false
				v, err := verbOut(out, overwrite, func(w io.Writer) (err error) {
					isJSON, err = restructureTo(w, d, m, as)
					return err
				})
				if text, ok := v.(string); ok && isJSON {
					return json.RawMessage(text), err // the document or the mapping, not a string of it
				}
				return v, err
			})
		}, "restructure the document by a mapping file of shape paths (o-json restructure): answer the new document (or its jq program, or the complete mapping), or write it to out, a new file (an existing one only with overwrite)").Quiet().Positional("file", "mapping").
		Verb("export", func(c ochazuke.Ctx) ochazuke.Result {
			q := exportRequest{Map: ochazuke.StringArg(c.Args, "mapping", ""), Format: ochazuke.StringArg(c.Args, "as", ""), JQ: c.Args["jq"] == true, Out: ochazuke.StringArg(c.Args, "out", ""),
				Options: ExportOptions{Of: ochazuke.StringArg(c.Args, "of", "."), Columns: splitColumns(ochazuke.StringArg(c.Args, "columns", "")),
					Arrays: ochazuke.StringArg(c.Args, "arrays", arraysJoin), Join: ochazuke.StringArg(c.Args, "join", ";"), Null: ochazuke.StringArg(c.Args, "nulls", nullEmpty)}}
			overwrite := c.Args["overwrite"] == true
			return pending(c, func(d *Doc, _ string) (any, error) {
				return verbOut(q.Out, overwrite, func(w io.Writer) error { return q.writeTo(w, d) })
			})
		}, "one array of the document (restructured by mapping first) as csv, tsv, jsonl or a markdown table: answer it as text, or write it to out, a new file (an existing one only with overwrite)").Quiet().Positional("file")
	file := ochazuke.Parameter{Type: ochazuke.PropPath, Path: "file", Required: true, Description: "the JSON file; - reads stdin"}
	mapFile := ochazuke.Parameter{Type: ochazuke.PropPath, Path: "file", Description: "the mapping file (SPEC.json) that restructures the document first"}
	outFile := ochazuke.Parameter{Type: ochazuke.PropPath, Path: "file", New: true, Description: "write to this new file instead of answering the text"}
	overwrite := ochazuke.Parameter{Type: "boolean", Description: "with out, replace an existing file"}
	required := func(p ochazuke.Parameter) ochazuke.Parameter { p.Required = true; return p }
	params := map[string]map[string]ochazuke.Parameter{
		"doc.query": {"file": file, "query": {Type: "string", Required: true, Rest: true, Description: "the jq query, e.g. .items[] | .name (. is the whole document)"}},
		"doc.remap": {"file": file, "mapping": required(mapFile), "out": outFile, "overwrite": overwrite, "as": {Type: "string", Enum: []string{"document", "jq", "mapping"}, Description: "what to answer: the new document (the default), the jq program that makes it, or the complete mapping"}},
		"doc.export": {"file": file, "mapping": mapFile, "out": outFile, "overwrite": overwrite,
			"of": {Type: "string", Description: "the array's structure path (default . , the document)"},
			// as, not format: the shell's own --format says how rows print.
			"as":      {Type: "string", Enum: exportFormatNames(), Description: "the format (default: out's extension, else csv)"},
			"columns": {Type: "string", Description: "the columns to keep, in order, separated by commas"},
			"arrays":  {Type: "string", Enum: []string{arraysJoin, arraysJSON, arraysRows}, Description: "an array in a cell: join, json or rows"},
			"join":    {Type: "string", Description: "the separator for arrays join (default ;)"},
			"nulls":   {Type: "string", Enum: []string{nullEmpty, nullNull}, Description: "null as an empty cell or the text null"},
			"jq":      {Type: "boolean", Description: "answer the jq program that makes the csv or tsv instead"}},
		"doc.structure": {"file": file},
		"doc.tabulate":  {"file": file, "path": {Type: "string", Description: "the array: .items, or a [] path such as .items[].owner (default . , the root)"}},
		"doc.rows":      {"file": file, "path": {Type: "string", Description: "the array: .items, or a [] path such as .items[].owner (default . , the root)"}},
		"doc.get":       {"file": {Type: ochazuke.PropPath, Path: "file", Description: "the JSON file; - reads stdin"}, "id": documentSubject(), "path": {Type: "string", Description: "jq-style path: . .key [\"odd key\"] [0] [-1] [] (every item); omit id to read JSON instead of the document record"}},
	}
	results := map[string]string{"doc.structure": "shape", "doc.tabulate": "{path, columns, rows}", "doc.get": "value", "doc.rows": "row lines",
		"doc.query": "values", "doc.remap": "the document, the jq program or the mapping; or {out, bytes}", "doc.export": "text, or {out, bytes}"}
	for key, p := range params {
		op := r.Operations.At(ochazuke.Key(r.CanonicalKey(key)))
		op.Parameters = p
		op.Result = results[key]
		r.Operations.Set(op)
	}
	setHistory(r, ochazuke.HistoryChange, "doc.remap", "doc.export") // each may write its out file
	documents = registerDocuments(r, nil, nil)
	registerDocumentActions(r, documents, nil)
	declareDocumentPage(r)
	r.DefineListFacts(ochazuke.ListFacts{Noun: "item", Rename: map[string]string{"data": "row-data"}, ListParams: map[string]ochazuke.Parameter{"file": file, "path": {Type: "string"}}, List: func(c ochazuke.Ctx) ochazuke.Result {
		return pending(c, func(d *Doc, path string) (any, error) {
			table, err := tableAt(d, path)
			if err != nil {
				return nil, err
			}
			return tableRows(table), nil
		})
	}})
	sh := ocli.New(r, "o-json", stdin, stdout, stderr, ocli.Serves())
	sh.Prepare = func(context.Context) error { r.EnsureHistory(); return nil }
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
