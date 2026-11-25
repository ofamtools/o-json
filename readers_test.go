package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke/config"
)

const multiHarnessJSONL = `{"type":"user","timestamp":"2026-11-24T10:00:00Z","message":{"role":"user","content":"Fix the bug in app.go"}}
{"type":"assistant","timestamp":"2026-11-24T10:00:01Z","message":{"role":"assistant","model":"claude-4","content":[{"type":"thinking","thinking":"Inspecting app.go first"},{"type":"text","text":"Let me inspect app.go."},{"type":"tool_use","id":"tu_1","name":"read_file","input":{"path":"app.go"}}]}}
{"type":"user","timestamp":"2026-11-24T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"package main"}]}}
{"type":"message","timestamp":"2026-11-24T10:00:03Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"OMP planning edit"},{"type":"toolCall","id":"omp_1","name":"edit_file","arguments":{"path":"app.go"}}]}}
{"type":"message","timestamp":"2026-11-24T10:00:04Z","message":{"role":"toolResult","toolName":"edit_file","content":[{"type":"text","text":"patched app.go"}]}}
{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-11-24T10:00:05Z","model":"gemini-3","thinking":"Antigravity verifying build","content":"Running tests now.","tool_calls":[{"name":"run_command","args":{"CommandLine":"go test ./..."}}]}
{"step_index":2,"type":"GENERIC","created_at":"2026-11-24T10:00:06Z","source":"run_command","content":"PASS"}
{"type":"response_item","timestamp":"2026-11-24T10:00:07Z","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"Codex checking final diff"}]}}
{"type":"response_item","timestamp":"2026-11-24T10:00:08Z","payload":{"type":"function_call","call_id":"cx_1","name":"write_file","arguments":"{\"path\":\"notes.md\"}"}}
{"type":"response_item","timestamp":"2026-11-24T10:00:09Z","payload":{"type":"function_call_output","call_id":"cx_1","output":"ok"}}
{"type":"message","timestamp":"2026-11-24T10:00:10Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Pi double check"},{"type":"toolCall","id":"pi_1","name":"bash","arguments":{"cmd":"git status"}}]}}
{"type":"tool","id":"prt_1","sessionID":"ses_1","tool":"edit_file","state":{"input":{"path":"main.go"},"output":"done","status":"completed"}}
{"type":"reasoning","id":"prt_2","sessionID":"ses_1","text":"OpenCode reasoning note"}
{"type":"text","id":"prt_3","sessionID":"ses_1","text":"All done across harnesses."}
`

