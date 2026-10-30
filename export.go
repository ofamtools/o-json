package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/oden"
)

// Export: one array of a document as a table file. The rows are the
// array's items (or the values a [] path names); a value that is not an
// array is one row with one column, value. An object item flattens: its
// fields in shape order (the order the structure tree and the data pane
// list them: most common first, then first appearance), a nested object
// as dotted columns (owner.name), an empty object as the cell {}. An array
// in a cell is joined (export.arrays join, export.join between the
// items), its compact JSON (json), or one row per item with the other
// columns repeated (rows; an empty array keeps the row with an empty
// cell, several arrays give every combination). A field an item lacks is
// an empty cell; null is an empty cell or null (export.null). A columns
// list picks and orders the columns.
//
// The formats are rows of one table (exportFormats): a write's path
// extension picks one, and any other extension is JSON, as before.

// exportFormat is one way to write an export.
type exportFormat struct {
	name  string
	ext   string
	alias []string
	jq    string // the jq format step that writes it, "" for none
	keys  bool   // writes the items as they are: columns are top-level keys
	write func(io.Writer, *exported) error
}

var exportFormats = []exportFormat{
	{name: "csv", ext: ".csv", jq: "@csv", write: func(w io.Writer, x *exported) error { return x.writeDelimited(w, ',') }},
	{name: "tsv", ext: ".tsv", jq: "@tsv", write: func(w io.Writer, x *exported) error { return x.writeDelimited(w, '\t') }},
	{name: "jsonl", ext: ".jsonl", alias: []string{"ndjson"}, keys: true, write: func(w io.Writer, x *exported) error { return x.writeJSONL(w) }},
	{name: "md", ext: ".md", alias: []string{"markdown"}, write: func(w io.Writer, x *exported) error { return x.writeMarkdown(w) }},
}

// exportFormatNames lists the formats, for messages and enums.
func exportFormatNames() []string {
	out := make([]string, 0, len(exportFormats))
	for _, f := range exportFormats {
		out = append(out, f.name)
	}
	return out
}

// formatFor is the format a path's extension names, nil for JSON (.json
// and every extension no format claims).
func formatFor(path string) *exportFormat {
	ext := strings.ToLower(filepath.Ext(path))
	for i := range exportFormats {
		if exportFormats[i].ext == ext {
			return &exportFormats[i]
		}
	}
	return nil
}

// formatNamed is the format called name.
func formatNamed(name string) (*exportFormat, error) {
	name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "."))
	for i, f := range exportFormats {
		if f.name == name || slices.Contains(f.alias, name) {
			return &exportFormats[i], nil
		}
	}
	return nil, fmt.Errorf("unknown export format %q (one of %s)", name, strings.Join(exportFormatNames(), ", "))
}

// exportFormatOf is the format a write uses: name when given, else the
// path's extension, else csv.
func exportFormatOf(name, path string) (*exportFormat, error) {
	if strings.TrimSpace(name) != "" {
		return formatNamed(name)
	}
	if f := formatFor(path); f != nil {
		return f, nil
	}
	if path == "" || path == "-" {
		return &exportFormats[0], nil
	}
	return nil, fmt.Errorf("%s: no export format for its extension (one of %s; format names it)", path, strings.Join(exportFormatNames(), ", "))
}

// The array modes (export.arrays) and the null modes (export.null).
const (
	arraysJoin = "join"
	arraysJSON = "json"
	arraysRows = "rows"
	nullEmpty  = "empty"
	nullNull   = "null"
)

// ExportOptions say what an export holds.
type ExportOptions struct {
	Of      string   // the path of the array; "" is the root
	Columns []string // the columns to keep, in order; nil keeps all
	Arrays  string   // join, json or rows
	Join    string   // the separator for join
	Null    string   // empty or null

	keys bool // columns are top-level keys (jsonl), not flattened columns
}

func (o ExportOptions) nullText() string {
	if o.Null == nullNull {
		return "null"
	}
	return ""
}

// exported is an export: the items, their columns and the rows of cells.
type exported struct {
	opts    ExportOptions
	items   []*Node
	whole   bool // the value at Of is not an array: one row, column value
	fanned  bool
	Columns []string
	Rows    [][]string
}

