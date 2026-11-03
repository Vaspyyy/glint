package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Vaspyyy/glint/internal/dash"
	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

// Dashboard draws one row per project: what it is, its git state, how
// active it's been and how big its source is.
func (r *Renderer) Dashboard(w io.Writer, dir *fsx.Entry, rows []*dash.Row) {
	f := &flusher{w: w}
	head := r.Title(dir, nil)
	repos, dirty, active := 0, 0, 0
	var total int64
	for _, row := range rows {
		total += row.Size
		if row.Repo != nil {
			repos++
			if row.Changes > 0 || row.Repo.NoCommits {
				dirty++
			}
		}
		if r.cfg.Now.Sub(row.Active()) < 7*24*time.Hour {
			active++
		}
	}
	f.WriteString(head + r.faint.Render(fmt.Sprintf("  ·  %d folders", len(rows))))
	f.line()
	f.line()

	nameW := 26
	const (
		stackW = 13
		brW    = 16
		stateW = 13
		ageW   = 4
		sparkW = dash.Weeks
		sizeW  = 8
		gap    = 2
	)
	// drop columns, least useful first, until a row fits (one column of
	// slack: writing into the last column makes some terminals wrap)
	avail := r.cfg.Width - 1
	showStack, showBranch, showSpark := true, true, true
	used := nameW + gap + stackW + gap + brW + gap + stateW + gap + ageW + gap + sparkW + gap + sizeW
	if used > avail {
		showBranch, used = false, used-brW-gap
	}
	if used > avail {
		showSpark, used = false, used-sparkW-gap
	}
	if used > avail {
		showStack, used = false, used-stackW-gap
	}
	if used > avail { // last resort: shorter project names
		cut := min(used-avail, nameW-12)
		nameW, used = nameW-cut, used-cut
	}
	subjW := avail - used - 1
	showSubj := subjW >= 14

	hdr := r.fg[theme.Muted].Underline()
	cell := func(s string, wd int) string { return textw.Pad(s, wd) }
	sp := spaces(gap)
	f.WriteString(cell(hdr.Render("Project"), nameW) + sp)
	if showStack {
		f.WriteString(cell(hdr.Render("Stack"), stackW) + sp)
	}
	if showBranch {
		f.WriteString(cell(hdr.Render("Branch"), brW) + sp)
	}
	f.WriteString(cell(hdr.Render("State"), stateW) + sp)
	if showSubj {
		f.WriteString(cell(hdr.Render("Last commit"), ageW+1+subjW))
	} else {
		f.WriteString(cell(hdr.Render("Last"), ageW))
	}
	f.WriteString(sp)
	if showSpark {
		f.WriteString(cell(hdr.Render("12 weeks"), sparkW) + sp)
	}
	f.WriteString(spaces(sizeW-4) + hdr.Render("Size"))
	f.line()

	for _, row := range rows {
		loose := row.Repo == nil && row.Info == nil
		// project name with its stack icon
		icon, color := "\ue5ff", theme.Blue
		if row.Info != nil {
			icon, color = row.Info.Glyph, row.Info.Color
		}
		name := row.Name
		nameSt := r.fg[theme.Text].Bold()
		if loose {
			nameSt = r.fg[theme.Subtle]
		}
		nameCell := nameSt.Render(name)
		if r.cfg.Icons {
			nameCell = r.fg[color].Render(icon) + " " + nameCell
		}
		f.WriteString(cell(nameCell, nameW) + sp)

		if showStack {
			stack := ""
			if row.Info != nil {
				stack = r.fg[row.Info.Color].Render(row.Info.Stack)
			}
			f.WriteString(cell(stack, stackW) + sp)
		}

		if showBranch {
			br := ""
			if row.Repo != nil {
				br = r.fg[theme.Purple].Render(row.Repo.Branch)
				if row.Repo.Ahead > 0 {
					br = textw.Truncate(br, brW-3) + r.fg[theme.Green].Render(fmt.Sprintf("↑%d", row.Repo.Ahead))
				}
			}
			f.WriteString(cell(br, brW) + sp)
		}

		var state string
		switch {
		case row.Repo == nil && row.Empty:
			state = r.faint.Italic().Render("empty")
		case row.Repo == nil:
			state = r.faint.Render("no git")
		case row.Repo.NoCommits:
			state = r.fg[theme.Red].Render("✱ no commits")
		case row.Changes == 0:
			state = r.fg[theme.Green].Render("✓ clean")
		default:
			state = r.fg[theme.Yellow].Render("● " + strconv.Itoa(row.Changes) + " changed")
		}
		f.WriteString(cell(state, stateW) + sp)

		ago := ""
		subject := ""
		if row.Last != nil {
			d := r.cfg.Now.Sub(row.Last.Time)
			ago = r.ageStyle(d).Render(strings.TrimSpace(shortAge(d.Hours())))
			subject = row.Last.Subject
		} else if !row.Newest.IsZero() {
			d := r.cfg.Now.Sub(row.Newest)
			ago = r.ageStyle(d).Faint().Render(strings.TrimSpace(shortAge(d.Hours())))
			subject = "newest file"
		}
		f.WriteString(cell(ago, ageW))
		if showSubj {
			f.WriteString(" " + cell(r.faint.Render(textw.Truncate(subject, subjW)), subjW))
		}
		f.WriteString(sp)
		if showSpark {
			f.WriteString(r.spark(row.Weekly) + sp)
		}

		size := ""
		if row.Files > 0 {
			size = r.sizeText(row.Size)
		}
		f.WriteString(spaces(sizeW-textw.Width(size)) + size)
		f.line()
	}

	f.line()
	dot := r.faint.Render(" · ")
	f.WriteString(textw.Truncate(r.fg[theme.Blue].Bold().Render(strconv.Itoa(repos))+r.faint.Render(" repos")+dot+
		r.fg[theme.Yellow].Bold().Render(strconv.Itoa(dirty))+r.faint.Render(" with uncommitted work")+dot+
		r.fg[theme.Green].Bold().Render(strconv.Itoa(active))+r.faint.Render(" active this week")+dot+
		r.sizeText(total)+r.faint.Render(" of source"), avail))
	f.line()
	f.flush()
}

// spark draws weekly commit counts as a tiny bar chart.
func (r *Renderer) spark(weeks []int) string {
	if len(weeks) == 0 {
		return r.faint.Render(strings.Repeat("·", dash.Weeks))
	}
	peak := 0
	for _, v := range weeks {
		peak = max(peak, v)
	}
	var b strings.Builder
	for _, v := range weeks {
		if v == 0 {
			b.WriteString(r.faint.Render("·"))
			continue
		}
		frac := float64(v) / float64(peak)
		idx := min(int(frac*float64(len(sparks)-1)+0.5), len(sparks)-1)
		b.WriteString(r.p.Hex(r.p.Blend(theme.Teal, theme.Green, frac)).Render(string(sparks[idx])))
	}
	return b.String()
}
