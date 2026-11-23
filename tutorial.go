package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke/tutorial"
	"ofam.dev/ochazuke/widgets/pathpicker"
	"ofam.dev/oden"
)

const (
	defaultTutorialSpec = "docs/tutorials/o-json.json"
	defaultTutorialOut  = "docs/tutorials/out"
)

// tutorialDriver adapts o-json's synchronous headless Bubble Tea driver to
// ofam.dev/ochazuke/tutorial.Driver.
type tutorialDriver struct {
	d *driver
}

func (td *tutorialDriver) SendKey(key string) error {
	if key == "" {
		return nil
	}
	if isTutorialSpecialOrChord(key) {
		td.d.send(tutorialKeyMsg(key))
		return nil
	}
	if utf8.RuneCountInString(key) == 1 {
		if key == " " {
			td.d.send(tutorialKeyMsg("space"))
		} else {
			r, _ := utf8.DecodeRuneInString(key)
			td.d.send(tea.KeyPressMsg{Code: r, Text: key})
		}
		return nil
	}
	td.d.typeText(key)
	return nil
}

func (td *tutorialDriver) RenderANSI() string  { return td.d.sh.View().Content }
func (td *tutorialDriver) RenderPlain() string { return td.d.frame() }

var tutorialNamedKeys = map[string]rune{
	"enter":     tea.KeyEnter,
	"return":    tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"escape":    tea.KeyEscape,
	"tab":       tea.KeyTab,
	"backspace": tea.KeyBackspace,
	"delete":    tea.KeyDelete,
	"del":       tea.KeyDelete,
	"space":     tea.KeySpace,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"pgup":      tea.KeyPgUp,
	"pageup":    tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
	"pgdn":      tea.KeyPgDown,
	"pagedown":  tea.KeyPgDown,
	"insert":    tea.KeyInsert,
	"ins":       tea.KeyInsert,
}

func isTutorialSpecialOrChord(k string) bool {
	lower := strings.ToLower(k)
	if lower == "backtab" {
		return true
	}
	if _, ok := tutorialNamedKeys[lower]; ok {
		return true
	}
	for _, pref := range []string{"ctrl+", "control+", "alt+", "opt+", "option+", "shift+"} {
		if strings.HasPrefix(lower, pref) && len(k) > len(pref) {
			return true
		}
	}
	return false
}

func tutorialKeyMsg(n string) tea.KeyPressMsg {
	if strings.EqualFold(n, "backtab") {
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	var mod tea.KeyMod
	for {
		lower := strings.ToLower(n)
		switch {
		case strings.HasPrefix(lower, "ctrl+") && len(n) > 5:
			mod |= tea.ModCtrl
			n = n[5:]
		case strings.HasPrefix(lower, "control+") && len(n) > 8:
			mod |= tea.ModCtrl
			n = n[8:]
		case strings.HasPrefix(lower, "alt+") && len(n) > 4:
			mod |= tea.ModAlt
			n = n[4:]
		case strings.HasPrefix(lower, "opt+") && len(n) > 4:
			mod |= tea.ModAlt
			n = n[4:]
		case strings.HasPrefix(lower, "option+") && len(n) > 7:
			mod |= tea.ModAlt
			n = n[7:]
		case strings.HasPrefix(lower, "shift+") && len(n) > 6:
			mod |= tea.ModShift
			n = n[6:]
		default:
			goto done
		}
	}
done:
	lower := strings.ToLower(n)
	if lower == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " ", Mod: mod}
	}
	if r, ok := tutorialNamedKeys[lower]; ok {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}
	r, _ := utf8.DecodeRuneInString(n)
	if mod != 0 {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}
	return tea.KeyPressMsg{Code: r, Text: n}
}

// parseTutorialCommand extracts the target file path and --edit flag from a
// tutorial spec's launch command (e.g. "o-json docs/tutorials/fixtures/orders.json").
func parseTutorialCommand(cmd string) (file string, editable bool) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", false
	}
	args := fields
	if filepath.Base(args[0]) == "o-json" {
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--edit" || a == "-edit":
			editable = true
		case a == "--config" || a == "-config":
			i++
		case strings.HasPrefix(a, "--config=") || strings.HasPrefix(a, "-config="):
		case strings.HasPrefix(a, "-"):
		default:
			if file == "" {
				file = a
			}
		}
	}
	return file, editable
}

