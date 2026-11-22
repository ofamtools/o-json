package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"ofam.dev/ochazuke/core"
)

// JQResultCommand is the command a JQEffect's result dispatches, with Args
// {"slot", "value": JQResult, "error"}.
const JQResultCommand = "jq.result"

// JQResult is one finished jq run. Results holds every value jq printed, in
// order (empty when it printed nothing). Value is the single result, or all
// of them as an array when there were several, or nil when there were none.
type JQResult struct {
	Slot       string
	Expression string
	Value      any
	Results    []any
	Err        error
}

// JQEffect runs expression over raw off the UI loop. A newer effect in the
// same slot cancels this one, and the shell drops a cancelled result.
func JQEffect(slot, raw, expression string) core.AsyncEffect {
	return core.AsyncEffect{Slot: slot, Command: JQResultCommand, Run: func(ctx context.Context) (any, error) {
		res, err := RunJQResults(ctx, raw, expression)
		r := JQResult{Slot: slot, Expression: expression, Value: jqValue(res), Results: res, Err: err}
		return r, err
	}}
}

// RunJQ runs expression over raw and returns the single result, an array of
// several, or nil when jq printed nothing (RunJQResults tells nothing from
// null).
func RunJQ(ctx context.Context, raw, expression string) (any, error) {
	res, err := RunJQResults(ctx, raw, expression)
	if err != nil {
		return nil, err
	}
	return jqValue(res), nil
}

func jqValue(res []any) any {
	switch len(res) {
	case 0:
		return nil
	case 1:
		return res[0]
	}
	return res
}

// RunJQResults runs the jq on PATH with raw on stdin, no shell, and returns
// each value it printed. jq's stderr never reaches the terminal: it becomes
// the error when jq fails and is dropped when it succeeds. A cancelled
// context returns ctx.Err().
func RunJQResults(ctx context.Context, raw, expression string) ([]any, error) {
	bin, err := exec.LookPath("jq")
	if err != nil {
		return nil, errors.New("jq not found on PATH: install jq to run queries")
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "-c", "--", expression)
	cmd.Stdin = strings.NewReader(raw)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, jqError(stderr.String(), err)
	}
	var out []any
	for line := range strings.Lines(stdout.String()) {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		v, err := ParseJSON(line)
		if err != nil {
			return out, fmt.Errorf("jq printed something that is not JSON: %w", err)
		}
		out = append(out, v)
	}
	return out, nil
}

// jqError keeps jq's own message lines and drops its caret drawings and
// error counts.
func jqError(stderr string, err error) error {
	var msgs []string
	for line := range strings.Lines(stderr) {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "jq: ")
		if ok && !strings.HasSuffix(rest, "compile error") && !strings.HasSuffix(rest, "compile errors") {
			msgs = append(msgs, strings.TrimPrefix(rest, "error: "))
		}
	}
	if len(msgs) == 0 {
		if s := strings.TrimSpace(stderr); s != "" {
			msgs = append(msgs, strings.SplitN(s, "\n", 2)[0])
		} else {
			msgs = append(msgs, err.Error())
		}
	}
	return errors.New("jq: " + strings.Join(msgs, "; "))
}
