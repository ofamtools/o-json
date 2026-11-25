package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

//go:embed readers/*.json
var builtinReadersFS embed.FS

// Reader is a pure declarative JSON specification for projecting
// heterogeneous structured records into a unified timeline/reader view.
type Reader struct {
	Name          string              `json:"name"`
	Description   string              `json:"description,omitempty"`
	DefaultFormat string              `json:"defaultFormat,omitempty"`
	Adapters      []ReaderAdapter     `json:"adapters"`
	Toggles       []ReaderToggle      `json:"toggles,omitempty"`
	Timeline      ReaderTimelineStyle `json:"timeline,omitempty"`
}

// ReaderAdapter declares how one provider/record shape matches and emits entries.
type ReaderAdapter struct {
	Name    string              `json:"name"`
	Match   []ReaderMatchRule   `json:"match"`
	Emit    []ReaderEmitRule    `json:"emit,omitempty"`
	Extract map[string][]string `json:"extract,omitempty"`
}

// ReaderMatchRule tests a single path condition on a JSON value.
type ReaderMatchRule struct {
	Path    string   `json:"path"`
	Exists  *bool    `json:"exists,omitempty"`
	IsArray *bool    `json:"isArray,omitempty"`
	Equals  string   `json:"equals,omitempty"`
	In      []string `json:"in,omitempty"`
	Prefix  string   `json:"prefix,omitempty"`
}

// ReaderEmitRule extracts one canonical entry (or one per element of ForEach).
type ReaderEmitRule struct {
	ForEach    string              `json:"forEach,omitempty"`
	Match      []ReaderMatchRule   `json:"match,omitempty"`
	RequireAny []string            `json:"requireAny,omitempty"`
	Extract    map[string][]string `json:"extract"`
}

// ReaderToggle declares a boolean or regex/substring filter toggle in pure data.
type ReaderToggle struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Kind       string   `json:"kind"` // "bool" or "pattern"
	Default    any      `json:"default,omitempty"`
	MatchKind  []string `json:"matchKind,omitempty"`
	MatchField string   `json:"matchField,omitempty"`
	Key        string   `json:"key,omitempty"`
}

// ReaderStyleHint describes semantic presentation tokens for a role or entry kind.
type ReaderStyleHint struct {
	Icon  string `json:"icon,omitempty"`
	Label string `json:"label,omitempty"`
	Tone  string `json:"tone,omitempty"`
}

// ReaderTimelineStyle maps roles and kinds to presentation hints.
type ReaderTimelineStyle struct {
	Roles map[string]ReaderStyleHint `json:"roles,omitempty"`
	Kinds map[string]ReaderStyleHint `json:"kinds,omitempty"`
}

// TimelineEntry is one canonical projected timeline item.
type TimelineEntry struct {
	SourceIndex int    `json:"sourceIndex"`
	Adapter     string `json:"adapter,omitempty"`
	Role        string `json:"role,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
	Model       string `json:"model,omitempty"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	ToolCallID  string `json:"toolCallId,omitempty"`
	ToolName    string `json:"toolName,omitempty"`
	ToolInput   any    `json:"toolInput,omitempty"`
	ToolOutput  string `json:"toolOutput,omitempty"`
}

// ReaderState holds live toggle values for an active Reader.
type ReaderState struct {
	Bools    map[string]bool
	Patterns map[string]string
}

// DefaultReaderState initializes a ReaderState from a reader's declarative defaults.
func DefaultReaderState(r Reader) ReaderState {
	st := ReaderState{Bools: map[string]bool{}, Patterns: map[string]string{}}
	for _, t := range r.Toggles {
		switch t.Kind {
		case "bool":
			v := true
			if b, ok := t.Default.(bool); ok {
				v = b
			}
			st.Bools[t.ID] = v
		case "pattern":
			s, _ := t.Default.(string)
			st.Patterns[t.ID] = s
		}
	}
	return st
}

