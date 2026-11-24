package ui

import (
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/model"
)

// sideItem is one row group in the sidebar list.
type sideItem struct {
	key        string
	lines      []string
	selectable bool
	open       func(m *Model) tea.Cmd // enter / click
}

// sortedChats returns pinned chats (in the user's pinned order) and the
// remaining chats ordered by most recent activity, like Teams.
func (m *Model) sortedChats() (pinned, recent []model.Chat) {
	q := strings.ToLower(strings.TrimSpace(m.chatFilter.Value()))
	byID := map[string]model.Chat{}
	for _, c := range m.chats {
		if q != "" && !strings.Contains(strings.ToLower(c.Title(m.me.ID)), q) {
			continue
		}
		if m.st.IsPinned(c.ID) {
			byID[c.ID] = c
		} else {
			recent = append(recent, c)
		}
	}
	for _, id := range m.st.PinOrder {
		if c, ok := byID[id]; ok {
			pinned = append(pinned, c)
		}
	}
	model.SortChats(recent)
	return
}

func (m *Model) chatItem(c model.Chat, w int) sideItem {
	s := m.s
	title := c.Title(m.me.ID)
	unread := c.Unread(m.me.ID)
	muted := m.st.Muted["chat:"+c.ID]
	// avatar chip + presence, like the Teams chat list
	neutral := lipgloss.NewStyle().Background(s.t.Border).Foreground(s.t.Text).Bold(true)
	icon := " " + neutral.Render(padRight(" "+initials(title), 4))
	switch {
	case c.ChatType == "oneOnOne":
		if o, ok := c.Other(m.me.ID); ok {
			p, known := m.presence[o.ID]
			icon = s.presenceDot(p, known) + s.avatar(o.ID, o.DisplayName)
		}
	case c.ChatType == "meeting":
		icon = " " + neutral.Render(" ▣  ")
	default:
		icon = " " + neutral.Render(padRight(" "+initials(title), 4))
	}
	ts := ""
	preview := ""
	if lm := c.LastMessage; lm != nil {
		ts = relTime(lm.Created, m.cfg.TimeFormat)
		who := ""
		if lm.From.ID == m.me.ID {
			who = "You: "
		} else if c.ChatType != "oneOnOne" && lm.From.DisplayName != "" {
			who = strings.Fields(lm.From.DisplayName)[0] + ": "
		}
		preview = who + plainOneLine(lm.Text)
		if lm.Deleted {
			preview = "This message has been deleted."
		}
	}
	nameStyle := s.Text
	if m.dragKey == "chat:"+c.ID {
		nameStyle = s.Accent.Bold(true)
	}
	prevStyle := s.Faint
	if unread && !muted {
		nameStyle = s.Bold
		prevStyle = s.Text
		ts = s.Accent.Bold(true).Render("● ") + ts
	}
	suffix := ""
	if muted {
		suffix = " " + s.Faint.Render("🔕")
	}
	line1 := spread(" "+icon+" "+nameStyle.Render(trunc(title, w-16))+suffix, s.Faint.Render(ts)+" ", w)
	line2 := "       " + prevStyle.Render(trunc(preview, w-8))
	id := c.ID
	return sideItem{key: "chat:" + c.ID, lines: []string{line1, line2}, selectable: true, open: func(m *Model) tea.Cmd {
		return m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: id})
	}}
}

func header(s styles, text string, w int) string {
	return s.Faint.Render(trunc(" "+strings.ToUpper(text), w))
}

