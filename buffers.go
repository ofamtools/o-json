package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Save errors the app can test with errors.Is to pick its next step.
var (
	ErrNoPath     = errors.New("no file name: choose where to save")
	ErrReadOnly   = errors.New("read-only")
	ErrIncomplete = errors.New("incomplete JSON")
)

// SaveFormats lists every format Save writes, in the order a picker shows them.
var SaveFormats = []string{"pretty", "minified", "jsonl", "csv", "tsv"}

const maxUndo = 100

// Snapshot is one undo step: the raw text and the pipeline over it.
type Snapshot struct {
	Raw      string
	Pipeline []Operation
}

// Buffer is one open document. Value and Structure hold the last text that
// parsed, so a half-typed edit keeps the structure on screen while ParseErr
// says what is wrong. Pipeline is the transform recipe shown by Preview.
type Buffer struct {
	ID        int
	Path      string // where Save writes by default; "" for scratch and stdin
	Name      string
	Dirty     bool // RawText differs from what was read or last saved
	Editable  bool
	RawText   string
	ParseErr  error
	Value     any
	Structure *Schema
	JSONL     bool // the text holds several top-level values
	Pipeline  []Operation
	Undo      []Snapshot
	Redo      []Snapshot

	origin  string // the file a read-only buffer protects
	saved   string
	typing  bool // consecutive SetRaw calls share one undo step
	gen     int
	pgen    int
	preview any
	perr    error
}

// NewBuffer makes a buffer over raw read from path ("" for stdin or a
// paste). It always returns a usable buffer; the error is the parse error,
// for the caller to report.
func NewBuffer(path, raw string, editable bool) (*Buffer, error) {
	b := &Buffer{Path: path, origin: path, Name: "[No Name]", Editable: editable, saved: raw}
	if path != "" {
		b.Name = filepath.Base(path)
	}
	b.parse(raw)
	return b, b.ParseErr
}

// NewScratch is the blank, editable, unnamed buffer o-json starts with when
// given no file.
func NewScratch() *Buffer {
	b, _ := NewBuffer("", "", true)
	return b
}

// OpenBuffer reads path into a new buffer. A path that names no file yet
// opens as a new empty editable buffer of that name, as in vim. A file that
// does not parse still opens; its ParseErr says why.
func OpenBuffer(path string, editable bool) (*Buffer, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		b, _ := NewBuffer(path, "", true)
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	b, _ := NewBuffer(path, string(data), editable)
	return b, nil
}

// SetRaw replaces the text and parses it at once. Consecutive calls form one
// undo step until BreakUndo, Apply or an undo ends it. Edit gating belongs to
// the caller.
func (b *Buffer) SetRaw(raw string) {
	if raw == b.RawText {
		return
	}
	if !b.typing {
		b.push()
		b.typing = true
	}
	b.Redo = nil
	b.parse(raw)
}

// BreakUndo ends the current run of SetRaw calls, so the next one starts a
// new undo step (call it when insert mode ends).
func (b *Buffer) BreakUndo() { b.typing = false }

// Reformat rewrites the text pretty or minified as one undo step. JSONL
// text is rewritten one value per line either way.
func (b *Buffer) Reformat(pretty bool) error {
	if b.ParseErr != nil {
		return fmt.Errorf("%w: %s", ErrIncomplete, b.Status())
	}
	if b.Value == nil && strings.TrimSpace(b.RawText) == "" {
		return nil
	}
	format := "minified"
	switch {
	case b.JSONL:
		format = "jsonl" // JSONL stays one value per line
	case pretty:
		format = "pretty"
	}
	data, err := encode(b.Value, format)
	if err != nil {
		return err
	}
	b.BreakUndo()
	b.SetRaw(string(bytes.TrimSuffix(data, []byte("\n"))))
	b.BreakUndo()
	return nil
}

// Apply appends op to the pipeline when the result computes, as one undo step.
func (b *Buffer) Apply(op Operation) error { return b.ApplySteps(op) }

