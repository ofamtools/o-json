package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"unicode"
	"unicode/utf8"
)

var subUsage = map[string]string{
	"structure": "o-json structure FILE|- [--format text|json]",
	"flatten":   "o-json flatten FILE|- [--select PATH,PATH] [--field PATH]... [--grain PATH] [--inner] [--cross] [--format csv|json|table|tsv|jsonl] [--jq]",
	"query":     "o-json query FILE|- [--compact] [--] EXPR",
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// parseArgs lets flags and positional words come in any order. Words after
// "--" are always positional. A dash-leading word that is not a flag this
// command defines (a jq expression such as -.a or -1) is positional too.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			name, _, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
			f := fs.Lookup(name)
			if f != nil || name == "h" || name == "help" {
				flags = append(flags, a)
				if f != nil && !hasVal && i+1 < len(args) {
					if b, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !b.IsBoolFlag() {
						i++
						flags = append(flags, args[i])
					}
				}
				continue
			}
		}
		pos = append(pos, a)
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return pos, nil
}

// readSource reads a file, or stdin for "-".
func readSource(name string, stdin io.Reader) (string, error) {
	if name == "-" {
		data, err := io.ReadAll(stdin)
		return string(data), err
	}
	data, err := os.ReadFile(name)
	return string(data), err
}

// subcommand runs structure, flatten or query and prints only on success.
func subcommand(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := "usage: " + subUsage[name]
	fs := flag.NewFlagSet("o-json "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usage); fs.PrintDefaults() }
	var format *string
	var fields multiFlag
	var selects *string
	var grain *string
	var inner, cross, showJQ, compact *bool
	switch name {
	case "structure":
		format = fs.String("format", "text", "output format: text or json")
	case "flatten":
		format = fs.String("format", "csv", "output format: csv, tsv, json, jsonl or table")
		selects = fs.String("select", "", "comma-separated PATHs to include; commas inside [\"quoted\"] keys are kept")
		fs.Var(&fields, "field", "PATH to include, e.g. orders[].id (repeatable; none means every field)")
		grain = fs.String("grain", "", "array PATH whose elements become the rows, e.g. orders[]")
		inner = fs.Bool("inner", false, "drop parent rows whose nested array is empty")
		cross = fs.Bool("cross", false, "allow a cross product of sibling arrays")
		showJQ = fs.Bool("jq", false, "print the equivalent jq program instead of the rows")
	case "query":
		compact = fs.Bool("compact", false, "print each result on one line")
	}
	pos, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	want := map[string]int{"structure": 1, "flatten": 1, "query": 2}[name]
	if len(pos) != want {
		fmt.Fprintf(stderr, "o-json %s: expected %d argument(s), got %d\n%s\n", name, want, len(pos), usage)
		return 2
	}
	fail := func(code int, err error) int {
		fmt.Fprintf(stderr, "o-json %s: %v\n", name, err)
		return code
	}
	raw, err := readSource(pos[0], stdin)
	if err != nil {
		return fail(1, err)
	}
	var out bytes.Buffer
	if name == "query" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		res, err := RunJQResults(ctx, raw, pos[1])
		if errors.Is(err, context.Canceled) {
			return 130
		}
		if err != nil {
			return fail(1, err)
		}
		for _, v := range res {
			data, err := FormatJSON(v, !*compact)
			if err != nil {
				return fail(1, err)
			}
			out.Write(data)
			out.WriteByte('\n')
		}
		if _, err := stdout.Write(out.Bytes()); err != nil {
			return fail(1, err)
		}
		return 0
	}
	v, err := ParseDocument(raw)
	if err != nil {
		return fail(1, fmt.Errorf("%s: %w", pos[0], err))
	}
	switch name {
	case "structure":
		err = writeStructure(&out, InferSchema(v), *format)
	case "flatten":
		var sel []string
		if sel, err = splitPaths(*selects); err == nil {
			err = flattenCmd(&out, v, append(sel, fields...), *grain, *format, *inner, *cross, *showJQ)
		}
	}
	if err != nil {
		return fail(2, err)
	}
	if _, err := stdout.Write(out.Bytes()); err != nil {
		return fail(1, err)
	}
	return 0
}

