package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/picker"
)

// readerEntries returns the declarative timeline projection for buffer b
// under the currently active Reader and toggle state.
func (a *App) readerEntries(b *Buffer) []TimelineEntry {
	if a.reader == nil {
		return nil
	}
	v := a.display(b)
	return ProjectTimeline(v, *a.reader, a.readerState)
}

// readerNode renders view 5 (the declarative Reader / Timeline view) with
// semantic tones and icons driven purely by Reader.Timeline.
func (a *App) readerNode(w *window, b *Buffer, id string, width, h int, focused bool) (ui.ViewNode, string) {
	if a.reader == nil {
		msg := "No reader active · press g r to select a reader (e.g. ai-session) or launch with --reader ai-session"
		n := textLine(clipText(msg, width), "muted")
		n.ID = id
		return n, fmt.Sprintf("5 Reader · %s · none", bufName(b))
	}
	entries := a.readerEntries(b)
	title := fmt.Sprintf("5 %s · %s · %d items", a.reader.Name, bufName(b), len(entries))
	// Append declarative toggle status badges to the panel title.
	var badges []string
	for _, tg := range a.reader.Toggles {
		switch tg.Kind {
		case "bool":
			on := a.readerState.Bools[tg.ID]
			state := "on"
			if !on {
				state = "off"
			}
			badges = append(badges, fmt.Sprintf("%s:%s", tg.Label, state))
		case "pattern":
			if pat := strings.TrimSpace(a.readerState.Patterns[tg.ID]); pat != "" {
				badges = append(badges, fmt.Sprintf("%s:/%s/", tg.Label, pat))
			}
		}
	}
	if len(badges) > 0 {
		title += " · " + strings.Join(badges, " ")
	}
	if len(entries) == 0 {
		msg := fmt.Sprintf("No matching entries for reader %q (toggle filters with T / O / P or switch to 1-4 for raw JSON)", a.reader.Name)
		n := textLine(clipText(msg, width), "muted")
		n.ID = id
		return n, title
	}
	cur := min(max(w.readerIdx, 0), len(entries)-1)
	w.readerIdx = cur
	w.readerOff = min(max(w.readerOff, cur-h+1), cur)
	off := min(max(w.readerOff, 0), max(len(entries)-h, 0))
	w.readerOff = off

	bg := "selected.inactive"
	if focused {
		bg = "selected"
	}

	var sb strings.Builder
	var spans []ui.ToneSpan
	pos := 0
	for i := off; i < min(len(entries), off+h); i++ {
		e := entries[i]
		rh := roleStyleHint(e.Role, &a.reader.Timeline)
		kh := kindStyleHint(e.Kind, &a.reader.Timeline)
		prefix := fmt.Sprintf("%s %-9s", rh.Icon, rh.Label)
		tag := ""
		if e.Kind != "" && e.Kind != "message" {
			tag = " [" + kh.Label + "]"
		}
		if e.ToolName != "" {
			tag += " " + e.ToolName
		}
		body := singleLine(timelineSummaryText(e))
		rawLine := prefix + tag + "  " + body
		line := clipText(rawLine, width)
		n := utf8.RuneCountInString(line)
		if i == cur {
			spans = append(spans, ui.ToneSpan{From: pos, To: pos + max(n, 1), Tone: "text", Bg: bg})
		} else {
			prefLen := min(utf8.RuneCountInString(prefix), n)
			if prefLen > 0 && rh.Tone != "" {
				spans = append(spans, ui.ToneSpan{From: pos, To: pos + prefLen, Tone: rh.Tone})
			}
			tagLen := utf8.RuneCountInString(tag)
			if tagLen > 0 && prefLen < n && kh.Tone != "" {
				end := min(prefLen+tagLen, n)
				spans = append(spans, ui.ToneSpan{From: pos + prefLen, To: pos + end, Tone: kh.Tone})
			}
		}
		sb.WriteString(line + "\n")
		pos += n + 1
	}
	n := textLine(strings.TrimSuffix(sb.String(), "\n"), "text")
	n.ID, n.Props["spans"] = id, spans
	return n, title
}

// readerMotion navigates the Reader/Timeline view and keeps u.focus synced to the
// underlying raw JSON record so pressing 1-4 inspects that exact record.
func (a *App) readerMotion(s core.State, c core.Command) (core.State, []core.Effect) {
	w := a.active()
	b := a.buf(w.buf)
	entries := a.readerEntries(b)
	if len(entries) == 0 {
		return s, nil
	}
	w.readerIdx = moveIdx(w.readerIdx, len(entries), a.geometry().rows(), c)
	a.syncReaderFocus(w, b, entries)
	return s, nil
}

func (a *App) syncReaderFocus(w *window, b *Buffer, entries []TimelineEntry) {
	if len(entries) == 0 {
		return
	}
	idx := min(max(w.readerIdx, 0), len(entries)-1)
	srcIdx := entries[idx].SourceIndex
	if _, isArr := a.display(b).([]any); isArr && srcIdx >= 0 {
		a.bufUI(b).focus = Path{pathIdx(srcIdx)}
	}
}