func TestAISessionReaderMultiHarnessAndToggles(t *testing.T) {
	reader, ok, err := FindReader("ai-session", "")
	if err != nil || !ok {
		t.Fatalf("FindReader(ai-session) ok=%v err=%v", ok, err)
	}
	v, isJSONL, err := ParseFormat(multiHarnessJSONL, "jsonl")
	if err != nil || !isJSONL {
		t.Fatalf("parse jsonl: err=%v jsonl=%v", err, isJSONL)
	}

	// 1. Default state: shows messages, thinking, and tools across all 6 harnesses.
	all := ProjectTimeline(v, reader, DefaultReaderState(reader))
	if len(all) < 15 {
		t.Fatalf("expected >=15 timeline entries across 6 harnesses, got %d: %+v", len(all), all)
	}
	wantAdapters := []string{
		"claude-omp-pi-string-message",
		"claude-omp-pi-blocks",
		"omp-pi-tool-result",
		"antigravity-planner",
		"antigravity-tool-result",
		"codex-reasoning",
		"codex-tool-call",
		"codex-tool-output",
		"opencode-tool-part",
		"opencode-reasoning-part",
		"opencode-text-part",
	}
	seenAdapters := map[string]bool{}
	for _, e := range all {
		seenAdapters[e.Adapter] = true
	}
	for _, want := range wantAdapters {
		if !seenAdapters[want] {
			t.Errorf("missing expected adapter %q in timeline (seen: %v)", want, seenAdapters)
		}
	}

	// Verify toolCallId -> toolName correlation on Claude tool_result and Codex function_call_output.
	foundClaudeResult, foundCodexResult := false, false
	for _, e := range all {
		if e.Kind == "tool_result" && e.ToolCallID == "tu_1" && e.ToolName == "read_file" {
			foundClaudeResult = true
		}
		if e.Kind == "tool_result" && e.ToolCallID == "cx_1" && e.ToolName == "write_file" {
			foundCodexResult = true
		}
	}
	if !foundClaudeResult || !foundCodexResult {
		t.Fatalf("toolCallId correlation failed: claude=%v codex=%v", foundClaudeResult, foundCodexResult)
	}

	// 2. Hide thinking toggle.
	noThinkState := DefaultReaderState(reader)
	noThinkState.Bools["thinking"] = false
	noThink := ProjectTimeline(v, reader, noThinkState)
	for _, e := range noThink {
		if e.Kind == "thinking" {
			t.Fatalf("thinking entry leaked when thinking=false: %+v", e)
		}
	}

	// 3. Hide tools toggle.
	noToolsState := DefaultReaderState(reader)
	noToolsState.Bools["tools"] = false
	noTools := ProjectTimeline(v, reader, noToolsState)
	for _, e := range noTools {
		if e.Kind == "tool_call" || e.Kind == "tool_result" {
			t.Fatalf("tool entry leaked when tools=false: %+v", e)
		}
	}

	// 4. Tool pattern filter (edit|write|run_command) keeps matching tools + non-tool messages, drops read_file and bash!
	patState := DefaultReaderState(reader)
	patState.Patterns["toolPattern"] = "edit|write|run_command"
	filtered := ProjectTimeline(v, reader, patState)
	seenTools := map[string]bool{}
	seenMessages := 0
	for _, e := range filtered {
		if e.Kind == "tool_call" || e.Kind == "tool_result" {
			seenTools[e.ToolName] = true
		}
		if e.Kind == "message" {
			seenMessages++
		}
	}
	if seenMessages == 0 {
		t.Fatal("tool pattern filter unexpectedly dropped non-tool messages")
	}
	for _, kept := range []string{"edit_file", "write_file", "run_command"} {
		if !seenTools[kept] {
			t.Errorf("expected tool %q to pass filter, seen=%v", kept, seenTools)
		}
	}
	for _, dropped := range []string{"read_file", "bash"} {
		if seenTools[dropped] {
			t.Errorf("expected tool %q to be filtered out, seen=%v", dropped, seenTools)
		}
	}
}

