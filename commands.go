package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/unixsuperhero/oden"
)

// The top-level commands, structure, table and get, print text for a
// person (or --format json / markdown). They share the readers and
// layouts with the doc noun (shell.go), which prints JSON for a host.

// textCommands are the commands o-json adds to oden beside the doc noun.
func textCommands(stdin io.Reader, stdout, stderr io.Writer) []oden.Command {
	return []oden.Command{
		{
			Name: "structure", Summary: "print the document's shape: every path with its types, presence and optional fields",
			Usage: "[--format text|json] FILE|-", Source: "o-json",
			Details: "A path through an array uses [] for every item; a field is optional when some objects at its parent lack it,\nand fields are listed most common first, so optional ones come last.",
			Run: func(_ context.Context, args []string) error {
				fs, format := formatFlags("structure", stderr, "text", "text or json")
				words, err := parseFlags(fs, args)
				if err != nil {
					return err
				}
				doc, _, err := readArgs(words, stdin, 0)
				if err != nil {
					return err
				}
				switch *format {
				case "text":
					return WriteStructure(stdout, doc.Shape)
				case "json":
					return writeIndented(stdout, doc.Shape)
				}
				return badFormat(*format, "text, json")
			},
		},
		{
			Name: "table", Summary: "print an array (or the values a [] path names) as a table, optional columns last",
			Usage: "[--format text|json|markdown] FILE|- [PATH]", Source: "o-json",
			Details: "PATH defaults to the root (.); .items[].owner makes a row of every item's owner.\nIn text and markdown an optional column's title ends in ?; a missing field is a blank cell.",
			Run: func(_ context.Context, args []string) error {
				fs, format := formatFlags("table", stderr, "text", "text, json or markdown")
				words, err := parseFlags(fs, args)
				if err != nil {
					return err
				}
				doc, rest, err := readArgs(words, stdin, 1)
				if err != nil {
					return err
				}
				t, err := tableAt(doc, first(rest, "."))
				if err != nil {
					return err
				}
				switch *format {
				case "text":
					return t.WriteText(stdout)
				case "markdown", "md":
					return t.WriteMarkdown(stdout)
				case "json":
					return t.WriteJSON(stdout)
				}
				return badFormat(*format, "text, json, markdown")
			},
		},
		{
			Name: "get", Summary: "print the value at a path as JSON (a [] path prints each value it names)",
			Usage: "[--raw] [--format json] FILE|- PATH", Source: "o-json",
			Details: "--raw prints a string without quotes. --format json prints a [] path's values as one array.",
			Run: func(_ context.Context, args []string) error {
				fs, format := formatFlags("get", stderr, "text", "text (each value on its own) or json (a [] path's values as one array)")
				raw := fs.Bool("raw", false, "print strings without quotes")
				words, err := parseFlags(fs, args)
				if err != nil {
					return err
				}
				doc, rest, err := readArgs(words, stdin, 1)
				if err != nil {
					return err
				}
				if len(rest) == 0 {
					return errors.New("a path is required (. is the whole document)")
				}
				values, fanned, err := Select(doc.Root, rest[0])
				if err != nil {
					return err
				}
				if !fanned && len(values) == 0 {
					return fmt.Errorf("nothing at %s", rest[0])
				}
				if *format == "json" && fanned {
					return writeIndented(stdout, values)
				}
				if *format != "text" && *format != "json" {
					return badFormat(*format, "text, json")
				}
				var b strings.Builder
				for _, v := range values {
					if *raw && v.Kind == KString {
						b.WriteString(v.Value)
					} else {
						b.WriteString(Pretty(v))
					}
					b.WriteByte('\n')
				}
				_, err = io.WriteString(stdout, b.String())
				return err
			},
		},
		{
			Name: "restructure", Summary: "make a new document from a mapping of shape paths, or print the mapping's jq program",
			Usage: "FILE|- --map SPEC.json [--jq | --mapping] [--out PATH [--overwrite]]", Source: "o-json",
			Details: `SPEC is {"map": [{"from": ".items[]", "to": ".records[]"}, {"from": ".items[].owner.name", "to": ".records[].owner"}, ...]}:
from is a structure path, to the path in the new document; an entry without from is a new object, to null drops from.
An optional field is copied only where it is present. --jq prints a jq program that does the same and never fails on this
shape; --mapping prints the mapping with every shape it leaves out as a drop. Output goes to stdout, or with --out to a new
file; an existing file (the source among them) is replaced only with --overwrite.`,
			Run: func(_ context.Context, args []string) error {
				fs := flag.NewFlagSet("o-json restructure", flag.ContinueOnError)
				fs.SetOutput(stderr)
				spec := fs.String("map", "", "the mapping file (required)")
				asJQ := fs.Bool("jq", false, "print the jq program instead of the new document")
				asMapping := fs.Bool("mapping", false, "print the complete mapping instead of the new document")
				out := fs.String("out", "", "write to this path instead of stdout")
				overwrite := fs.Bool("overwrite", false, "with --out, replace an existing file (the source too)")
				words, err := parseFlags(fs, args)
				if err != nil {
					return err
				}
				switch {
				case *spec == "":
					return errors.New("--map SPEC.json is required")
				case *asJQ && *asMapping:
					return errors.New("--jq and --mapping are one or the other")
				case *overwrite && *out == "":
					return errors.New("--overwrite needs --out (the file to replace)")
				}
				specData, err := os.ReadFile(*spec)
				if err != nil {
					return err
				}
				m, err := ReadMapping(specData)
				if err != nil {
					return err
				}
				doc, _, err := readArgs(words, stdin, 0)
				if err != nil {
					return err
				}
				as := "document"
				if *asJQ {
					as = "jq"
				} else if *asMapping {
					as = "mapping"
				}
				data, err := restructureOutput(doc, m, as)
				if err != nil {
					return err
				}
				if *out != "" {
					return writeNew(*out, data, *overwrite)
				}
				_, err = stdout.Write(data)
				return err
			},
		},
	}
}