// cell is a flattened value: its text, or null.
type cell struct {
	text string
	null bool
}

type fragment map[string]cell

// Export lays out the array at opts.Of.
func Export(root *Node, opts ExportOptions) (*exported, error) {
	if opts.Arrays == "" {
		opts.Arrays = arraysJoin
	}
	if !slices.Contains([]string{arraysJoin, arraysJSON, arraysRows}, opts.Arrays) {
		return nil, fmt.Errorf("unknown arrays mode %q (join, json or rows)", opts.Arrays)
	}
	if opts.Null == "" {
		opts.Null = nullEmpty
	}
	if opts.Null != nullEmpty && opts.Null != nullNull {
		return nil, fmt.Errorf("unknown null mode %q (empty or null)", opts.Null)
	}
	of := strings.TrimSpace(opts.Of)
	if of == "" {
		of = "."
	}
	opts.Of = of
	values, fanned, err := Select(root, of)
	if err != nil {
		return nil, err
	}
	x := &exported{opts: opts, fanned: fanned}
	switch {
	case fanned:
		x.items = values
	case len(values) == 0:
		return nil, fmt.Errorf("nothing at %s", of)
	case values[0].Kind == KArray:
		x.items = values[0].Children
	default:
		x.items, x.whole = values[:1], true
	}
	var all []string
	if x.whole {
		all = []string{"value"}
	} else if len(x.items) > 0 {
		shape := Infer(&Node{Kind: KArray, Children: x.items}).items
		seen := map[string]bool{}
		x.columnsOf(shape, "", seen, &all)
	}
	x.Columns = all
	if opts.Columns != nil {
		if opts.keys {
			all = topKeys(x.items)
		}
		for _, c := range opts.Columns {
			if !slices.Contains(all, c) {
				return nil, fmt.Errorf("no column %q at %s (columns: %s)", c, of, strings.Join(all, ", "))
			}
		}
		x.Columns = opts.Columns
	}
	for _, it := range x.items {
		var frags []fragment
		if x.whole {
			frags = []fragment{{"value": wholeCell(it)}}
		} else {
			frags = x.flatten(it, "")
		}
		for _, f := range frags {
			row := make([]string, len(x.Columns))
			for j, c := range x.Columns {
				if v, ok := f[c]; ok {
					row[j] = v.text
					if v.null {
						row[j] = opts.nullText()
					}
				}
			}
			x.Rows = append(x.Rows, row)
		}
	}
	return x, nil
}

// topKeys are the object items' keys, in first appearance.
func topKeys(items []*Node) []string {
	var out []string
	for _, it := range items {
		if it.Kind != KObject {
			continue
		}
		for _, c := range it.Children {
			if !slices.Contains(out, c.Key) {
				out = append(out, c.Key)
			}
		}
	}
	return out
}

// columnName is a flattened value's column: its dotted path, value at
// the top.
func columnName(prefix string) string {
	if prefix == "" {
		return "value"
	}
	return prefix
}

func dottedKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// columnsOf adds the columns a shape's values flatten to, in shape order:
// its own column (a scalar, a joined or JSON array, an empty object),
// then its fields' columns, then (rows) its items' columns.
func (x *exported) columnsOf(s *Shape, prefix string, seen map[string]bool, out *[]string) {
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			*out = append(*out, name)
		}
	}
	var fields []*Shape
	for _, c := range s.Children {
		if c.IsField() {
			fields = append(fields, c)
		}
	}
	own := s.Kinds[KString]+s.Kinds[KNumber]+s.Kinds[KBool]+s.Kinds[KNull] > 0 ||
		s.Kinds[KObject] > 0 && len(fields) == 0 ||
		s.Kinds[KArray] > 0 && x.opts.Arrays != arraysRows
	if own {
		add(columnName(prefix))
	}
	for _, f := range fields {
		x.columnsOf(f, dottedKey(prefix, f.Key), seen, out)
	}
	if s.Kinds[KArray] > 0 && x.opts.Arrays == arraysRows && s.items != nil {
		x.columnsOf(s.items, prefix, seen, out)
	}
}

