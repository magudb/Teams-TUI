package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
)

type rect struct{ x, y, w, h int }

func (r rect) in(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

type geometry struct {
	rail, sideList, main, msgs, threadMsgs, compose rect
	sideOwner                                       []int
	sideItems                                       []sideItem
	msgsConv, threadConv                            *convView
}

func lipglossColor(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func lipglossBorder(c lipgloss.TerminalColor) lipgloss.Style {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c)
}

func (m *Model) railWidth() int {
	if m.w < 100 {
		return 4
	}
	return 10
}

func (m *Model) sidebarWidth() int {
	if !m.showSidebar {
		return 0
	}
	w := m.cfg.SidebarWidth
	if m.w < 100 {
		w = min(w, 28)
	}
	return max(0, min(w, m.w-m.railWidth()-30))
}

func (m *Model) mainX() int {
	x := m.railWidth()
	if sw := m.sidebarWidth(); sw > 0 {
		x += sw + 1
	}
	return x
}

func (m *Model) mainWidth() int { return max(10, m.w-m.mainX()) }

func (m *Model) splitThread() bool { return m.thread != nil && m.mainWidth() >= 110 }

func (m *Model) threadWidth() int { return m.mainWidth() * 45 / 100 }

func (m *Model) composeWidth() int {
	w := m.mainWidth()
	if m.splitThread() {
		w = m.threadWidth()
	}
	return max(10, w-4)
}

func (m *Model) invalidateRender() {
	for _, cv := range m.convs {
		cv.dirty = true
	}
}

// View renders the UI.
func (m *Model) View() string {
	if !m.ready {
		return "Loading…"
	}
	if m.w < 50 || m.h < 12 {
		return "Terminal too small — please resize (min 50×12)."
	}
	m.compose.resize()
	bodyH := m.h - 1
	var cols []string

	cols = append(cols, m.railView(bodyH))
	if sw := m.sidebarWidth(); sw > 0 {
		cols = append(cols, m.sidebarView(sw, bodyH))
		sep := strings.TrimSuffix(strings.Repeat(m.s.Border.Render("│")+"\n", bodyH), "\n")
		cols = append(cols, sep)
	}
	cols = append(cols, m.mainView(m.mainWidth(), bodyH))
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	out := body + "\n" + m.statusView()
	if m.modal != nil {
		out = overlay(out, m.modalView(), m.w, m.h)
	}
	return zone.Scan(out)
}

func (m *Model) railView(h int) string {
	s := m.s
	w := m.railWidth()
	narrow := w < 10
	var lines []string
	lines = append(lines, "")
	m.geo.rail = rect{0, 0, w, h}
	for i := Section(0); i < numSections; i++ {
		if i == SecTeams && m.cfg.CombinedChat {
			continue
		}
		icon := sectionIcons[i]
		if m.cfg.ASCIIIcons {
			icon = sectionASCII[i]
		}
		badge := ""
		switch i {
		case SecChat:
			if n := m.unreadCount(); n > 0 {
				badge = s.Badge.Render(itoa(min(n, 99)))
			}
		case SecActivity:
			if n := m.unreadActivity(); n > 0 {
				badge = s.Badge.Render(itoa(min(n, 99)))
			}
		}
		active := i == m.section
		var l1, l2 string
		if narrow {
			l1 = " " + icon + badge
		} else {
			l1 = centerIn(icon+badge, w)
			l2 = centerIn(sectionNames[i], w)
		}
		st := s.Muted
		if active {
			st = s.Accent.Bold(true)
			l1 = s.Accent.Render("▌") + ansi.TruncateLeft(padRight(l1, w), 1, "")
			if !narrow {
				l2 = s.Accent.Render("▌") + ansi.TruncateLeft(padRight(st.Render(l2), w), 1, "")
			}
		} else if !narrow {
			l2 = st.Render(l2)
		}
		block := []string{padRight(l1, w)}
		if !narrow {
			block = append(block, padRight(l2, w))
		}
		block = append(block, "")
		for j, l := range block {
			block[j] = padRight(l, w)
		}
		lines = append(lines, strings.Split(zone.Mark("rail:"+itoa(int(i)), strings.Join(block, "\n")), "\n")...)
	}
	// bottom: my avatar with presence and help
	for len(lines) < h-4 {
		lines = append(lines, "")
	}
	help := "?"
	if !narrow {
		help = "? Help"
	}
	lines = append(lines[:h-4], zone.Mark("rail:help", padRight(centerIn(s.Muted.Render(help), w), w)), "")
	me := s.presenceDot(m.myPresence, m.myPresence.Availability != "") + " " + s.ChipMine.Render(initials(m.me.DisplayName))
	if narrow {
		me = s.presenceDot(m.myPresence, m.myPresence.Availability != "")
	}
	lines = append(lines, zone.Mark("rail:me", padRight(centerIn(me, w), w)), "")
	return box(lines, w, h)
}

func centerIn(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return trunc(s, w)
	}
	return strings.Repeat(" ", (w-sw)/2) + s
}

