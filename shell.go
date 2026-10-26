package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

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
	// Each verb reads the file as pending work: off the loop, and its value
	// reaches the shell as is (a synchronous result is round-tripped
	// through a JSON map, which would sort the document's keys).
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
	r.Define("doc").
		Verb("structure", func(c ochazuke.Ctx) ochazuke.Result {
			return pending(c, func(d *Doc, _ string) (any, error) { return d.Shape, nil })
		}, "the document's shape as a tree of {key, path, count, of, types, min_items, max_items, optional, children}; fields most common first, optional ones last").Positional("file").
		Verb("table", func(c ochazuke.Ctx) ochazuke.Result {
			return pending(c, func(d *Doc, path string) (any, error) { return tableAt(d, path) })
		}, "an array (or the values a [] path names) as {path, columns: [{name, count, optional, types, self}], rows}; columns most common first, optional ones last, rows as in the document").Positional("file", "path").
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
		}, "the value at a path, keys in document order; a [] path answers an array of every value it names").Positional("file", "path")
	file := ochazuke.Parameter{Type: "string", Required: true, Description: "the JSON file; - reads stdin"}
	params := map[string]map[string]ochazuke.Parameter{
		"doc.structure": {"file": file},
		"doc.table":     {"file": file, "path": {Type: "string", Description: "the array: .items, or a [] path such as .items[].owner (default . , the root)"}},
		"doc.get":       {"file": file, "path": {Type: "string", Required: true, Description: "jq-style path: . .key [\"odd key\"] [0] [-1] [] (every item)"}},
	}
	results := map[string]string{"doc.structure": "shape", "doc.table": "{path, columns, rows}", "doc.get": "value"}
	for key, p := range params {
		op := r.Operations[key]
		op.Parameters = p
		op.Result = results[key]
		r.Operations[key] = op
	}
	sh := ocli.New(r, "o-json", stdin, stdout, quietJobs{stderr})
	sh.Usage = ocli.DefaultUsage("o-json")
	return sh
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

// quietJobs is the shell's stderr minus its "job #N VERB done" lines.
// ochazuke/cli's Shell.Run (cli/shell.go, v0.1.1 line 188) prints one per
// job a command started, and every doc verb is pending work; the
// operation's Quiet flag does not reach that line. (o-csv has the same
// filter.) A failed job's line passes through.
type quietJobs struct{ w io.Writer }

func (q quietJobs) Write(p []byte) (int, error) {
	if line := string(p); strings.HasPrefix(line, "job #") && strings.HasSuffix(line, " done\n") {
		return len(p), nil
	}
	return q.w.Write(p)
}
