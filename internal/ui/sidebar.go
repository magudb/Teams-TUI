package ui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/magudb/Teams-TUI/internal/model"
)

// sideItem is one row group in the sidebar list.
type sideItem struct {
	key        string
	lines      []string
	selectable bool
	open       func(m *Model) tea.Cmd // enter / click
	// selLines renders the item when selected (accent fill); nil = derive
	// from lines by stripping styles.
	selLines func() []string
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
	for _, key := range m.st.Favorites {
		if c, ok := byID[strings.TrimPrefix(key, "chat:")]; ok && strings.HasPrefix(key, "chat:") {
			pinned = append(pinned, c)
		}
	}
	model.SortChats(recent)
	return
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
		lines := it.lines
		if i == sel {
			if it.selLines != nil {
				lines = it.selLines()
			} else {
				lines = make([]string, len(it.lines))
				for k, l := range it.lines {
					lines[k] = ansi.Strip(l)
				}
			}
			// accent fill when the sidebar has focus, block fill otherwise
			for k, l := range lines {
				if it.selLines == nil {
					l = m.s.Sel.Render(ansi.Strip(l))
				}
				lines[k] = withBg(l, styleOn(m.s.Sel), w)
			}
		}
		for _, l := range lines {
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
	k = strings.TrimPrefix(strings.TrimPrefix(k, "u:"), "fav:")
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

func (m *Model) openChatActions(c *model.Chat) {
	id := c.ID
	pinLabel := "★ Add to favorites"
	if m.st.IsPinned(id) {
		pinLabel = "☆ Remove from favorites"
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
		key := "fav:chat:" + id
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