func (m *Model) sideItems(w int) []sideItem {
	s := m.s
	var items []sideItem
	switch m.section {
	case SecChat:
		if m.cfg.CombinedChat {
			items = append(items, m.combinedItems(w)...)
		} else {
			items = append(items, m.chatListItems(w, false)...)
		}

	case SecTeams:
		items = append(items, m.teamTreeItems(w)...)

	case SecActivity:
		if len(m.activity) == 0 {
			items = append(items, sideItem{lines: []string{s.Muted.Render(" No new activity"), "", s.Faint.Render(" Mentions and new messages"), s.Faint.Render(" show up here.")}})
		}
		for i, a := range m.activity {
			a := a
			icon := s.Accent.Render("💬")
			if a.kind == "mention" {
				icon = s.Warn.Render("＠")
			}
			st := s.Text
			if !a.read {
				st = s.Bold
			}
			l1 := spread(" "+icon+" "+st.Render(trunc(a.title, w-12)), s.Faint.Render(relTime(a.at, m.cfg.TimeFormat))+" ", w)
			l2 := "    " + s.Faint.Render(trunc(plainOneLine(a.preview), w-5))
			idx := i
			items = append(items, sideItem{key: "act:" + itoa(i) + a.msgID, lines: []string{l1, l2}, selectable: true, open: func(m *Model) tea.Cmd {
				m.activity[idx].read = true
				return m.openConvAt(a.ref, a.msgID)
			}})
		}

	case SecCalendar:
		var day []model.Event
		for _, e := range m.events {
			if overlapsDay(e, m.calDay) {
				day = append(day, e)
			}
		}
		if len(day) == 0 {
			items = append(items, sideItem{lines: []string{s.Muted.Render(" No meetings"), "", s.Faint.Render(" Press n to schedule one.")}})
		}
		now := time.Now()
		for _, e := range day {
			e := e
			tm := e.Start.Format(m.cfg.TimeFormat) + "–" + e.End.Format(m.cfg.TimeFormat)
			if e.IsAllDay {
				tm = "All day"
			}
			st := s.Text
			mark := " "
			switch {
			case e.IsCancelled || e.Response == "declined":
				st = s.Faint.Strikethrough(true)
			case now.After(e.Start) && now.Before(e.End):
				mark = s.OK.Render("▌")
				st = s.Bold
			case now.After(e.End):
				st = s.Muted
			}
			resp := ""
			switch e.Response {
			case "notResponded", "none":
				resp = s.Warn.Render(" ?")
			case "tentativelyAccepted":
				resp = s.Faint.Render(" ~")
			}
			l1 := mark + st.Render(trunc(e.Subject, w-4)) + resp
			loc := tm
			if e.IsOnline {
				loc += " · Teams"
			} else if e.Location != "" {
				loc += " · " + e.Location
			}
			items = append(items, sideItem{key: "ev:" + e.ID, lines: []string{l1, "  " + s.Faint.Render(trunc(loc, w-3))}, selectable: true, open: func(m *Model) tea.Cmd {
				m.selEvent = e.ID
				return nil
			}})
		}

	case SecFiles:
		if len(m.fileStack) > 0 {
			items = append(items, sideItem{key: "file:..", selectable: true, lines: []string{" " + s.Accent.Render("↰ ..")}, open: func(m *Model) tea.Cmd {
				m.fileStack = m.fileStack[:len(m.fileStack)-1]
				m.files = nil
				return m.loadFiles()
			}})
		}
		if m.files == nil {
			items = append(items, sideItem{lines: []string{s.Muted.Render(" Loading files…")}})
		}
		for _, f := range m.files {
			f := f
			icon := fileIcon(f)
			meta := relTime(f.Modified, m.cfg.TimeFormat)
			if !f.IsFolder {
				meta = humanSize(f.Size) + " · " + meta
			}
			items = append(items, sideItem{key: "file:" + f.ID, selectable: true,
				lines: []string{" " + icon + " " + trunc(f.Name, w-5), "    " + s.Faint.Render(trunc(meta, w-5))},
				open: func(m *Model) tea.Cmd {
					if f.IsFolder {
						m.fileStack = append(m.fileStack, f)
						m.files = nil
						return m.loadFiles()
					}
					m.selFile = f.ID
					return nil
				}})
		}

	case SecSearch:
		for _, h := range m.hits {
			h := h
			where := m.convTitle(h.Ref)
			l1 := spread(" "+s.Bold.Render(trunc(h.From, w/2)), s.Faint.Render(relTime(h.Created, m.cfg.TimeFormat))+" ", w)
			l2 := "  " + trunc(plainOneLine(h.Summary), w-3)
			l3 := "  " + s.Faint.Render(trunc(where, w-3))
			items = append(items, sideItem{key: "hit:" + h.MsgID, selectable: true, lines: []string{l1, l2, l3}, open: func(m *Model) tea.Cmd {
				return m.openConvAt(h.Ref, h.MsgID)
			}})
		}
		if len(m.hits) == 0 && m.searchQ == "" {
			items = append(items, sideItem{lines: []string{s.Faint.Render(" Type and press enter to search"), s.Faint.Render(" all chats and channels.")}})
		}
	}
	return items
}

