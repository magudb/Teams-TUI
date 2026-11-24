package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/model"
)

// Sidebar items in the "Blocks" style: muted bold section headers, two-line
// rows with a badge or time on the right, accent-filled selection.

// sideRow lays out one row with one column of padding on each side.
func sideRow(left, right string, w int) string { return " " + spread(left, right, w-2) }

// badge renders an accent count badge; on a selected (accent) row it inverts.
func (m *Model) badge(text string, sel bool) string {
	if sel {
		return lipgloss.NewStyle().Background(m.s.t.Paper).Foreground(m.s.t.Accent).Bold(true).Render(" " + text + " ")
	}
	return m.s.Badge.Render(" " + text + " ")
}

func (m *Model) sectionTitle(title string) sideItem {
	return sideItem{lines: []string{" " + m.s.Muted.Bold(true).Render(title)}}
}

// collapsibleTitle is a section header that toggles when opened.
func (m *Model) collapsibleTitle(id, title string) (sideItem, bool) {
	collapsed := m.st.Expanded["collapsed:"+id]
	label := title
	if collapsed {
		label = "▸ " + title
	}
	it := sideItem{key: "sec:" + id, selectable: true, lines: []string{" " + m.s.Muted.Bold(true).Render(label)},
		open: func(m *Model) tea.Cmd {
			m.st.Expanded["collapsed:"+id] = !m.st.Expanded["collapsed:"+id]
			_ = m.st.Save()
			return nil
		}}
	return it, !collapsed
}

func blank() sideItem { return sideItem{lines: []string{""}} }

// unreadBadgeText is the count of unread messages if the chat is loaded.
func (m *Model) unreadBadgeText(c model.Chat) string {
	if cv := m.convs["chat:"+c.ID]; cv != nil && cv.loadedOnce {
		n := 0
		for _, x := range cv.msgs {
			if x.From.ID != m.me.ID && x.Created.After(c.LastRead) && x.MessageType == "message" {
				n++
			}
		}
		if n > 0 {
			return itoa(min(n, 99))
		}
	}
	return "•"
}

func (m *Model) chatIcon(c model.Chat) string {
	s := m.s
	switch c.ChatType {
	case "oneOnOne":
		if o, ok := c.Other(m.me.ID); ok {
			p, known := m.presence[o.ID]
			return s.presenceDot(p, known)
		}
	case "meeting":
		return s.ic.Video
	}
	return s.ic.Users
}

func (m *Model) chatItem(c model.Chat, w int, key string) sideItem {
	build := func(sel bool) []string {
		s := m.s
		title := c.Title(m.me.ID)
		unread := c.Unread(m.me.ID) && !m.st.Muted["chat:"+c.ID]
		preview := ""
		ts := ""
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
		if m.st.Muted["chat:"+c.ID] {
			title += " (muted)"
		}
		icon := m.chatIcon(c)

		nameSt, prevSt, timeSt := s.Text, s.Muted, s.Muted
		if unread {
			nameSt, prevSt = s.Bold, s.Text
		}
		if sel {
			on := lipgloss.NewStyle().Foreground(s.t.OnAccent)
			nameSt, prevSt, timeSt = on.Bold(true), on, on
		}
		if m.dragKey == "chat:"+c.ID {
			nameSt = s.Accent.Bold(true)
		}
		right := timeSt.Render(ts)
		if unread {
			right = m.badge(m.unreadBadgeText(c), sel)
		}
		rw := ansi.StringWidth(right)
		l1 := sideRow(icon+" "+nameSt.Render(trunc(title, w-rw-6)), right, w)
		l2 := "   " + prevSt.Render(trunc(preview, w-4))
		return []string{l1, l2}
	}
	id := c.ID
	return sideItem{key: key, height: 2, render: build, selectable: true,
		open: func(m *Model) tea.Cmd {
			m.sideSel[m.section] = strings.TrimPrefix(key, "u:")
			return m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: id})
		}}
}