// LoadReaders loads embedded built-in readers and overlays user-defined
// JSON readers from ~/.config/o-json/readers/*.json and <cfgDir>/readers/*.json.
func LoadReaders(cfgDir string) ([]Reader, error) {
	byName := map[string]Reader{}
	entries, err := fs.ReadDir(builtinReadersFS, "readers")
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := builtinReadersFS.ReadFile("readers/" + e.Name())
			if err != nil {
				return nil, err
			}
			var r Reader
			if err := json.Unmarshal([]byte(StripJSONC(string(data))), &r); err != nil {
				return nil, fmt.Errorf("embedded reader %s: %w", e.Name(), err)
			}
			if r.Name == "" {
				r.Name = strings.TrimSuffix(e.Name(), ".json")
			}
			byName[r.Name] = r
		}
	}
	for _, dir := range userReaderDirs(cfgDir) {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, f.Name()))
			if err != nil {
				continue
			}
			var r Reader
			if err := json.Unmarshal([]byte(StripJSONC(string(data))), &r); err != nil {
				return nil, fmt.Errorf("reader %s: %w", filepath.Join(dir, f.Name()), err)
			}
			if r.Name == "" {
				r.Name = strings.TrimSuffix(f.Name(), ".json")
			}
			byName[r.Name] = r
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Reader, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, nil
}

func userReaderDirs(cfgDir string) []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", "o-json", "readers"))
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, "o-json", "readers"))
	}
	if cfgDir != "" {
		dirs = append(dirs, filepath.Join(cfgDir, "readers"))
	}
	var uniq []string
	seen := map[string]bool{}
	for _, d := range dirs {
		clean := filepath.Clean(d)
		if !seen[clean] {
			seen[clean] = true
			uniq = append(uniq, clean)
		}
	}
	return uniq
}

// FindReader looks up a named reader from embedded + user directories.
func FindReader(name, cfgDir string) (Reader, bool, error) {
	list, err := LoadReaders(cfgDir)
	if err != nil {
		return Reader{}, false, err
	}
	for _, r := range list {
		if strings.EqualFold(r.Name, name) {
			return r, true, nil
		}
	}
	return Reader{}, false, nil
}

// ProjectTimeline runs reader's declarative adapters and toggles over v.
func ProjectTimeline(v any, reader Reader, state ReaderState) []TimelineEntry {
	var records []any
	switch t := v.(type) {
	case []any:
		records = t
	case Object:
		if msgs, ok := t.Get("messages"); ok {
			if arr, isArr := msgs.([]any); isArr {
				records = arr
			}
		}
		if records == nil {
			records = []any{t}
		}
	default:
		return nil
	}

	var raw []TimelineEntry
	toolNamesByCallID := map[string]string{}

	for i, rec := range records {
		for _, ad := range reader.Adapters {
			if !matchRules(rec, nil, ad.Match) {
				continue
			}
			emitted := runAdapterEmit(rec, i, ad)
			raw = append(raw, emitted...)
			break
		}
	}

	// Generic toolCallId -> toolName correlation (e.g. Claude tool_result / Codex function_call_output).
	for i := range raw {
		e := &raw[i]
		if e.Kind == "tool_call" && e.ToolCallID != "" && e.ToolName != "" {
			toolNamesByCallID[e.ToolCallID] = e.ToolName
		}
	}
	for i := range raw {
		e := &raw[i]
		if e.Kind == "tool_result" && e.ToolName == "" && e.ToolCallID != "" {
			if name, ok := toolNamesByCallID[e.ToolCallID]; ok {
				e.ToolName = name
			} else {
				e.ToolName = e.ToolCallID
			}
		}
		if e.Kind == "tool_result" && e.ToolName == "" {
			e.ToolName = "tool"
		}
	}

	return filterTimeline(raw, reader, state)
}

func runAdapterEmit(rec any, srcIdx int, ad ReaderAdapter) []TimelineEntry {
	rules := ad.Emit
	if len(rules) == 0 && len(ad.Extract) > 0 {
		rules = []ReaderEmitRule{{Extract: ad.Extract}}
	}
	var out []TimelineEntry
	for i := 0; i < len(rules); {
		r := rules[i]
		if r.ForEach == "" {
			if matchRules(rec, nil, r.Match) {
				if e, ok := extractEntry(rec, nil, srcIdx, ad.Name, r); ok {
					out = append(out, e)
				}
			}
			i++
			continue
		}
		// Group consecutive rules with the same ForEach path so block order is preserved.
		j := i + 1
		for j < len(rules) && rules[j].ForEach == r.ForEach {
			j++
		}
		group := rules[i:j]
		if p, err := ParsePath(r.ForEach); err == nil {
			if target, ok := AtPath(rec, p); ok {
				if arr, isArr := target.([]any); isArr {
					for _, elem := range arr {
						for _, gr := range group {
							if matchRules(elem, rec, gr.Match) {
								if e, ok := extractEntry(elem, rec, srcIdx, ad.Name, gr); ok {
									out = append(out, e)
								}
								break
							}
						}
					}
				}
			}
		}
		i = j
	}
	return out
}