// chatListItems renders pinned + recent chats. In the combined view the
// section headers are smaller sub-headers.
func (m *Model) chatListItems(w int, combined bool) []sideItem {
	s := m.s
	var items []sideItem
	hdr := func(t string) {
		if combined {
			items = append(items, sideItem{lines: []string{s.Faint.Render(trunc("   "+t, w))}})
		} else {
			items = append(items, sideItem{lines: []string{header(s, t, w)}})
		}
	}
	pinned, recent := m.sortedChats()
	if len(pinned) > 0 {
		hdr("Pinned")
		for _, c := range pinned {
			items = append(items, m.chatItem(c, w))
		}
		if !combined {
			items = append(items, sideItem{lines: []string{""}})
		}
		hdr("Recent")
	} else if !combined {
		hdr("Recent")
	}
	for _, c := range recent {
		items = append(items, m.chatItem(c, w))
	}
	if !m.chatsLoaded {
		items = append(items, sideItem{lines: []string{s.Muted.Render(" " + m.spinner.View() + " Loading chats…")}})
	}
	return items
}

// teamTreeItems renders the teams/channels tree (with hidden teams at the end).
func (m *Model) teamTreeItems(w int) []sideItem {
	s := m.s
	var items []sideItem
	if len(m.teams) == 0 {
		items = append(items, sideItem{lines: []string{s.Muted.Render(" Loading teams…")}})
	}
	visible, hidden := m.orderedTeams()
	for _, t := range visible {
		items = append(items, m.teamItems(t, w, false)...)
	}
	if len(hidden) > 0 {
		exp := m.st.Expanded[hiddenTeamsKey]
		arrow := "▸"
		if exp {
			arrow = "▾"
		}
		items = append(items, sideItem{key: "hidden:", selectable: true,
			lines: []string{" " + s.Muted.Render(arrow+" Hidden teams ("+itoa(len(hidden))+")")},
			open:  func(m *Model) tea.Cmd { return m.toggleTeam(hiddenTeamsKey) }})
		if exp {
			for _, t := range hidden {
				items = append(items, m.teamItems(t, w, true)...)
			}
		}
	}
	return items
}

func (m *Model) teamItems(t model.Team, w int, dim bool) []sideItem {
	s := m.s
	var items []sideItem
	exp := m.st.Expanded[t.ID]
	arrow := "▸"
	if exp {
		arrow = "▾"
	}
	nameStyle := s.Bold
	if dim {
		nameStyle = s.Muted
	}
	if m.dragKey == "team:"+t.ID {
		nameStyle = s.Accent.Bold(true)
	}
	badge := s.ChipMine.Render(" " + initials(t.DisplayName) + " ")
	id := t.ID
	dot := ""
	if !exp {
		for _, ch := range m.channels[t.ID] {
			if m.channelUnread(ch) && !m.st.Muted[chanKey(t.ID, ch.ID)] {
				dot = " " + s.Accent.Render("●")
				break
			}
		}
	}
	items = append(items, sideItem{key: "team:" + t.ID, selectable: true,
		lines: []string{" " + s.Muted.Render(arrow) + " " + badge + " " + nameStyle.Render(trunc(t.DisplayName, w-12)) + dot},
		open:  func(m *Model) tea.Cmd { return m.toggleTeam(id) }})
	if !exp {
		return items
	}
	chs, ok := m.channels[t.ID]
	if !ok {
		items = append(items, sideItem{lines: []string{s.Faint.Render("      loading…")}})
	}
	for _, ch := range chs {
		ch := ch
		icon := "#"
		if ch.MembershipType == "private" {
			icon = "🔒"
		} else if ch.MembershipType == "shared" {
			icon = "⇄"
		}
		active := m.conv != nil && m.conv.ref.Kind == model.KindChannel && m.conv.ref.ChannelID == ch.ID
		st := s.Text
		unread := m.channelUnread(ch)
		switch {
		case active:
			st = s.Accent.Bold(true)
		case unread:
			st = s.Bold
		case dim:
			st = s.Muted
		}
		dot := ""
		if unread {
			dot = " " + s.Accent.Render("●")
		}
		items = append(items, sideItem{key: "chan:" + ch.ID, selectable: true,
			lines: []string{"      " + s.Faint.Render(icon) + " " + st.Render(trunc(ch.DisplayName, w-12)) + dot},
			open: func(m *Model) tea.Cmd {
				return m.openConv(model.ConvRef{Kind: model.KindChannel, TeamID: ch.TeamID, ChannelID: ch.ID})
			}})
	}
	return items
}

