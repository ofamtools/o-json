package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// --- TOML codec (key-order preserving, zero external deps) -------------------

// ParseTOML decodes a TOML document into an ordered Object.
func ParseTOML(raw string) (any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrEmpty
	}
	p := &tomlParser{s: strings.ReplaceAll(raw, "\r\n", "\n")}
	return p.parse()
}

type tomlParser struct {
	s    string
	i    int
	line int
}

func (p *tomlParser) parse() (Object, error) {
	p.line = 1
	root := Object{}
	var curPath []string
	var curArray bool
	for {
		p.skipWSAndComments()
		if p.i >= len(p.s) {
			break
		}
		if p.s[p.i] == '[' {
			isArr := p.i+1 < len(p.s) && p.s[p.i+1] == '['
			if isArr {
				p.i += 2
			} else {
				p.i++
			}
			keys, err := p.parseKeyPath()
			if err != nil {
				return nil, fmt.Errorf("toml line %d: %w", p.line, err)
			}
			p.skipHorizWS()
			if isArr {
				if p.i+1 >= len(p.s) || p.s[p.i] != ']' || p.s[p.i+1] != ']' {
					return nil, fmt.Errorf("toml line %d: expected ]]", p.line)
				}
				p.i += 2
				root, err = tomlAppendTable(root, keys)
			} else {
				if p.i >= len(p.s) || p.s[p.i] != ']' {
					return nil, fmt.Errorf("toml line %d: expected ]", p.line)
				}
				p.i++
				root, err = tomlEnsureTable(root, keys)
			}
			if err != nil {
				return nil, fmt.Errorf("toml line %d: %w", p.line, err)
			}
			curPath, curArray = keys, isArr
			p.skipLineTail()
			continue
		}
		keys, err := p.parseKeyPath()
		if err != nil {
			return nil, fmt.Errorf("toml line %d: %w", p.line, err)
		}
		p.skipHorizWS()
		if p.i >= len(p.s) || p.s[p.i] != '=' {
			return nil, fmt.Errorf("toml line %d: expected =", p.line)
		}
		p.i++
		p.skipHorizWS()
		val, err := p.parseValue()
		if err != nil {
			return nil, fmt.Errorf("toml line %d: %w", p.line, err)
		}
		root, err = tomlSetAt(root, curPath, curArray, keys, val)
		if err != nil {
			return nil, fmt.Errorf("toml line %d: %w", p.line, err)
		}
		p.skipLineTail()
	}
	return root, nil
}

func (p *tomlParser) skipHorizWS() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *tomlParser) skipWSAndComments() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t':
			p.i++
		case '\n':
			p.i++
			p.line++
		case '#':
			for p.i < len(p.s) && p.s[p.i] != '\n' {
				p.i++
			}
		default:
			return
		}
	}
}

func (p *tomlParser) skipLineTail() {
	p.skipHorizWS()
	if p.i < len(p.s) && p.s[p.i] == '#' {
		for p.i < len(p.s) && p.s[p.i] != '\n' {
			p.i++
		}
	}
	if p.i < len(p.s) && p.s[p.i] == '\n' {
		p.i++
		p.line++
	}
}

func (p *tomlParser) parseKeyPath() ([]string, error) {
	var keys []string
	for {
		p.skipHorizWS()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("unexpected end in key")
		}
		var k string
		var err error
		switch p.s[p.i] {
		case '"':
			k, err = p.parseBasicString()
		case '\'':
			k, err = p.parseLiteralString()
		default:
			start := p.i
			for p.i < len(p.s) && (isTOMLBareKeyChar(p.s[p.i])) {
				p.i++
			}
			if p.i == start {
				return nil, fmt.Errorf("invalid key character %q", p.s[p.i])
			}
			k = p.s[start:p.i]
		}
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
		p.skipHorizWS()
		if p.i < len(p.s) && p.s[p.i] == '.' {
			p.i++
			continue
		}
		return keys, nil
	}
}

func isTOMLBareKeyChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
}

func (p *tomlParser) parseValue() (any, error) {
	p.skipHorizWS()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("missing value")
	}
	switch {
	case strings.HasPrefix(p.s[p.i:], `"""`):
		return p.parseMultilineBasicString()
	case strings.HasPrefix(p.s[p.i:], `'''`):
		return p.parseMultilineLiteralString()
	case p.s[p.i] == '"':
		return p.parseBasicString()
	case p.s[p.i] == '\'':
		return p.parseLiteralString()
	case p.s[p.i] == '[':
		return p.parseArray()
	case p.s[p.i] == '{':
		return p.parseInlineTable()
	default:
		return p.parseBareValue()
	}
}

