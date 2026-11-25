package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// --- YAML codec (key-order preserving, zero external deps) -------------------

type yamlLine struct {
	num    int
	indent int
	text   string // trimmed of leading indent and trailing comment/spaces
	raw    string // original line without \r\n
}

// ParseYAML decodes a YAML document into o-json's native value model
// (Object, []any, string, json.Number, bool, nil), preserving mapping key order.
func ParseYAML(raw string) (any, error) {
	lines := tokenizeYAMLLines(raw)
	if len(lines) == 0 {
		return nil, ErrEmpty
	}
	idx := 0
	for idx < len(lines) && (lines[idx].text == "---" || lines[idx].text == "...") {
		idx++
	}
	if idx >= len(lines) {
		return nil, ErrEmpty
	}
	v, _, err := parseYAMLNode(lines, idx, lines[idx].indent)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func tokenizeYAMLLines(raw string) []yamlLine {
	rawLines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var out []yamlLine
	for i, rl := range rawLines {
		indent := 0
		for indent < len(rl) && rl[indent] == ' ' {
			indent++
		}
		trimmed := strings.TrimSpace(stripYAMLComment(rl[indent:]))
		if trimmed == "" {
			continue
		}
		out = append(out, yamlLine{num: i + 1, indent: indent, text: trimmed, raw: rl})
	}
	return out
}

func stripYAMLComment(s string) string {
	inSingle, inDouble, esc := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if esc {
			esc = false
			continue
		}
		switch {
		case inDouble:
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inDouble = false
			}
		case inSingle:
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					inSingle = false
				}
			}
		default:
			if c == '"' {
				inDouble = true
			} else if c == '\'' {
				inSingle = true
			} else if c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
				return s[:i]
			}
		}
	}
	return s
}

func parseYAMLNode(lines []yamlLine, idx, baseIndent int) (any, int, error) {
	if idx >= len(lines) {
		return nil, idx, nil
	}
	line := lines[idx]
	if line.text == "..." || line.text == "---" {
		return nil, idx, nil
	}
	if strings.HasPrefix(line.text, "[") || strings.HasPrefix(line.text, "{") {
		v, err := parseYAMLFlow(line.text)
		if err != nil {
			return nil, idx, fmt.Errorf("yaml line %d: %w", line.num, err)
		}
		return v, idx + 1, nil
	}
	if isYAMLSeqItem(line.text) {
		return parseYAMLSeq(lines, idx, line.indent)
	}
	if _, _, ok := splitYAMLKeyVal(line.text); ok {
		return parseYAMLMap(lines, idx, line.indent)
	}
	v, err := parseYAMLScalar(line.text)
	if err != nil {
		return nil, idx, fmt.Errorf("yaml line %d: %w", line.num, err)
	}
	return v, idx + 1, nil
}

