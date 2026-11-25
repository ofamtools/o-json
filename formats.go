package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// InputFormats lists every explicit format accepted by --format / --from.
var InputFormats = []string{"json", "jsonl", "jsonc", "yaml", "toml", "csv", "tsv"}

// OutputFormats lists every format accepted by convert --to.
var OutputFormats = []string{"json", "jsonl", "jsonc", "yaml", "toml", "csv", "tsv", "md", "pretty", "minified"}

// NormalizeFormat canonicalizes format aliases (yml->yaml, ndjson->jsonl, markdown->md).
func NormalizeFormat(f string) string {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "yml":
		return "yaml"
	case "ndjson":
		return "jsonl"
	case "markdown":
		return "md"
	case "tab":
		return "tsv"
	default:
		return strings.ToLower(strings.TrimSpace(f))
	}
}

// InferInputFormat returns the format implied by a file extension, or "" for auto JSON/JSONL.
func InferInputFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return ""
	case ".jsonl", ".ndjson":
		return "jsonl"
	case ".jsonc":
		return "jsonc"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".csv":
		return "csv"
	case ".tsv", ".tab":
		return "tsv"
	}
	return ""
}

// ParseFormat decodes raw according to an explicit opt-in format. When format is
// empty, it preserves exact existing auto-handling for JSON and JSONL.
func ParseFormat(raw, format string) (any, bool, error) {
	f := NormalizeFormat(format)
	switch f {
	case "":
		v, err := ParseJSON(raw)
		if err == nil {
			return v, false, nil
		}
		if errors.Is(err, ErrEmpty) {
			return nil, false, err
		}
		w, derr := ParseDocument(raw)
		if derr != nil {
			if errOffset(derr, raw) > errOffset(err, raw) {
				return nil, false, derr
			}
			return nil, false, err
		}
		return w, true, nil
	case "json":
		v, err := ParseJSON(raw)
		return v, false, err
	case "jsonl":
		v, err := parseJSONLStrict(raw)
		return v, true, err
	case "jsonc":
		stripped := StripJSONC(raw)
		v, err := ParseJSON(stripped)
		return v, false, err
	case "yaml":
		v, err := ParseYAML(raw)
		return v, false, err
	case "toml":
		v, err := ParseTOML(raw)
		return v, false, err
	case "csv":
		v, err := ParseDelimited(raw, ',')
		return v, false, err
	case "tsv":
		v, err := ParseDelimited(raw, '\t')
		return v, false, err
	default:
		return nil, false, fmt.Errorf("unknown input format %q (use %s)", format, strings.Join(InputFormats, ", "))
	}
}

// parseJSONLStrict reads raw as newline-delimited JSON, always returning an array
// (even when there is a single line).
func parseJSONLStrict(raw string) ([]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrEmpty
	}
	var out []any
	for start := 0; start < len(raw); {
		end := strings.IndexByte(raw[start:], '\n')
		next := len(raw)
		if end >= 0 {
			end += start
			next = end + 1
		} else {
			end = len(raw)
		}
		line := strings.TrimRight(raw[start:end], "\r")
		if strings.TrimSpace(line) != "" {
			v, err := ParseJSON(line)
			if err != nil {
				var pe *ParseError
				if errors.As(err, &pe) {
					return nil, parseErr(raw, int64(start+pe.Offset), errors.New(pe.Msg))
				}
				return nil, err
			}
			out = append(out, v)
		}
		start = next
	}
	if len(out) == 0 {
		return nil, ErrEmpty
	}
	return out, nil
}

// StripJSONC removes // line comments, /* */ block comments, and trailing commas
// before } or ] while preserving string literals and newlines (for error line numbers).
func StripJSONC(raw string) string {
	// Pass 1: strip comments outside strings, preserving newlines.
	var noComments strings.Builder
	noComments.Grow(len(raw))
	inStr, esc := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			noComments.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			noComments.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(raw) && raw[i+1] == '/' {
			i += 2
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			if i < len(raw) {
				noComments.WriteByte('\n')
			}
			continue
		}
		if c == '/' && i+1 < len(raw) && raw[i+1] == '*' {
			i += 2
			for i+1 < len(raw) && !(raw[i] == '*' && raw[i+1] == '/') {
				if raw[i] == '\n' {
					noComments.WriteByte('\n')
				}
				i++
			}
			i++ // skip '/'
			continue
		}
		noComments.WriteByte(c)
	}
	s := noComments.String()

	// Pass 2: remove trailing commas before } or ] outside strings.
	var out strings.Builder
	out.Grow(len(s))
	inStr, esc = false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			out.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		out.WriteByte(c)
	}
	return out.String()
}