func (m *Model) sidebarView(w, h int) string {
	hdr := m.sidebarHeader(w)
	listH := h - len(hdr)
	lay := m.layoutSidebar(w, listH)
	m.geo.sideList = rect{m.railWidth(), len(hdr), w, listH}
	m.geo.sideOwner = lay.owner
	m.geo.sideItems = lay.items
	lines := append(hdr, lay.lines...)
	return box(lines, w, h)
}

func (m *Model) mainView(w, h int) string {
	x := m.mainX()
	m.geo.main = rect{x, 0, w, h}
	m.geo.msgs, m.geo.threadMsgs, m.geo.compose = rect{}, rect{}, rect{}
	m.geo.msgsConv, m.geo.threadConv = nil, nil
	switch {
	case m.section == SecCalendar:
		return m.calendarView(w, h)
	case m.section == SecFiles && m.conv == nil:
		return m.fileDetailView(w, h)
	case m.conv == nil:
		return m.welcomeView(w, h)
	}
	if m.splitThread() {
		tw := m.threadWidth()
		cw := w - tw - 1
		left := m.convPane(m.conv, x, cw, h, false)
		sep := strings.TrimSuffix(strings.Repeat(m.s.Border.Render("│")+"\n", h), "\n")
		right := m.convPane(m.thread, x+cw+1, tw, h, true)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
	}
	return m.convPane(m.activeConv(), x, w, h, true)
}

func (m *Model) convHeader(cv *convView, w int) []string {
	s := m.s
	btn := func(id, label string) string { return zone.Mark("hdr:"+id, s.Button.Render(label)) }
	var title, sub, right string
	switch cv.ref.Kind {
	case model.KindChat:
		c := m.chatByID(cv.ref.ChatID)
		if c == nil {
			title = " " + s.Title.Render(m.convTitle(cv.ref))
			sub = "Meeting chat"
			break
		}
		t := c.Title(m.me.ID)
		if o, ok := c.Other(m.me.ID); ok {
			p, known := m.presence[o.ID]
			title = " " + s.presenceDot(p, known) + " " + s.Title.Render(t)
			if known {
				sub = presenceLabel(p)
				if p.StatusMessage != "" {
					sub += " · “" + p.StatusMessage + "”"
				}
			}
			right = btn("call", "📞 Call") + " " + btn("video", "📹 Video") + " " + btn("more", "⋯") + " "
		} else {
			title = " " + s.Title.Render(t)
			sub = itoa(len(c.Members)) + " people"
			right = btn("people", "👥 "+itoa(len(c.Members))) + " " + btn("call", "📞 Call") + " " + btn("more", "⋯") + " "
			if c.JoinURL != "" {
				right = btn("join", "📹 Join") + " " + btn("people", "👥 "+itoa(len(c.Members))) + " " + btn("more", "⋯") + " "
				if e := m.eventForChat(c.ID); e != nil {
					sub = e.Start.Format("Mon 2 Jan "+m.cfg.TimeFormat) + " · " + sub
					if e.Live(time.Now()) {
						sub = "● Live now · " + sub
					}
				}
			}
		}
		if m.st.Muted["chat:"+c.ID] {
			sub += " · 🔕 muted"
		}
	case model.KindChannel:
		if cv.ref.ThreadID != "" {
			chName := "channel"
			for _, ch := range m.channels[cv.ref.TeamID] {
				if ch.ID == cv.ref.ChannelID {
					chName = "# " + ch.DisplayName
				}
			}
			title = " " + btn("back", "← "+chName) + "  " + s.Title.Render("Thread")
			sub = "Replies to "
			for _, x := range cv.msgs {
				if x.ID == cv.ref.ThreadID {
					what := x.Subject
					if what == "" {
						what = "“" + trunc(plainOneLine(htmltext.Plain(x.Body)), 40) + "”"
					}
					sub += m.displayName(&x) + "'s post " + what
				}
			}
			sub += " · esc to go back"
			right = btn("closethread", "✕") + " "
		} else {
			title = " " + s.Title.Render(m.convTitle(cv.ref))
			for _, ch := range m.channels[cv.ref.TeamID] {
				if ch.ID == cv.ref.ChannelID && ch.Description != "" {
					sub = ch.Description
				}
			}
			right = btn("meet", "📹 Meet") + " " + btn("more", "⋯") + " "
		}
	}
	line1 := spread(title, right, w)
	var line2 string
	if cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" {
		tab := func(i int, label string) string {
			st := s.Tab
			if m.mainTab == i {
				st = s.TabActive
			}
			return zone.Mark("tab:"+itoa(i), st.Render(label))
		}
		line2 = " " + tab(0, "Posts") + "   " + tab(1, "Files")
		if sub != "" {
			line2 += "   " + s.Faint.Render(trunc(sub, w-30))
		}
	} else {
		line2 = " " + s.Muted.Render(trunc(sub, w-2))
	}
	return []string{line1, line2, s.Border.Render(strings.Repeat("─", w))}
}