// ApplySteps appends ops to the pipeline as one transaction: each step is
// resolved against the steps before it, and the pipeline changes (as one
// undo step) only when every step resolves and the result computes.
func (b *Buffer) ApplySteps(ops ...Operation) error {
	if b.Value == nil && b.Structure == nil {
		return errors.New("nothing to transform: the buffer is empty")
	}
	if len(ops) == 0 {
		return nil
	}
	next := append([]Operation(nil), b.Pipeline...)
	for _, op := range ops {
		r, err := ResolveStep(b.Value, next, op)
		if err != nil {
			return err
		}
		next = append(next, r)
	}
	v, err := ApplyPipeline(b.Value, next)
	if err != nil {
		return err
	}
	b.typing = false
	b.push()
	b.Redo = nil
	b.Pipeline = next
	b.gen++
	b.pgen, b.preview, b.perr = b.gen, v, nil
	return nil
}

// UndoStep restores the previous raw text and pipeline.
func (b *Buffer) UndoStep() bool { return b.step(&b.Undo, &b.Redo) }

// RedoStep re-applies the step UndoStep took back.
func (b *Buffer) RedoStep() bool { return b.step(&b.Redo, &b.Undo) }

func (b *Buffer) step(from, to *[]Snapshot) bool {
	b.typing = false
	if len(*from) == 0 {
		return false
	}
	s := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, b.snapshot())
	b.Pipeline = s.Pipeline
	b.parse(s.Raw)
	return true
}

func (b *Buffer) snapshot() Snapshot {
	return Snapshot{Raw: b.RawText, Pipeline: append([]Operation(nil), b.Pipeline...)}
}

func (b *Buffer) push() {
	b.Undo = append(b.Undo, b.snapshot())
	if len(b.Undo) > maxUndo {
		b.Undo = b.Undo[len(b.Undo)-maxUndo:]
	}
}

// parse sets RawText, keeping the last valid Value when raw does not parse.
// Blank text is an empty buffer, not an error.
func (b *Buffer) parse(raw string) {
	b.RawText, b.Dirty = raw, raw != b.saved
	b.gen++
	if strings.TrimSpace(raw) == "" {
		b.Value, b.Structure, b.ParseErr, b.JSONL = nil, nil, nil, false
		return
	}
	v, err := ParseJSON(raw)
	jsonl := false
	if err != nil {
		w, derr := ParseDocument(raw)
		if derr != nil {
			// Report whichever parse got further: the single value for a
			// half-typed document, the line for broken JSONL.
			if errOffset(derr, raw) > errOffset(err, raw) {
				err = derr
			}
			b.ParseErr = err
			return
		}
		v, jsonl = w, true
	}
	b.Value, b.Structure, b.ParseErr, b.JSONL = v, InferSchema(v), nil, jsonl
}

// errOffset is the byte offset a parse error points at, or -1.
func errOffset(err error, raw string) int {
	var se *json.SyntaxError
	var pe *ParseError
	switch {
	case errors.As(err, &pe):
		return min(max(pe.Offset, 0), len(raw))
	case errors.As(err, &se):
		return min(max(int(se.Offset), 0), len(raw))
	case errors.Is(err, io.ErrUnexpectedEOF):
		return len(raw)
	}
	return -1
}

// Status is "" when the text parses, else "incomplete JSON: line X, col Y".
func (b *Buffer) Status() string {
	if b.ParseErr == nil {
		return ""
	}
	var pe *ParseError
	if errors.As(b.ParseErr, &pe) {
		return fmt.Sprintf("incomplete JSON: line %d, col %d", pe.Line, pe.Col)
	}
	off := errOffset(b.ParseErr, b.RawText)
	if off < 0 {
		return "incomplete JSON: " + b.ParseErr.Error()
	}
	head := b.RawText[:off]
	line := strings.Count(head, "\n") + 1
	col := len([]rune(head[strings.LastIndexByte(head, '\n')+1:])) + 1
	return fmt.Sprintf("incomplete JSON: line %d, col %d", line, col)
}

