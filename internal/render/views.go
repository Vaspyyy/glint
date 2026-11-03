package render

import (
	"io"
	"strconv"
	"strings"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/theme"
)

// Row is one output line. A nil E with Err set is an inline error (tree view).
type Row struct {
	E      *fsx.Entry
	Prefix string
	Err    error
}

func Rows(es []*fsx.Entry) []Row {
	rows := make([]Row, len(es))
	for i, e := range es {
		rows[i] = Row{E: e}
	}
	return rows
}

// Flatten walks a built tree into rows with box-drawing prefixes (root excluded).
func Flatten(root *fsx.Entry) []Row {
	var rows []Row
	var walk func(e *fsx.Entry, indent string)
	walk = func(e *fsx.Entry, indent string) {
		n := len(e.Children)
		for i, c := range e.Children {
			conn, next := "├── ", "│   "
			if i == n-1 && e.ChildErr == nil {
				conn, next = "╰── ", "    "
			}
			rows = append(rows, Row{E: c, Prefix: indent + conn})
			walk(c, indent+next)
		}
		if e.ChildErr != nil {
			rows = append(rows, Row{Prefix: indent + "╰── ", Err: e.ChildErr})
		}
	}
	walk(root, "")
	return rows
}

func (r *Renderer) anyGit(rows []Row) bool {
	if !r.cfg.Git {
		return false
	}
	for _, row := range rows {
		if row.E != nil && !row.E.Git.Clean() {
			return true
		}
	}
	return false
}

type flusher struct {
	w io.Writer
	strings.Builder
}

func (f *flusher) line() {
	f.WriteByte('\n')
	if f.Len() > 32<<10 {
		f.flush()
	}
}

func (f *flusher) flush() {
	io.WriteString(f.w, f.String())
	f.Reset()
}

// List prints one entry per line, with tree prefixes if present.
func (r *Renderer) List(w io.Writer, rows []Row) {
	f := &flusher{w: w}
	gitCol := r.anyGit(rows)
	names, nameW := r.names(rows)
	for i, row := range rows {
		if row.E == nil {
			f.WriteString(r.faint.Render(row.Prefix))
			f.WriteString(r.errText(row.Err))
			f.line()
			continue
		}
		if gitCol {
			f.WriteString(r.gitShort(row.E.Git))
			f.WriteByte(' ')
		}
		f.WriteString(r.withPeek(names[i], nameW, row.E))
		f.line()
	}
	f.flush()
}

// Grid lays entries out column-major, like ls, fitted to the terminal width.
func (r *Renderer) Grid(w io.Writer, es []*fsx.Entry) {
	n := len(es)
	if n == 0 {
		return
	}
	gitCol := r.anyGit(Rows(es))
	cells := make([]string, n)
	widths := make([]int, n)
	sum := 0
	for i, e := range es {
		c := r.Name(e, false)
		if gitCol {
			c = r.gitShort(e.Git) + " " + c
		}
		cells[i] = c
		widths[i] = width(c)
		sum += widths[i]
	}

	const gap = 2
	W := max(r.cfg.Width, 1)
	var colW []int
	rows := max(1, sum/W)
	for ; rows <= n; rows++ {
		cols := (n + rows - 1) / rows
		colW = make([]int, cols)
		for i, wd := range widths {
			if c := i / rows; wd > colW[c] {
				colW[c] = wd
			}
		}
		total := gap * (cols - 1)
		for _, x := range colW {
			total += x
		}
		if total <= W || cols == 1 {
			break
		}
	}

	f := &flusher{w: w}
	for row := range rows {
		for c := range colW {
			i := c*rows + row
			if i >= n {
				break
			}
			f.WriteString(cells[i])
			if (c+1)*rows+row < n {
				f.WriteString(spaces(colW[c] - widths[i] + gap))
			}
		}
		f.line()
	}
	f.flush()
}

type column int

const (
	colInode column = iota
	colPerms
	colLinks
	colUser
	colGroup
	colSize
	colTime
	colBar
	colHeat
	colGit
	colCommit
)