// convPane renders header + messages (+ compose if withCompose) for cv.
func (m *Model) convPane(cv *convView, x, w, h int, withCompose bool) string {
	hdr := m.convHeader(cv, w)
	var compose string
	ch := 0
	if withCompose && !(cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" && m.mainTab == 1) {
		compose = m.composeView(w)
		ch = lipgloss.Height(compose)
	}
	msgH := max(1, h-len(hdr)-ch)
	var body []string
	if cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" && m.mainTab == 1 {
		body = m.filesTabLines(w, h-len(hdr))
		msgH = h - len(hdr)
	} else {
		m.renderConv(cv, w-1)
		if m.focus == FocusMessages && cv == m.activeConv() {
			cv.ensureVisible(msgH)
		}
		body = cv.view(msgH)
		r := rect{x, len(hdr), w, msgH}
		if cv == m.thread && m.splitThread() {
			m.geo.threadMsgs, m.geo.threadConv = r, cv
		} else {
			m.geo.msgs, m.geo.msgsConv = r, cv
		}
		// scroll indicator
		if total := len(cv.lines); total > msgH && msgH > 2 {
			pos := cv.scroll * (msgH - 1) / max(1, total-msgH)
			for i := range body {
				body[i] = padRight(body[i], w-1)
			}
			for len(body) < msgH {
				body = append(body, strings.Repeat(" ", w-1))
			}
			for i := range body {
				mark := m.s.Border.Render("│")
				if i == pos {
					mark = m.s.Muted.Render("┃")
				}
				body[i] += mark
			}
		}
	}
	lines := append([]string{}, hdr...)
	lines = append(lines, padLines(body, msgH)...)
	if compose != "" {
		m.geo.compose = rect{x, len(hdr) + msgH, w, ch}
		lines = append(lines, strings.Split(compose, "\n")...)
	}
	return box(lines, w, h)
}

func padLines(l []string, n int) []string {
	for len(l) < n {
		l = append(l, "")
	}
	return l[:n]
}

func (m *Model) filesTabLines(w, h int) []string {
	s := m.s
	lines := []string{""}
	if m.tabFiles == nil {
		return append(lines, s.Muted.Render("  Loading files…"))
	}
	if len(m.tabFiles) == 0 {
		return append(lines, s.Muted.Render("  No files in this channel yet."))
	}
	lines = append(lines, s.Faint.Render(padRight("  Name", w-30)+padRight("Modified", 14)+"Size"))
	for i, f := range m.tabFiles {
		size := ""
		if !f.IsFolder {
			size = humanSize(f.Size)
		}
		l := padRight("  "+fileIcon(f)+" "+f.Name, w-30) + padRight(relTime(f.Modified, m.cfg.TimeFormat), 14) + size
		lines = append(lines, zone.Mark("tabfile:"+itoa(i), padRight(l, w)))
	}
	lines = append(lines, "", s.Faint.Render("  click a file to open it in the browser · d on selection downloads"))
	return lines
}

func (m *Model) welcomeView(w, h int) string {
	s := m.s
	logo := []string{
		"  ████████╗",
		"  ╚══██╔══╝",
		"     ██║   ",
		"     ██║   ",
		"     ╚═╝   ",
	}
	var lines []string
	for range max(0, h/4) {
		lines = append(lines, "")
	}
	for _, l := range logo {
		lines = append(lines, centerIn(s.Accent.Render(l), w))
	}
	name := m.me.DisplayName
	if name == "" {
		name = "there"
	}
	lines = append(lines, "", centerIn(s.Title.Render("Welcome, "+name), w), "")
	tips := []string{
		"alt+1…6     switch Activity / Chat / Teams / Calendar / Files / Search",
		"tab         cycle focus: sidebar → messages → compose",
		"ctrl+n      new chat          ctrl+k   command palette",
		"ctrl+e or / search            ?        all shortcuts",
		"mouse       click anything, scroll with the wheel",
	}
	for _, t := range tips {
		lines = append(lines, centerIn(s.Muted.Render(t), w))
	}
	return box(lines, w, h)
}

