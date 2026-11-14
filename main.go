// Command o-json is the family's JSON structure and table viewer.
//
//	o-json [view] [--smoke] FILE|-              (the viewer)
//	o-json structure [--format text|json] FILE|-
//	o-json table [--format text|json|markdown] FILE|- [PATH]
//	o-json get [--raw] [--format json] FILE|- PATH
//	o-json query [--compact] FILE|- QUERY       (jq, through gojq)
//	o-json restructure FILE|- --map SPEC.json [--jq | --mapping] [--out PATH [--overwrite]]
//	o-json export FILE|- [--map SPEC.json] [--of PATH] [--format F] [--jq] [--out PATH [--overwrite]]
//	o-json doc open [FILE|-]                     (the viewer, as a verb: what o-json FILE runs)
//	o-json doc structure|tabulate|get|rows|query FILE [PATH|QUERY]   (JSON, for a host)
//	o-json doc remap FILE MAPPING [--out PATH]   (restructure, as a verb)
//	o-json doc export FILE [--out PATH]          (export, as a verb)
//	o-json catalog
//
// Every command reads a file, or stdin as - (or when stdin is not a
// terminal and no file is named). The viewer refuses no source: a path
// that names no file yet is a new document of that name.
package main

import (
	"context"
	"fmt"
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

// Main runs one invocation. A first word that names a command (catalog,
// get, table, setup, ...) runs it, even when a file of that name is in the
// directory: ./NAME opens the file. Otherwise a first word that is an
// existing file (or -, stdin) opens it in the viewer, and anything oden does
// not know (flags, a path, a path that names no file yet) reaches the
// viewer, which runs doc.open on it (openFromCommandLine).
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	dataDir := ""
	if len(args) > 0 && args[0] == "--data" {
		if len(args) < 2 {
			return fmt.Errorf("--data requires a state directory")
		}
		dataDir, args = args[1], args[2:]
	}
	tuiArgs := func(args []string) []string {
		if dataDir != "" {
			return append([]string{"--data", dataDir}, args...)
		}
		return args
	}
	app := oden.New(appName(), args, oden.WithBuiltins(), oden.WithStdio(stdin, stdout, stderr),
		oden.WithDefault(func(_ context.Context, args []string) error {
			if len(args) > 0 && !strings.HasPrefix(args[0], "-") && !pathWord(args[0]) && !isFile(args[0]) {
				return oden.ErrNotFound
			}
			return runTUI(tuiArgs(args), stdin, stdout, stderr)
		}))
	// o-json's own commands are added before Mount: oden gives a name to
	// the first command that claims it, so view stays the viewer and not
	// the framework's short name for ochazuke.view (v0.50.0).
	app.Add(textCommands(stdin, stdout, stderr)...)
	app.Add(exportCommand(stdin, stdout, stderr))
	app.Add(viewCommand(stdin, stdout, stderr, dataDir))
	app.Add(storyCommands()...) // the tester's story command; none in a normal build
	shell := newShell(stdin, stdout, stderr, dataDir)
	ocli.Mount(app, shell)
	defer shell.Runtime.Config().Flush()
	if len(args) > 0 && args[0] == "-" {
		// "-" is stdin, a file word too: oden would read it as a command
		// word and print the usage. A file named like a command is not
		// special-cased: the command word wins (./NAME opens the file).
		return runTUI(tuiArgs(args), stdin, stdout, stderr)
	}
	// Mount's help note says every result is JSON, which is true of the doc
	// noun only; structure, table and get print text.
	app.HelpText = `Files:
  Every command reads a file, or stdin as - (or when stdin is not a terminal).
Output:
  structure, table and get print text (--format json for JSON).
  doc VERB prints JSON for a host: the same answers, keys in document order.`
	return app.Run(context.Background())
}