func matchRules(cur, parent any, rules []ReaderMatchRule) bool {
	for _, r := range rules {
		val, exists := resolveReaderPath(cur, parent, r.Path)
		if r.Exists != nil && exists != *r.Exists {
			return false
		}
		if r.IsArray != nil {
			_, isArr := val.([]any)
			if isArr != *r.IsArray {
				return false
			}
		}
		if r.Equals != "" {
			if !exists || scalarString(val) != r.Equals {
				return false
			}
		}
		if len(r.In) > 0 {
			if !exists || !slices.Contains(r.In, scalarString(val)) {
				return false
			}
		}
		if r.Prefix != "" {
			if !exists || !strings.HasPrefix(scalarString(val), r.Prefix) {
				return false
			}
		}
	}
	return true
}

func resolveReaderPath(cur, parent any, expr string) (any, bool) {
	if strings.HasPrefix(expr, "=") {
		return expr[1:], true
	}
	target := cur
	pathStr := expr
	if rest, ok := strings.CutPrefix(expr, "$parent."); ok {
		if parent == nil {
			return nil, false
		}
		target = parent
		pathStr = rest
	}
	p, err := ParsePath(pathStr)
	if err != nil {
		return nil, false
	}
	return AtPath(target, p)
}

func extractEntry(cur, parent any, srcIdx int, adapter string, rule ReaderEmitRule) (TimelineEntry, bool) {
	e := TimelineEntry{SourceIndex: srcIdx, Adapter: adapter}
	getStr := func(field string) string {
		for _, expr := range rule.Extract[field] {
			val, ok := resolveReaderPath(cur, parent, expr)
			if !ok || isEmptyValue(val) {
				continue
			}
			if s := extractTextValue(val); strings.TrimSpace(s) != "" {
				return s
			}
		}
		return ""
	}
	getAny := func(field string) any {
		for _, expr := range rule.Extract[field] {
			val, ok := resolveReaderPath(cur, parent, expr)
			if !ok || isEmptyValue(val) {
				continue
			}
			return val
		}
		return nil
	}

	e.Role = strings.ToLower(strings.TrimSpace(getStr("role")))
	e.Kind = strings.ToLower(strings.TrimSpace(getStr("kind")))
	e.Timestamp = getStr("timestamp")
	e.Model = getStr("model")
	e.Text = getStr("text")
	e.Thinking = getStr("thinking")
	e.ToolCallID = getStr("toolCallId")
	e.ToolName = getStr("toolName")
	e.ToolInput = getAny("toolInput")
	e.ToolOutput = getStr("toolOutput")

	if len(rule.RequireAny) > 0 {
		has := false
		for _, req := range rule.RequireAny {
			if entryFieldNonEmpty(e, req) {
				has = true
				break
			}
		}
		if !has {
			return TimelineEntry{}, false
		}
	}
	return e, true
}

func entryFieldNonEmpty(e TimelineEntry, field string) bool {
	switch field {
	case "text":
		return strings.TrimSpace(e.Text) != ""
	case "thinking":
		return strings.TrimSpace(e.Thinking) != ""
	case "toolName":
		return strings.TrimSpace(e.ToolName) != ""
	case "toolInput":
		return !isEmptyValue(e.ToolInput)
	case "toolOutput":
		return strings.TrimSpace(e.ToolOutput) != ""
	case "role":
		return e.Role != ""
	case "kind":
		return e.Kind != ""
	}
	return false
}

func entryFieldValue(e TimelineEntry, field string) string {
	switch field {
	case "role":
		return e.Role
	case "kind":
		return e.Kind
	case "timestamp":
		return e.Timestamp
	case "model":
		return e.Model
	case "text":
		return e.Text
	case "thinking":
		return e.Thinking
	case "toolName":
		return e.ToolName
	case "toolOutput":
		return e.ToolOutput
	case "adapter":
		return e.Adapter
	}
	return ""
}

func extractTextValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case []any:
		var parts []string
		for _, elem := range t {
			switch item := elem.(type) {
			case string:
				if strings.TrimSpace(item) != "" {
					parts = append(parts, item)
				}
			case Object:
				for _, k := range []string{"text", "thinking", "content", "summary"} {
					if x, ok := item.Get(k); ok {
						if s := extractTextValue(x); strings.TrimSpace(s) != "" {
							parts = append(parts, s)
							break
						}
					}
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
		b, _ := FormatJSON(t, false)
		return string(b)
	default:
		b, _ := FormatJSON(t, false)
		return string(b)
	}
}

func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}

// filterTimeline applies all declarative toggles in reader against entries.
func filterTimeline(entries []TimelineEntry, reader Reader, state ReaderState) []TimelineEntry {
	out := make([]TimelineEntry, 0, len(entries))
	for _, e := range entries {
		keep := true
		for _, tg := range reader.Toggles {
			if len(tg.MatchKind) > 0 && !slices.Contains(tg.MatchKind, e.Kind) {
				continue
			}
			switch tg.Kind {
			case "bool":
				enabled, ok := state.Bools[tg.ID]
				if !ok {
					enabled = true
					if def, isBool := tg.Default.(bool); isBool {
						enabled = def
					}
				}
				if !enabled {
					keep = false
				}
			case "pattern":
				pat := strings.TrimSpace(state.Patterns[tg.ID])
				if pat == "" {
					continue
				}
				target := entryFieldValue(e, tg.MatchField)
				if !matchPattern(target, pat) {
					keep = false
				}
			}
			if !keep {
				break
			}
		}
		if keep {
			out = append(out, e)
		}
	}
	return out
}

func matchPattern(s, pat string) bool {
	if re, err := regexp.Compile("(?i)" + pat); err == nil {
		return re.MatchString(s)
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(pat))
}

// TimelineFromValue converts an already-canonical or raw JSON value into TimelineEntries
// (using ai-session reader as fallback when records are raw logs).
func TimelineFromValue(v any) []TimelineEntry {
	if arr, ok := v.([]any); ok && len(arr) > 0 {
		canonical := true
		var out []TimelineEntry
		for i, elem := range arr {
			o, isObj := elem.(Object)
			if !isObj {
				canonical = false
				break
			}
			_, hasRole := o.Get("role")
			_, hasKind := o.Get("kind")
			if !hasRole || !hasKind {
				canonical = false
				break
			}
			e := TimelineEntry{
				SourceIndex: i,
				Role:        extractTextValue(cell(o, "role")),
				Kind:        extractTextValue(cell(o, "kind")),
				Timestamp:   extractTextValue(cell(o, "timestamp")),
				Model:       extractTextValue(cell(o, "model")),
				Text:        extractTextValue(cell(o, "text")),
				Thinking:    extractTextValue(cell(o, "thinking")),
				ToolName:    extractTextValue(cell(o, "toolName")),
				ToolOutput:  extractTextValue(cell(o, "toolOutput")),
			}
			if ti, ok := o.Get("toolInput"); ok {
				e.ToolInput = ti
			}
			out = append(out, e)
		}
		if canonical {
			return out
		}
	}
	if r, ok, _ := FindReader("ai-session", ""); ok {
		return ProjectTimeline(v, r, DefaultReaderState(r))
	}
	return nil
}

// TimelineEntriesValue converts timeline entries back into []any of ordered Objects
// for JSON/JSONL encoding.
func TimelineEntriesValue(entries []TimelineEntry) []any {
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		o := Object{
			{"sourceIndex", json.Number(strconv.Itoa(e.SourceIndex))},
			{"role", e.Role},
			{"kind", e.Kind},
		}
		if e.Timestamp != "" {
			o = append(o, Field{"timestamp", e.Timestamp})
		}
		if e.Adapter != "" {
			o = append(o, Field{"adapter", e.Adapter})
		}
		if e.Model != "" {
			o = append(o, Field{"model", e.Model})
		}
		if e.Text != "" {
			o = append(o, Field{"text", e.Text})
		}
		if e.Thinking != "" {
			o = append(o, Field{"thinking", e.Thinking})
		}
		if e.ToolName != "" {
			o = append(o, Field{"toolName", e.ToolName})
		}
		if !isEmptyValue(e.ToolInput) {
			o = append(o, Field{"toolInput", e.ToolInput})
		}
		if e.ToolOutput != "" {
			o = append(o, Field{"toolOutput", e.ToolOutput})
		}
		out = append(out, o)
	}
	return out
}