func (m *Model) calendarView(w, h int) string {
	s := m.s
	var lines []string
	// week strip
	monday := m.calDay.AddDate(0, 0, -((int(m.calDay.Weekday()) + 6) % 7))
	var cells []string
	cw := max(6, (w-2)/7)
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i)
		n := 0
		for _, e := range m.events {
			if overlapsDay(e, d) && !e.IsCancelled {
				n++
			}
		}
		label := d.Format("Mon 02")
		if n > 0 {
			label += " ·" + itoa(n)
		}
		st := s.Muted
		if sameDay(d, time.Now()) {
			st = s.Accent
		}
		if sameDay(d, m.calDay) {
			st = s.ButtonHot
		}
		cells = append(cells, zone.Mark("day:"+itoa(i), st.Render(padRight(" "+label, cw-1))))
	}
	lines = append(lines, " "+s.Title.Render(m.calDay.Format("January 2006"))+"   "+s.Faint.Render("[ ] prev/next day · t today · n new meeting"))
	lines = append(lines, " "+strings.Join(cells, " "))
	lines = append(lines, s.Border.Render(strings.Repeat("─", w)))

	e := m.selectedEvent()
	if e == nil {
		lines = append(lines, "", s.Muted.Render("  Select a meeting in the list to see details."))
		return box(lines, w, h)
	}
	lines = append(lines, m.eventDetailLines(e, w)...)
	return box(lines, w, h)
}

func (m *Model) fileDetailView(w, h int) string {
	s := m.s
	f := m.selectedFile()
	if f == nil {
		return m.welcomeView(w, h)
	}
	lines := []string{"", " " + fileIcon(*f) + " " + s.Title.Render(trunc(f.Name, w-6)), ""}
	if !f.IsFolder {
		lines = append(lines, " "+s.Muted.Render("Size: "+humanSize(f.Size)))
	} else {
		lines = append(lines, " "+s.Muted.Render("Folder · "+itoa(f.ChildCount)+" items"))
	}
	lines = append(lines, " "+s.Muted.Render("Modified: "+f.Modified.Format("2 Jan 2006 15:04")))
	if f.ModifiedBy != "" {
		lines = append(lines, " "+s.Muted.Render("By: "+f.ModifiedBy))
	}
	lines = append(lines, "")
	btn := func(id, label string, hot bool) string {
		st := s.Button
		if hot {
			st = s.ButtonHot
		}
		return zone.Mark("file:"+id, st.Render(label))
	}
	if f.IsFolder {
		lines = append(lines, " "+btn("enter", " Open folder ", true))
	} else {
		lines = append(lines, " "+btn("open", " ↗ Open ", true)+" "+btn("download", "[⤓ Download]", false)+" "+btn("copy", "[⧉ Copy link]", false))
	}
	lines = append(lines, "", s.Faint.Render(" enter open · d download · y copy link · backspace up"))
	return box(lines, w, h)
}

func (m *Model) statusView() string {
	s := m.s
	me := " " + s.presenceDot(m.myPresence, m.myPresence.Availability != "") + " " + presenceLabel(m.myPresence)
	if m.myPresence.StatusMessage != "" {
		me += s.Faint.Render(" · " + trunc(m.myPresence.StatusMessage, 30))
	}
	me = zone.Mark("status:me", me)
	if b := m.meetingBanner(); b != "" {
		me += "  " + b
	}
	var mid string
	switch {
	case m.toast != "":
		if m.toastErr {
			mid = s.Danger.Render("⚠ " + m.toast)
		} else {
			mid = s.Accent.Render(m.toast)
		}
	default:
		mid = s.Faint.Render(m.hints())
	}
	right := ""
	if m.busy > 0 {
		right += m.spinner.View() + " "
	}
	if m.demo {
		right += s.Warn.Render("DEMO") + " "
	}
	right += s.Faint.Render(time.Now().Format(m.cfg.TimeFormat)) + " "
	leftW := ansi.StringWidth(me)
	midW := m.w - leftW - ansi.StringWidth(right) - 4
	line := me + "  " + padRight(mid, max(0, midW)) + "  " + right
	return padRight(line, m.w)
}

func (m *Model) hints() string {
	if m.modal != nil {
		return "esc close"
	}
	switch m.focus {
	case FocusCompose:
		return "enter send · alt+enter newline · @ mention · /attach · ↑ edit last · esc back"
	case FocusMessages:
		return "↑↓ select · r reply · + react · 1-6 quick react · e edit · d delete · o open · y copy · tab compose"
	}
	switch m.section {
	case SecChat:
		return "↑↓ move · enter open · ctrl+n new · / filter · p pin · m mute · . actions · ? help"
	case SecTeams:
		return "↑↓ move · enter open/expand · ? help"
	case SecCalendar:
		return "↑↓ move · [ ] day · j join · a accept · n new meeting"
	case SecFiles:
		return "↑↓ move · enter open · d download · backspace up · r recent/my files"
	case SecSearch:
		return "type query + enter · ↑↓ results · enter open"
	case SecActivity:
		return "↑↓ move · enter open · ? help"
	}
	return "? help"
}