func flattenCmd(w io.Writer, v any, fieldArgs []string, grainArg, format string, inner, cross, showJQ bool) error {
	var fields []Path
	for _, f := range fieldArgs {
		p, err := ParsePath(f)
		if err != nil {
			return fmt.Errorf("--field %q: %w", f, err)
		}
		fields = append(fields, p)
	}
	var grain Path
	if grainArg != "" {
		p, err := ParsePath(grainArg)
		if err != nil {
			return fmt.Errorf("--grain %q: %w", grainArg, err)
		}
		grain = p
	}
	if grain == nil && !cross {
		if sib := SiblingArrays(v, fields); len(sib) > 0 {
			names := make([]string, len(sib))
			for i, p := range sib {
				names[i] = p.String()
			}
			return fmt.Errorf("sibling arrays %s could each be the rows: pick one with --grain, or --cross for every combination", strings.Join(names, ", "))
		}
	}
	op, err := ResolveOperation(v, Operation{Kind: "flatten", Paths: fields, Grain: grain, Inner: inner, Cross: cross})
	if err != nil {
		return err
	}
	if showJQ {
		_, err := fmt.Fprintln(w, SynthesizeJQ([]Operation{op}))
		return err
	}
	r, err := Flatten(v, op.Paths, op.Grain, op.Cross, op.Inner)
	if err != nil {
		return err
	}
	switch format {
	case "csv":
		return ExportCSV(w, r)
	case "tsv":
		return ExportTSV(w, r)
	case "table":
		return writeTable(w, r)
	case "json", "jsonl":
		if format == "json" {
			format = "pretty"
		}
		data, err := encode(r.Value(), format)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	return fmt.Errorf("--format %q: use csv, tsv, json, jsonl or table", format)
}

// splitPaths splits a --select list at commas that sit outside brackets and
// quotes, so ["x,y"],orders[].id is two paths.
func splitPaths(s string) ([]string, error) {
	var out []string
	depth, start := 0, 0
	inQuote, esc := false, false
	add := func(end int) {
		if p := strings.TrimSpace(s[start:end]); p != "" {
			out = append(out, p)
		}
	}
	for i, r := range s {
		switch {
		case esc:
			esc = false
		case inQuote && r == '\\':
			esc = true
		case r == '"' && depth > 0:
			inQuote = !inQuote
		case inQuote:
		case r == '[':
			depth++
		case r == ']' && depth > 0:
			depth--
		case r == ',' && depth == 0:
			add(i)
			start = i + 1
		}
	}
	if inQuote || depth > 0 {
		return nil, fmt.Errorf("--select %q: unclosed [ or quote", s)
	}
	add(len(s))
	return out, nil
}

// tableCell is one table cell: strings as they are, null as null, anything
// else as compact JSON, with control characters escaped so a cell is one line.
func tableCell(v any) string {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case nil:
		s = "null"
	default:
		if data, err := FormatJSON(v, false); err == nil {
			s = string(data)
		} else {
			s = fmt.Sprint(v)
		}
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsControl(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// writeTable prints r as aligned text columns in the relation's own order.
func writeTable(w io.Writer, r Relation) error {
	if len(r.Columns) == 0 {
		return nil
	}
	head := make([]string, len(r.Columns))
	rule := make([]string, len(r.Columns))
	lines := make([][]string, len(r.Rows))
	for i, c := range r.Columns {
		head[i] = tableCell(c)
		rule[i] = head[i]
	}
	for n, row := range r.Rows {
		lines[n] = make([]string, len(r.Columns))
		for i, c := range r.Columns {
			v, _ := Lookup(row, c)
			lines[n][i] = tableCell(v)
			if l := utf8.RuneCountInString(lines[n][i]); l > utf8.RuneCountInString(rule[i]) {
				rule[i] = lines[n][i]
			}
		}
	}
	for i, c := range rule {
		rule[i] = strings.Repeat("-", max(utf8.RuneCountInString(c), 1))
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, line := range append([][]string{head, rule}, lines...) {
		fmt.Fprintln(tw, strings.Join(line, "\t"))
	}
	return tw.Flush()
}

// structNode is one schema node in `structure --format json`.
type structNode struct {
	Name     string        `json:"name"`
	Path     string        `json:"path"`
	Type     string        `json:"type"`
	Count    int           `json:"count"`
	Total    int           `json:"total,omitempty"`
	Sample   any           `json:"sample,omitempty"`
	Children []*structNode `json:"children,omitempty"`
}

func toNode(s *Schema) *structNode {
	n := &structNode{Name: s.Name, Path: s.Path.String(), Type: s.Type, Count: s.Count, Total: s.Total, Sample: s.Sample}
	for _, c := range s.Children {
		n.Children = append(n.Children, toNode(c))
	}
	return n
}

// writeStructure prints the merged shape: one line per field with its type,
// how often it is present and, for arrays, how many items.
func writeStructure(w io.Writer, s *Schema, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(toNode(s))
	case "text":
	default:
		return fmt.Errorf("--format %q: use text or json", format)
	}
	type row struct{ name, typ, count, note string }
	var rows []row
	var walk func(s *Schema, depth int, element bool)
	walk = func(s *Schema, depth int, element bool) {
		name := s.Name
		if depth == 0 && name == "" {
			name = "$"
		}
		r := row{name: strings.Repeat("  ", depth) + name, typ: s.Type, count: fmt.Sprint(s.Count)}
		var notes []string
		if !element && s.Total > 0 {
			r.count = fmt.Sprintf("%d/%d", s.Count, s.Total)
			if s.Count < s.Total {
				notes = append(notes, "optional")
			}
		}
		if len(s.Children) == 1 && isElement(s.Children[0]) {
			items := fmt.Sprintf("%d items", s.Children[0].Count)
			if s.Count > 1 {
				items += fmt.Sprintf(" in %d arrays", s.Count)
			}
			notes = append(notes, items)
		}
		r.note = strings.Join(notes, "  ")
		rows = append(rows, r)
		for _, c := range s.Children {
			walk(c, depth+1, isElement(c))
		}
	}
	if s != nil {
		walk(s, 0, false)
	}
	nw, tw, cw := 0, 0, 0
	for _, r := range rows {
		nw, tw, cw = max(nw, len([]rune(r.name))), max(tw, len(r.typ)), max(cw, len(r.count))
	}
	for _, r := range rows {
		line := fmt.Sprintf("%-*s  %-*s  %*s  %s", nw+pad(r.name), r.name, tw, r.typ, cw, r.count, r.note)
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
	return nil
}

// isElement reports whether s describes the elements of an array.
func isElement(s *Schema) bool { return len(s.Path) > 0 && s.Path[len(s.Path)-1].Array }

// pad corrects %-*s, which counts bytes, for names with multi-byte runes.
func pad(s string) int { return len(s) - len([]rune(s)) }