func fileIcon(f model.DriveItem) string {
	if f.IsFolder {
		return "📂"
	}
	switch strings.ToLower(fileExt(f.Name)) {
	case ".doc", ".docx":
		return "📝"
	case ".xls", ".xlsx", ".csv":
		return "📊"
	case ".ppt", ".pptx":
		return "📙"
	case ".pdf":
		return "📕"
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
		return "📷"
	case ".zip", ".gz", ".tar":
		return "📦"
	}
	return "📄"
}

func fileExt(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i:]
	}
	return ""
}

func (m *Model) toggleTeam(id string) tea.Cmd {
	m.st.Expanded[id] = !m.st.Expanded[id]
	_ = m.st.Save()
	if m.st.Expanded[id] {
		if _, ok := m.channels[id]; !ok {
			return m.loadChannels(id)
		}
	}
	return nil
}

// sidebarHeader returns the header lines of the sidebar (title, buttons, inputs).
func (m *Model) sidebarHeader(w int) []string {
	s := m.s
	title := s.Title.Render(" " + sectionNames[m.section])
	var right string
	btn := func(id, label string) string { return zone.Mark("sb:"+id, s.Button.Render(label)) }
	switch m.section {
	case SecChat:
		right = btn("filter", "⌕") + " " + btn("newchat", "✎ New") + " "
		if m.cfg.CombinedChat {
			title = s.Title.Render(" Chat")
		}
	case SecTeams:
		right = btn("refresh", "↻") + " "
	case SecCalendar:
		right = btn("prevday", "◀") + " " + btn("today", "Today") + " " + btn("nextday", "▶") + " " + btn("newmeeting", "+") + " "
	case SecFiles:
		label := "Recent"
		if m.filesKey == "root" || len(m.fileStack) > 0 {
			label = "My files"
		}
		right = btn("filesmode", "⇄ "+label) + " "
	case SecActivity:
		right = btn("readall", "✓ Mark all read") + " "
	}
	lines := []string{spread(title, right, w)}
	switch m.section {
	case SecChat:
		if m.filtering || m.chatFilter.Value() != "" {
			m.chatFilter.Width = w - 4
			lines = append(lines, " "+m.chatFilter.View())
		}
	case SecSearch:
		m.searchInput.Width = w - 5
		lines = append(lines, " "+m.searchInput.View())
	case SecCalendar:
		lines = append(lines, s.Accent.Render(" "+dayLabel(m.calDay)+" · "+m.calDay.Format(m.cfg.DateFormat)))
	case SecFiles:
		if len(m.fileStack) > 0 {
			var names []string
			for _, f := range m.fileStack {
				names = append(names, f.Name)
			}
			lines = append(lines, s.Faint.Render(" "+trunc("My files › "+strings.Join(names, " › "), w-2)))
		}
	}
	lines = append(lines, s.Border.Render(strings.Repeat("─", w)))
	return lines
}

type sideLayout struct {
	lines []string
	owner []int // item index per visible line
	items []sideItem
}

func (m *Model) layoutSidebar(w, h int) sideLayout {
	items := m.sideItems(w)
	// resolve selection
	if !m.sideTouched[m.section] {
		m.sideSel[m.section] = firstSelectable(items)
	}
	selKey := m.sideSel[m.section]
	sel := -1
	for i, it := range items {
		if it.selectable && it.key == selKey {
			sel = i
		}
	}
	if sel < 0 {
		m.sideSel[m.section] = firstSelectable(items)
		for i, it := range items {
			if it.selectable && it.key == m.sideSel[m.section] {
				sel = i
			}
		}
	}
	var all []string
	var owner []int
	selStart, selEnd := 0, 0
	for i, it := range items {
		if i == sel {
			selStart = len(all)
		}
		for _, l := range it.lines {
			if i == sel {
				st := m.s.SelDim
				if m.focus == FocusSidebar {
					st = m.s.Sel
				}
				l = st.Render(padRight(ansi.Strip(l), w))
				if m.focus == FocusSidebar {
					l = m.s.Accent.Render("▎") + ansi.TruncateLeft(l, 1, "")
				}
			}
			all = append(all, l)
			owner = append(owner, i)
		}
		if i == sel {
			selEnd = len(all) - 1
		}
	}
	scroll := m.sideScroll[m.section]
	if sel >= 0 {
		if selStart < scroll {
			scroll = selStart
		}
		if selEnd >= scroll+h {
			scroll = selEnd - h + 1
		}
	}
	scroll = max(0, min(scroll, len(all)-h))
	m.sideScroll[m.section] = scroll
	end := min(len(all), scroll+h)
	return sideLayout{lines: all[scroll:end], owner: owner[scroll:end], items: items}
}

