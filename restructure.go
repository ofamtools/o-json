package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// defaultRedactKeyRe matches common sensitive field names when redact is called
// without explicit fields or pattern.
var defaultRedactKeyRe = regexp.MustCompile(`(?i)(^|[._-])(password|passwd|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|authorization|auth[_-]?token|credential|cookie|session[_-]?id|ssn|email)($|[._-])`)

// defaultRedactValRe matches common secret token patterns in string values.
var defaultRedactValRe = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9._~+/=-]{8,}|sk-[a-z0-9]{16,}|gh[pousr]_[a-z0-9]{16,}|xox[baprs]-[a-z0-9-]{10,}|AKIA[0-9A-Z]{16}|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,})`)

// normalizeRestructure validates and fills defaults for the declarative
// restructuring operations: coalesce, cast, dedupe, unwind, redact, pivot, unpivot.
func normalizeRestructure(op Operation) (Operation, bool, error) {
	switch op.Kind {
	case "coalesce":
		if len(op.Fields) == 0 {
			return op, true, fmt.Errorf("coalesce needs fields (candidate paths)")
		}
		if op.Name == "" {
			return op, true, fmt.Errorf("coalesce needs name (target field)")
		}
		for _, f := range op.Fields {
			if _, err := ParsePath(f); err != nil {
				return op, true, fmt.Errorf("coalesce field %q: %w", f, err)
			}
		}
		return op, true, nil
	case "cast":
		if op.Mode == "" {
			return op, true, fmt.Errorf("cast needs mode (string, number, int, bool)")
		}
		switch op.Mode {
		case "string", "number", "int", "bool":
		default:
			return op, true, fmt.Errorf("cast mode %q is not string, number, int or bool", op.Mode)
		}
		return op, true, nil
	case "dedupe":
		if op.Field != "" {
			if _, err := ParsePath(op.Field); err != nil {
				return op, true, fmt.Errorf("dedupe field %q: %w", op.Field, err)
			}
		}
		return op, true, nil
	case "unwind":
		if op.Field == "" {
			return op, true, fmt.Errorf("unwind needs field (nested array key or path)")
		}
		if _, err := ParsePath(op.Field); err != nil {
			return op, true, fmt.Errorf("unwind field %q: %w", op.Field, err)
		}
		return op, true, nil
	case "redact":
		if op.Name == "" {
			op.Name = "[REDACTED]"
		}
		if op.Query != "" {
			if _, err := regexp.Compile(op.Query); err != nil {
				return op, true, fmt.Errorf("redact invalid regex %q: %v", op.Query, err)
			}
		}
		return op, true, nil
	case "pivot":
		if op.Field == "" {
			return op, true, fmt.Errorf("pivot needs field (column whose values become headers)")
		}
		if op.Name == "" {
			return op, true, fmt.Errorf("pivot needs name (value column)")
		}
		return op, true, nil
	case "unpivot":
		if op.Field == "" {
			op.Field = "key"
		}
		if op.Name == "" {
			op.Name = "value"
		}
		if op.Field == op.Name {
			return op, true, fmt.Errorf("unpivot key column %q and value column cannot be the same", op.Field)
		}
		return op, true, nil
	}
	return op, false, nil
}

// applyRestructure executes one restructuring operation.
func applyRestructure(v any, op Operation) (any, bool, error) {
	switch op.Kind {
	case "coalesce":
		out, err := opCoalesce(v, op.Fields, op.Name)
		return out, true, err
	case "cast":
		out, err := opCast(v, op.Field, op.Mode)
		return out, true, err
	case "dedupe":
		out, err := opDedupe(v, op.Field)
		return out, true, err
	case "unwind":
		out, err := opUnwind(v, op.Field, op.PreserveEmpty)
		return out, true, err
	case "redact":
		out, err := opRedact(v, op.Fields, op.Query, op.Name)
		return out, true, err
	case "pivot":
		out, err := opPivot(v, op.Fields, op.Field, op.Name)
		return out, true, err
	case "unpivot":
		out, err := opUnpivot(v, op.Fields, op.Field, op.Name)
		return out, true, err
	}
	return v, false, nil
}

// opCoalesce picks the first non-null, non-empty value from candidate paths and
// writes it to targetName on each object.
func opCoalesce(v any, fields []string, targetName string) (any, error) {
	parsed := make([]Path, len(fields))
	for i, f := range fields {
		p, err := ParsePath(f)
		if err != nil {
			return nil, err
		}
		parsed[i] = p
	}
	return eachRow(v, func(o Object) (Object, error) {
		var chosen any
		for _, p := range parsed {
			val, ok := AtPath(o, p)
			if !ok || isEmptyValue(val) {
				continue
			}
			chosen = val
			break
		}
		return o.set(targetName, chosen), nil
	})
}

func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case Object:
		return len(t) == 0
	}
	return false
}

// opCast coerces field values (or array scalar elements when field == "") to
// string, number, int, or bool. Null stays null.
func opCast(v any, field, mode string) (any, error) {
	if field == "" {
		if arr, ok := v.([]any); ok {
			out := make([]any, len(arr))
			for i, e := range arr {
				c, err := castValue(e, mode)
				if err != nil {
					return nil, fmt.Errorf("cast[%d]: %w", i, err)
				}
				out[i] = c
			}
			return out, nil
		}
		return castValue(v, mode)
	}
	return eachRow(v, func(o Object) (Object, error) {
		idx := o.index(field)
		if idx < 0 {
			return o, nil
		}
		c, err := castValue(o[idx].Value, mode)
		if err != nil {
			return nil, fmt.Errorf("cast %q: %w", field, err)
		}
		out := append(Object{}, o...)
		out[idx].Value = c
		return out, nil
	})
}

func castValue(v any, mode string) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch mode {
	case "string":
		return jqString(v), nil
	case "number":
		switch t := v.(type) {
		case json.Number:
			return t, nil
		case bool:
			if t {
				return json.Number("1"), nil
			}
			return json.Number("0"), nil
		case string:
			s := strings.TrimSpace(t)
			if s == "" {
				return nil, nil
			}
			f, err := strconv.ParseFloat(s, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, fmt.Errorf("cannot cast %q to number", t)
			}
			if _, ok := jsonNumber(s); ok {
				return json.Number(s), nil
			}
			return json.Number(strconv.FormatFloat(f, 'f', -1, 64)), nil
		default:
			return nil, fmt.Errorf("cannot cast %s to number", typeName(v))
		}
	case "int":
		switch t := v.(type) {
		case json.Number:
			if i, err := t.Int64(); err == nil {
				return json.Number(strconv.FormatInt(i, 10)), nil
			}
			f, err := t.Float64()
			if err != nil {
				return nil, fmt.Errorf("cannot cast %q to int", t)
			}
			return json.Number(strconv.FormatInt(int64(f), 10)), nil
		case bool:
			if t {
				return json.Number("1"), nil
			}
			return json.Number("0"), nil
		case string:
			s := strings.TrimSpace(t)
			if s == "" {
				return nil, nil
			}
			if i, err := strconv.ParseInt(s, 10, 64); err == nil {
				return json.Number(strconv.FormatInt(i, 10)), nil
			}
			f, err := strconv.ParseFloat(s, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, fmt.Errorf("cannot cast %q to int", t)
			}
			return json.Number(strconv.FormatInt(int64(f), 10)), nil
		default:
			return nil, fmt.Errorf("cannot cast %s to int", typeName(v))
		}
	case "bool":
		switch t := v.(type) {
		case bool:
			return t, nil
		case json.Number:
			f, err := t.Float64()
			return err == nil && f != 0, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(t)) {
			case "true", "1", "yes", "y", "on":
				return true, nil
			case "false", "0", "no", "n", "off", "":
				return false, nil
			default:
				return nil, fmt.Errorf("cannot cast %q to bool", t)
			}
		default:
			return nil, fmt.Errorf("cannot cast %s to bool", typeName(v))
		}
	}
	return nil, fmt.Errorf("unknown cast mode %q", mode)
}

// opDedupe deduplicates array elements by field path (or full canonical value when empty),
// preserving first occurrence.
func opDedupe(v any, field string) (any, error) {
	arr, err := needArray(v, "dedupe")
	if err != nil {
		return nil, err
	}
	var p Path
	if field != "" {
		if p, err = ParsePath(field); err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	out := make([]any, 0, len(arr))
	for _, e := range arr {
		keyVal := e
		if len(p) > 0 {
			keyVal, _ = AtPath(e, p)
		}
		k := canonicalKey(keyVal)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out, nil
}

// canonicalKey returns a stable JSON key for equality comparison (sorting object keys).
func canonicalKey(v any) string {
	switch t := v.(type) {
	case Object:
		ks := make([]string, len(t))
		for i, f := range t {
			ks[i] = f.Key
		}
		slices.Sort(ks)
		parts := make([]string, len(ks))
		for i, k := range ks {
			val, _ := t.Get(k)
			parts[i] = quoteKey(k) + ":" + canonicalKey(val)
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = canonicalKey(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return "N:" + strconv.FormatFloat(f, 'g', -1, 64)
		}
		return "N:" + string(t)
	default:
		b, _ := FormatJSON(v, false)
		return string(b)
	}
}

// opUnwind explodes a nested array field into one parent copy per element.
// When the array is empty or missing, preserveEmpty keeps one copy with nil.
func opUnwind(v any, field string, preserveEmpty bool) (any, error) {
	p, err := ParsePath(field)
	if err != nil {
		return nil, err
	}
	unwindOne := func(o Object) ([]any, error) {
		val, ok := AtPath(o, p)
		if !ok || val == nil {
			if preserveEmpty {
				cp, err := ReplaceAt(o, p, nil)
				if err != nil {
					return []any{o}, nil
				}
				return []any{cp}, nil
			}
			return nil, nil
		}
		arr, isArr := val.([]any)
		if !isArr {
			// Non-array scalar stays as single row.
			return []any{o}, nil
		}
		if len(arr) == 0 {
			if preserveEmpty {
				cp, err := ReplaceAt(o, p, nil)
				if err != nil {
					return nil, err
				}
				return []any{cp}, nil
			}
			return nil, nil
		}
		rows := make([]any, 0, len(arr))
		for _, elem := range arr {
			cp, err := ReplaceAt(o, p, elem)
			if err != nil {
				return nil, err
			}
			rows = append(rows, cp)
		}
		return rows, nil
	}
	switch t := v.(type) {
	case Object:
		return unwindOne(t)
	case []any:
		out := []any{}
		for _, e := range t {
			o, ok := e.(Object)
			if !ok {
				if preserveEmpty {
					out = append(out, e)
				}
				continue
			}
			rows, err := unwindOne(o)
			if err != nil {
				return nil, err
			}
			out = append(out, rows...)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unwind needs an object or array, got %s", typeName(v))
	}
}

// opRedact masks matching keys and/or regex patterns recursively with replacement.
func opRedact(v any, fields []string, pattern, replacement string) (any, error) {
	if replacement == "" {
		replacement = "[REDACTED]"
	}
	var customRe *regexp.Regexp
	if pattern != "" {
		var err error
		if customRe, err = regexp.Compile(pattern); err != nil {
			return nil, err
		}
	}
	var fieldRes []*regexp.Regexp
	fieldExact := map[string]bool{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		fieldExact[strings.ToLower(f)] = true
		if re, err := regexp.Compile("(?i)" + f); err == nil {
			fieldRes = append(fieldRes, re)
		}
	}
	useDefaults := len(fields) == 0 && customRe == nil

	keyMatches := func(k string) bool {
		if fieldExact[strings.ToLower(k)] {
			return true
		}
		for _, re := range fieldRes {
			if re.MatchString(k) {
				return true
			}
		}
		if customRe != nil && customRe.MatchString(k) {
			return true
		}
		if useDefaults && defaultRedactKeyRe.MatchString(k) {
			return true
		}
		return false
	}

	var walk func(x any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case Object:
			out := make(Object, len(t))
			for i, f := range t {
				if keyMatches(f.Key) {
					out[i] = Field{Key: f.Key, Value: replacement}
				} else {
					out[i] = Field{Key: f.Key, Value: walk(f.Value)}
				}
			}
			return out
		case []any:
			out := make([]any, len(t))
			for i, e := range t {
				out[i] = walk(e)
			}
			return out
		case string:
			if customRe != nil && customRe.MatchString(t) {
				return customRe.ReplaceAllString(t, replacement)
			}
			if useDefaults && defaultRedactValRe.MatchString(t) {
				return defaultRedactValRe.ReplaceAllString(t, replacement)
			}
			return t
		default:
			return x
		}
	}
	return walk(v), nil
}

// opPivot reshapes tall rows into wide rows. indexFields are the grouping keys
// (inferred as all columns other than pivotCol and valCol when empty).
func opPivot(v any, indexFields []string, pivotCol, valCol string) (any, error) {
	arr, err := needArray(v, "pivot")
	if err != nil {
		return nil, err
	}
	idxCols := slices.Clone(indexFields)
	if len(idxCols) == 0 {
		seen := map[string]bool{pivotCol: true, valCol: true}
		for _, e := range arr {
			if o, ok := e.(Object); ok {
				for _, f := range o {
					if !seen[f.Key] {
						seen[f.Key] = true
						idxCols = append(idxCols, f.Key)
					}
				}
			}
		}
	}

	type groupRow struct {
		base Object
		vals map[string]any
	}
	var order []string
	groups := map[string]*groupRow{}
	var pivotCols []string
	seenPivot := map[string]bool{}

	for _, e := range arr {
		o, ok := e.(Object)
		if !ok {
			continue
		}
		keyParts := make([]string, len(idxCols))
		base := make(Object, len(idxCols))
		for i, c := range idxCols {
			val := cell(o, c)
			keyParts[i] = canonicalKey(val)
			base[i] = Field{Key: c, Value: val}
		}
		gk := strings.Join(keyParts, "\x00")
		g := groups[gk]
		if g == nil {
			g = &groupRow{base: base, vals: map[string]any{}}
			groups[gk] = g
			order = append(order, gk)
		}
		pkRaw := cell(o, pivotCol)
		if pkRaw == nil {
			continue
		}
		pk := jqString(pkRaw)
		if !seenPivot[pk] {
			seenPivot[pk] = true
			pivotCols = append(pivotCols, pk)
		}
		g.vals[pk] = cell(o, valCol)
	}

	out := make([]any, 0, len(order))
	for _, gk := range order {
		g := groups[gk]
		row := append(Object{}, g.base...)
		for _, pk := range pivotCols {
			row = row.set(pk, g.vals[pk])
		}
		out = append(out, row)
	}
	return out, nil
}

// opUnpivot melts wide columns (fields) into tall rows with (keyCol, valCol).
// When fields is empty, melts all columns except the first column (or all columns
// if there is only one).
func opUnpivot(v any, fields []string, keyCol, valCol string) (any, error) {
	arr, err := needArray(v, "unpivot")
	if err != nil {
		return nil, err
	}
	meltSet := map[string]bool{}
	meltCols := slices.Clone(fields)
	if len(meltCols) == 0 {
		var allCols []string
		seen := map[string]bool{}
		for _, e := range arr {
			if o, ok := e.(Object); ok {
				for _, f := range o {
					if !seen[f.Key] {
						seen[f.Key] = true
						allCols = append(allCols, f.Key)
					}
				}
			}
		}
		if len(allCols) > 1 {
			meltCols = allCols[1:]
		} else {
			meltCols = allCols
		}
	}
	for _, c := range meltCols {
		meltSet[c] = true
	}

	out := []any{}
	for _, e := range arr {
		o, ok := e.(Object)
		if !ok {
			continue
		}
		var keep Object
		for _, f := range o {
			if !meltSet[f.Key] {
				keep = append(keep, f)
			}
		}
		for _, col := range meltCols {
			val, exists := o.Get(col)
			if !exists {
				continue
			}
			row := append(Object{}, keep...)
			row = row.set(keyCol, col)
			row = row.set(valCol, val)
			out = append(out, row)
		}
	}
	return out, nil
}