// runTutorialSpec executes spec headlessly through o-json's real Bubble Tea
// shell and default bindings, verifying all Expect assertions.
func runTutorialSpec(spec tutorial.Spec, baseDir string) (tutorial.Result, error) {
	workDir := baseDir
	if spec.WorkDir != "" {
		if filepath.IsAbs(spec.WorkDir) {
			workDir = spec.WorkDir
		} else if baseDir != "" {
			workDir = filepath.Join(baseDir, spec.WorkDir)
		} else {
			workDir = spec.WorkDir
		}
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	target, editable := parseTutorialCommand(spec.Command)
	var buf *Buffer
	if target == "" {
		buf = NewScratch()
	} else {
		path := target
		if !filepath.IsAbs(path) && workDir != "" {
			path = filepath.Join(workDir, path)
		}
		var err error
		buf, err = OpenBuffer(path, editable)
		if err != nil {
			return tutorial.Result{}, fmt.Errorf("open tutorial fixture %q: %w", path, err)
		}
	}

	cfg := Config{
		FS:   pathpicker.OSFS{},
		Cwd:  workDir,
		Copy: func(string) error { return nil },
	}
	app := newApp(cfg, buf)
	w, h := spec.Width, spec.Height
	if w <= 0 {
		w = tutorial.DefaultWidth
	}
	if h <= 0 {
		h = tutorial.DefaultHeight
	}
	td := &tutorialDriver{d: newDriver(app.shell(), w, h)}
	return tutorial.Run(spec, td, defaultBindings())
}

func findVHSBinary() (string, error) {
	if p, err := exec.LookPath("vhs"); err == nil {
		return p, nil
	}
	const brewVHS = "/opt/homebrew/bin/vhs"
	if info, err := os.Stat(brewVHS); err == nil && !info.IsDir() {
		return brewVHS, nil
	}
	return "", errors.New("vhs binary not found on PATH or at /opt/homebrew/bin/vhs")
}

// buildTutorial loads specPath, runs it headlessly, writes the HTML/tape/SRT/JSON
// bundle into outDir, and optionally renders the MP4 video via VHS.
func buildTutorial(ctx context.Context, specPath, outDir string, runVHS bool, stdout, stderr io.Writer) (tutorial.BundlePaths, string, error) {
	spec, err := tutorial.LoadSpec(specPath)
	if err != nil {
		return tutorial.BundlePaths{}, "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return tutorial.BundlePaths{}, "", err
	}
	res, err := runTutorialSpec(spec, cwd)
	if err != nil {
		return tutorial.BundlePaths{}, "", err
	}
	bindings := defaultBindings()

	videoName := "o-json.mp4"
	if spec.Tool != "" {
		videoName = strings.ToLower(strings.TrimSpace(spec.Tool)) + ".mp4"
	}

	opts := tutorial.BundleOptions{
		HTML: tutorial.HTMLOptions{
			IncludeTOC: true,
		},
		Tape: tutorial.TapeOptions{
			OutputMP4:   videoName,
			WidthPx:     1280,
			HeightPx:    720,
			FontSize:    15,
			TypingSpeed: "40ms",
			Padding:     14,
			LaunchMs:    1500,
		},
	}

	paths, err := tutorial.WriteBundle(outDir, res, bindings, opts)
	if err != nil {
		return tutorial.BundlePaths{}, "", err
	}

	var videoPath string
	if runVHS {
		vhsBin, err := findVHSBinary()
		if err != nil {
			return paths, "", err
		}
		absTape, err := filepath.Abs(paths.Tape)
		if err != nil {
			return paths, "", err
		}
		absOutDir, err := filepath.Abs(outDir)
		if err != nil {
			return paths, "", err
		}
		videoPath = filepath.Join(absOutDir, videoName)

		vhsDir := cwd
		if spec.WorkDir != "" {
			if filepath.IsAbs(spec.WorkDir) {
				vhsDir = spec.WorkDir
			} else {
				vhsDir = filepath.Join(cwd, spec.WorkDir)
			}
		}

		// Isolate XDG_CONFIG_HOME so user overrides do not alter deterministic VHS capture.
		cleanCfg, err := os.MkdirTemp("", "o-json-tut-cfg-")
		if err != nil {
			return paths, "", err
		}
		defer os.RemoveAll(cleanCfg)

		cmd := exec.CommandContext(ctx, vhsBin, absTape, "-o", videoPath)
		cmd.Dir = vhsDir
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+cleanCfg)
		if err := cmd.Run(); err != nil {
			return paths, "", fmt.Errorf("vhs render %s: %w", absTape, err)
		}

		// Re-render index.html embedding the generated video file.
		opts.HTML.VideoPath = videoName
		if err := os.WriteFile(paths.HTML, []byte(tutorial.RenderHTML(res, opts.HTML)), 0o644); err != nil {
			return paths, videoPath, fmt.Errorf("update %s with video link: %w", paths.HTML, err)
		}
	}

	return paths, videoPath, nil
}

func tutorialCommand(stdout, stderr io.Writer) oden.Command {
	return oden.Command{
		Name:    "tutorial",
		Summary: "build deterministic interactive tutorials (HTML, VHS tape, SRT, MP4)",
		Children: []oden.Command{
			{
				Name:    "build",
				Summary: "run a tutorial spec headlessly and emit HTML, VHS tape, SRT, and optional MP4",
				Usage:   "[--spec PATH] [--out DIR] [--vhs]",
				Run: func(ctx context.Context, args []string) error {
					fs := flag.NewFlagSet("o-json tutorial build", flag.ContinueOnError)
					fs.SetOutput(stderr)
					specPath := fs.String("spec", defaultTutorialSpec, "path to tutorial spec JSON")
					outDir := fs.String("out", defaultTutorialOut, "output directory for HTML, .tape, .srt, and video")
					runVHS := fs.Bool("vhs", false, "render MP4 video using Charm VHS")
					pos, err := parseArgs(fs, args)
					if err != nil {
						if errors.Is(err, flag.ErrHelp) {
							return nil
						}
						return &oden.ExitError{Code: 2}
					}
					if len(pos) > 0 {
						fmt.Fprintf(stderr, "o-json tutorial build: unexpected argument(s): %s\n", strings.Join(pos, " "))
						return &oden.ExitError{Code: 2}
					}
					paths, videoPath, err := buildTutorial(ctx, *specPath, *outDir, *runVHS, stdout, stderr)
					if err != nil {
						fmt.Fprintln(stderr, "o-json tutorial build:", err)
						return &oden.ExitError{Code: 1}
					}
					fmt.Fprintf(stdout, "built tutorial bundle in %s\n", paths.Dir)
					fmt.Fprintf(stdout, "  html: %s\n", paths.HTML)
					fmt.Fprintf(stdout, "  tape: %s\n", paths.Tape)
					fmt.Fprintf(stdout, "  srt:  %s\n", paths.SRT)
					fmt.Fprintf(stdout, "  data: %s\n", paths.DataJSON)
					if videoPath != "" {
						fmt.Fprintf(stdout, "  mp4:  %s\n", videoPath)
					}
					return nil
				},
			},
		},
	}
}