func (p *tomlParser) parseBasicString() (string, error) {
	start := p.i
	p.i++
	for p.i < len(p.s) && p.s[p.i] != '"' && p.s[p.i] != '\n' {
		if p.s[p.i] == '\\' {
			p.i++
		}
		p.i++
	}
	if p.i >= len(p.s) || p.s[p.i] != '"' {
		return "", fmt.Errorf("unclosed string")
	}
	p.i++
	var out string
	if err := json.Unmarshal([]byte(p.s[start:p.i]), &out); err != nil {
		return "", err
	}
	return out, nil
}

func (p *tomlParser) parseLiteralString() (string, error) {
	p.i++
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != '\'' && p.s[p.i] != '\n' {
		p.i++
	}
	if p.i >= len(p.s) || p.s[p.i] != '\'' {
		return "", fmt.Errorf("unclosed literal string")
	}
	out := p.s[start:p.i]
	p.i++
	return out, nil
}

func (p *tomlParser) parseMultilineBasicString() (string, error) {
	p.i += 3
	if p.i < len(p.s) && p.s[p.i] == '\n' {
		p.i++
		p.line++
	}
	var b strings.Builder
	for p.i < len(p.s) {
		if strings.HasPrefix(p.s[p.i:], `"""`) {
			p.i += 3
			return b.String(), nil
		}
		if p.s[p.i] == '\\' && p.i+1 < len(p.s) {
			p.i++
			switch p.s[p.i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case '\n':
				p.line++
				for p.i+1 < len(p.s) && (p.s[p.i+1] == ' ' || p.s[p.i+1] == '\t' || p.s[p.i+1] == '\n') {
					p.i++
					if p.s[p.i] == '\n' {
						p.line++
					}
				}
			default:
				b.WriteByte(p.s[p.i])
			}
			p.i++
			continue
		}
		if p.s[p.i] == '\n' {
			p.line++
		}
		b.WriteByte(p.s[p.i])
		p.i++
	}
	return "", fmt.Errorf("unclosed multiline string")
}

func (p *tomlParser) parseMultilineLiteralString() (string, error) {
	p.i += 3
	if p.i < len(p.s) && p.s[p.i] == '\n' {
		p.i++
		p.line++
	}
	start := p.i
	for p.i < len(p.s) {
		if strings.HasPrefix(p.s[p.i:], `'''`) {
			out := p.s[start:p.i]
			p.i += 3
			return out, nil
		}
		if p.s[p.i] == '\n' {
			p.line++
		}
		p.i++
	}
	return "", fmt.Errorf("unclosed multiline literal string")
}

func (p *tomlParser) parseArray() ([]any, error) {
	p.i++ // '['
	out := []any{}
	for {
		p.skipWSAndComments()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("unclosed array")
		}
		if p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skipWSAndComments()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
		return nil, fmt.Errorf("expected , or ] in array")
	}
}

func (p *tomlParser) parseInlineTable() (Object, error) {
	p.i++ // '{'
	out := Object{}
	for {
		p.skipHorizWS()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("unclosed inline table")
		}
		if p.s[p.i] == '}' {
			p.i++
			return out, nil
		}
		keys, err := p.parseKeyPath()
		if err != nil {
			return nil, err
		}
		p.skipHorizWS()
		if p.i >= len(p.s) || p.s[p.i] != '=' {
			return nil, fmt.Errorf("expected = in inline table")
		}
		p.i++
		p.skipHorizWS()
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		out, err = tomlSetNested(out, keys, v)
		if err != nil {
			return nil, err
		}
		p.skipHorizWS()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return out, nil
		}
		return nil, fmt.Errorf("expected , or } in inline table")
	}
}