// Preview is the last valid value run through the pipeline.
func (b *Buffer) Preview() (any, error) {
	if len(b.Pipeline) == 0 {
		return b.Value, nil
	}
	if b.pgen != b.gen {
		b.preview, b.perr = ApplyPipeline(b.Value, b.Pipeline)
		b.pgen = b.gen
	}
	return b.preview, b.perr
}

// Label is the buffer's line in the :b picker.
func (b *Buffer) Label() string {
	s := fmt.Sprintf("%d %s", b.ID, b.Name)
	if b.Dirty {
		s += " [+]"
	}
	if !b.Editable {
		s += " VIEW"
	}
	if len(b.Pipeline) > 0 {
		s += fmt.Sprintf(" (%d steps)", len(b.Pipeline))
	}
	return s
}

// FormatForPath is the format a file name implies: csv, tsv, jsonl, or ""
// for JSON.
func FormatForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		return "csv"
	case ".tsv", ".tab":
		return "tsv"
	case ".jsonl", ".ndjson":
		return "jsonl"
	}
	return ""
}

// FormatOffers lists the formats the current preview can be saved as: JSONL
// for arrays, CSV and TSV for anything that tabulates.
func (b *Buffer) FormatOffers() []string {
	out := []string{"pretty", "minified"}
	v, err := b.Preview()
	if err != nil || v == nil {
		return out
	}
	if _, ok := v.([]any); ok {
		out = append(out, "jsonl")
	}
	if len(TableRelation(v).Columns) > 0 {
		out = append(out, "csv", "tsv")
	}
	return out
}

// Save writes the preview to path (b.Path when empty) atomically. An empty
// format follows the extension; JSON with no pipeline keeps the text as
// typed. Text that does not parse is never saved, and a read-only buffer
// never overwrites the file it was opened from. An editable buffer saving
// its own document as JSON takes the new name and is clean again; a
// pipeline result, CSV or TSV is an export and leaves the buffer as it was.
func (b *Buffer) Save(path, format string) error {
	if path == "" {
		path = b.Path
	}
	if path == "" {
		return ErrNoPath
	}
	if b.ParseErr != nil {
		return fmt.Errorf("%w: %s: fix it or undo before saving", ErrIncomplete, b.Status())
	}
	if b.Value == nil && strings.TrimSpace(b.RawText) == "" {
		return errors.New("the buffer is empty: nothing to save")
	}
	if !b.Editable && b.origin != "" && samePath(path, b.origin) {
		return fmt.Errorf("%w: %s is the original file: press ctrl+e to edit, or save under another name", ErrReadOnly, b.origin)
	}
	if format == "" {
		format = FormatForPath(path)
	}
	verbatim := len(b.Pipeline) == 0 && (format == "" || format == "jsonl" && b.JSONL)
	var data []byte
	if verbatim {
		data = []byte(b.RawText)
		if !strings.HasSuffix(b.RawText, "\n") {
			data = append(data, '\n')
		}
	} else {
		if format == "" {
			format = "pretty"
		}
		v, err := b.Preview()
		if err != nil {
			return err
		}
		if data, err = encode(v, format); err != nil {
			return err
		}
	}
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	if !b.Editable || len(b.Pipeline) > 0 || format == "csv" || format == "tsv" {
		return nil
	}
	b.Path, b.origin, b.Name = path, path, filepath.Base(path)
	text := string(data)
	if verbatim {
		text = b.RawText
	}
	b.saved = text
	if text != b.RawText {
		b.BreakUndo()
		b.SetRaw(text)
		b.BreakUndo()
	}
	b.Dirty = false
	return nil
}