// ParseDelimited parses CSV or TSV input with a header row into []any of ordered Objects.
func ParseDelimited(raw string, comma rune) ([]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrEmpty
	}
	r := csv.NewReader(strings.NewReader(raw))
	r.Comma = comma
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, ErrEmpty
	}
	headers := recs[0]
	for i, h := range headers {
		h = strings.TrimPrefix(h, "\ufeff") // strip UTF-8 BOM on first header
		if h == "" {
			h = fmt.Sprintf("col%d", i+1)
		}
		headers[i] = h
	}
	out := make([]any, 0, max(len(recs)-1, 0))
	for _, rec := range recs[1:] {
		row := make(Object, 0, len(headers))
		for i, h := range headers {
			val := ""
			if i < len(rec) {
				val = rec[i]
			}
			row = append(row, Field{Key: h, Value: val})
		}
		for i := len(headers); i < len(rec); i++ {
			row = append(row, Field{Key: fmt.Sprintf("col%d", i+1), Value: rec[i]})
		}
		out = append(out, row)
	}
	return out, nil
}

// EncodeFormat serializes v to any supported output format.
func EncodeFormat(v any, format string, pretty bool) ([]byte, error) {
	f := NormalizeFormat(format)
	switch f {
	case "json", "jsonc":
		if pretty {
			return encode(v, "pretty")
		}
		return encode(v, "minified")
	case "pretty", "minified", "jsonl", "csv", "tsv", "yaml", "toml", "md":
		return encode(v, f)
	default:
		return nil, fmt.Errorf("unknown output format %q (use json, jsonl, jsonc, yaml, toml, csv, tsv, md)", format)
	}
}

// ExportMarkdownTable writes r as a GitHub Flavored Markdown table.
func ExportMarkdownTable(w io.Writer, r Relation) error {
	if len(r.Columns) == 0 {
		return nil
	}
	esc := func(s string) string {
		s = strings.ReplaceAll(s, "|", `\|`)
		s = strings.ReplaceAll(s, "\r\n", "<br>")
		s = strings.ReplaceAll(s, "\n", "<br>")
		s = strings.ReplaceAll(s, "\r", "<br>")
		return s
	}
	head := make([]string, len(r.Columns))
	sep := make([]string, len(r.Columns))
	for i, c := range r.Columns {
		head[i] = esc(c)
		sep[i] = "---"
	}
	if _, err := fmt.Fprintf(w, "| %s |\n| %s |\n", strings.Join(head, " | "), strings.Join(sep, " | ")); err != nil {
		return err
	}
	cells := make([]string, len(r.Columns))
	for _, row := range r.Rows {
		for i, c := range r.Columns {
			x := cell(row, c)
			if i < len(row) && row[i].Key == c {
				x = row[i].Value
			}
			if x == nil {
				cells[i] = ""
			} else {
				cells[i] = esc(jqString(x))
			}
		}
		if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(cells, " | ")); err != nil {
			return err
		}
	}
	return nil
}

// convertCmd implements `o-json convert FILE|- --from FMT --to FMT [--compact|--pretty] [--timeline]`.
func convertCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := "usage: " + subUsage["convert"]
	fs := flag.NewFlagSet("o-json convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usage); fs.PrintDefaults() }
	from := fs.String("from", "", "input format: json, jsonl, jsonc, yaml, toml, csv, tsv (default: auto)")
	to := fs.String("to", "json", "output format: json, jsonl, jsonc, yaml, toml, csv, tsv, md")
	compact := fs.Bool("compact", false, "compact JSON output")
	pretty := fs.Bool("pretty", true, "pretty-print JSON output")
	timeline := fs.Bool("timeline", false, "render Markdown output as a timeline when --to md")
	pos, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "o-json convert: expected 1 argument, got %d\n%s\n", len(pos), usage)
		return 2
	}
	fail := func(code int, err error) int {
		fmt.Fprintf(stderr, "o-json convert: %v\n", err)
		return code
	}
	raw, err := readSource(pos[0], stdin)
	if err != nil {
		return fail(1, err)
	}
	inFmt := *from
	if inFmt == "" && pos[0] != "-" {
		inFmt = InferInputFormat(pos[0])
	}
	v, _, err := ParseFormat(raw, inFmt)
	if err != nil {
		return fail(1, fmt.Errorf("%s: %w", pos[0], err))
	}
	outFmt := NormalizeFormat(*to)
	if outFmt == "md" && *timeline {
		entries := TimelineFromValue(v)
		var buf bytes.Buffer
		if err := ExportMarkdownTimeline(&buf, entries, nil); err != nil {
			return fail(2, err)
		}
		if _, err := stdout.Write(buf.Bytes()); err != nil {
			return fail(1, err)
		}
		return 0
	}
	data, err := EncodeFormat(v, outFmt, *pretty && !*compact)
	if err != nil {
		return fail(2, err)
	}
	if _, err := stdout.Write(data); err != nil {
		return fail(1, err)
	}
	return 0
}