// flatten is a value's fragments: one per row it makes (rows mode makes
// one per array item).
func (x *exported) flatten(n *Node, prefix string) []fragment {
	name := columnName(prefix)
	switch n.Kind {
	case KObject:
		if len(n.Children) == 0 {
			return []fragment{{name: {text: "{}"}}}
		}
		out := []fragment{{}}
		for _, c := range n.Children {
			out = product(out, x.flatten(c, dottedKey(prefix, c.Key)))
		}
		return out
	case KArray:
		switch x.opts.Arrays {
		case arraysRows:
			if len(n.Children) == 0 {
				return []fragment{{}}
			}
			var out []fragment
			for _, c := range n.Children {
				out = append(out, x.flatten(c, prefix)...)
			}
			return out
		case arraysJSON:
			return []fragment{{name: {text: Compact(n, 0)}}}
		}
		parts := make([]string, len(n.Children))
		for i, c := range n.Children {
			switch {
			case c.Kind == KNull:
				parts[i] = x.opts.nullText()
			case c.IsContainer():
				parts[i] = Compact(c, 0)
			default:
				parts[i] = c.Value
			}
		}
		return []fragment{{name: {text: strings.Join(parts, x.opts.Join)}}}
	}
	return []fragment{{name: {text: n.Value, null: n.Kind == KNull}}}
}

// wholeCell is a value that is not an array as one cell: a scalar's
// text, a container's compact JSON.
func wholeCell(n *Node) cell {
	if n.IsContainer() {
		return cell{text: Compact(n, 0)}
	}
	return cell{text: n.Value, null: n.Kind == KNull}
}

// product is every combination of a fragment of a and one of b (b's
// columns win, as a later key does).
func product(a, b []fragment) []fragment {
	out := make([]fragment, 0, len(a)*len(b))
	for _, x := range a {
		for _, y := range b {
			f := make(fragment, len(x)+len(y))
			for k, v := range x {
				f[k] = v
			}
			for k, v := range y {
				f[k] = v
			}
			out = append(out, f)
		}
	}
	return out
}

// writeDelimited writes the header and the rows with encoding/csv.
func (x *exported) writeDelimited(w io.Writer, comma rune) error {
	if len(x.Columns) == 0 {
		return nil
	}
	cw := csv.NewWriter(w)
	cw.Comma = comma
	if err := cw.Write(x.Columns); err != nil {
		return err
	}
	if err := cw.WriteAll(x.Rows); err != nil {
		return err
	}
	return cw.Error()
}