func (p *tomlParser) parseBareValue() (any, error) {
	start := p.i
	for p.i < len(p.s) && !strings.ContainsRune(",]}\n#", rune(p.s[p.i])) {
		p.i++
	}
	raw := strings.TrimSpace(p.s[start:p.i])
	if raw == "" {
		return nil, fmt.Errorf("empty value")
	}
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	clean := strings.ReplaceAll(raw, "_", "")
	if strings.HasPrefix(clean, "0x") || strings.HasPrefix(clean, "0o") || strings.HasPrefix(clean, "0b") {
		n, err := strconv.ParseInt(clean, 0, 64)
		if err == nil {
			return json.Number(strconv.FormatInt(n, 10)), nil
		}
	}
	if _, ok := jsonNumber(clean); ok {
		return json.Number(clean), nil
	}
	if strings.HasPrefix(clean, "+") {
		if _, ok := jsonNumber(clean[1:]); ok {
			return json.Number(clean[1:]), nil
		}
	}
	// RFC3339 date/time values stay strings in JSON.
	if looksLikeTOMLDateTime(raw) {
		return raw, nil
	}
	return nil, fmt.Errorf("invalid TOML value %q", raw)
}

func looksLikeTOMLDateTime(s string) bool {
	if len(s) >= 8 && unicode.IsDigit(rune(s[0])) && (strings.Contains(s, "-") || strings.Contains(s, ":")) {
		return true
	}
	return false
}

func tomlEnsureTable(root Object, path []string) (Object, error) {
	if len(path) == 0 {
		return root, nil
	}
	k := path[0]
	cur, ok := root.Get(k)
	if !ok {
		sub, err := tomlEnsureTable(Object{}, path[1:])
		if err != nil {
			return nil, err
		}
		return root.set(k, sub), nil
	}
	switch t := cur.(type) {
	case Object:
		sub, err := tomlEnsureTable(t, path[1:])
		if err != nil {
			return nil, err
		}
		return root.set(k, sub), nil
	case []any:
		if len(t) == 0 {
			return nil, fmt.Errorf("empty array table %q", k)
		}
		last, ok := t[len(t)-1].(Object)
		if !ok {
			return nil, fmt.Errorf("%q is not a table", k)
		}
		sub, err := tomlEnsureTable(last, path[1:])
		if err != nil {
			return nil, err
		}
		arr := append([]any{}, t...)
		arr[len(arr)-1] = sub
		return root.set(k, arr), nil
	default:
		return nil, fmt.Errorf("%q is already a scalar", k)
	}
}

func tomlAppendTable(root Object, path []string) (Object, error) {
	if len(path) == 1 {
		k := path[0]
		cur, ok := root.Get(k)
		if !ok {
			return root.set(k, []any{Object{}}), nil
		}
		arr, ok := cur.([]any)
		if !ok {
			return nil, fmt.Errorf("%q is not an array of tables", k)
		}
		return root.set(k, append(append([]any{}, arr...), Object{})), nil
	}
	k := path[0]
	cur, ok := root.Get(k)
	if !ok {
		sub, err := tomlAppendTable(Object{}, path[1:])
		if err != nil {
			return nil, err
		}
		return root.set(k, sub), nil
	}
	switch t := cur.(type) {
	case Object:
		sub, err := tomlAppendTable(t, path[1:])
		if err != nil {
			return nil, err
		}
		return root.set(k, sub), nil
	case []any:
		if len(t) == 0 {
			return nil, fmt.Errorf("empty array table %q", k)
		}
		last, ok := t[len(t)-1].(Object)
		if !ok {
			return nil, fmt.Errorf("%q is not a table", k)
		}
		sub, err := tomlAppendTable(last, path[1:])
		if err != nil {
			return nil, err
		}
		arr := append([]any{}, t...)
		arr[len(arr)-1] = sub
		return root.set(k, arr), nil
	default:
		return nil, fmt.Errorf("%q is not a table", k)
	}
}

func tomlSetAt(root Object, tablePath []string, inArray bool, keys []string, val any) (Object, error) {
	if len(tablePath) == 0 {
		return tomlSetNested(root, keys, val)
	}
	k := tablePath[0]
	cur, ok := root.Get(k)
	if !ok {
		return nil, fmt.Errorf("missing table %q", k)
	}
	if len(tablePath) == 1 && inArray {
		arr, ok := cur.([]any)
		if !ok || len(arr) == 0 {
			return nil, fmt.Errorf("missing array table %q", k)
		}
		last, ok := arr[len(arr)-1].(Object)
		if !ok {
			return nil, fmt.Errorf("array table %q element is not an object", k)
		}
		nv, err := tomlSetNested(last, keys, val)
		if err != nil {
			return nil, err
		}
		next := append([]any{}, arr...)
		next[len(next)-1] = nv
		return root.set(k, next), nil
	}
	switch t := cur.(type) {
	case Object:
		nv, err := tomlSetAt(t, tablePath[1:], inArray, keys, val)
		if err != nil {
			return nil, err
		}
		return root.set(k, nv), nil
	case []any:
		last, ok := t[len(t)-1].(Object)
		if !ok {
			return nil, fmt.Errorf("%q element is not an object", k)
		}
		nv, err := tomlSetAt(last, tablePath[1:], inArray, keys, val)
		if err != nil {
			return nil, err
		}
		next := append([]any{}, t...)
		next[len(next)-1] = nv
		return root.set(k, next), nil
	default:
		return nil, fmt.Errorf("%q is not a table", k)
	}
}

