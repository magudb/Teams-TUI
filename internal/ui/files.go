package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/model"
)

// Files: sidebar lists locations, the main pane is a file table with a
// detail strip for the selected file.

func (m *Model) openLocation(key string) tea.Cmd {
	m.sideSel[SecFiles] = key
	m.sideTouched[SecFiles] = true
	if key == m.fileLoc && len(m.fileStack) == 0 && m.files != nil {
		return nil
	}
	m.fileLoc = key
	m.fileStack = nil
	m.files = nil
	m.fileSel = 0
	m.fileFilter = ""
	return m.loadFiles()
}

func (m *Model) locationTitle() string {
	switch {
	case m.fileLoc == "loc:root":
		return "My files"
	case m.fileLoc == "loc:shared":
		return "Shared with me"
	case strings.HasPrefix(m.fileLoc, "loc:chan:"):
		parts := strings.SplitN(strings.TrimPrefix(m.fileLoc, "loc:chan:"), ":", 2)
		if len(parts) == 2 {
			return m.convTitle(model.ConvRef{Kind: model.KindChannel, TeamID: parts[0], ChannelID: parts[1]})
		}
	}
	return "Recent"
}

// visibleFiles applies the name filter.
func (m *Model) visibleFiles() []model.DriveItem {
	q := strings.ToLower(strings.TrimSpace(m.fileFilter))
	if q == "" {
		return m.files
	}
	var out []model.DriveItem
	for _, f := range m.files {
		if strings.Contains(strings.ToLower(f.Name), q) {
			out = append(out, f)
		}
	}
	return out
}

func (m *Model) selectedFile() *model.DriveItem {
	fs := m.visibleFiles()
	if m.fileSel < 0 || m.fileSel >= len(fs) {
		return nil
	}
	f := fs[m.fileSel]
	return &f
}

func (m *Model) fileIcon(f model.DriveItem) string {
	if f.IsFolder {
		return m.s.ic.Folder
	}
	return m.s.ic.File
}

// fileColumns returns the widths of Name, Modified, By and Size.
func fileColumns(w int) (name, mod, by, size int) {
	size, mod, by = 10, 10, 18
	if w < 70 {
		by = 0
	}
	name = w - 4 - mod - by - size
	return
}

func (m *Model) fileRow(f model.DriveItem, w int, header bool) string {
	nw, mw, bw, sw := fileColumns(w)
	if header {
		row := padRight("Name", nw) + padRight("Modified", mw)
		if bw > 0 {
			row += padRight("By", bw)
		}
		return row + padRight("Size", sw)
	}
	size := humanSize(f.Size)
	if f.IsFolder {
		size = itoa(f.ChildCount) + " items"
	}
	row := padRight(m.fileIcon(f)+" "+f.Name, nw) + padRight(relTime(f.Modified, m.cfg.TimeFormat), mw)
	if bw > 0 {
		row += padRight(f.ModifiedBy, bw)
	}
	return row + padRight(size, sw)
}