// ensureSel makes sure the current section has a valid selection.
func (m *Model) ensureSel(items []sideItem) {
	for _, it := range items {
		if it.selectable && it.key == m.sideSel[m.section] {
			return
		}
	}
	m.sideSel[m.section] = firstSelectable(items)
}

// firstSelectable prefers the first real item over collapsible section headers.
func firstSelectable(items []sideItem) string {
	for _, it := range items {
		if it.selectable && !strings.HasPrefix(it.key, "sec:") {
			return it.key
		}
	}
	for _, it := range items {
		if it.selectable {
			return it.key
		}
	}
	return ""
}

func (m *Model) sideMove(delta int) {
	items := m.sideItems(m.sidebarWidth())
	if !m.sideTouched[m.section] {
		m.sideSel[m.section] = firstSelectable(items)
		m.sideTouched[m.section] = true
	}
	if _, ok := m.sideSel[m.section]; !ok || m.sideSel[m.section] == "" {
		m.ensureSel(items)
		return
	}
	cur := -1
	for i, it := range items {
		if it.selectable && it.key == m.sideSel[m.section] {
			cur = i
		}
	}
	i := cur
	for {
		i += delta
		if i < 0 || i >= len(items) {
			return
		}
		if items[i].selectable {
			m.sideSel[m.section] = items[i].key
			return
		}
	}
}

func (m *Model) sideJump(end bool) {
	m.sideTouched[m.section] = true
	items := m.sideItems(m.sidebarWidth())
	if end {
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].selectable {
				m.sideSel[m.section] = items[i].key
				return
			}
		}
	}
	m.sideSel[m.section] = firstSelectable(items)
}

func (m *Model) sideOpen() tea.Cmd {
	items := m.sideItems(m.sidebarWidth())
	if !m.sideTouched[m.section] {
		m.sideSel[m.section] = firstSelectable(items)
	}
	m.sideTouched[m.section] = true
	m.ensureSel(items)
	for _, it := range items {
		if it.selectable && it.key == m.sideSel[m.section] && it.open != nil {
			return it.open(m)
		}
	}
	return nil
}

func (m *Model) selectedChatID() string {
	k := m.sideSel[SecChat]
	k = strings.TrimPrefix(k, "u:")
	if strings.HasPrefix(k, "chat:") {
		return strings.TrimPrefix(k, "chat:")
	}
	return ""
}

func (m *Model) chatByID(id string) *model.Chat {
	for i := range m.chats {
		if m.chats[i].ID == id {
			return &m.chats[i]
		}
	}
	return nil
}

func (m *Model) selectedEvent() *model.Event {
	id := m.selEvent
	if k := m.sideSel[SecCalendar]; strings.HasPrefix(k, "ev:") {
		id = strings.TrimPrefix(k, "ev:")
	}
	for i := range m.events {
		if m.events[i].ID == id {
			return &m.events[i]
		}
	}
	return nil
}

func (m *Model) selectedFile() *model.DriveItem {
	id := strings.TrimPrefix(m.sideSel[SecFiles], "file:")
	for i := range m.files {
		if m.files[i].ID == id {
			return &m.files[i]
		}
	}
	return nil
}