// ExportRelation writes r to path as csv or tsv atomically, the table the
// user sees. Like Save it never overwrites the file a read-only buffer was
// opened from (through any alias), and it leaves the buffer as it was.
func (b *Buffer) ExportRelation(path, format string, r Relation) error {
	if path == "" {
		return ErrNoPath
	}
	if !b.Editable && b.origin != "" && samePath(path, b.origin) {
		return fmt.Errorf("%w: %s is the original file: press ctrl+e to edit, or save under another name", ErrReadOnly, b.origin)
	}
	if len(r.Columns) == 0 {
		return fmt.Errorf("%s needs rows: the table is empty", format)
	}
	export := ExportCSV
	switch format {
	case "csv":
	case "tsv":
		export = ExportTSV
	default:
		return fmt.Errorf("a table exports as csv or tsv, not %q", format)
	}
	var buf bytes.Buffer
	if err := export(&buf, r); err != nil {
		return err
	}
	return atomicWrite(path, buf.Bytes())
}

// encode renders v in one of SaveFormats, ending with a newline.
func encode(v any, format string) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case "pretty", "minified":
		data, err := FormatJSON(v, format == "pretty")
		if err != nil {
			return nil, err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	case "jsonl":
		items, ok := v.([]any)
		if !ok {
			items = []any{v}
		}
		for _, it := range items {
			data, err := FormatJSON(it, false)
			if err != nil {
				return nil, err
			}
			buf.Write(data)
			buf.WriteByte('\n')
		}
	case "csv", "tsv":
		r := TableRelation(v)
		if len(r.Columns) == 0 {
			return nil, fmt.Errorf("%s needs rows: flatten (F) or pick a table first", format)
		}
		export := ExportCSV
		if format == "tsv" {
			export = ExportTSV
		}
		if err := export(&buf, r); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown format %q (use %s)", format, strings.Join(SaveFormats, ", "))
	}
	return buf.Bytes(), nil
}

// atomicWrite writes through a temporary file beside the target and renames
// it into place, keeping the target's mode and writing through a symlink.
func atomicWrite(path string, data []byte) error {
	target := path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		target = real
	}
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(target); err == nil {
		if st.IsDir() {
			return fmt.Errorf("%s is a directory", path)
		}
		mode = st.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, mode)
	}
	if err == nil {
		err = os.Rename(tmp, target)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// samePath reports whether a and b name one file: the same cleaned path
// after symlinks, or the same inode (a hard link).
func samePath(a, b string) bool {
	norm := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		abs, _ := filepath.Abs(p)
		return abs
	}
	if norm(a) == norm(b) {
		return true
	}
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(sa, sb)
}

// BufferRing holds the open buffers, the current one and the alternate.
type BufferRing struct {
	bufs     []*Buffer
	cur, alt int
	next     int
}

// Add gives b the next unique ID and makes it current.
func (r *BufferRing) Add(b *Buffer) {
	r.next++
	b.ID = r.next
	r.bufs = append(r.bufs, b)
	r.alt, r.cur = r.cur, b.ID
}

// Current is the shown buffer, nil before the first Add.
func (r *BufferRing) Current() *Buffer { return r.Get(r.cur) }

// Get finds a buffer by ID.
func (r *BufferRing) Get(id int) *Buffer {
	for _, b := range r.bufs {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// List is every buffer in the order opened.
func (r *BufferRing) List() []*Buffer { return r.bufs }

// Switch makes buffer id current; the old one becomes the alternate.
func (r *BufferRing) Switch(id int) bool {
	if id == r.cur || r.Get(id) == nil {
		return false
	}
	r.alt, r.cur = r.cur, id
	return true
}

// Cycle moves delta places through the ring, wrapping (:bn is 1, :bp -1).
func (r *BufferRing) Cycle(delta int) bool {
	n := len(r.bufs)
	if n < 2 {
		return false
	}
	i := 0
	for j, b := range r.bufs {
		if b.ID == r.cur {
			i = j
		}
	}
	return r.Switch(r.bufs[((i+delta)%n+n)%n].ID)
}

// Alternate swaps to the previously current buffer (ctrl+6).
func (r *BufferRing) Alternate() bool { return r.Switch(r.alt) }