func isYAMLSeqItem(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

func parseYAMLSeq(lines []yamlLine, idx, baseIndent int) ([]any, int, error) {
	out := []any{}
	for idx < len(lines) {
		line := lines[idx]
		if line.text == "..." || line.text == "---" || line.indent < baseIndent {
			break
		}
		if line.indent > baseIndent || !isYAMLSeqItem(line.text) {
			break
		}
		rest := ""
		if len(line.text) > 2 {
			rest = strings.TrimSpace(line.text[2:])
		}
		if rest == "" {
			idx++
			if idx < len(lines) && lines[idx].indent > baseIndent {
				val, next, err := parseYAMLNode(lines, idx, lines[idx].indent)
				if err != nil {
					return nil, idx, err
				}
				out = append(out, val)
				idx = next
			} else {
				out = append(out, nil)
			}
			continue
		}
		if isBlockScalarHeader(rest) {
			val, next := parseYAMLBlockScalar(lines, idx, baseIndent, rest)
			out = append(out, val)
			idx = next
			continue
		}
		if k, vstr, ok := splitYAMLKeyVal(rest); ok {
			// Inline mapping item starting on the "- " line.
			itemIndent := line.indent + 2
			synth := yamlLine{num: line.num, indent: itemIndent, text: rest, raw: strings.Repeat(" ", itemIndent) + rest}
			sub := make([]yamlLine, 0, len(lines)-idx)
			sub = append(sub, synth)
			end := idx + 1
			for end < len(lines) && lines[end].indent >= itemIndent && lines[end].text != "---" && lines[end].text != "..." {
				sub = append(sub, lines[end])
				end++
			}
			_ = k
			_ = vstr
			obj, consumed, err := parseYAMLMap(sub, 0, itemIndent)
			if err != nil {
				return nil, idx, err
			}
			out = append(out, obj)
			idx += consumed
			continue
		}
		if strings.HasPrefix(rest, "[") || strings.HasPrefix(rest, "{") {
			val, err := parseYAMLFlow(rest)
			if err != nil {
				return nil, idx, fmt.Errorf("yaml line %d: %w", line.num, err)
			}
			out = append(out, val)
			idx++
			continue
		}
		val, err := parseYAMLScalar(rest)
		if err != nil {
			return nil, idx, fmt.Errorf("yaml line %d: %w", line.num, err)
		}
		out = append(out, val)
		idx++
	}
	return out, idx, nil
}

func parseYAMLMap(lines []yamlLine, idx, baseIndent int) (Object, int, error) {
	out := Object{}
	for idx < len(lines) {
		line := lines[idx]
		if line.text == "..." || line.text == "---" || line.indent < baseIndent {
			break
		}
		if line.indent > baseIndent {
			return nil, idx, fmt.Errorf("yaml line %d: unexpected indentation", line.num)
		}
		if isYAMLSeqItem(line.text) {
			break
		}
		key, valStr, ok := splitYAMLKeyVal(line.text)
		if !ok {
			return nil, idx, fmt.Errorf("yaml line %d: expected key: value, got %q", line.num, line.text)
		}
		if valStr == "" {
			idx++
			if idx < len(lines) && lines[idx].indent > baseIndent {
				val, next, err := parseYAMLNode(lines, idx, lines[idx].indent)
				if err != nil {
					return nil, idx, err
				}
				out = out.set(key, val)
				idx = next
			} else if idx < len(lines) && lines[idx].indent == baseIndent && isYAMLSeqItem(lines[idx].text) {
				// Sequence indented at same level as its parent key (common YAML style).
				val, next, err := parseYAMLSeq(lines, idx, baseIndent)
				if err != nil {
					return nil, idx, err
				}
				out = out.set(key, val)
				idx = next
			} else {
				out = out.set(key, nil)
			}
			continue
		}
		if isBlockScalarHeader(valStr) {
			val, next := parseYAMLBlockScalar(lines, idx, baseIndent, valStr)
			out = out.set(key, val)
			idx = next
			continue
		}
		if strings.HasPrefix(valStr, "[") || strings.HasPrefix(valStr, "{") {
			val, err := parseYAMLFlow(valStr)
			if err != nil {
				return nil, idx, fmt.Errorf("yaml line %d: %w", line.num, err)
			}
			out = out.set(key, val)
			idx++
			continue
		}
		val, err := parseYAMLScalar(valStr)
		if err != nil {
			return nil, idx, fmt.Errorf("yaml line %d: %w", line.num, err)
		}
		out = out.set(key, val)
		idx++
	}
	return out, idx, nil
}

func isBlockScalarHeader(s string) bool {
	return s == "|" || s == "|-" || s == "|+" || s == ">" || s == ">-" || s == ">+"
}

func parseYAMLBlockScalar(lines []yamlLine, idx, parentIndent int, header string) (string, int) {
	folded := strings.HasPrefix(header, ">")
	chompStrip := strings.HasSuffix(header, "-")
	idx++
	if idx >= len(lines) || lines[idx].indent <= parentIndent {
		return "", idx
	}
	blockIndent := lines[idx].indent
	var parts []string
	prevLineNum := lines[idx].num - 1
	for idx < len(lines) && lines[idx].indent >= blockIndent {
		for gap := lines[idx].num - prevLineNum - 1; gap > 0; gap-- {
			parts = append(parts, "")
		}
		raw := lines[idx].raw
		if len(raw) >= blockIndent {
			parts = append(parts, raw[blockIndent:])
		} else {
			parts = append(parts, "")
		}
		prevLineNum = lines[idx].num
		idx++
	}
	var out string
	if folded {
		out = strings.Join(parts, " ")
	} else {
		out = strings.Join(parts, "\n")
	}
	if !chompStrip && out != "" {
		out += "\n"
	}
	return out, idx
}

func splitYAMLKeyVal(s string) (key, val string, ok bool) {
	inSingle, inDouble, esc := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if esc {
			esc = false
			continue
		}
		switch {
		case inDouble:
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inDouble = false
			}
		case inSingle:
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					inSingle = false
				}
			}
		default:
			if c == '"' {
				inDouble = true
			} else if c == '\'' {
				inSingle = true
			} else if c == ':' && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t') {
				rawKey := strings.TrimSpace(s[:i])
				if rawKey == "" {
					return "", "", false
				}
				k, err := unquoteYAMLKey(rawKey)
				if err != nil {
					return "", "", false
				}
				return k, strings.TrimSpace(s[i+1:]), true
			}
		}
	}
	return "", "", false
}

