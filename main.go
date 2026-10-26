// Command o-json is the family's JSON structure and table viewer.
//
//	o-json structure [--format text|json] FILE|-
//	o-json table [--format text|json|markdown] FILE|- [PATH]
//	o-json get [--raw] [--format json] FILE|- PATH
//	o-json doc structure|table|get FILE [PATH]   (JSON, for a host)
//	o-json catalog
//
// Every command reads a file, or stdin as - (or when stdin is not a
// terminal and no file is named).
package main

import (
	"context"
	"io"
	"os"
	"strings"

	ocli "github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/oden"
)

func main() {
	os.Exit(oden.Report(os.Stderr, progName(), Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)))
}

// progName is how messages name this command: the word under the parent
// oden app that ran it, else the bin name.
func progName() string {
	if parent := os.Getenv("ODEN_PARENT"); parent != "" {
		return parent + " json"
	}
	return "o-json"
}

// Main runs one invocation: oden resolves the command words.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	app := oden.New("o-json", bareJSON(args), oden.WithBuiltins(), oden.WithStdio(stdin, stdout, stderr))
	ocli.Mount(app, newShell(stdin, stdout, stderr))
	app.Add(textCommands(stdin, stdout, stderr)...)
	// Mount's help note says every result is JSON, which is true of the doc
	// noun only; structure, table and get print text.
	app.HelpText = `Files:
  Every command reads a file, or stdin as - (or when stdin is not a terminal).
Output:
  structure, table and get print text (--format json for JSON).
  doc VERB prints JSON for a host: the same answers, keys in document order.`
	return app.Run(context.Background())
}

// bareJSON drops a --json given without a value (from o-csv): the shell's
// --json takes arguments as JSON, and its output is always JSON, so
// o-json doc get FILE .a --json means what it says.
func bareJSON(args []string) []string {
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--json" && (i+1 == len(args) || strings.HasPrefix(args[i+1], "--")) {
			continue
		}
		out = append(out, arg)
	}
	return out
}
