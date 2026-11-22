// Command o-json is a terminal JSON workspace built on Ochazuke v2: a
// structure tree beside table, nested, pretty and raw views, relational
// flatten, shape flips, live jq, buffers and splits. The structure, flatten
// and query subcommands print the same answers without a TTY.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// run dispatches a subcommand only when the first word is exactly its name;
// ./structure opens a file of that name.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		if _, ok := subUsage[args[0]]; ok {
			return subcommand(args[0], args[1:], stdin, stdout, stderr)
		}
	}
	return interactive(args, stdin, stdout, stderr)
}

const usage = `usage: o-json [--edit] [--config DIR] [FILE|-]
       o-json --smoke [FILE]
       %s
       %s
       %s

With no FILE, o-json opens an editable scratch buffer, or reads stdin when
it is piped (JSON or JSONL). A FILE opens read-only; --edit or ctrl+e makes
it editable. A FILE that does not exist yet opens empty and editable. Press
? for the live key help, : for commands.

flags:
`

func interactive(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("o-json", flag.ContinueOnError)
	fl.SetOutput(stderr)
	edit := fl.Bool("edit", false, "open FILE or stdin editable")
	smoke := fl.Bool("smoke", false, "run the scripted session without a TTY, print the frame and checks")
	cfg := fl.String("config", configDir(), "directory holding bindings.json, themes.json and settings.json")
	fl.Usage = func() {
		fmt.Fprintf(stderr, usage, subUsage["structure"], subUsage["flatten"], subUsage["query"])
		fl.PrintDefaults()
	}
	pos, err := parseArgs(fl, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(pos) > 1 {
		fmt.Fprintf(stderr, "o-json: one FILE at a time (got %d); open more with ctrl+o\n", len(pos))
		return 2
	}
	file := ""
	if len(pos) == 1 {
		file = pos[0]
	}
	if *smoke {
		if err := runSmoke(stdout, file); err != nil {
			fmt.Fprintln(stderr, "o-json smoke:", err)
			return 1
		}
		return 0
	}
	var b *Buffer
	auto := file == "" && piped(stdin)
	if auto || file == "-" {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintln(stderr, "o-json: stdin:", err)
			return 1
		}
		if auto && len(raw) == 0 {
			file = ""
		} else {
			b, _ = NewBuffer("", string(raw), *edit)
			b.Name = "[stdin]"
		}
	}
	switch {
	case b != nil:
	case file == "":
		b = NewScratch()
	default:
		if b, err = OpenBuffer(file, *edit); err != nil {
			fmt.Fprintln(stderr, "o-json:", err)
			return 1
		}
	}
	// With piped stdin, Bubble Tea reads keys from the controlling TTY.
	if _, err := tea.NewProgram(newShell(b, *cfg)).Run(); err != nil {
		fmt.Fprintln(stderr, "o-json:", err)
		return 1
	}
	return 0
}

// piped reports whether stdin is a pipe or file rather than a terminal.
func piped(stdin io.Reader) bool {
	f, ok := stdin.(interface{ Stat() (os.FileInfo, error) })
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice == 0
}

// configDir is $XDG_CONFIG_HOME/o-json-next, else ~/.config/o-json-next.
func configDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "o-json-next")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "o-json-next")
	}
	return ""
}