func formatFlags(name string, stderr io.Writer, def, help string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("o-json "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs, fs.String("format", def, help)
}

// parseFlags parses flags anywhere among the words; a usage error is exit 2.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	words, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, &oden.ExitError{Code: 2}
	}
	return words, nil
}

// readArgs reads the document the words name and returns the words after
// it; at most extra may follow.
func readArgs(words []string, stdin io.Reader, extra int) (*Doc, []string, error) {
	file, rest, err := splitFile(words, stdin)
	if err != nil {
		return nil, nil, err
	}
	if len(rest) > extra {
		return nil, nil, fmt.Errorf("unexpected %q", strings.Join(rest[extra:], " "))
	}
	doc, err := ReadDoc(file, stdin)
	return doc, rest, err
}

// tableAt is the table of the values at a path.
func tableAt(doc *Doc, path string) (*Table, error) {
	values, fanned, err := Select(doc.Root, path)
	if err != nil {
		return nil, err
	}
	if !fanned && len(values) == 0 {
		return nil, fmt.Errorf("nothing at %s", path)
	}
	return NewTable(path, values, fanned), nil
}

func first(words []string, def string) string {
	if len(words) == 0 {
		return def
	}
	return words[0]
}

func badFormat(got, want string) error {
	return fmt.Errorf("unknown format %q (one of %s)", got, want)
}

// parseInterspersed parses flags that may come before or after the words
// (from o-csv).
func parseInterspersed(flags *flag.FlagSet, args []string) ([]string, error) {
	var words []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		args = flags.Args()
		if len(args) == 0 {
			return words, nil
		}
		if args[0] == "-" || !strings.HasPrefix(args[0], "-") {
			words = append(words, args[0])
			args = args[1:]
			continue
		}
		words = append(words, args...)
		return words, nil
	}
}

// errHelp ends a command whose flags printed their help: exit 0, nothing more.
var errHelp = &oden.ExitError{Code: 0}
