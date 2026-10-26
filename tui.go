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
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/oden"
)

// viewCommand is o-json view: the two-panel viewer.
func viewCommand(stdin io.Reader, stdout, stderr io.Writer) oden.Command {
	return oden.Command{
		Name: "view", Summary: "open the viewer: the structure tree on the left, the selected path's data on the right",
		Usage: "[--smoke] [--width N] [--height N] FILE|-", Source: "o-json",
		Details: "o-json FILE opens it too. Keys: ? in the viewer lists them; README.md has the table.",
		Run: func(_ context.Context, args []string) error {
			return runTUI(args, stdin, stdout, stderr)
		},
	}
}

// runTUI parses the viewer's flags and runs it (or renders one frame).
func runTUI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("o-json view", flag.ContinueOnError)
	flags.SetOutput(stderr)
	smoke := flags.Bool("smoke", false, "render one frame to stdout and exit")
	width := flags.Int("width", 80, "--smoke frame width")
	height := flags.Int("height", 24, "--smoke frame height")
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
	default:
		return errors.New("a file is required (- reads stdin)")
	}
	a, err := New()
	if err != nil {
		return err
	}
	a.Stdin = stdin
	if *smoke {
		if err := a.Load(path); err != nil {
			return err
		}
		a.m.Width, a.m.Height = *width, *height
		_, err := fmt.Fprintln(stdout, ansi.Strip(a.m.View().Content))
		return err
	}
	a.Clipboard = systemClipboard()
	var opts []tea.ProgramOption
	if path == "-" {
		tty, err := os.Open("/dev/tty")
		if err != nil {
			return fmt.Errorf("reading the document from stdin needs a terminal for keys: %w", err)
		}
		defer tty.Close()
		opts = append(opts, tea.WithInput(tty))
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "load", Target: pageID, Args: ochazuke.Arguments{"path": path}}); err != nil {
		return err
	}
	_, err = tea.NewProgram(a.m, opts...).Run()
	return err
}
