// Command o-json is the family's JSON structure and table viewer.
//
//	o-json [view] [--smoke] FILE|-              (the viewer)
//	o-json structure [--format text|json] FILE|-
//	o-json table [--format text|json|markdown] FILE|- [PATH]
//	o-json get [--raw] [--format json] FILE|- PATH
//	o-json query [--compact] FILE|- QUERY       (jq, through gojq)
//	o-json restructure FILE|- --map SPEC.json [--jq | --mapping] [--out PATH [--overwrite]]
//	o-json export FILE|- [--map SPEC.json] [--of PATH] [--format F] [--jq] [--out PATH [--overwrite]]
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
	"path/filepath"
	"strings"

	ocli "ofam.dev/ochazuke/cli"
	"ofam.dev/oden"
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
	return appName()
}

// appName keeps the trial binary's CLI identity separate. Ochazuke uses
// the executable name for its config and state directories as well.
func appName() string {
	if filepath.Base(os.Args[0]) == "o-jj" {
		return "o-jj"
	}
	return "o-json"
}

// Main runs one invocation. A first word that is an existing file (or -,
// stdin) opens it in the viewer even when it shadows a command name; otherwise oden
// resolves the command words, and anything it does not know (flags, -, a
// path) reaches the viewer.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) > 0 && (args[0] == "-" || isFile(args[0])) {
		// "-" is stdin, a file word too: oden would resolve it as a
		// command word and print the usage.
		return runTUI(args, stdin, stdout, stderr)
	}
	app := oden.New(appName(), args, oden.WithBuiltins(), oden.WithStdio(stdin, stdout, stderr),
		oden.WithDefault(func(_ context.Context, args []string) error {
			if len(args) > 0 && !strings.HasPrefix(args[0], "-") && !strings.ContainsAny(args[0], "./") {
				return oden.ErrNotFound
			}
			return runTUI(args, stdin, stdout, stderr)
		}))
	// o-json's own commands are added before Mount: oden gives a name to
	// the first command that claims it, so view stays the viewer and not
	// the framework's short name for ochazuke.view (v0.50.0).
	app.Add(textCommands(stdin, stdout, stderr)...)
	app.Add(exportCommand(stdin, stdout, stderr))
	app.Add(viewCommand(stdin, stdout, stderr))
	app.Add(storyCommands()...) // the tester's story command; none in a normal build
	ocli.Mount(app, newShell(stdin, stdout, stderr))
	// Mount's help note says every result is JSON, which is true of the doc
	// noun only; structure, table and get print text.
	app.HelpText = `Files:
  Every command reads a file, or stdin as - (or when stdin is not a terminal).
Output:
  structure, table and get print text (--format json for JSON).
  doc VERB prints JSON for a host: the same answers, keys in document order.`
	return app.Run(context.Background())
}