func unquoteYAMLKey(s string) (string, error) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var out string
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return "", err
		}
		return out, nil
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	return s, nil
}

func parseYAMLScalar(s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" || s == "~" || s == "Null" || s == "NULL" {
		return nil, nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var out string
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	switch s {
	case "true", "True", "TRUE":
		return true, nil
	case "false", "False", "FALSE":
		return false, nil
	}
	if _, ok := jsonNumber(s); ok {
		return json.Number(s), nil
	}
	return s, nil
}

func parseYAMLFlow(s string) (any, error) {
	s = strings.TrimSpace(s)
	if v, err := ParseJSON(s); err == nil {
		return v, nil
	}
	p := &flowParser{s: s}
	v, err := p.parseVal()
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.i < len(p.s) {
		return nil, fmt.Errorf("unexpected trailing %q", p.s[p.i:])
	}
	return v, nil
}

type flowParser struct {
	s string
	i int
}

func (p *flowParser) skipWS() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
}

func (p *flowParser) parseVal() (any, error) {
	p.skipWS()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("unexpected end of flow value")
	}
	switch p.s[p.i] {
	case '[':
		return p.parseArr()
	case '{':
		return p.parseObj()
	case '"':
		return p.parseDoubleQuote()
	case '\'':
		return p.parseSingleQuote()
	default:
		start := p.i
		for p.i < len(p.s) && !strings.ContainsRune(",]}:", rune(p.s[p.i])) {
			p.i++
		}
		return parseYAMLScalar(strings.TrimSpace(p.s[start:p.i]))
	}
}

func (p *flowParser) parseArr() ([]any, error) {
	p.i++ // '['
	out := []any{}
	for {
		p.skipWS()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("unclosed [")
		}
		if p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
		v, err := p.parseVal()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skipWS()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
		} else if p.i >= len(p.s) || p.s[p.i] != ']' {
			return nil, fmt.Errorf("expected , or ] in flow array")
		}
	}
}

func (p *flowParser) parseObj() (Object, error) {
	p.i++ // '{'
	out := Object{}
	for {
		p.skipWS()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("unclosed {")
		}
		if p.s[p.i] == '}' {
			p.i++
			return out, nil
		}
		var key string
		var err error
		if p.s[p.i] == '"' {
			key, err = p.parseDoubleQuote()
		} else if p.s[p.i] == '\'' {
			key, err = p.parseSingleQuote()
		} else {
			start := p.i
			for p.i < len(p.s) && p.s[p.i] != ':' && p.s[p.i] != '}' && p.s[p.i] != ',' {
				p.i++
			}
			key = strings.TrimSpace(p.s[start:p.i])
		}
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, fmt.Errorf("expected : after key %q", key)
		}
		p.i++
		v, err := p.parseVal()
		if err != nil {
			return nil, err
		}
		out = out.set(key, v)
		p.skipWS()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
		} else if p.i >= len(p.s) || p.s[p.i] != '}' {
			return nil, fmt.Errorf("expected , or } in flow object")
		}
	}
}

func (p *flowParser) parseDoubleQuote() (string, error) {
	start := p.i
	p.i++
	for p.i < len(p.s) && p.s[p.i] != '"' {
		if p.s[p.i] == '\\' {
			p.i++
		}
		p.i++
	}
	if p.i >= len(p.s) {
		return "", fmt.Errorf("unclosed double quote")
	}
	p.i++
	var out string
	if err := json.Unmarshal([]byte(p.s[start:p.i]), &out); err != nil {
		return "", err
	}
	return out, nil
}