func (m *Model) filesView(w, h int) []string {
	s := m.s
	bar := lipgloss.NewStyle().Background(s.t.Block).Foreground(s.t.Text)
	title := strings.ToUpper(m.locationTitle())
	if len(m.fileStack) > 0 {
		var names []string
		for _, f := range m.fileStack {
			names = append(names, f.Name)
		}
		title += s.Muted.Render(" › " + strings.Join(names, " › "))
	}
	filter := "/ filter"
	if m.fileFilter != "" {
		filter = "/ " + m.fileFilter
	}
	lines := []string{withBg(spread(" "+s.Title.Render(title), zone.Mark("hdr:filesfilter", s.Button.Render(filter))+" ", w), styleOn(bar), w), ""}
	lines = append(lines, "  "+s.Muted.Bold(true).Render(m.fileRow(model.DriveItem{}, w, true)))

	detailH := 3
	listH := h - len(lines) - detailH - 1
	files := m.visibleFiles()
	if m.files == nil {
		lines = append(lines, "  "+s.Muted.Render(m.spinner.View()+" Loading files…"))
	} else if len(files) == 0 {
		lines = append(lines, "  "+s.Muted.Render("Nothing here yet."))
	}
	start := max(0, m.fileSel-listH+1)
	for i := start; i < len(files) && i < start+listH; i++ {
		row := m.fileRow(files[i], w, false)
		line := "  " + row
		if i == m.fileSel {
			st := s.Sel
			if m.focus != FocusMessages {
				st = s.SelDim
			}
			line = withBg(st.Render("  "+row), bgPrefix(st.GetBackground()), w)
		}
		lines = append(lines, zone.Mark("file:row:"+itoa(i), line))
	}
	lines = padLines(lines, h-detailH)

	// detail strip
	var detail []string
	if f := m.selectedFile(); f != nil {
		where := m.locationTitle()
		for _, p := range m.fileStack {
			where += " › " + p.Name
		}
		detail = append(detail, " "+s.Bold.Render(trunc(f.Name, w/2))+" "+s.Muted.Render(trunc(where, w/2-2)))
		meta := "Modified " + dayLabelShort(f.Modified) + " " + f.Modified.Format(m.cfg.TimeFormat)
		if f.ModifiedBy != "" {
			meta += " by " + f.ModifiedBy
		}
		if !f.IsFolder {
			meta += " · " + humanSize(f.Size)
		}
		detail = append(detail, " "+s.Muted.Render(trunc(meta, w-2)))
		if f.IsFolder {
			detail = append(detail, " "+zone.Mark("file:enter", s.ButtonHot.Render(" "+s.ic.Folder+" Open folder ")))
		} else {
			detail = append(detail, " "+zone.Mark("file:open", s.ButtonHot.Render(" "+s.ic.External+" Open "))+"  "+
				zone.Mark("file:download", s.Button.Render(s.ic.Download+" d Download"))+"  "+
				zone.Mark("file:copy", s.Button.Render("y Copy link")))
		}
	}
	return append(lines, paint(padLines(detail, detailH), w, s.bgSide)...)
}

func dayLabelShort(t time.Time) string {
	switch l := dayLabel(t); l {
	case "Today", "Yesterday", "Tomorrow":
		return strings.ToLower(l)
	}
	return t.Format("2 Jan")
}

// filesTabLines is the Files tab of a channel.
func (m *Model) filesTabLines(w int) []string {
	s := m.s
	lines := []string{"", "  " + s.Muted.Bold(true).Render(m.fileRow(model.DriveItem{}, w, true))}
	if m.tabFiles == nil {
		return append(lines, "  "+s.Muted.Render("Loading files…"))
	}
	if len(m.tabFiles) == 0 {
		return append(lines, "  "+s.Muted.Render("No files in this channel yet."))
	}
	for i, f := range m.tabFiles {
		lines = append(lines, zone.Mark("tabfile:"+itoa(i), "  "+m.fileRow(f, w, false)))
	}
	return append(lines, "", "  "+s.Muted.Render("click a file to open it in the browser"))
}

func (m *Model) uploadPrompt() {
	m.openPrompt("Upload file to "+m.locationTitle(), "Path to file, e.g. ~/Documents/report.pdf", "", func(m *Model, p string) tea.Cmd {
		p = expandHome(p)
		if strings.TrimSpace(p) == "" {
			return nil
		}
		var folder *model.DriveItem
		if len(m.fileStack) > 0 {
			f := m.fileStack[len(m.fileStack)-1]
			folder = &f
		}
		loc := m.fileLoc
		return m.op(func(svc model.Service) opMsg {
			c, cancel := ctx()
			defer cancel()
			var err error
			if strings.HasPrefix(loc, "loc:chan:") && folder == nil {
				parts := strings.SplitN(strings.TrimPrefix(loc, "loc:chan:"), ":", 2)
				_, err = svc.Upload(c, model.ConvRef{Kind: model.KindChannel, TeamID: parts[0], ChannelID: parts[1]}, p)
			} else {
				_, err = svc.UploadFile(c, folder, p)
			}
			return opMsg{err: err, toast: "Uploaded", reloadFiles: true}
		})
	})
}
