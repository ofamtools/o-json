// Command o-json is a terminal JSON workspace built on Ochazuke v2: a
// structure tree beside table, nested, pretty and raw views, relational
// flatten, shape flips, live jq, buffers and splits. The structure, flatten
// and query subcommands print the same answers without a TTY.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke/cli"
	"ofam.dev/ochazuke/core"
	"ofam.dev/oden"
)

// Version is the o-json application version.
const Version = "2.0.0-next"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// run dispatches through the uniform Oden v2 / Ochazuke v2 CLI shell while
// preserving positional FILE opening, stdin piping, and the structure,
// flatten, and query subcommands.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	app := newOdenApp(args, stdin, stdout, stderr)
	return oden.Report(stderr, progName(), app.Run(context.Background()))
}

func progName() string {
	if parent := os.Getenv("ODEN_PARENT"); parent != "" {
		return parent + " json"
	}
	return "o-json"
}

func newOdenApp(args []string, stdin io.Reader, stdout, stderr io.Writer) *oden.App {
	info := cli.AppInfo{
		Name:     "o-json",
		Version:  Version,
		Summary:  "Terminal JSON workspace: structure tree, table/nested/pretty/raw views, relational flatten, live jq",
		Modes:    func() []core.Mode { return modes },
		Bindings: func() []core.Binding { return defaultBindings() },
		Specs:    func() []core.CommandSpec { return commandSpecs() },
		RunTUI: func(_ context.Context, tuiArgs []string) error {
			if code := interactive(tuiArgs, stdin, stdout, stderr); code != 0 {
				return &oden.ExitError{Code: code}
			}
			return nil
		},
		RunSmoke: func(out io.Writer, _ bool) error {
			return runSmoke(out, "")
		},
	}

	app := cli.NewApp(info, args, oden.WithStdio(stdin, stdout, stderr))

	subCmd := func(name, summary, usage string) oden.Command {
		return oden.Command{
			Name:    name,
			Summary: summary,
			Usage:   usage,
			Run: func(_ context.Context, subArgs []string) error {
				if code := subcommand(name, subArgs, stdin, stdout, stderr); code != 0 {
					return &oden.ExitError{Code: code}
				}
				return nil
			},
		}
	}

	app.Add(
		subCmd("structure", "inspect merged JSON schema structure", "FILE|- [--format text|json]"),
		subCmd("flatten", "flatten nested JSON into tabular rows or jq", "FILE|- [--select PATH,PATH] [--field PATH]... [--grain PATH] [--inner] [--cross] [--format csv|json|table|tsv|jsonl] [--jq]"),
		subCmd("query", "run a jq expression against JSON input", "FILE|- [--compact] [--] EXPR"),
		oden.Command{
			Name:     "capabilities",
			Audience: oden.AudienceMachine,
			Summary:  "print application capabilities (--json for JSON)",
			Usage:    "[--json|--format json|table]",
			Handle: func(c *oden.Ctx) error {
				p, err := c.Parse(nil)
				if err != nil {
					return err
				}
				entries := app.Catalog().Entries
				cmds := make([]string, 0, len(entries))
				for _, e := range entries {
					cmds = append(cmds, e.Name)
				}
				out := oden.Capabilities{
					Name:         "o-json",
					Version:      Version,
					Summary:      info.Summary,
					Capabilities: []string{"tui", "jq", "flatten", "structure"},
					Commands:     cmds,
				}
				if p.Format() == "json" {
					return c.WriteJSON(out)
				}
				rows := [][]string{
					{"name", out.Name},
					{"version", out.Version},
					{"summary", out.Summary},
					{"capabilities", strings.Join(out.Capabilities, ", ")},
					{"commands", strings.Join(out.Commands, ", ")},
				}
				return c.WriteTable([]string{"field", "value"}, rows)
			},
		},
	)

	// Preserve positional FILE opening (`o-json data.json`), `--edit`, `--config`,
	// and stdin piping (`cat data.json | o-json` or `o-json -`).
	app.Default = func(_ context.Context, rootArgs []string) error {
		if code := interactive(rootArgs, stdin, stdout, stderr); code != 0 {
			return &oden.ExitError{Code: code}
		}
		return nil
	}

	app.HelpText = fmt.Sprintf(usage, subUsage["structure"], subUsage["flatten"], subUsage["query"])
	return app
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