// chatListItems renders the Recent chats (favorites are listed at the top).
func (m *Model) chatListItems(w int, first bool) []sideItem {
	var items []sideItem
	_, recent := m.sortedChats()
	add := func(id, title string, chats []model.Chat) {
		if len(chats) == 0 {
			return
		}
		if !first {
			items = append(items, blank())
		}
		first = false
		hdr, open := m.collapsibleTitle(id, title)
		items = append(items, hdr)
		if !open {
			return
		}
		for _, c := range chats {
			items = append(items, m.chatItem(c, w, "chat:"+c.ID))
		}
	}
	add("recent", "Recent", recent)
	if m.nextChats != "" {
		items = append(items, sideItem{key: "more:chats", selectable: true, lines: []string{"  Load older chats"}, open: func(m *Model) tea.Cmd { return m.chatPagesCmd(true) }})
	}
	if !m.chatsLoaded {
		items = append(items, sideItem{lines: []string{" " + m.s.Muted.Render(m.spinner.View()+" Loading chats…")}})
	}
	return items
}

// teamTreeItems renders the teams/channels tree (with hidden teams at the end).
func (m *Model) teamTreeItems(w int) []sideItem {
	s := m.s
	var items []sideItem
	if len(m.teams) == 0 {
		items = append(items, sideItem{lines: []string{" " + s.Muted.Render("Loading teams…")}})
	}
	visible, hidden := m.orderedTeams()
	for i, t := range visible {
		if i > 0 {
			items = append(items, blank())
		}
		items = append(items, m.teamItems(t, w, false)...)
	}
	if len(hidden) > 0 {
		exp := m.st.Expanded[hiddenTeamsKey]
		arrow := "▸"
		if exp {
			arrow = "▾"
		}
		items = append(items, blank(), sideItem{key: "hidden:", selectable: true,
			lines: []string{" " + s.Muted.Bold(true).Render(arrow+" Hidden teams ("+itoa(len(hidden))+")")},
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
	unread := 0
	for _, ch := range m.channels[t.ID] {
		if m.channelUnread(ch) && !m.st.Muted[chanKey(t.ID, ch.ID)] {
			unread++
		}
	}
	nameSt := s.Muted.Bold(true)
	if m.dragKey == "team:"+t.ID {
		nameSt = s.Accent.Bold(true)
	}
	right := ""
	if !exp && unread > 0 {
		right = s.Accent.Bold(true).Render("●")
	}
	id := t.ID
	items = append(items, sideItem{key: "team:" + t.ID, selectable: true,
		lines: []string{sideRow(nameSt.Render(arrow+" "+trunc(t.DisplayName, w-8)), right, w)},
		open:  func(m *Model) tea.Cmd { return m.toggleTeam(id) }})
	if !exp {
		return items
	}
	chs, ok := m.channels[t.ID]
	if !ok {
		items = append(items, sideItem{lines: []string{"   " + s.Faint.Render("loading…")}})
	}
	for _, ch := range chs {
		items = append(items, m.channelItem(ch, w, "chan:"+ch.ID, dim, false))
	}
	return items
}

func (m *Model) channelIcon(ch model.Channel) string {
	switch ch.MembershipType {
	case "private":
		return m.s.ic.Lock
	case "shared":
		return "⇄"
	}
	return m.s.ic.Hash
}

// channelItem is a one-line channel row (two lines with a preview in Unread).
func (m *Model) channelItem(ch model.Channel, w int, key string, dim, withPreview bool) sideItem {
	s := m.s
	act := m.chanAct[chanKey(ch.TeamID, ch.ID)]
	unread := m.channelUnread(ch) && !m.st.Muted[chanKey(ch.TeamID, ch.ID)]
	active := m.conv != nil && m.conv.ref.Kind == model.KindChannel && m.conv.ref.ChannelID == ch.ID
	build := func(sel bool) []string {
		nameSt, iconSt, prevSt := s.Text, s.Muted, s.Text
		switch {
		case unread:
			nameSt = s.Bold
		case active:
			nameSt = s.Accent.Bold(true)
		case dim:
			nameSt = s.Muted
		}
		if sel {
			on := lipgloss.NewStyle().Foreground(s.t.OnAccent)
			nameSt, iconSt, prevSt = on.Bold(true), on, on
		}
		right := ""
		if unread {
			n := act.count
			txt := "•"
			if n > 0 {
				txt = itoa(min(n, 99))
			}
			right = m.badge(txt, sel)
		}
		name := ch.DisplayName
		if withPreview || strings.HasPrefix(key, "fav:") {
			if tn := m.teamName(ch.TeamID); tn != "" {
				name += " · " + tn
			}
		}
		rw := ansi.StringWidth(right)
		lines := []string{sideRow(iconSt.Render(m.channelIcon(ch))+" "+nameSt.Render(trunc(name, w-rw-6)), right, w)}
		if withPreview {
			who := ""
			if act.from != "" {
				who = strings.Fields(act.from)[0] + ": "
			}
			lines = append(lines, "   "+prevSt.Render(trunc(plainOneLine(who+act.preview), w-4)))
		}
		return lines
	}
	height := 1
	if withPreview {
		height++
	}
	teamID, chID := ch.TeamID, ch.ID
	return sideItem{key: key, selectable: true, height: height, render: build,
		open: func(m *Model) tea.Cmd {
			m.sideSel[m.section] = strings.TrimPrefix(key, "u:")
			if !strings.HasPrefix(key, "fav:") {
				m.st.Expanded[teamID] = true
			}
			return m.openConv(model.ConvRef{Kind: model.KindChannel, TeamID: teamID, ChannelID: chID})
		}}
}

// favoriteKey maps a sidebar key to its favorites key ("chat:<id>" or
// "chan:<team>:<channel>"), or "" if it can't be a favorite.
func (m *Model) favoriteKey(sel string) string {
	sel = strings.TrimPrefix(strings.TrimPrefix(sel, "u:"), "fav:")
	switch {
	case strings.HasPrefix(sel, "chat:"):
		return sel
	case strings.HasPrefix(sel, "chan:"):
		id := strings.TrimPrefix(sel, "chan:")
		for teamID, chs := range m.channels {
			for _, ch := range chs {
				if ch.ID == id {
					return "chan:" + teamID + ":" + id
				}
			}
		}
	}
	return ""
}

func (m *Model) toggleFavorite(sel string) {
	key := m.favoriteKey(sel)
	if key == "" {
		m.setToast("Only chats and channels can be favorites", false)
		return
	}
	m.st.ToggleFavorite(key)
	_ = m.st.Save()
	if m.st.IsFavorite(key) {
		m.setToast("Added to favorites", false)
	} else {
		m.setToast("Removed from favorites", false)
	}
}

// favoriteItems is the Favorites section: chats and channels in your order.
func (m *Model) favoriteItems(w int) []sideItem {
	var rows []sideItem
	for _, key := range m.st.Favorites {
		switch {
		case strings.HasPrefix(key, "chat:"):
			if c := m.chatByID(strings.TrimPrefix(key, "chat:")); c != nil && m.chatMatchesFilter(*c) {
				rows = append(rows, m.chatItem(*c, w, "fav:"+key))
			}
		case strings.HasPrefix(key, "chan:"):
			parts := strings.SplitN(strings.TrimPrefix(key, "chan:"), ":", 2)
			if len(parts) != 2 {
				continue
			}
			for _, ch := range m.channels[parts[0]] {
				if ch.ID == parts[1] {
					rows = append(rows, m.channelItem(ch, w, "fav:chan:"+ch.ID, false, false))
				}
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	hdr, open := m.collapsibleTitle("favorites", "Favorites")
	if !open {
		return []sideItem{hdr}
	}
	return append([]sideItem{hdr}, rows...)
}

// combinedItems builds the combined Chat list: Favorites, Unread, Channels, Recent.
func (m *Model) combinedItems(w int) []sideItem {
	items := m.favoriteItems(w)
	var unreadChats []model.Chat
	for _, c := range m.chats {
		if c.Unread(m.me.ID) && !m.st.Muted["chat:"+c.ID] && m.chatMatchesFilter(c) && !m.st.IsFavorite("chat:"+c.ID) {
			unreadChats = append(unreadChats, c)
		}
	}
	model.SortChats(unreadChats)
	var unreadChs []model.Channel
	for _, ch := range m.unreadChannels() {
		if !m.st.IsFavorite("chan:" + ch.TeamID + ":" + ch.ID) {
			unreadChs = append(unreadChs, ch)
		}
	}
	first := len(items) == 0
	if len(unreadChats)+len(unreadChs) > 0 {
		hdr, open := m.collapsibleTitle("unread", "Unread")
		if !first {
			items = append(items, blank())
		}
		items = append(items, hdr)
		first = false
		if open {
			for _, ch := range unreadChs {
				items = append(items, m.channelItem(ch, w, "u:chan:"+ch.ID, false, true))
			}
			for _, c := range unreadChats {
				items = append(items, m.chatItem(c, w, "u:chat:"+c.ID))
			}
		}
	}
	if !first {
		items = append(items, blank())
	}
	hdr, open := m.collapsibleTitle("channels", "Channels")
	items = append(items, hdr)
	if open {
		items = append(items, m.teamTreeItems(w)...)
	}
	items = append(items, m.chatListItems(w, false)...)
	return items
}

func (m *Model) sideItems(w int) []sideItem {
	s := m.s
	var items []sideItem
	switch m.section {
	case SecChat:
		if m.cfg.CombinedChat {
			return m.combinedItems(w)
		}
		items = m.favoriteItems(w)
		var unread []model.Chat
		for _, c := range m.chats {
			if c.Unread(m.me.ID) && !m.st.Muted["chat:"+c.ID] && m.chatMatchesFilter(c) && !m.st.IsFavorite("chat:"+c.ID) {
				unread = append(unread, c)
			}
		}
		var chs []model.Channel
		for _, ch := range m.unreadChannels() {
			if !m.st.IsFavorite("chan:" + ch.TeamID + ":" + ch.ID) {
				chs = append(chs, ch)
			}
		}
		if len(unread)+len(chs) > 0 {
			if len(items) > 0 {
				items = append(items, blank())
			}
			items = append(items, m.sectionTitle("Unread"))
			for _, ch := range chs {
				items = append(items, m.channelItem(ch, w, "u:chan:"+ch.ID, false, true))
			}
			for _, c := range unread {
				items = append(items, m.chatItem(c, w, "u:chat:"+c.ID))
			}
			return append(items, m.chatListItems(w, false)...)
		}
		return append(items, m.chatListItems(w, len(items) == 0)...)

	case SecTeams:
		return m.teamTreeItems(w)

	case SecActivity:
		if len(m.activity) == 0 {
			return []sideItem{{lines: []string{" " + s.Muted.Render("No new activity"), "", " " + s.Faint.Render("Mentions, replies and reactions"), " " + s.Faint.Render("show up here.")}}}
		}
		for i, a := range m.activity {
			items = append(items, m.activityItem(i, a, w))
		}

	case SecCalendar:
		items = append(items, sideItem{lines: []string{" " + s.Accent.Bold(true).Render(dayLabel(m.calDay)+" · "+m.calDay.Format("Mon 2 Jan"))}}, blank())
		var day []model.Event
		for _, e := range m.events {
			if overlapsDay(e, m.calDay) {
				day = append(day, e)
			}
		}
		if len(day) == 0 {
			items = append(items, sideItem{lines: []string{" " + s.Muted.Render("No meetings"), "", " " + s.Faint.Render("Press n to schedule one.")}})
		}
		for _, e := range day {
			items = append(items, m.eventItem(e, w))
		}

	case SecFiles:
		items = append(items, m.sectionTitle("Locations"))
		loc := func(key, label string) sideItem {
			return sideItem{key: key, selectable: true, lines: []string{sideRow(label, "", w)}, open: func(m *Model) tea.Cmd { return m.openLocation(key) }}
		}
		items = append(items, loc("loc:recent", "Recent"), loc("loc:root", "My files"), loc("loc:shared", "Shared with me"))
		var chans []sideItem
		visible, _ := m.orderedTeams()
		for _, t := range visible {
			for _, ch := range m.channels[t.ID] {
				key := "loc:chan:" + t.ID + ":" + ch.ID
				right := ""
				if n, ok := m.fileCounts[key]; ok {
					right = s.Muted.Render(itoa(n))
				}
				label := trunc(ch.DisplayName, w-12) + s.Muted.Render(" "+trunc(t.DisplayName, max(0, w-14-len(ch.DisplayName))))
				chans = append(chans, sideItem{key: key, selectable: true,
					lines: []string{sideRow(s.Muted.Render(m.channelIcon(ch))+" "+label, right, w)},
					open:  func(m *Model) tea.Cmd { return m.openLocation(key) }})
			}
		}
		if len(chans) > 0 {
			items = append(items, blank(), m.sectionTitle("Channel files"))
			items = append(items, chans...)
		}

	case SecSearch:
		for i, h := range m.hits {
			if i > 0 {
				items = append(items, blank())
			}
			items = append(items, m.hitItem(h, w))
		}
		if len(m.hits) == 0 && m.searchQ == "" {
			items = append(items, sideItem{lines: []string{" " + s.Faint.Render("Type and press enter to search"), " " + s.Faint.Render("all chats and channels.")}})
		}

	case SecSettings:
		for _, sec := range settingsSections {
			sec := sec
			items = append(items, sideItem{key: "set:" + sec, selectable: true, lines: []string{sideRow(sec, "", w)},
				open: func(m *Model) tea.Cmd { m.settingsJump(sec); return nil }})
		}
	}
	return items
}

func (m *Model) activityItem(i int, a activityItem, w int) sideItem {
	s := m.s
	ic := s.ic
	icon := ic.Chat
	switch a.kind {
	case "mention":
		icon = "@"
	case "reply":
		icon = ic.Reply
	case "reaction":
		icon = ic.ThumbsUp
	case "meeting":
		icon = ic.Calendar
	case "call":
		icon = ic.Phone
	case "added":
		icon = ic.Chat
	}
	build := func(sel bool) []string {
		titleSt, prevSt, timeSt := s.Text, s.Muted, s.Muted
		if !a.read {
			titleSt, prevSt = s.Bold, s.Text
		}
		if sel {
			on := lipgloss.NewStyle().Foreground(s.t.OnAccent)
			titleSt, prevSt, timeSt = on.Bold(true), on, on
		}
		right := timeSt.Render(relTime(a.at, m.cfg.TimeFormat))
		if !a.read && !sel {
			right = m.badge("new", false)
		}
		rw := ansi.StringWidth(right)
		return []string{
			sideRow(icon+" "+titleSt.Render(trunc(a.title, w-rw-6)), right, w),
			"   " + prevSt.Render(trunc(plainOneLine(a.preview), w-4)),
		}
	}
	idx := i
	return sideItem{key: "act:" + itoa(i) + a.msgID, selectable: true, height: 2, render: build,
		open: func(m *Model) tea.Cmd {
			m.activity[idx].read = true
			if a.kind == "meeting" {
				m.calDay = today()
				m.sideSel[SecCalendar] = "ev:" + a.msgID
				m.sideTouched[SecCalendar] = true
				return m.switchSection(SecCalendar)
			}
			m.jumpMention = a.msgID
			if a.kind != "mention" {
				m.jumpMention = ""
			}
			return m.openConvAt(a.ref, a.msgID)
		}}
}

func (m *Model) eventItem(e model.Event, w int) sideItem {
	s := m.s
	now := time.Now()
	tm := e.Start.Format(m.cfg.TimeFormat) + "–" + e.End.Format(m.cfg.TimeFormat)
	if e.IsAllDay {
		tm = "All day"
	}
	where := ""
	switch {
	case e.Response == "declined":
		where = " · declined"
	case e.IsCancelled:
		where = " · cancelled"
	case e.IsOnline:
		where = " · Teams"
	case e.Location != "":
		where = " · " + e.Location
	}
	build := func(sel bool) []string {
		titleSt, subSt := s.Text, s.Muted
		switch {
		case e.IsCancelled || e.Response == "declined":
			titleSt = s.Faint.Strikethrough(true)
		case now.After(e.End):
			titleSt = s.Muted
		}
		if sel {
			on := lipgloss.NewStyle().Foreground(s.t.OnAccent)
			titleSt, subSt = on.Bold(true), on.Bold(true)
		}
		resp := ""
		switch e.Response {
		case "notResponded", "none":
			resp = " " + s.Warn.Bold(true).Render("?")
		case "tentativelyAccepted":
			resp = " " + s.Warn.Render("~")
		}
		right := ""
		if e.Live(now) {
			right = m.badge("live", sel)
		} else if d := e.Start.Sub(now); d > 0 && d <= 15*time.Minute && e.Response != "declined" {
			right = m.badge(itoa(int(d.Round(time.Minute).Minutes()))+" min", sel)
		}
		rw := ansi.StringWidth(right)
		return []string{
			sideRow(titleSt.Render(trunc(e.Subject, w-rw-6))+resp, right, w),
			"   " + subSt.Render(trunc(tm+where, w-4)),
		}
	}
	id := e.ID
	return sideItem{key: "ev:" + e.ID, selectable: true, height: 2, render: build,
		open: func(m *Model) tea.Cmd { m.selEvent = id; return nil }}
}

func (m *Model) hitItem(h model.SearchHit, w int) sideItem {
	s := m.s
	where := m.convTitle(h.Ref)
	icon := s.ic.Users
	if h.Ref.Kind == model.KindChannel {
		icon = s.ic.Hash
	}
	build := func(sel bool) []string {
		nameSt, textSt, subSt, hiSt := s.Bold, s.Text, s.Muted, s.Bold
		if sel {
			on := lipgloss.NewStyle().Foreground(s.t.OnAccent)
			nameSt, textSt, subSt, hiSt = on.Bold(true), on, on, on.Bold(true)
		}
		summary := trunc(plainOneLine(h.Summary), w-4)
		return []string{
			sideRow(nameSt.Render(trunc(h.From, w/2)), subSt.Render(relTime(h.Created, m.cfg.TimeFormat)), w),
			" " + highlight(summary, m.searchQ, textSt, hiSt),
			" " + subSt.Render(icon+" "+trunc(where, w-5)),
		}
	}
	return sideItem{key: "hit:" + h.MsgID, selectable: true, height: 3, render: build,
		open: func(m *Model) tea.Cmd { return m.openConvAt(h.Ref, h.MsgID) }}
}

// highlight renders text with case-insensitive matches of q in hi.
func highlight(text, q string, base, hi lipgloss.Style) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return base.Render(text)
	}
	lower := strings.ToLower(text)
	lq := strings.ToLower(q)
	var b strings.Builder
	pos := 0
	for {
		i := strings.Index(lower[pos:], lq)
		if i < 0 {
			break
		}
		b.WriteString(base.Render(text[pos : pos+i]))
		b.WriteString(hi.Render(text[pos+i : pos+i+len(q)]))
		pos += i + len(q)
	}
	b.WriteString(base.Render(text[pos:]))
	return b.String()
}

// sidebarHeader is the inverse title bar (plus inputs) above the list.
func (m *Model) sidebarHeader(w int) []string {
	s := m.s
	inv := s.Inverse
	hint := func(id, label string) string { return zone.Mark("sb:"+id, inv.Render(label)) }
	title := strings.ToUpper(sectionNames[m.section])
	var right string
	switch m.section {
	case SecChat:
		right = hint("filter", "/ filter") + inv.Render("  ") + hint("newchat", "n new")
	case SecTeams:
		right = hint("refresh", "↻")
	case SecActivity:
		right = hint("readall", "m mark all read")
	case SecCalendar:
		right = hint("prevday", "[") + inv.Render(" ") + hint("nextday", "]") + inv.Render(" day  ") + hint("today", "t today")
	case SecFiles:
		right = hint("upload", "u upload")
	case SecSearch:
		if m.searchQ != "" {
			n := len(m.hits)
			right = inv.Render(itoa(n) + " results")
			if n == 1 {
				right = inv.Render("1 result")
			}
		}
	}
	bar := inv.Render(" ") + inv.Bold(true).Render(title)
	gap := w - ansi.StringWidth(bar) - ansi.StringWidth(right) - 1
	lines := []string{bar + inv.Render(strings.Repeat(" ", max(1, gap))) + right + inv.Render(" ")}
	switch m.section {
	case SecSearch:
		m.searchInput.Width = w - 5
		m.searchInput.Prompt = s.Accent.Render("› ")
		lines = append(lines, "", withBg(" "+m.searchInput.View(), s.bgPaper, w), "")
	case SecChat:
		if m.filtering || m.chatFilter.Value() != "" {
			m.chatFilter.Width = w - 5
			m.chatFilter.Prompt = s.Accent.Render("› ")
			lines = append(lines, "", withBg(" "+m.chatFilter.View(), s.bgPaper, w))
		}
		lines = append(lines, "")
	default:
		lines = append(lines, "")
	}
	return lines
}