func (p *flowParser) parseSingleQuote() (string, error) {
	p.i++ // '\''
	var b strings.Builder
	for p.i < len(p.s) {
		if p.s[p.i] == '\'' {
			if p.i+1 < len(p.s) && p.s[p.i+1] == '\'' {
				b.WriteByte('\'')
				p.i += 2
				continue
			}
			p.i++
			return b.String(), nil
		}
		b.WriteByte(p.s[p.i])
		p.i++
	}
	return "", fmt.Errorf("unclosed single quote")
}

// EncodeYAML renders v as clean 2-space indented YAML, preserving key order.
func EncodeYAML(v any) ([]byte, error) {
	var b bytes.Buffer
	writeYAML(&b, v, 0, false)
	if b.Len() == 0 || b.Bytes()[b.Len()-1] != '\n' {
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

func writeYAML(b *bytes.Buffer, v any, indent int, inlineFirst bool) {
	pad := strings.Repeat(" ", indent)
	switch t := v.(type) {
	case nil:
		b.WriteString("null\n")
	case bool:
		b.WriteString(strconv.FormatBool(t) + "\n")
	case json.Number:
		b.WriteString(string(t) + "\n")
	case string:
		writeYAMLString(b, t, indent)
	case Object:
		if len(t) == 0 {
			b.WriteString("{}\n")
			return
		}
		for i, f := range t {
			if i > 0 || !inlineFirst {
				b.WriteString(pad)
			}
			b.WriteString(yamlKey(f.Key) + ":")
			switch cv := f.Value.(type) {
			case Object:
				if len(cv) == 0 {
					b.WriteString(" {}\n")
				} else {
					b.WriteByte('\n')
					writeYAML(b, cv, indent+2, false)
				}
			case []any:
				if len(cv) == 0 {
					b.WriteString(" []\n")
				} else {
					b.WriteByte('\n')
					writeYAML(b, cv, indent+2, false)
				}
			default:
				b.WriteByte(' ')
				writeYAML(b, f.Value, indent+2, true)
			}
		}
	case []any:
		if len(t) == 0 {
			b.WriteString("[]\n")
			return
		}
		for i, e := range t {
			if i > 0 || !inlineFirst {
				b.WriteString(pad)
			}
			switch cv := e.(type) {
			case Object:
				if len(cv) == 0 {
					b.WriteString("- {}\n")
				} else {
					b.WriteString("- ")
					writeYAML(b, cv, indent+2, true)
				}
			case []any:
				if len(cv) == 0 {
					b.WriteString("- []\n")
				} else {
					b.WriteString("-\n")
					writeYAML(b, cv, indent+2, false)
				}
			default:
				b.WriteString("- ")
				writeYAML(b, e, indent+2, true)
			}
		}
	default:
		b.WriteString(yamlScalar(fmt.Sprint(t)) + "\n")
	}
}

func writeYAMLString(b *bytes.Buffer, s string, indent int) {
	if strings.Contains(s, "\n") && !strings.ContainsAny(s, "\r\x00") {
		chomp := "|-"
		body := s
		if strings.HasSuffix(s, "\n") && !strings.HasSuffix(s, "\n\n") {
			chomp = "|"
			body = strings.TrimSuffix(s, "\n")
		}
		b.WriteString(chomp + "\n")
		pad := strings.Repeat(" ", indent)
		for _, line := range strings.Split(body, "\n") {
			if line == "" {
				b.WriteByte('\n')
			} else {
				b.WriteString(pad + line + "\n")
			}
		}
		return
	}
	b.WriteString(yamlScalar(s) + "\n")
}

func yamlKey(k string) string {
	if needsYAMLQuote(k) || strings.ContainsAny(k, " \t:") {
		return quoteKey(k)
	}
	return k
}

func yamlScalar(s string) string {
	if needsYAMLQuote(s) {
		return quoteKey(s)
	}
	return s
}

func needsYAMLQuote(s string) bool {
	if s == "" || s == "null" || s == "~" || s == "Null" || s == "NULL" ||
		s == "true" || s == "false" || s == "True" || s == "False" || s == "TRUE" || s == "FALSE" {
		return true
	}
	if _, ok := jsonNumber(s); ok {
		return true
	}
	if s[0] == ' ' || s[len(s)-1] == ' ' || s[0] == '\t' || s[len(s)-1] == '\t' {
		return true
	}
	if strings.ContainsRune("-?:,[]{}#&*!|>'\"%@`", rune(s[0])) {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return strings.Contains(s, ": ") || strings.Contains(s, " #") || strings.HasSuffix(s, ":")
}