func roleStyleHint(role string, style *ReaderTimelineStyle) ReaderStyleHint {
	if style != nil {
		if h, ok := style.Roles[role]; ok {
			return h
		}
	}
	label := strings.ToUpper(role)
	if label == "" {
		label = "EVENT"
	}
	return ReaderStyleHint{Icon: "•", Label: label, Tone: "text"}
}

func kindStyleHint(kind string, style *ReaderTimelineStyle) ReaderStyleHint {
	if style != nil {
		if h, ok := style.Kinds[kind]; ok {
			return h
		}
	}
	return ReaderStyleHint{Icon: "•", Label: kind, Tone: "text"}
}

// ExportMarkdownTimeline writes entries as a readable Markdown timeline document.
func ExportMarkdownTimeline(w io.Writer, entries []TimelineEntry, style *ReaderTimelineStyle) error {
	for i, e := range entries {
		if i > 0 {
			fmt.Fprintln(w)
		}
		rh := roleStyleHint(e.Role, style)
		kh := kindStyleHint(e.Kind, style)
		header := fmt.Sprintf("### %s %s", rh.Icon, rh.Label)
		if e.Kind != "" && e.Kind != "message" {
			header += " · " + kh.Label
		}
		if e.ToolName != "" {
			header += " `" + e.ToolName + "`"
		}
		if e.Timestamp != "" {
			header += " (" + e.Timestamp + ")"
		}
		fmt.Fprintln(w, header)
		fmt.Fprintln(w)
		switch e.Kind {
		case "thinking":
			fmt.Fprintln(w, "<details>")
			fmt.Fprintln(w, "<summary>Thinking</summary>")
			fmt.Fprintln(w)
			fmt.Fprintln(w, e.Thinking)
			fmt.Fprintln(w)
			fmt.Fprintln(w, "</details>")
		case "tool_call":
			if !isEmptyValue(e.ToolInput) {
				fmt.Fprintln(w, "```json")
				if s, ok := e.ToolInput.(string); ok {
					fmt.Fprintln(w, s)
				} else if b, err := FormatJSON(e.ToolInput, true); err == nil {
					fmt.Fprintln(w, string(b))
				}
				fmt.Fprintln(w, "```")
			}
			if e.ToolOutput != "" {
				fmt.Fprintln(w, "\n**Output:**")
				fmt.Fprintln(w, "```")
				fmt.Fprintln(w, e.ToolOutput)
				fmt.Fprintln(w, "```")
			}
		case "tool_result":
			fmt.Fprintln(w, "```")
			fmt.Fprintln(w, e.ToolOutput)
			fmt.Fprintln(w, "```")
		default:
			if e.Text != "" {
				fmt.Fprintln(w, e.Text)
			}
		}
	}
	return nil
}

// ExportTextTimeline writes a compact plain-text timeline for terminal piping.
func ExportTextTimeline(w io.Writer, entries []TimelineEntry, style *ReaderTimelineStyle) error {
	for _, e := range entries {
		rh := roleStyleHint(e.Role, style)
		kh := kindStyleHint(e.Kind, style)
		head := fmt.Sprintf("[%d] %s %s", e.SourceIndex, rh.Icon, rh.Label)
		if e.Kind != "" && e.Kind != "message" {
			head += " [" + kh.Label + "]"
		}
		if e.ToolName != "" {
			head += " " + e.ToolName
		}
		if e.Timestamp != "" {
			head += " " + e.Timestamp
		}
		body := timelineSummaryText(e)
		fmt.Fprintf(w, "%s: %s\n", head, singleLine(body))
	}
	return nil
}

func timelineSummaryText(e TimelineEntry) string {
	switch e.Kind {
	case "thinking":
		return e.Thinking
	case "tool_call":
		if !isEmptyValue(e.ToolInput) {
			return valueText(e.ToolInput)
		}
		return e.ToolOutput
	case "tool_result":
		return e.ToolOutput
	default:
		if e.Text != "" {
			return e.Text
		}
		if e.Thinking != "" {
			return e.Thinking
		}
		return e.ToolOutput
	}
}

