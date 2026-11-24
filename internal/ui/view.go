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

// Layout follows the "Blocks" design: a dark icon rail, a sidebar on a
// darker paper fill with an inverse header bar, the main pane on paper with a
// block-filled header, and a powerline status bar.

type rect struct{ x, y, w, h int }

func (r rect) in(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

type geometry struct {
	rail, sideList, main, msgs, threadMsgs, compose rect
	sideOwner                                       []int
	sideItems                                       []sideItem
	msgsConv, threadConv                            *convView
}

func (m *Model) narrow() bool { return m.w < 100 }

func (m *Model) railWidth() int {
	if m.narrow() {
		return 4
	}
	return 6
}

func (m *Model) sidebarWidth() int {
	if !m.showSidebar || m.meetingMode() {
		return 0
	}
	w := m.cfg.SidebarWidth
	if m.narrow() {
		w = min(w, 28)
	}
	return max(0, min(w, m.w-m.railWidth()-30))
}

func (m *Model) mainX() int { return m.railWidth() + m.sidebarWidth() }

func (m *Model) mainWidth() int { return max(10, m.w-m.mainX()) }

// A thread opens beside the channel at 100+ columns, and replaces it below.
func (m *Model) splitThread() bool { return m.thread != nil && !m.narrow() && m.mainWidth() >= 70 }

func (m *Model) threadWidth() int { return m.mainWidth() * 45 / 100 }

func (m *Model) composeWidth() int {
	w := m.mainWidth()
	if m.splitThread() {
		w = m.threadWidth()
	} else if m.meetingMode() {
		w = m.mainWidth() - m.peopleWidth()
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
	cols := []string{m.railView(bodyH)}
	if sw := m.sidebarWidth(); sw > 0 {
		cols = append(cols, m.sidebarView(sw, bodyH))
	} else {
		m.geo.sideList = rect{}
	}
	cols = append(cols, m.mainView(m.mainWidth(), bodyH))
	out := lipgloss.JoinHorizontal(lipgloss.Top, cols...) + "\n" + m.statusView()
	switch {
	case m.alertVisible():
		out = overlay(dim(out, m.s), m.alertView(), m.w, m.h)
	case m.modal != nil:
		out = overlay(dim(out, m.s), m.modalView(), m.w, m.h)
	}
	return zone.Scan(out)
}

// dim fades the background behind a modal.
func dim(s string, st styles) string {
	lines := strings.Split(s, "\n")
	fade := lipgloss.NewStyle().Foreground(st.t.Faint)
	for i, l := range lines {
		lines[i] = withBg(fade.Render(ansi.Strip(l)), st.bgPaper, ansi.StringWidth(ansi.Strip(l)))
	}
	return strings.Join(lines, "\n")
}

// ---- rail -------------------------------------------------------------------------

func (m *Model) sectionIcon(sec Section) string {
	ic := m.s.ic
	return [...]string{ic.Bell, ic.Chat, ic.Teams, ic.Calendar, ic.Files, ic.Search, ic.Settings}[sec]
}

func (m *Model) railView(h int) string {
	s := m.s
	w := m.railWidth()
	m.geo.rail = rect{0, 0, w, h}
	rail := lipgloss.NewStyle().Foreground(s.t.RailFg)
	active := lipgloss.NewStyle().Foreground(s.t.OnAccent).Background(s.t.Accent)
	lines := make([]string, h)
	row := func(y int, id, content string, on bool) {
		if y < 0 || y >= h {
			return
		}
		l := centerIn(content, w)
		if on {
			lines[y] = zone.Mark(id, active.Render(padRight(l, w)))
		} else {
			lines[y] = zone.Mark(id, padRight(l, w))
		}
	}
	y := 1
	for sec := Section(0); sec < SecSettings; sec++ {
		icon := rail.Render(m.sectionIcon(sec))
		if sec == m.section {
			icon = m.sectionIcon(sec)
		}
		if !m.narrow() {
			n := 0
			switch sec {
			case SecActivity:
				n = m.unreadActivity()
			case SecChat:
				n = m.unreadCount()
			}
			if n > 0 {
				st := lipgloss.NewStyle().Foreground(s.t.RailBadge).Bold(true)
				if sec == m.section {
					st = lipgloss.NewStyle().Foreground(s.t.OnAccent).Background(s.t.Accent).Bold(true)
				}
				icon += st.Render(itoa(min(n, 99)))
			}
		}
		row(y, "rail:"+itoa(int(sec)), icon, sec == m.section)
		y += 2
	}
	row(h-6, "rail:help", rail.Render(m.s.ic.Help), false)
	gear := rail.Render(m.s.ic.Settings)
	if m.section == SecSettings {
		gear = m.s.ic.Settings
	}
	row(h-4, "rail:"+itoa(int(SecSettings)), gear, m.section == SecSettings)
	dot := lipgloss.NewStyle().Foreground(s.t.RailDot).Render("●")
	if m.myPresence.Availability != "" && m.myPresence.Availability != model.Available {
		dot = s.presenceDot(m.myPresence, true)
	}
	row(h-2, "rail:me", dot, false)
	return strings.Join(paint(lines, w, s.bgRail), "\n")
}

func centerIn(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return trunc(s, w)
	}
	return strings.Repeat(" ", (w-sw)/2) + s
}

// styleOn returns the escape prefix for st's foreground and background, so a
// fill survives the resets emitted by inner styled text.
func styleOn(st lipgloss.Style) string {
	out := lipgloss.NewStyle().Foreground(st.GetForeground()).Background(st.GetBackground()).Render("x")
	if i := strings.Index(out, "x"); i > 0 {
		return out[:i]
	}
	return ""
}

// headerBar renders a one-line bar: left text, right text, filled with st.
func headerBar(st lipgloss.Style, left, right string, w int) string {
	return withBg(spread(" "+left, right+" ", w), styleOn(st), w)
}

// ---- sidebar ----------------------------------------------------------------------

func (m *Model) sidebarView(w, h int) string {
	hdr := m.sidebarHeader(w)
	listH := h - len(hdr)
	lay := m.layoutSidebar(w, listH)
	m.geo.sideList = rect{m.railWidth(), len(hdr), w, listH}
	m.geo.sideOwner = lay.owner
	m.geo.sideItems = lay.items
	lines := append(hdr, lay.lines...)
	lines = padLines(lines, h)
	return strings.Join(paint(lines, w, m.s.bgSide), "\n")
}

// ---- main -------------------------------------------------------------------------

func (m *Model) mainView(w, h int) string {
	x := m.mainX()
	m.geo.main = rect{x, 0, w, h}
	m.geo.msgs, m.geo.threadMsgs, m.geo.compose = rect{}, rect{}, rect{}
	m.geo.msgsConv, m.geo.threadConv = nil, nil
	var lines []string
	switch {
	case m.meetingMode():
		return m.meetingView(x, w, h)
	case m.section == SecSettings:
		lines = m.settingsView(w, h)
	case m.section == SecCalendar:
		lines = m.calendarView(w, h)
	case m.section == SecFiles && m.conv == nil:
		lines = m.filesView(w, h)
	case m.conv == nil:
		lines = m.welcomeView(w, h)
	case m.splitThread():
		tw := m.threadWidth()
		cw := w - tw
		left := m.convPane(m.conv, x, cw, h, false, false)
		right := m.convPane(m.thread, x+cw, tw, h, true, true)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	default:
		return m.convPane(m.activeConv(), x, w, h, true, false)
	}
	return strings.Join(paint(padLines(lines, h), w, m.s.bgPaper), "\n")
}

func (m *Model) hdrBtn(id, label string) string {
	return zone.Mark("hdr:"+id, m.s.Button.Render(label))
}

// convHeader is the block-filled bar above a conversation.
func (m *Model) convHeader(cv *convView, w int, side bool) string {
	s := m.s
	ic := s.ic
	bar := lipgloss.NewStyle().Background(s.t.Block).Foreground(s.t.Text)
	sub := func(t string) string { return " " + s.Muted.Render(t) }
	var left, right string
	switch {
	case cv.ref.Kind == model.KindChannel && cv.ref.ThreadID != "":
		// thread pane (or full-width thread on narrow terminals)
		if side {
			return headerBar(s.Inverse, s.Inverse.Bold(true).Render("THREAD"), zone.Mark("hdr:closethread", "esc close"), w)
		}
		chName := "channel"
		for _, ch := range m.channels[cv.ref.TeamID] {
			if ch.ID == cv.ref.ChannelID {
				chName = ch.DisplayName
			}
		}
		left = m.hdrBtn("back", "← "+ic.Hash+" "+chName) + "  " + s.Title.Render("THREAD")
		right = zone.Mark("hdr:closethread", s.Button.Render("esc close"))
	case cv.ref.Kind == model.KindChannel:
		name := "channel"
		for _, ch := range m.channels[cv.ref.TeamID] {
			if ch.ID == cv.ref.ChannelID {
				name = ch.DisplayName
			}
		}
		left = ic.Hash + " " + s.Title.Render(strings.ToUpper(name))
		tab := func(i int, label string) string {
			st := s.Tab
			if m.mainTab == i {
				st = s.TabActive
			}
			return zone.Mark("tab:"+itoa(i), st.Render(" "+label+" "))
		}
		right = tab(0, "Posts") + tab(1, "Files")
	default:
		c := m.chatByID(cv.ref.ChatID)
		if c == nil {
			left = s.Title.Render(strings.ToUpper(m.convTitle(cv.ref))) + sub("meeting chat")
			break
		}
		t := strings.ToUpper(c.Title(m.me.ID))
		if o, ok := c.Other(m.me.ID); ok {
			p, known := m.presence[o.ID]
			info := ""
			if known {
				info = presenceLabel(p)
				if p.StatusMessage != "" {
					info += " · “" + p.StatusMessage + "”"
				}
			}
			left = s.Title.Render(t) + sub(info)
			right = m.hdrBtn("call", ic.Phone+" Call") + " " + m.hdrBtn("video", ic.Video+" Video") + " " + m.hdrBtn("more", ic.More)
		} else {
			info := itoa(len(c.Members)) + " people"
			if c.ChatType == "meeting" || c.JoinURL != "" {
				if e := m.eventForChat(c.ID); e != nil && e.Live(time.Now()) {
					info = "live now · " + info
				}
			}
			left = s.Title.Render(t) + sub(info)
			right = m.hdrBtn("people", ic.Users+" "+itoa(len(c.Members))) + " "
			if c.JoinURL != "" {
				right += m.hdrBtn("join", ic.Video+" Join") + " "
			} else {
				right += m.hdrBtn("call", ic.Phone+" Call") + " "
			}
			right += m.hdrBtn("more", ic.More)
		}
		if m.st.Muted["chat:"+c.ID] {
			left += sub("· muted")
		}
	}
	return withBg(spread(" "+left, right+" ", w), styleOn(bar), w)
}

// convPane renders header, messages and (optionally) the compose box for cv.
// side panes (the thread beside a channel) use the sidebar fill.
func (m *Model) convPane(cv *convView, x, w, h int, withCompose, side bool) string {
	s := m.s
	bg := s.bgPaper
	if side {
		bg = s.bgSide
	}
	hdr := m.convHeader(cv, w, side)
	filesTab := cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" && m.mainTab == 1
	var compose []string
	if withCompose && !filesTab {
		compose = m.composeLines(cv, w, side)
	}
	msgH := max(1, h-1-len(compose))
	var body []string
	if filesTab {
		body = m.filesTabLines(w)
		msgH = h - 1
	} else {
		m.renderConv(cv, w, side)
		if m.focus == FocusMessages && cv == m.activeConv() {
			cv.ensureVisible(msgH)
		}
		body = cv.view(msgH)
		r := rect{x, 1, w, msgH}
		if side || (cv == m.thread && m.splitThread()) {
			m.geo.threadMsgs, m.geo.threadConv = r, cv
		} else {
			m.geo.msgs, m.geo.msgsConv = r, cv
		}
	}
	lines := []string{hdr}
	lines = append(lines, paint(padLines(body, msgH), w, bg)...)
	if len(compose) > 0 {
		m.geo.compose = rect{x, 1 + msgH, w, len(compose)}
		cbg := s.bgSide
		if side {
			cbg = s.bgBlock
		}
		lines = append(lines, paint(compose, w, cbg)...)
	}
	return strings.Join(padLines(lines, h), "\n")
}

func padLines(l []string, n int) []string {
	for len(l) < n {
		l = append(l, "")
	}
	return l[:n]
}

func (m *Model) welcomeView(w, h int) []string {
	s := m.s
	var lines []string
	lines = append(lines, headerBar(lipgloss.NewStyle().Background(s.t.Block).Foreground(s.t.Text), s.Title.Render("TEAMS-TUI"), "", w))
	for range max(0, h/3-2) {
		lines = append(lines, "")
	}
	name := m.me.DisplayName
	if name == "" {
		name = "there"
	}
	lines = append(lines, centerIn(s.Inverse.Bold(true).Render(" teams-tui ")+" "+s.Muted.Render(version), w), "",
		centerIn(s.Title.Render("Welcome, "+name), w), "")
	tips := []string{
		"alt+1…6   Activity · Chat · Teams · Calendar · Files · Search",
		"tab       sidebar → messages → compose",
		"ctrl+n    new chat        ctrl+k   command palette",
		"ctrl+e    search          ?        all shortcuts",
		"mouse     click anything, scroll with the wheel",
	}
	tw := 0
	for _, t := range tips {
		tw = max(tw, ansi.StringWidth(t))
	}
	for _, t := range tips {
		lines = append(lines, centerIn(s.Muted.Render(padRight(t, tw)), w))
	}
	return lines
}

// version is shown on the welcome and login screens; set by main.
var version = "dev"

// SetVersion sets the version shown in the UI.
func SetVersion(v string) { version = v }

// ---- status bar -----------------------------------------------------------------------

func (m *Model) statusView() string {
	s := m.s
	ic := s.ic
	arrow := func(from, to lipgloss.Color) string {
		if ic.Arrow == "" {
			return ""
		}
		return lipgloss.NewStyle().Foreground(from).Background(to).Render(ic.Arrow)
	}
	ink := lipgloss.NewStyle().Background(s.t.Ink).Foreground(s.t.RailFg)
	if !s.isLight() {
		ink = lipgloss.NewStyle().Background(s.t.RailBg).Foreground(s.t.RailFg)
	}
	inkBg := lipgloss.Color(s.t.Ink)
	if !s.isLight() {
		inkBg = s.t.RailBg
	}

	// presence segment
	pc := s.t.Good
	switch m.myPresence.Availability {
	case model.Busy, "BusyIdle", model.DoNotDisturb:
		pc = s.t.Danger
	case model.Away, model.BeRightBack:
		pc = s.t.Warn
	case model.Offline:
		pc = s.t.Muted
	}
	label := presenceLabel(m.myPresence)
	if m.myPresence.Availability == "" {
		label = "Available"
	}
	presence := zone.Mark("status:me", lipgloss.NewStyle().Background(pc).Foreground(s.t.OnAccent).Bold(true).Render(" ● "+label+" "))
	segs := presence

	// meeting segment
	nextBg := inkBg
	if e := m.upcomingMeeting(); e != nil {
		segs += arrow(pc, s.t.Accent)
		txt := " " + ic.Calendar + " " + trunc(e.Subject, 24) + " · " + untilLabel(e) + " · alt+j Join "
		if m.narrow() {
			txt = " " + ic.Calendar + " " + trunc(e.Subject, 14) + " " + shortUntil(e) + " · Join "
		}
		segs += zone.Mark("status:join", lipgloss.NewStyle().Background(s.t.Accent).Foreground(s.t.OnAccent).Bold(true).Render(txt))
		segs += arrow(s.t.Accent, inkBg)
	} else {
		segs += arrow(pc, nextBg)
	}

	clock := lipgloss.NewStyle().Background(s.t.Paper).Foreground(s.t.Text).Render(" " + time.Now().Format(m.cfg.TimeFormat) + " ")
	right := ""
	if m.busy > 0 {
		right += ink.Render(m.spinner.View() + " ")
	}
	if m.demo {
		right += ink.Foreground(s.t.RailBadge).Bold(true).Render(" DEMO ")
	}
	right += clock

	var mid string
	switch {
	case len(m.alerts) > 0 && !m.alertOpen:
		a := m.alerts[0]
		mid = zone.Mark("status:alert", ink.Foreground(s.t.RailBadge).Bold(true).Render(" "+m.alertIcon(a)+" "+a.head+" · "+a.title+" — alt+a"))
	case m.toast != "" && m.toastErr:
		mid = ink.Foreground(s.t.RailBadge).Bold(true).Render(" ⚠ " + m.toast)
	case m.toast != "":
		mid = ink.Foreground(s.t.OnInk).Render(" " + m.toast)
	default:
		mid = ink.Render(" " + m.hints())
	}
	midW := m.w - ansi.StringWidth(segs) - ansi.StringWidth(right)
	if midW < 0 {
		return trunc(segs, m.w)
	}
	mid = withBg(trunc(mid, midW), s.bgOf(inkBg), midW)
	return segs + mid + right
}

func shortUntil(e *model.Event) string {
	if e.Live(time.Now()) {
		return "live"
	}
	d := time.Until(e.Start).Round(time.Minute)
	return itoa(max(0, int(d.Minutes()))) + "m"
}

func (s styles) isLight() bool { return s.t.Paper == "#F4F1EA" }

func (s styles) bgOf(c lipgloss.Color) string { return bgPrefix(c) }

func (m *Model) hints() string {
	if m.alertVisible() {
		return "←→ choose · enter select · esc dismiss"
	}
	if m.modal != nil {
		return "↑↓ move · enter run · esc close"
	}
	switch m.focus {
	case FocusCompose:
		return "enter send · alt+enter newline · @ mention · / commands · ↑ edit last · esc back"
	case FocusMessages:
		switch {
		case m.section == SecSettings:
			return "↑↓ move · enter change · ctrl+s save · esc back"
		case m.section == SecFiles && m.conv == nil:
			return "↑↓ move · enter open · d download · y copy link · u upload · / filter · backspace up"
		case m.meetingMode():
			return "alt+j join · tab compose · p people · x transcript · esc back"
		}
		cv := m.activeConv()
		if cv != nil {
			if msg := cv.selected(); msg != nil && m.hasCardActions(cv, msg) {
				return "A card actions · ↑↓ select · T open in Teams · r reply · + react"
			}
		}
		return "↑↓ select · r reply · + react · e edit · d delete · y copy · tab compose"
	}
	switch m.section {
	case SecChat:
		return "↑↓ move · enter open · n new · / filter · p favorite · m mute · . actions"
	case SecTeams:
		return "enter open thread · ←→ collapse · alt+↑↓ reorder · H hide · ? help"
	case SecCalendar:
		return "↑↓ move · [ ] day · j join · c chat · a/t/d respond · n new meeting"
	case SecFiles:
		return "↑↓ move · enter open · d download · y copy link · backspace up · u upload"
	case SecSearch:
		return "type query + enter · ↑↓ results · enter open · esc back"
	case SecActivity:
		return "↑↓ move · enter open · m mark all read · ? help"
	case SecSettings:
		return "↑↓ move · enter change · ctrl+s save · esc back"
	}
	return "? help"
}

// conversation title used in a few headers (plain).
func (m *Model) plainSnippet(msg *model.Message, n int) string {
	return trunc(plainOneLine(htmltext.Plain(msg.Body)), n)
}
