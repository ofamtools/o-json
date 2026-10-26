package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// A table is an array (or the values a "[]" path names) laid out as rows:
// one row per item, one column per key any object item has, ordered by how
// many items have it (most first, ties by first appearance), so optional
// columns, the ones some items lack, come last. A missing field is a blank
// cell. Items that are not objects fill a "(value)" column.

// TableColumn is one column of a table.
type TableColumn struct {
	Name     string `json:"name"`
	Count    int    `json:"count"` // items that have the field
	Optional bool   `json:"optional,omitempty"`
	Types    string `json:"types"`
	// Self marks the "(value)" column of items that are not objects.
	Self bool `json:"self,omitempty"`
}

// Title is the column's header text.
func (c TableColumn) Title() string {
	if c.Self {
		return "(value)"
	}
	return c.Name
}

// Table is the rows and columns of an array.
type Table struct {
	Path    string        `json:"path"`
	Columns []TableColumn `json:"columns"`
	Rows    []*Node       `json:"rows"` // the items, as they are in the document
}

// selfValue is the "(value)" column's index into a row's cells.
const selfValue = -1

// NewTable lays out values: one array is its items; several values (a
// "[]" path) are the rows themselves.
func NewTable(path string, values []*Node, fanned bool) *Table {
	rows := values
	if !fanned && len(values) == 1 && values[0].Kind == KArray {
		rows = values[0].Children
	}
	t := &Table{Path: path, Rows: rows}
	type col struct {
		c     TableColumn
		kinds map[Kind]bool
		first int
	}
	cols := map[string]*col{}
	var self *col
	for _, row := range rows {
		if row.Kind != KObject {
			if self == nil {
				self = &col{c: TableColumn{Self: true}, kinds: map[Kind]bool{}}
			}
			self.c.Count++
			self.kinds[row.Kind] = true
			continue
		}
		for _, f := range row.Children {
			c := cols[f.Key]
			if c == nil {
				c = &col{c: TableColumn{Name: f.Key}, kinds: map[Kind]bool{}, first: len(cols)}
				cols[f.Key] = c
			}
			c.c.Count++
			c.kinds[f.Kind] = true
		}
	}
	ordered := make([]*col, 0, len(cols)+1)
	for _, c := range cols {
		ordered = append(ordered, c)
	}
	slices.SortFunc(ordered, func(a, b *col) int {
		if a.c.Count != b.c.Count {
			return b.c.Count - a.c.Count
		}
		return a.first - b.first
	})
	if self != nil {
		ordered = append(ordered, self)
	}
	for _, c := range ordered {
		c.c.Optional = c.c.Count < len(rows)
		var kinds []string
		for _, k := range kindOrder {
			if c.kinds[k] {
				kinds = append(kinds, string(k))
			}
		}
		c.c.Types = strings.Join(kinds, "|")
		t.Columns = append(t.Columns, c.c)
	}
	return t
}

// Cell is a row's value in a column, nil when the row lacks it.
func (t *Table) Cell(row *Node, col int) *Node {
	c := t.Columns[col]
	if c.Self {
		if row.Kind != KObject {
			return row
		}
		return nil
	}
	return row.Field(c.Name)
}

// cellLimit caps how much of a container a cell compacts.
const cellLimit = 200

// CellText is the text of a row's cell (blank when missing).
func (t *Table) CellText(row *Node, col int) string {
	return Text(t.Cell(row, col), cellLimit)
}

// textCellWidth caps a column in the text format.
const textCellWidth = 40

// WriteText prints the table as aligned columns; optional column titles
// end in "?".
func (t *Table) WriteText(w io.Writer) error {
	if len(t.Columns) == 0 {
		_, err := fmt.Fprintf(w, "(%d rows, no columns)\n", len(t.Rows))
		return err
	}
	head := make([]string, len(t.Columns))
	widths := make([]int, len(t.Columns))
	for j, c := range t.Columns {
		head[j] = c.Title()
		if c.Optional {
			head[j] += "?"
		}
		widths[j] = ansi.StringWidth(head[j])
	}
	cells := make([][]string, len(t.Rows))
	for i, row := range t.Rows {
		cells[i] = make([]string, len(t.Columns))
		for j := range t.Columns {
			v := oneLine(t.CellText(row, j))
			cells[i][j] = v
			widths[j] = min(max(widths[j], ansi.StringWidth(v)), textCellWidth)
		}
	}
	var b bytes.Buffer
	line := func(vals []string) {
		parts := make([]string, len(vals))
		for j, v := range vals {
			v = ansi.Truncate(v, widths[j], "…")
			parts[j] = v + strings.Repeat(" ", widths[j]-ansi.StringWidth(v))
		}
		b.WriteString(strings.TrimRight(strings.Join(parts, "  "), " ") + "\n")
	}
	line(head)
	for _, row := range cells {
		line(row)
	}
	_, err := w.Write(b.Bytes())
	return err
}

// WriteMarkdown prints the table as a Markdown table.
func (t *Table) WriteMarkdown(w io.Writer) error {
	var b bytes.Buffer
	esc := func(s string) string { return strings.ReplaceAll(oneLine(s), "|", `\|`) }
	b.WriteString("|")
	for _, c := range t.Columns {
		title := c.Title()
		if c.Optional {
			title += "?"
		}
		b.WriteString(" " + esc(title) + " |")
	}
	b.WriteString("\n|")
	for range t.Columns {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, row := range t.Rows {
		b.WriteString("|")
		for j := range t.Columns {
			b.WriteString(" " + esc(t.CellText(row, j)) + " |")
		}
		b.WriteString("\n")
	}
	_, err := w.Write(b.Bytes())
	return err
}

// WriteJSON prints the table as indented JSON: {path, columns, rows}, the
// rows as they are in the document.
func (t *Table) WriteJSON(w io.Writer) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// oneLine replaces line breaks and tabs with spaces.
func oneLine(s string) string {
	if !strings.ContainsAny(s, "\n\r\t") {
		return s
	}
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
}