// readerCmd implements `o-json reader list [--format text|json] [--config DIR]`.
func readerCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := "usage: " + subUsage["reader"]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	if args[0] != "list" {
		fmt.Fprintf(stderr, "o-json reader: unknown subcommand %q (use list)\n%s\n", args[0], usage)
		return 2
	}
	fs := flag.NewFlagSet("o-json reader list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "output format: text or json")
	cfg := fs.String("config", configDir(), "directory holding readers/*.json")
	if _, err := parseArgs(fs, args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	list, err := LoadReaders(*cfg)
	if err != nil {
		fmt.Fprintf(stderr, "o-json reader list: %v\n", err)
		return 1
	}
	switch *format {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(list); err != nil {
			fmt.Fprintf(stderr, "o-json reader list: %v\n", err)
			return 1
		}
	case "text":
		for _, r := range list {
			fmt.Fprintf(stdout, "%-16s  %-6s  %s\n", r.Name, r.DefaultFormat, r.Description)
		}
	default:
		fmt.Fprintf(stderr, "o-json reader list: --format %q: use text or json\n", *format)
		return 2
	}
	return 0
}

// timelineCmd implements `o-json timeline FILE|- [--reader ai-session] [--from FMT] [--hide-thinking] [--hide-tools] [--tools PATTERN] [--export text|md|jsonl|json] [--config DIR]`.
func timelineCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := "usage: " + subUsage["timeline"]
	fs := flag.NewFlagSet("o-json timeline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usage); fs.PrintDefaults() }
	readerName := fs.String("reader", "ai-session", "declarative reader name")
	from := fs.String("from", "", "input format (default: reader's defaultFormat or auto)")
	hideThinking := fs.Bool("hide-thinking", false, "hide thinking entries")
	hideTools := fs.Bool("hide-tools", false, "hide tool call/result entries")
	toolsPat := fs.String("tools", "", "regex/substring filter for toolName")
	exportFmt := fs.String("export", "text", "export format: text, md, jsonl or json")
	cfg := fs.String("config", configDir(), "directory holding readers/*.json")
	pos, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "o-json timeline: expected 1 argument, got %d\n%s\n", len(pos), usage)
		return 2
	}
	fail := func(code int, err error) int {
		fmt.Fprintf(stderr, "o-json timeline: %v\n", err)
		return code
	}
	reader, ok, err := FindReader(*readerName, *cfg)
	if err != nil {
		return fail(1, err)
	}
	if !ok {
		return fail(2, fmt.Errorf("unknown reader %q (run `o-json reader list`)", *readerName))
	}
	raw, err := readSource(pos[0], stdin)
	if err != nil {
		return fail(1, err)
	}
	inFmt := *from
	if inFmt == "" && pos[0] != "-" {
		inFmt = InferInputFormat(pos[0])
	}
	if inFmt == "" {
		inFmt = reader.DefaultFormat
	}
	v, _, err := ParseFormat(raw, inFmt)
	if err != nil {
		return fail(1, fmt.Errorf("%s: %w", pos[0], err))
	}
	st := DefaultReaderState(reader)
	if *hideThinking {
		st.Bools["thinking"] = false
	}
	if *hideTools {
		st.Bools["tools"] = false
	}
	if *toolsPat != "" {
		st.Patterns["toolPattern"] = *toolsPat
	}
	entries := ProjectTimeline(v, reader, st)
	var buf bytes.Buffer
	switch NormalizeFormat(*exportFmt) {
	case "text":
		err = ExportTextTimeline(&buf, entries, &reader.Timeline)
	case "md":
		err = ExportMarkdownTimeline(&buf, entries, &reader.Timeline)
	case "jsonl":
		var data []byte
		data, err = encode(TimelineEntriesValue(entries), "jsonl")
		buf.Write(data)
	case "json":
		var data []byte
		data, err = encode(TimelineEntriesValue(entries), "pretty")
		buf.Write(data)
	default:
		return fail(2, fmt.Errorf("--export %q: use text, md, jsonl or json", *exportFmt))
	}
	if err != nil {
		return fail(1, err)
	}
	if _, err := stdout.Write(buf.Bytes()); err != nil {
		return fail(1, err)
	}
	return 0
}