func (m *Model) openChatActions(c *model.Chat) {
	id := c.ID
	pinLabel := "📌 Pin"
	if m.st.IsPinned(id) {
		pinLabel = "📌 Unpin"
	}
	muteLabel := "🔕 Mute notifications"
	if m.st.Muted["chat:"+id] {
		muteLabel = "🔔 Unmute notifications"
	}
	items := []modalItem{
		{label: "↗ Open", run: func(m *Model) tea.Cmd {
			return m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: id})
		}},
		{label: pinLabel, run: func(m *Model) tea.Cmd {
			m.st.TogglePin(id)
			_ = m.st.Save()
			return nil
		}},
		{label: muteLabel, run: func(m *Model) tea.Cmd {
			m.st.Muted["chat:"+id] = !m.st.Muted["chat:"+id]
			_ = m.st.Save()
			return nil
		}},
		{label: "✓ Mark as read", run: func(m *Model) tea.Cmd { return m.markRead(id) }},
	}
	if m.st.IsPinned(id) {
		key := "chat:" + id
		items = append(items,
			modalItem{label: "↑ Move up", hint: "alt+↑", run: func(m *Model) tea.Cmd { m.moveBy(key, -1); return nil }},
			modalItem{label: "↓ Move down", hint: "alt+↓", run: func(m *Model) tea.Cmd { m.moveBy(key, 1); return nil }},
		)
	}
	if c.ChatType == "group" {
		items = append(items,
			modalItem{label: "✎ Rename", run: func(m *Model) tea.Cmd {
				m.openPrompt("Rename group chat", "Chat name", c.Topic, func(m *Model, s string) tea.Cmd { return m.renameChatID(id, s) })
				return nil
			}},
			modalItem{label: "＋ Add people", run: func(m *Model) tea.Cmd { m.openAddPeople(id); return nil }},
			modalItem{label: "⎋ Leave chat", run: func(m *Model) tea.Cmd {
				m.openConfirm("Leave this chat?", func(m *Model) tea.Cmd {
					return m.op(func(svc model.Service) opMsg {
						cx, cancel := ctx()
						defer cancel()
						return opMsg{err: svc.LeaveChat(cx, id), toast: "Left chat", chats: true}
					})
				})
				return nil
			}},
		)
	}
	if c.ChatType == "oneOnOne" {
		if o, ok := c.Other(m.me.ID); ok {
			items = append(items, modalItem{label: "👤 View profile", run: func(m *Model) tea.Cmd { m.openProfile(o); return nil }})
		}
	}
	items = append(items,
		modalItem{label: "👥 Show members", run: func(m *Model) tea.Cmd { m.openMembers(c); return nil }},
		modalItem{label: "⊘ Hide chat", run: func(m *Model) tea.Cmd {
			return m.op(func(svc model.Service) opMsg {
				cx, cancel := ctx()
				defer cancel()
				return opMsg{err: svc.HideChat(cx, id), toast: "Chat hidden", chats: true}
			})
		}},
	)
	if c.WebURL != "" {
		u := c.WebURL
		items = append(items, modalItem{label: "🌐 Open in Teams", run: func(m *Model) tea.Cmd { return m.openLink(u) }})
	}
	m.openList(c.Title(m.me.ID), items, false)
}

func (m *Model) openMembers(c *model.Chat) {
	members := append([]model.User(nil), c.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].DisplayName < members[j].DisplayName })
	var items []modalItem
	for _, u := range members {
		u := u
		p, ok := m.presence[u.ID]
		items = append(items, modalItem{label: m.s.presenceDot(p, ok) + " " + u.DisplayName, hint: u.Email, run: func(m *Model) tea.Cmd { m.openProfile(u); return nil }})
	}
	m.openList("Members ("+itoa(len(members))+")", items, true)
}

func (m *Model) openProfile(u model.User) {
	p, ok := m.presence[u.ID]
	status := "Unknown"
	if ok {
		status = presenceLabel(p)
	}
	items := []modalItem{
		{label: "💬 Chat", run: func(m *Model) tea.Cmd {
			svc := m.svc
			return func() tea.Msg {
				c, cancel := ctx()
				defer cancel()
				ch, err := svc.CreateChat(c, []model.User{u}, "")
				return chatCreatedMsg{ch, err}
			}
		}},
		{label: "📅 Schedule meeting", run: func(m *Model) tea.Cmd {
			m.openMeetingForm("")
			if u.Email != "" {
				m.modal.fields[3].SetValue(u.Email)
			}
			return nil
		}},
	}
	if u.Email != "" {
		items = append(items, modalItem{label: "✉ Email " + u.Email, run: func(m *Model) tea.Cmd { return m.openLink("mailto:" + u.Email) }})
	}
	title := u.DisplayName + " — " + status
	if u.JobTitle != "" {
		title += " · " + u.JobTitle
	}
	if ok && p.StatusMessage != "" {
		title += "\n“" + p.StatusMessage + "”"
	}
	m.openList(title, items, false).width = 60
}