func tomlSetNested(obj Object, keys []string, val any) (Object, error) {
	if len(keys) == 1 {
		return obj.set(keys[0], val), nil
	}
	k := keys[0]
	var sub Object
	if cur, ok := obj.Get(k); ok {
		var isObj bool
		if sub, isObj = cur.(Object); !isObj {
			return nil, fmt.Errorf("key %q is not a table", k)
		}
	}
	nv, err := tomlSetNested(sub, keys[1:], val)
	if err != nil {
		return nil, err
	}
	return obj.set(k, nv), nil
}

// EncodeTOML renders an Object as TOML, preserving key order.
func EncodeTOML(v any) ([]byte, error) {
	root, ok := v.(Object)
	if !ok {
		return nil, fmt.Errorf("toml requires a top-level object, got %s", typeName(v))
	}
	var b bytes.Buffer
	if err := writeTOMLTable(&b, root, nil); err != nil {
		return nil, err
	}
	if b.Len() == 0 || b.Bytes()[b.Len()-1] != '\n' {
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

func writeTOMLTable(b *bytes.Buffer, obj Object, prefix []string) error {
	var subTables []Field
	var arrTables []Field
	for _, f := range obj {
		if f.Value == nil {
			continue // TOML has no null literal; omit null keys cleanly
		}
		if sub, ok := f.Value.(Object); ok {
			subTables = append(subTables, Field{f.Key, sub})
			continue
		}
		if arr, ok := f.Value.([]any); ok && isObjectArray(arr) {
			arrTables = append(arrTables, Field{f.Key, arr})
			continue
		}
		s, err := formatTOMLInline(f.Value)
		if err != nil {
			return err
		}
		b.WriteString(tomlKey(f.Key) + " = " + s + "\n")
	}
	for _, f := range subTables {
		path := append(append([]string{}, prefix...), f.Key)
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("[" + formatTOMLPath(path) + "]\n")
		if err := writeTOMLTable(b, f.Value.(Object), path); err != nil {
			return err
		}
	}
	for _, f := range arrTables {
		path := append(append([]string{}, prefix...), f.Key)
		for _, elem := range f.Value.([]any) {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("[[" + formatTOMLPath(path) + "]]\n")
			if err := writeTOMLTable(b, elem.(Object), path); err != nil {
				return err
			}
		}
	}
	return nil
}

func isObjectArray(a []any) bool {
	if len(a) == 0 {
		return false
	}
	for _, e := range a {
		if _, ok := e.(Object); !ok {
			return false
		}
	}
	return true
}

func formatTOMLPath(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = tomlKey(p)
	}
	return strings.Join(out, ".")
}

func tomlKey(k string) string {
	if k == "" {
		return `""`
	}
	for i := 0; i < len(k); i++ {
		if !isTOMLBareKeyChar(k[i]) {
			return quoteKey(k)
		}
	}
	return k
}

func formatTOMLInline(v any) (string, error) {
	switch t := v.(type) {
	case bool:
		return strconv.FormatBool(t), nil
	case json.Number:
		return string(t), nil
	case string:
		return quoteKey(t), nil
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if e == nil {
				continue
			}
			s, err := formatTOMLInline(e)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case Object:
		parts := make([]string, 0, len(t))
		for _, f := range t {
			if f.Value == nil {
				continue
			}
			s, err := formatTOMLInline(f.Value)
			if err != nil {
				return "", err
			}
			parts = append(parts, tomlKey(f.Key)+" = "+s)
		}
		return "{" + strings.Join(parts, ", ") + "}", nil
	default:
		return quoteKey(fmt.Sprint(t)), nil
	}
}
