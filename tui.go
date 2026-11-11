package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// viewCommand is o-json view: the two-panel viewer.
func viewCommand(stdin io.Reader, stdout, stderr io.Writer, dataDir string) oden.Command {
	return oden.Command{
		Name: "view", Summary: "open the viewer: the structure tree on the left, the selected path's data on the right",
		Usage: "[--smoke] [--width N] [--height N] [FILE|-]", Source: appName(),
		Details: "o-json FILE opens it too; with no file (and stdin a terminal) the viewer opens an empty document. Keys: ? in the viewer lists them; README.md has the table.",
		Run: func(_ context.Context, args []string) error {
			if dataDir != "" {
				args = append([]string{"--data", dataDir}, args...)
			}
			return runTUI(args, stdin, stdout, stderr)
		},
	}
}

// openFromCommandLine runs doc.open for the viewer's file word (none: an
// empty unnamed document).
func openFromCommandLine(a *App, words []string) error {
	req := ochazuke.Request{Noun: "doc", Verb: "open", Target: pageID, Args: ochazuke.Arguments{}}
	if len(words) > 0 {
		req.Args["path"] = words[0]
	}
	_, err := a.r.Execute(req)
	return err
}

// runTUI parses the viewer's flags and runs it (or renders one frame).
func runTUI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("o-json view", flag.ContinueOnError)
	flags.SetOutput(stderr)
	smoke := flags.Bool("smoke", false, "render one frame to stdout and exit")
	width := flags.Int("width", 80, "--smoke frame width")
	height := flags.Int("height", 24, "--smoke frame height")
	dataDir := flags.String("data", "", "document registry and history state directory")
	words, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	path := ""
	switch {
	case len(words) > 1:
		return errors.New("one file at a time")
	case len(words) == 1:
		path = words[0]
	case !isTerminal(stdin):
		path = "-"
	}
	// No file and stdin a terminal: an empty document, "[no file]", as vim
	// opens an empty buffer.
	a, err := New()
	if err != nil {
		return err
	}
	a.Stdin = keepInput(stdin) // read once, kept: the stdin document reads again
	if *dataDir != "" {
		a.r.Config().StateDir = *dataDir
	}
	if *smoke {
		if path != "" {
			if err := a.LoadSource(path); err != nil {
				return err
			}
		}
		a.m.Width, a.m.Height = *width, *height
		_, err := fmt.Fprintln(stdout, ansi.Strip(a.m.View().Content))
		return err
	}
	a.LoadRecent()
	a.Clipboard = systemClipboard()
	a.Paste = systemPaste()
	var opts []tea.ProgramOption
	if path == "-" {
		tty, err := os.Open("/dev/tty")
		if err != nil {
			return fmt.Errorf("reading the document from stdin needs a terminal for keys: %w", err)
		}
		defer tty.Close()
		opts = append(opts, tea.WithInput(tty))
	}
	// The command line's file word is doc.open's source: o-json FILE, - and
	// no word at all are that verb, as the palette and a host run it.
	args = nil
	if path != "" {
		args = []string{path}
	}
	if err := openFromCommandLine(a, args); err != nil {
		return err
	}
	_, err = tea.NewProgram(a.m, opts...).Run()
	return err
}