func TestReaderCLIAndUserExtensibleConfig(t *testing.T) {
	// Custom user reader in --config DIR/readers/*.json.
	cfgDir := t.TempDir()
	readersDir := filepath.Join(cfgDir, "readers")
	if err := os.MkdirAll(readersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customJSON := `{
		"name": "audit-log",
		"description": "Custom audit log timeline",
		"defaultFormat": "jsonl",
		"adapters": [{
			"name": "audit",
			"match": [{"path": "actor", "exists": true}],
			"extract": {
				"role": ["actor"],
				"kind": ["=message"],
				"timestamp": ["ts"],
				"text": ["event"]
			}
		}]
	}`
	if err := os.WriteFile(filepath.Join(readersDir, "audit-log.json"), []byte(customJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := run([]string{"reader", "list", "--config", cfgDir}, nil, &out, &errb)
	if code != 0 || !strings.Contains(out.String(), "ai-session") || !strings.Contains(out.String(), "audit-log") {
		t.Fatalf("reader list code=%d out=%q err=%q", code, out.String(), errb.String())
	}

	// Timeline CLI with --hide-thinking, --tools filter, and --export md.
	out.Reset()
	errb.Reset()
	code = run([]string{
		"timeline", "-",
		"--reader", "ai-session",
		"--hide-thinking",
		"--tools", "edit_file|run_command",
		"--export", "md",
	}, strings.NewReader(multiHarnessJSONL), &out, &errb)
	if code != 0 || errb.Len() > 0 {
		t.Fatalf("timeline --export md code=%d err=%q", code, errb.String())
	}
	md := out.String()
	if strings.Contains(md, "<details>") || strings.Contains(md, "read_file") || strings.Contains(md, "`bash`") {
		t.Fatalf("timeline md leaked hidden thinking or non-matching tools:\n%s", md)
	}
	if !strings.Contains(md, "`edit_file`") || !strings.Contains(md, "`run_command`") || !strings.Contains(md, "Fix the bug in app.go") {
		t.Fatalf("timeline md missing expected entries:\n%s", md)
	}

	// Timeline CLI --export jsonl.
	out.Reset()
	code = run([]string{"timeline", "-", "--reader", "audit-log", "--config", cfgDir, "--export", "jsonl"},
		strings.NewReader("{\"actor\":\"alice\",\"ts\":\"2026-11-24\",\"event\":\"deployed\"}\n"), &out, &errb)
	if code != 0 || !strings.Contains(out.String(), `"text":"deployed"`) {
		t.Fatalf("custom reader timeline code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}

func TestReaderTUIInteractiveAndRawSwitching(t *testing.T) {
	// Ensure zero binding check errors with all new reader bindings.
	for _, d := range config.CheckBindings(modes, defaultBindings(), commandSpecs()...) {
		if d.Severity == config.SevError {
			t.Errorf("binding check error #%d: %s", d.Index, d.Message)
		}
	}

	dir := t.TempDir()
	b, err := NewBufferWithFormat("session.jsonl", multiHarnessJSONL, "jsonl", false)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(Config{Cwd: dir, Reader: "ai-session"}, b)
	d := newDriver(a.shell(), 140, 36)

	if a.active().view != viewReader || a.reader == nil || a.reader.Name != "ai-session" {
		t.Fatalf("expected active ai-session reader view, got view=%d reader=%v", a.active().view, a.reader)
	}
	initialCount := len(a.readerEntries(b))
	frame := d.frame()
	if !strings.Contains(frame, "ai-session") || !strings.Contains(frame, "Thinking:on") || !strings.Contains(frame, "Tools:on") {
		t.Fatalf("timeline frame missing reader header/badges:\n%s", frame)
	}

	// Move down inside Timeline view and verify focus syncs to underlying raw record.
	d.keys("tab", "j", "j")
	if got := a.curUI().focus.String(); !strings.HasPrefix(got, "$[") {
		t.Fatalf("timeline motion did not sync raw focus, got %q", got)
	}

	// Toggle thinking off with T.
	d.keys("T")
	if a.readerState.Bools["thinking"] || len(a.readerEntries(b)) >= initialCount {
		t.Fatalf("T did not hide thinking: bool=%v count=%d/%d", a.readerState.Bools["thinking"], len(a.readerEntries(b)), initialCount)
	}
	// Toggle tools off with O.
	noThinkCount := len(a.readerEntries(b))
	d.keys("O")
	if a.readerState.Bools["tools"] || len(a.readerEntries(b)) >= noThinkCount {
		t.Fatalf("O did not hide tools: bool=%v count=%d/%d", a.readerState.Bools["tools"], len(a.readerEntries(b)), noThinkCount)
	}
	// Re-enable tools and apply tool pattern filter with P.
	d.keys("O", "P")
	d.typeText("edit_file")
	d.keys("enter")
	if a.readerState.Patterns["toolPattern"] != "edit_file" {
		t.Fatalf("P did not set toolPattern, got %q", a.readerState.Patterns["toolPattern"])
	}

	// Switch seamlessly between Timeline (5) and Raw JSON views (1-4) and back to 5.
	d.keys("3")
	if a.active().view != viewPretty {
		t.Fatalf("3 did not switch to Pretty view, got %d", a.active().view)
	}
	d.keys("4")
	if a.active().view != viewRaw {
		t.Fatalf("4 did not switch to Raw view, got %d", a.active().view)
	}
	d.keys("alt+5")
	if a.active().view != viewReader {
		t.Fatalf("alt+5 from Raw view did not switch back to Timeline view, got %d", a.active().view)
	}

	// Export Markdown timeline from viewReader.
	mdPath := filepath.Join(dir, "session.md")
	d.keys(":")
	d.typeText("w " + mdPath)
	d.keys("enter")
	mdBytes, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("reading exported markdown timeline: %v (status: %q)", err, a.status)
	}
	if !strings.Contains(string(mdBytes), "### ") || !strings.Contains(string(mdBytes), "`edit_file`") {
		t.Fatalf("exported markdown timeline unexpected:\n%s", string(mdBytes))
	}
}