// activateReader loads and enables a declarative reader by name.
func (a *App) activateReader(name string) bool {
	if name == "" || name == "off" || name == "none" {
		a.reader = nil
		a.readerState = ReaderState{}
		a.info("reader turned off")
		return true
	}
	r, ok, err := FindReader(name, a.cfgDir)
	if err != nil {
		a.fail("reader: " + err.Error())
		return false
	}
	if !ok {
		a.warn("unknown reader " + name)
		return false
	}
	a.reader = &r
	a.readerState = DefaultReaderState(r)
	w := a.active()
	w.view = viewReader
	w.readerIdx, w.readerOff = 0, 0
	a.syncReaderFocus(w, a.buf(w.buf), a.readerEntries(a.buf(w.buf)))
	a.note(fmt.Sprintf("reader %s active · 5 timeline · 1-4 raw JSON · T thinking · O tools · P tool filter", r.Name))
	return true
}

func (a *App) readerToggleBool(toggleID string) hfn {
	return func(s core.State, c core.Command) (core.State, []core.Effect) {
		if a.reader == nil {
			a.warn("no reader active · press g r to select a reader (e.g. ai-session)")
			return s, nil
		}
		cur := a.readerState.Bools[toggleID]
		a.readerState.Bools[toggleID] = !cur
		w := a.active()
		w.readerIdx, w.readerOff = 0, 0
		a.syncReaderFocus(w, a.buf(w.buf), a.readerEntries(a.buf(w.buf)))
		state := "shown"
		if !a.readerState.Bools[toggleID] {
			state = "hidden"
		}
		a.info(fmt.Sprintf("%s %s %s", a.reader.Name, toggleID, state))
		return s, nil
	}
}

func (a *App) readerFilterOpen(s core.State, c core.Command) (core.State, []core.Effect) {
	if a.reader == nil {
		a.warn("no reader active · press g r to select a reader (e.g. ai-session)")
		return s, nil
	}
	cur := a.readerState.Patterns["toolPattern"]
	a.openPrompt("reader-tool-filter", "tool filter regex/substring (empty shows all)", cur, nil, "toolPattern")
	return s, nil
}

func (a *App) readerSelect(s core.State, c core.Command) (core.State, []core.Effect) {
	list, err := LoadReaders(a.cfgDir)
	if err != nil {
		a.fail("readers: " + err.Error())
		return s, nil
	}
	items := make([]picker.Item, 0, len(list)+1)
	for _, r := range list {
		items = append(items, picker.Item{ID: r.Name, Title: r.Name, Subtitle: r.Description})
	}
	items = append(items, picker.Item{ID: "off", Title: "off", Subtitle: "disable active reader and return to raw JSON views"})
	m := picker.New("Readers", items)
	m.Scope = "pick"
	return openModal(s, modalPick, "pick", &pickModal{kind: "reader", m: m}), nil
}

// recipeSavePrompt opens a prompt to save the current buffer's pipeline as a JSON recipe.
func (a *App) recipeSavePrompt(s core.State, c core.Command) (core.State, []core.Effect) {
	def := "recipe.json"
	if b := a.cur(); b.Path != "" {
		stem := b.Name
		if i := strings.LastIndexByte(stem, '.'); i > 0 {
			stem = stem[:i]
		}
		def = stem + ".recipe.json"
	}
	a.openPrompt("recipe-save", "save pipeline recipe to path", def, nil, "")
	return s, nil
}

func (a *App) recipeSave(s core.State, c core.Command) (core.State, []core.Effect) {
	path := strings.TrimSpace(c.Arg("path"))
	if path == "" {
		return a.recipeSavePrompt(s, c)
	}
	b := a.cur()
	ops := a.visualPipeline(b)
	r := Recipe{
		Name:       strings.TrimSuffix(b.Name, ".json"),
		From:       b.SourceFormat,
		Operations: ops,
	}
	if err := SaveRecipe(path, r); err != nil {
		a.fail("recipe save: " + err.Error())
		return s, nil
	}
	a.note(fmt.Sprintf("saved recipe (%d steps) to %s", len(ops), path))
	return s, nil
}

func (a *App) recipeLoad(s core.State, c core.Command) (core.State, []core.Effect) {
	path := strings.TrimSpace(c.Arg("path"))
	if path == "" {
		a.warn("usage: :recipe load PATH")
		return s, nil
	}
	r, err := LoadRecipe(path)
	if err != nil {
		a.fail("recipe load: " + err.Error())
		return s, nil
	}
	b := a.cur()
	if err := b.ApplySteps(r.Operations...); err != nil {
		a.fail("recipe apply: " + err.Error())
		return s, nil
	}
	a.touch(b)
	a.follow = true
	a.note(fmt.Sprintf("applied recipe %s (%d steps) · u undoes", path, len(r.Operations)))
	return s, nil
}