var colTitle = [...]string{"Inode", "Permissions", "Links", "User", "Group", "Size", "Modified", "", "Heat", "Git", "Last commit"}
var colRight = [...]bool{true, false, true, false, false, true, true, false, false, false, false}

func (r *Renderer) columns() []column {
	var cols []column
	if r.cfg.Inode {
		cols = append(cols, colInode)
	}
	cols = append(cols, colPerms)
	if r.cfg.Links {
		cols = append(cols, colLinks)
	}
	if r.cfg.Owner {
		cols = append(cols, colUser)
	}
	if r.cfg.Group {
		cols = append(cols, colGroup)
	}
	cols = append(cols, colSize)
	if r.cfg.Bars {
		cols = append(cols, colBar)
	}
	cols = append(cols, colTime)
	if r.cfg.Heat {
		cols = append(cols, colHeat)
	}
	if r.cfg.Git {
		cols = append(cols, colGit)
	}
	if r.cfg.Commits {
		cols = append(cols, colCommit)
	}
	return cols
}

func (r *Renderer) cell(c column, e *fsx.Entry) string {
	switch c {
	case colInode:
		return r.fg[theme.Purple].Render(strconv.FormatUint(e.Sys.Inode, 10))
	case colPerms:
		if r.cfg.Octal {
			return r.fg[theme.Purple].Render(octal(e.Mode))
		}
		return r.perms(e)
	case colLinks:
		return r.fg[theme.Subtle].Render(strconv.FormatUint(e.Sys.Nlink, 10))
	case colUser:
		return r.user(e)
	case colGroup:
		return r.group(e)
	case colSize:
		return r.size(e)
	case colTime:
		return r.when(e.ModTime)
	case colGit:
		return r.gitLong(e.Git)
	case colBar:
		return r.bar(e)
	case colHeat:
		return r.heat(e)
	case colCommit:
		return r.commit(e)
	}
	return ""
}

// Long prints the detailed table view. Rows may carry tree prefixes.
func (r *Renderer) Long(w io.Writer, rows []Row) {
	cols := r.columns()
	n := len(cols)
	titles := colTitle
	if r.cfg.Octal {
		titles[colPerms] = "Mode"
	}
	r.measure(rows)
	names, nameW := r.names(rows)
	cells := make([][]string, len(rows))
	cw := make([][]int, len(rows))
	widths := make([]int, n)
	if r.cfg.Header {
		for j, c := range cols {
			widths[j] = len(titles[c])
		}
	}
	for i, row := range rows {
		if row.E == nil {
			continue
		}
		cells[i] = make([]string, n)
		cw[i] = make([]int, n)
		for j, c := range cols {
			s := r.cell(c, row.E)
			cells[i][j], cw[i][j] = s, width(s)
			widths[j] = max(widths[j], cw[i][j])
		}
	}

	const gap = "  "
	f := &flusher{w: w}
	if r.cfg.Header {
		st := r.fg[theme.Muted].Underline()
		for j, c := range cols {
			t := titles[c]
			pad := spaces(widths[j] - len(t))
			if colRight[c] {
				f.WriteString(pad + st.Render(t))
			} else {
				f.WriteString(st.Render(t) + pad)
			}
			f.WriteString(gap)
		}
		f.WriteString(st.Render("Name"))
		f.line()
	}

	indent := len(gap) * n
	for _, x := range widths {
		indent += x
	}
	for i, row := range rows {
		if row.E == nil {
			f.WriteString(spaces(indent) + r.faint.Render(row.Prefix) + r.errText(row.Err))
			f.line()
			continue
		}
		for j, c := range cols {
			pad := spaces(widths[j] - cw[i][j])
			if colRight[c] {
				f.WriteString(pad + cells[i][j])
			} else {
				f.WriteString(cells[i][j] + pad)
			}
			f.WriteString(gap)
		}
		f.WriteString(r.withPeek(names[i], nameW, row.E))
		f.line()
	}
	f.flush()
}