// writeMarkdown writes a Markdown table.
func (x *exported) writeMarkdown(w io.Writer) error {
	if len(x.Columns) == 0 {
		return nil
	}
	var b strings.Builder
	esc := func(s string) string { return strings.ReplaceAll(oneLine(s), "|", `\|`) }
	row := func(cells []string) {
		b.WriteString("|")
		for _, c := range cells {
			b.WriteString(" " + esc(c) + " |")
		}
		b.WriteString("\n")
	}
	row(x.Columns)
	b.WriteString("|" + strings.Repeat(" --- |", len(x.Columns)) + "\n")
	for _, r := range x.Rows {
		row(r)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeJSONL writes each item as one line of JSON, not flattened; a
// columns list keeps those top-level keys of an object item, in order.
func (x *exported) writeJSONL(w io.Writer) error {
	var b strings.Builder
	for _, it := range x.items {
		if x.opts.Columns == nil || it.Kind != KObject {
			b.WriteString(Compact(it, 0))
			b.WriteByte('\n')
			continue
		}
		b.WriteByte('{')
		first := true
		for _, c := range x.opts.Columns {
			f := it.Field(c)
			if f == nil {
				continue
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			k, _ := json.Marshal(c)
			b.Write(k)
			b.WriteByte(':')
			b.WriteString(Compact(f, 0))
		}
		b.WriteString("}\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// exportBytes is an export written in a format.
func exportBytes(root *Node, f *exportFormat, opts ExportOptions) ([]byte, error) {
	opts.keys = f.keys
	x, err := Export(root, opts)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if err := f.write(&b, x); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// --- jq ---

// exportJQ is a jq program that makes the same csv or tsv from the
// document Export read: the same items, the same flattening and the
// columns as Export found them. It reads the values jq does; the text can
// differ where the formats do (jq's @csv quotes every string, its @tsv
// escapes tab, newline, carriage return and backslash where encoding/csv
// quotes), so the two agree as records, not always byte for byte.
func exportJQ(x *exported, f *exportFormat) (string, error) {
	if f.jq == "" {
		return "", fmt.Errorf("%s has no jq form (csv and tsv do)", f.name)
	}
	steps, err := parsePath(x.opts.Of)
	if err != nil {
		return "", err
	}
	stream := jqSelect(steps)
	str := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	null := str(x.opts.nullText())
	var b strings.Builder
	b.WriteString(`def _key($p; $k): if $p == "" then $k else $p + "." + $k end;` + "\n")
	b.WriteString(`def _name($p): if $p == "" then "value" else $p end;` + "\n")
	b.WriteString(`def _el: if type == "string" then . elif type == "null" then ` + null + ` elif type == "object" or type == "array" then tojson else tostring end;` + "\n")
	var array string
	switch x.opts.Arrays {
	case arraysRows:
		array = `if length == 0 then {} else .[] | _frags($p) end`
	case arraysJSON:
		array = `{(_name($p)): tojson}`
	default:
		array = `{(_name($p)): (map(_el) | join(` + str(x.opts.Join) + `))}`
	}
	b.WriteString(`def _frags($p): def _fields($es): if ($es | length) == 0 then {} else ($es[0].value | _frags(_key($p; $es[0].key))) as $a | _fields($es[1:]) as $b | $a + $b end;` +
		` if type == "object" then (if length == 0 then {(_name($p)): "{}"} else _fields(to_entries) end)` +
		` elif type == "array" then (` + array + `) else {(_name($p)): .} end;` + "\n")
	cols, _ := json.Marshal(x.Columns)
	var items string
	switch {
	case x.whole:
		items = stream + ` | {"value": (if type == "object" or type == "array" then tojson else . end)}`
	case x.fanned:
		items = stream + " | _frags(\"\")"
	default:
		items = stream + " | .[] | _frags(\"\")"
	}
	fmt.Fprintf(&b, "%s as $cols | ($cols, (%s | . as $f | [$cols[] as $c | if ($f | has($c)) then ($f[$c] | if . == null then %s else . end) else \"\" end])) | %s\n",
		cols, items, null, f.jq)
	return "(" + b.String() + ")", nil
}

// jqSelect is the jq stream of the values Select names: a key only on an
// object that has it, an index only in range, [] on any container.
func jqSelect(steps []pathStep) string {
	if len(steps) == 0 {
		return "."
	}
	var parts []string
	for _, st := range steps {
		switch st.kind {
		case 'k':
			k, _ := json.Marshal(st.key)
			parts = append(parts, fmt.Sprintf(`(select(type == "object" and has(%s)) | .[%s])`, k, k))
		case 'i':
			parts = append(parts, fmt.Sprintf(`(select(type == "array") | (if %d < 0 then length + %d else %d end) as $i | select($i >= 0 and $i < length) | .[$i])`, st.index, st.index, st.index))
		case 'a':
			parts = append(parts, `(select(type == "object" or type == "array") | .[])`)
		}
	}
	return strings.Join(parts, " | ")
}

// --- the viewer ---

const (
	settingExportArrays = "export.arrays"
	settingExportJoin   = "export.join"
	settingExportNull   = "export.null"
)

func declareExportSettings(r *ochazuke.Runtime) error {
	for _, s := range []ochazuke.Setting{
		{Name: settingExportArrays, Type: ochazuke.SettingString, Default: arraysJoin, Enum: []string{arraysJoin, arraysJSON, arraysRows},
			Description: "How an export (viewer.export, w to a .csv .tsv or .md file) writes an array in a cell: join its items (export.join between them), its compact JSON, or one row per item with the other columns repeated"},
		{Name: settingExportJoin, Type: ochazuke.SettingString, Default: ";",
			Description: "The separator between an array's items in an exported cell (export.arrays join)"},
		{Name: settingExportNull, Type: ochazuke.SettingString, Default: nullEmpty, Enum: []string{nullEmpty, nullNull},
			Description: "How an export writes null: an empty cell, or the text null (a missing field is always an empty cell)"},
	} {
		if err := declareOwn(r, s); err != nil {
			return err
		}
	}
	return nil
}

// exportOptions are the settings' options for an export of the array at
// of ("" for the one under the cursor).
func (a *App) exportOptions(of string, columns []string) ExportOptions {
	s := a.r.Settings()
	if strings.TrimSpace(of) == "" {
		of = a.exportPath()
	}
	return ExportOptions{Of: of, Columns: columns, Arrays: s.String(settingExportArrays), Join: s.String(settingExportJoin), Null: s.String(settingExportNull)}
}

// exportPath is the array an export takes by default: the one under the
// cursor (the selected shape's items, or the nearest array holding it),
// else the table the data pane shows, else the document.
func (a *App) exportPath() string {
	for _, s := range []*Shape{a.selectedShape(), a.shown} {
		if s == nil {
			continue
		}
		if s.items != nil {
			return s.items.Path
		}
		for at := s; at != nil; at = at.parent {
			if at.Key == "[]" {
				return at.Path
			}
		}
	}
	if a.shown != nil && a.tab != nil {
		return a.shown.Path
	}
	return "."
}

// shownRoot is the document the viewer shows: the edited one, else the
// original.
func (a *App) shownRoot() (*Node, error) {
	if a.Doc == nil {
		return nil, errors.New("no document to export: e opens a file, p pastes JSON")
	}
	if a.editedShown() {
		d := a.restructuredDoc()
		if d.err != nil {
			return nil, d.err
		}
		return d.root, nil
	}
	return a.Doc.Root, nil
}

// encodeFor is what a write to path holds: root as indented JSON, or, when
// the path's extension names an export format, the export of the array
// under the cursor. json reports the JSON case (a write that saves the
// document).
func (a *App) encodeFor(path string, root *Node) (data []byte, json bool, err error) {
	f := formatFor(path)
	if f == nil {
		return []byte(Pretty(root) + "\n"), true, nil
	}
	data, err = exportBytes(root, f, a.exportOptions("", nil))
	return data, false, err
}

// splitColumns reads a columns argument: names separated by commas.
func splitColumns(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// export writes the export viewer.export asks for, replacing a file only
// when replace is set (viewer.export-overwrite).
func (a *App) export(c ochazuke.Ctx, replace bool) ochazuke.Result {
	path := ochazuke.StringArg(c.Args, "path", "")
	f, err := exportFormatOf(ochazuke.StringArg(c.Args, "format", ""), path)
	if err != nil {
		return ochazuke.Failed(err)
	}
	root, err := a.shownRoot()
	if err != nil {
		return ochazuke.Failed(err)
	}
	data, err := exportBytes(root, f, a.exportOptions(ochazuke.StringArg(c.Args, "of", ""), splitColumns(ochazuke.StringArg(c.Args, "columns", ""))))
	if err != nil {
		return ochazuke.Failed(err)
	}
	mode := writeNewFile
	if replace {
		mode = writeReplace
	}
	ask := ochazuke.Arguments{}
	for k, v := range c.Args {
		ask[k] = v
	}
	return a.writeFileAsk(c, path, data, mode, "the "+f.name+" export", "viewer.export-overwrite", ask, nil)
}

// exportJQText is the jq program of the shown document's restructure (when
// edited) followed by its csv or tsv export.
func (a *App) exportJQText(format, of, columns string) (string, error) {
	f, err := formatNamed(format)
	if err != nil {
		return "", err
	}
	root, err := a.shownRoot()
	if err != nil {
		return "", err
	}
	x, err := Export(root, a.exportOptions(of, splitColumns(columns)))
	if err != nil {
		return "", err
	}
	tail, err := exportJQ(x, f)
	if err != nil {
		return "", err
	}
	if !a.editedShown() {
		return tail + "\n", nil
	}
	p, err := a.plan()
	if err != nil {
		return "", err
	}
	return "(" + strings.TrimSuffix(p.JQ(), "\n") + ") | " + tail + "\n", nil
}

// registerExportVerbs adds viewer.export and its confirm.
func (a *App) registerExportVerbs(params func(string, map[string]ochazuke.Parameter)) {
	r := a.r
	r.Define("viewer").
		Verb("export", func(c ochazuke.Ctx) ochazuke.Result { return a.export(c, false) },
			"Export one array of the shown document (the edited one, restructure and value edits applied) to a new file as csv, tsv, jsonl or md: by default the array under the cursor, else the table the data pane shows; of names another, columns picks and orders the columns. Nested objects are dotted columns; arrays in a cell follow export.arrays; an existing file asks first (viewer.export-overwrite), the source never").When("viewer-document").
		Verb("export-overwrite", func(c ochazuke.Ctx) ochazuke.Result { return a.export(c, true) },
			"Replace an existing file with the export (never the source)").Confirm("Replace {{path}} with the export?").When("viewer-document")
	args := map[string]ochazuke.Parameter{
		"path":    {Type: ochazuke.PathParameterType, New: true, Required: true, Description: "the file to write, e.g. NAME.csv (the extension picks the format)"},
		"format":  {Type: "string", Enum: exportFormatNames(), Description: "csv, tsv, jsonl or md; omit to take it from the path's extension"},
		"of":      {Type: "string", Description: "the array's structure path, e.g. .items or .items[]; omit for the array under the cursor"},
		"columns": {Type: "string", Description: "the columns to keep, in order, separated by commas (owner.name for a nested field); omit for all"},
	}
	params("viewer.export", args)
	params("viewer.export-overwrite", args)
	setHistory(r, ochazuke.HistoryChange, "viewer.export", "viewer.export-overwrite")
	a.registerOpenIn(params)
}

// openInApps are the programs viewer.open-in hands the current table to,
// in the order it tries them, each with the export format it reads.
var openInApps = []struct{ name, format string }{
	{"o-csv", "csv"},
}

// lookPath finds a program on PATH (a test replaces it).
var lookPath = exec.LookPath

// openInApp is the program viewer.open-in runs for name ("" the first on
// PATH): its entry and its path, ok false when none is there.
func openInApp(name string) (app, format, bin string, ok bool) {
	for _, o := range openInApps {
		if name != "" && o.name != name {
			continue
		}
		if p, err := lookPath(o.name); err == nil {
			return o.name, o.format, p, true
		}
	}
	return "", "", "", false
}

// registerOpenIn adds viewer.open-in: the table the export would write,
// in a temporary file (removed once the program exits), opened by a
// program that takes the terminal (ochazuke.Foreground) until it exits.
func (a *App) registerOpenIn(params func(string, map[string]ochazuke.Parameter)) {
	r := a.r
	r.DefineGuard("viewer-opens-in", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		_, _, _, ok := openInApp(ochazuke.StringArg(ev.Args, "app", ""))
		return ok && a.Doc != nil
	})
	r.Define("viewer").
		Verb("open-in", func(c ochazuke.Ctx) ochazuke.Result {
			name, format, bin, ok := openInApp(ochazuke.StringArg(c.Args, "app", ""))
			if !ok {
				return ochazuke.Failed(errors.New("no program to open the table in is on PATH (o-csv)"))
			}
			f, err := formatNamed(format)
			if err != nil {
				return ochazuke.Failed(err)
			}
			root, err := a.shownRoot()
			if err != nil {
				return ochazuke.Failed(err)
			}
			data, err := exportBytes(root, f, a.exportOptions(ochazuke.StringArg(c.Args, "of", ""), splitColumns(ochazuke.StringArg(c.Args, "columns", ""))))
			if err != nil {
				return ochazuke.Failed(err)
			}
			dir, err := os.MkdirTemp("", "o-json-open-in-")
			if err != nil {
				return ochazuke.Failed(err)
			}
			base := strings.TrimSuffix(filepath.Base(a.Path), filepath.Ext(a.Path))
			if base == "" || base == "." || base == "-" {
				base = "o-json"
			}
			file := filepath.Join(dir, base+"."+f.name)
			if err := os.WriteFile(file, data, 0o600); err != nil {
				_ = os.RemoveAll(dir)
				return ochazuke.Failed(err)
			}
			return ochazuke.Foreground(exec.Command(bin, file)).Label(name + " " + filepath.Base(file)).
				Then(func(ochazuke.Ctx, any, error) { _ = os.RemoveAll(dir) })
		}, "Open the table the export would write (the array under the cursor, else the data pane's; of and columns as viewer.export takes them) in another program, o-csv first: a temporary csv it reads, removed once it exits. The program has the terminal until it exits. Available when one is on PATH").When("viewer-opens-in")
	names := make([]string, 0, len(openInApps))
	for _, o := range openInApps {
		names = append(names, o.name)
	}
	params("viewer.open-in", map[string]ochazuke.Parameter{
		"app":     {Type: "string", Enum: names, Description: "the program; omit for the first on PATH"},
		"of":      {Type: "string", Description: "the array's structure path; omit for the one under the cursor"},
		"columns": {Type: "string", Description: "the columns to keep, in order, separated by commas"},
	})
	setHistory(r, ochazuke.HistoryAction, "viewer.open-in")
}

// --- the command line ---

// exportCommand is o-json export.
func exportCommand(stdin io.Reader, stdout, stderr io.Writer) oden.Command {
	return oden.Command{
		Name: "export", Summary: "write one array of the document (restructured by --map) as csv, tsv, jsonl or a markdown table",
		Usage:  "FILE|- [--map SPEC.json] [--of PATH] [--format csv|tsv|jsonl|md] [--columns A,B] [--arrays join|json|rows] [--join SEP] [--null empty|null] [--jq] [--out PATH [--overwrite]]",
		Source: "o-json",
		Details: `The rows are the array at --of (default ., the document; .items[].tags fans out over every item's tags), one per item;
a value that is not an array is one row with one column, value. An object item's fields are the columns in structure order,
a nested object as dotted columns (owner.name). An array in a cell is joined with --join (--arrays join, the default), its
compact JSON (json), or one row per item with the other columns repeated (rows). A missing field is an empty cell, null an
empty cell or null (--null). --columns picks and orders the columns; jsonl writes the items as they are, one per line.
--map restructures first (and applies its patches), as o-json restructure does. The format is --format, else --out's
extension, else csv. --jq prints a jq program (for jq -r) that makes the same csv or tsv. Output goes to stdout, or with
--out to a new file; an existing file is replaced only with --overwrite.`,
		Run: func(_ context.Context, args []string) error {
			fs := flag.NewFlagSet("o-json export", flag.ContinueOnError)
			fs.SetOutput(stderr)
			spec := fs.String("map", "", "restructure with this mapping file first")
			of := fs.String("of", ".", "the array's structure path")
			format := fs.String("format", "", "csv, tsv, jsonl or md (default: --out's extension, else csv)")
			columns := fs.String("columns", "", "the columns to keep, in order, separated by commas")
			arrays := fs.String("arrays", arraysJoin, "an array in a cell: join, json or rows")
			join := fs.String("join", ";", "the separator for --arrays join")
			null := fs.String("null", nullEmpty, "null as an empty cell (empty) or the text null")
			asJQ := fs.Bool("jq", false, "print the jq program that makes the csv or tsv instead")
			out := fs.String("out", "", "write to this path instead of stdout")
			overwrite := fs.Bool("overwrite", false, "with --out, replace an existing file")
			words, err := parseFlags(fs, args)
			if err != nil {
				return err
			}
			if *overwrite && *out == "" {
				return errors.New("--overwrite needs --out (the file to replace)")
			}
			f, err := exportFormatOf(*format, *out)
			if err != nil {
				return err
			}
			doc, _, err := readArgs(words, stdin, 0)
			if err != nil {
				return err
			}
			root, program := doc.Root, ""
			if *spec != "" {
				data, err := os.ReadFile(*spec)
				if err != nil {
					return err
				}
				m, err := ReadMapping(data)
				if err != nil {
					return err
				}
				plan, err := NewDocPlan(m, doc.Root, doc.Shape)
				if err != nil {
					return err
				}
				root, program = plan.Apply(doc.Root), strings.TrimSuffix(plan.JQ(), "\n")
			}
			opts := ExportOptions{Of: *of, Columns: splitColumns(*columns), Arrays: *arrays, Join: *join, Null: *null}
			var data []byte
			if *asJQ {
				x, err := Export(root, opts)
				if err != nil {
					return err
				}
				tail, err := exportJQ(x, f)
				if err != nil {
					return err
				}
				if program != "" {
					tail = "(" + program + ") | " + tail
				}
				data = []byte(tail + "\n")
			} else if data, err = exportBytes(root, f, opts); err != nil {
				return err
			}
			if *out != "" {
				return writeNew(*out, data, *overwrite)
			}
			_, err = stdout.Write(data)
			return err
		},
	}
}
