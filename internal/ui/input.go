package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/model"
)

func timeNow() time.Time { return time.Now() }

// handleKey dispatches a key press.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	cmd := tea.Batch(m.handleKeyInner(msg), m.eventSelectionChanged())
	if p := m.pendingTake(); p != nil {
		return tea.Batch(cmd, p)
	}
	return cmd
}

func (m *Model) handleKeyInner(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		return tea.Quit
	}
	if m.modal != nil {
		return m.modalKey(msg)
	}

	// global shortcuts (work everywhere, including compose)
	switch k {
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6":
		return m.switchSection(Section(k[4] - '1'))
	case "ctrl+k":
		m.openPalette()
		return nil
	case "ctrl+n":
		m.openPeoplePicker("New chat", true, "")
		return nil
	case "ctrl+e":
		cmd := m.switchSection(SecSearch)
		m.setFocus(FocusSidebar)
		m.searchInput.Focus()
		return cmd
	case "ctrl+b":
		m.showSidebar = !m.showSidebar
		if !m.showSidebar && m.focus == FocusSidebar {
			m.setFocus(FocusMessages)
		}
		m.invalidateRender()
		return nil
	case "ctrl+r":
		return m.refresh()
	case "alt+j":
		return m.joinEvent(m.upcomingMeeting())
	case "f1":
		m.openHelp()
		return nil
	}

	switch m.focus {
	case FocusCompose:
		return m.composeKey(msg)
	case FocusMessages:
		return m.messagesKey(msg)
	default:
		return m.sidebarKey(msg)
	}
}

func (m *Model) commonKey(k string) (tea.Cmd, bool) {
	switch k {
	case "q":
		return tea.Quit, true
	case "?":
		m.openHelp()
		return nil, true
	case "tab":
		m.cycleFocus(1)
		return nil, true
	case "shift+tab":
		m.cycleFocus(-1)
		return nil, true
	case "/":
		if m.section == SecChat && m.focus == FocusSidebar {
			m.filtering = true
			m.chatFilter.Focus()
			return nil, true
		}
		cmd := m.switchSection(SecSearch)
		m.setFocus(FocusSidebar)
		m.searchInput.Focus()
		return cmd, true
	}
	return nil, false
}

func (m *Model) sidebarKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()

	// text inputs in the sidebar
	if m.section == SecSearch && m.searchInput.Focused() {
		switch k {
		case "enter":
			m.searchInput.Blur()
			return m.doSearch()
		case "esc":
			m.searchInput.Blur()
			return nil
		case "down", "tab":
			m.searchInput.Blur()
			return nil
		}
		var c tea.Cmd
		m.searchInput, c = m.searchInput.Update(msg)
		return c
	}
	if m.section == SecChat && m.filtering {
		switch k {
		case "enter", "down", "tab":
			m.filtering = false
			m.chatFilter.Blur()
			m.sideJump(false)
			if k == "enter" {
				return m.sideOpen()
			}
			return nil
		case "esc":
			m.filtering = false
			m.chatFilter.Blur()
			m.chatFilter.SetValue("")
			return nil
		}
		var c tea.Cmd
		m.chatFilter, c = m.chatFilter.Update(msg)
		m.sideJump(false)
		return c
	}

	if c, ok := m.commonKey(k); ok {
		return c
	}
	switch k {
	case "up", "k":
		m.sideMove(-1)
		return m.sideFollow()
	case "down", "j":
		if m.section == SecCalendar && k == "j" {
			return m.eventAction("join")
		}
		m.sideMove(1)
		return m.sideFollow()
	case "g", "home":
		m.sideJump(false)
		return m.sideFollow()
	case "G", "end":
		m.sideJump(true)
		return m.sideFollow()
	case "pgup":
		for range 8 {
			m.sideMove(-1)
		}
		return m.sideFollow()
	case "pgdown":
		for range 8 {
			m.sideMove(1)
		}
		return m.sideFollow()
	case "enter", "right", "l":
		cmd := m.sideOpen()
		sel := strings.TrimPrefix(m.sideSel[m.section], "u:")
		opensConv := strings.HasPrefix(sel, "chat:") || strings.HasPrefix(sel, "chan:") ||
			m.section == SecActivity || m.section == SecSearch
		if opensConv && k == "enter" {
			m.setFocus(FocusCompose)
		}
		return cmd
	case "esc":
		if m.section == SecChat && m.chatFilter.Value() != "" {
			m.chatFilter.SetValue("")
		}
		return nil
	}

	switch k {
	case "alt+up", "K", "alt+k":
		m.moveBy(m.sideSel[m.section], -1)
		return nil
	case "alt+down", "J", "alt+j":
		m.moveBy(m.sideSel[m.section], 1)
		return nil
	}

	sel := m.sideSel[m.section]
	isTeamItem := strings.HasPrefix(sel, "team:") || strings.HasPrefix(sel, "chan:") || sel == "hidden:"
	switch {
	case k == "n" && (m.section == SecChat || m.section == SecTeams):
		m.openPeoplePicker("New chat", true, "")
		return nil
	case (m.section == SecChat || m.section == SecTeams) && isTeamItem:
		switch k {
		case "left", "h":
			if strings.HasPrefix(sel, "team:") {
				id := strings.TrimPrefix(sel, "team:")
				if m.st.Expanded[id] {
					return m.toggleTeam(id)
				}
			}
			// jump to parent team
			for _, t := range m.teams {
				for _, ch := range m.channels[t.ID] {
					if "chan:"+ch.ID == sel {
						m.sideSel[m.section] = "team:" + t.ID
					}
				}
			}
		case " ":
			if strings.HasPrefix(sel, "team:") {
				return m.toggleTeam(strings.TrimPrefix(sel, "team:"))
			}
		case ".":
			m.openItemActions(sel)
		case "H":
			if strings.HasPrefix(sel, "team:") {
				m.toggleHiddenTeam(strings.TrimPrefix(sel, "team:"))
			}
		case "m":
			if strings.HasPrefix(sel, "chan:") {
				id := strings.TrimPrefix(sel, "chan:")
				for _, t := range m.teams {
					for _, ch := range m.channels[t.ID] {
						if ch.ID == id {
							key := chanKey(t.ID, id)
							m.st.Muted[key] = !m.st.Muted[key]
							_ = m.st.Save()
						}
					}
				}
			}
		}
		return nil
	case m.section == SecChat:
		c := m.chatByID(m.selectedChatID())
		if c == nil {
			break
		}
		switch k {
		case ".", " ":
			m.openChatActions(c)
		case "p":
			m.st.TogglePin(c.ID)
			_ = m.st.Save()
		case "m":
			m.st.Muted["chat:"+c.ID] = !m.st.Muted["chat:"+c.ID]
			_ = m.st.Save()
		}
		return nil
	}

	switch m.section {
	case SecCalendar:
		switch k {
		case "[":
			return m.shiftDay(-1)
		case "]":
			return m.shiftDay(1)
		case "t":
			return m.shiftDay(0)
		case "n":
			m.openMeetingForm("")
		case "a":
			return m.eventAction("accept")
		case "T":
			return m.eventAction("tentative")
		case "D":
			return m.eventAction("decline")
		case "o":
			return m.eventAction("web")
		case "c":
			return m.openMeetingChat(m.selectedEvent())
		case "x":
			return m.showTranscript(m.selectedEvent())
		case "R":
			return m.downloadRecording(m.selectedEvent())
		}
	case SecFiles:
		f := m.selectedFile()
		switch k {
		case "d":
			return m.fileAction("download", f)
		case "o":
			if f != nil && !f.IsFolder {
				return m.fileAction("open", f)
			}
		case "y":
			return m.fileAction("copy", f)
		case "backspace":
			if len(m.fileStack) > 0 {
				m.fileStack = m.fileStack[:len(m.fileStack)-1]
				m.files = nil
				return m.loadFiles()
			}
		case "r":
			m.fileStack = nil
			if m.filesKey == "root" {
				m.filesKey = ""
			} else {
				m.filesKey = "root"
			}
			m.files = nil
			return m.loadFiles()
		}
	case SecActivity:
		if k == "c" {
			m.markAllActivityRead()
		}
	}
	return nil
}

func (m *Model) markAllActivityRead() {
	for i := range m.activity {
		m.activity[i].read = true
	}
}

// sideFollow previews the selection in the main pane where it is cheap to do so.
func (m *Model) sideFollow() tea.Cmd {
	switch m.section {
	case SecFiles:
		m.conv = nil
	}
	return nil
}

func (m *Model) messagesKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	cv := m.activeConv()
	if cv == nil {
		m.setFocus(FocusSidebar)
		return nil
	}
	if c, ok := m.commonKey(k); ok {
		return c
	}
	h := max(3, m.geo.msgs.h)
	if cv == m.thread && m.splitThread() {
		h = max(3, m.geo.threadMsgs.h)
	}
	switch k {
	case "up", "k":
		if cv.sel <= 0 && cv.next != "" && !cv.loading {
			return m.loadOlder(cv)
		}
		if cv.sel < 0 {
			cv.selectMsg(len(cv.msgs) - 1)
		} else {
			cv.selectMsg(cv.sel - 1)
		}
	case "down", "j":
		if cv.sel >= len(cv.msgs)-1 {
			m.setFocus(FocusCompose)
			return nil
		}
		cv.selectMsg(cv.sel + 1)
	case "pgup", "ctrl+u":
		cv.sel = -1
		cv.dirty = true
		cv.scrollBy(-h+2, h)
		if cv.scroll == 0 && cv.next != "" {
			return m.loadOlder(cv)
		}
	case "pgdown", "ctrl+d":
		cv.sel = -1
		cv.dirty = true
		cv.scrollBy(h-2, h)
	case "g", "home":
		cv.selectMsg(0)
	case "G", "end":
		cv.selectMsg(len(cv.msgs) - 1)
		cv.follow = true
	case "u":
		return m.loadOlder(cv)
	case "enter":
		if cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" && cv.selected() != nil {
			return m.msgAction("thread")
		}
		m.setFocus(FocusCompose)
	case "i", "a":
		m.setFocus(FocusCompose)
	case "r":
		return m.msgAction("reply")
	case "+", "=":
		return m.msgAction("react")
	case "e":
		return m.msgAction("edit")
	case "d", "delete":
		return m.msgAction("delete")
	case "y", "c":
		return m.msgAction("copy")
	case "o":
		return m.msgAction("open")
	case ".", " ":
		return m.msgAction("more")
	case "A":
		return m.msgAction("card")
	case "1", "2", "3", "4", "5", "6":
		if sel := cv.selected(); sel != nil && !strings.HasPrefix(sel.ID, "tmp:") {
			return m.toggleReaction(cv.ref, sel.ID, quickReactions[k[0]-'1'])
		}
	case "esc", "left", "h":
		if m.thread != nil {
			m.closeThread()
			return nil
		}
		m.setFocus(FocusSidebar)
	}
	return nil
}

func (m *Model) loadOlder(cv *convView) tea.Cmd {
	if cv.next == "" || cv.loading {
		return nil
	}
	cv.loading = true
	return m.loadMessages(cv.ref, cv.next, true, false)
}

// ---- mouse ---------------------------------------------------------------------------

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	cmd := tea.Batch(m.handleMouseInner(msg), m.eventSelectionChanged())
	if p := m.pendingTake(); p != nil {
		return tea.Batch(cmd, p)
	}
	return cmd
}

func (m *Model) handleMouseInner(msg tea.MouseMsg) tea.Cmd {
	if m.modal != nil {
		return m.modalMouse(msg)
	}
	wheel := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown
	if wheel {
		d := 3
		if msg.Button == tea.MouseButtonWheelUp {
			d = -3
		}
		switch {
		case m.geo.msgs.in(msg.X, msg.Y) && m.geo.msgsConv != nil:
			cv := m.geo.msgsConv
			cv.scrollBy(d, m.geo.msgs.h)
			if cv.scroll == 0 && d < 0 && cv.next != "" {
				return m.loadOlder(cv)
			}
		case m.geo.threadMsgs.in(msg.X, msg.Y) && m.geo.threadConv != nil:
			m.geo.threadConv.scrollBy(d, m.geo.threadMsgs.h)
		case m.geo.sideList.in(msg.X, msg.Y):
			m.sideScroll[m.section] = max(0, m.sideScroll[m.section]+d)
			// move selection along so it stays in view
			if d < 0 {
				m.sideMove(-1)
			} else {
				m.sideMove(1)
			}
		}
		return nil
	}
	if m.dragKey != "" && msg.Action != tea.MouseActionPress {
		return m.sidebarDrag(msg)
	}
	if msg.Action != tea.MouseActionPress {
		return nil
	}
	right := msg.Button == tea.MouseButtonRight
	if msg.Button != tea.MouseButtonLeft && !right {
		return nil
	}

	// inline widgets first
	if c, ok := m.clickZones(msg); ok {
		return c
	}

	switch {
	case m.geo.sideList.in(msg.X, msg.Y):
		row := msg.Y - m.geo.sideList.y
		if row < 0 || row >= len(m.geo.sideOwner) {
			return nil
		}
		idx := m.geo.sideOwner[row]
		if idx < 0 || idx >= len(m.geo.sideItems) {
			return nil
		}
		it := m.geo.sideItems[idx]
		if !it.selectable {
			return nil
		}
		m.sideSel[m.section] = it.key
		m.setFocus(FocusSidebar)
		if right {
			m.openItemActions(it.key)
			return nil
		}
		if strings.HasPrefix(it.key, "sec:") || it.key == "hidden:" {
			// section headers toggle immediately
			return it.open(m)
		}
		// open on release (so items can be dragged to reorder them)
		m.sideTouched[m.section] = true
		m.dragKey = it.key
		return nil
	case m.geo.msgs.in(msg.X, msg.Y) && m.geo.msgsConv != nil:
		return m.clickMessage(m.geo.msgsConv, msg.Y-m.geo.msgs.y, right)
	case m.geo.threadMsgs.in(msg.X, msg.Y) && m.geo.threadConv != nil:
		return m.clickMessage(m.geo.threadConv, msg.Y-m.geo.threadMsgs.y, right)
	case m.geo.compose.in(msg.X, msg.Y):
		m.setFocus(FocusCompose)
	}
	return nil
}

// sidebarItemAt returns the sidebar item under the mouse, if any.
func (m *Model) sidebarItemAt(msg tea.MouseMsg) (sideItem, bool) {
	if !m.geo.sideList.in(msg.X, msg.Y) {
		return sideItem{}, false
	}
	row := msg.Y - m.geo.sideList.y
	if row < 0 || row >= len(m.geo.sideOwner) {
		return sideItem{}, false
	}
	idx := m.geo.sideOwner[row]
	if idx < 0 || idx >= len(m.geo.sideItems) || !m.geo.sideItems[idx].selectable {
		return sideItem{}, false
	}
	return m.geo.sideItems[idx], true
}

// sidebarDrag handles motion/release after a press on a sidebar item:
// release on the same item opens it, release on another item of the same
// list (pinned chats, teams) moves it there.
func (m *Model) sidebarDrag(msg tea.MouseMsg) tea.Cmd {
	target, ok := m.sidebarItemAt(msg)
	if msg.Action == tea.MouseActionMotion {
		if ok && target.key != m.dragKey {
			if _, _, can := m.reorderGroup(m.dragKey); can {
				m.setToast("Release to move here", false)
			}
		}
		return nil
	}
	drag := m.dragKey
	m.dragKey = ""
	if !ok {
		return nil
	}
	if target.key == drag {
		m.sideSel[m.section] = drag
		if target.open != nil {
			return target.open(m)
		}
		return nil
	}
	if m.dropOn(drag, target.key) {
		m.setToast("Moved", false)
	}
	return nil
}

func (m *Model) openItemActions(key string) {
	key = strings.TrimPrefix(key, "u:")
	switch {
	case strings.HasPrefix(key, "chat:"):
		if c := m.chatByID(strings.TrimPrefix(key, "chat:")); c != nil {
			m.openChatActions(c)
		}
	case strings.HasPrefix(key, "team:"):
		if t := m.teamByID(strings.TrimPrefix(key, "team:")); t != nil {
			m.openTeamActions(*t)
		}
	}
}

func (m *Model) clickMessage(cv *convView, row int, right bool) tea.Cmd {
	line := cv.scroll + row
	if line < 0 || line >= len(cv.owner) {
		return nil
	}
	idx := cv.owner[line]
	if idx == -2 {
		return m.loadOlder(cv)
	}
	if idx < 0 {
		return nil
	}
	// clicking in the channel pane while a thread is open opens that post's thread
	if cv == m.conv && m.thread != nil {
		return m.openThread(cv.ref, cv.msgs[idx].ID)
	}
	m.setFocus(FocusMessages)
	cv.selectMsg(idx)
	if right {
		return m.msgAction("more")
	}
	return nil
}

func (m *Model) clickZones(msg tea.MouseMsg) (tea.Cmd, bool) {
	in := func(id string) bool { return zone.Get(id).InBounds(msg) }

	// rail
	for i := Section(0); i < numSections; i++ {
		if in("rail:" + itoa(int(i))) {
			cmd := m.switchSection(i)
			if m.focus != FocusCompose {
				m.setFocus(FocusSidebar)
			}
			return cmd, true
		}
	}
	if in("rail:help") {
		m.openHelp()
		return nil, true
	}
	if in("status:join") {
		return m.joinEvent(m.upcomingMeeting()), true
	}
	if in("rail:me") || in("status:me") {
		m.openPresencePicker()
		return nil, true
	}

	// sidebar header buttons
	for _, id := range []string{"filter", "newchat", "refresh", "prevday", "today", "nextday", "newmeeting", "filesmode", "readall"} {
		if !in("sb:" + id) {
			continue
		}
		switch id {
		case "filter":
			m.setFocus(FocusSidebar)
			m.filtering = true
			m.chatFilter.Focus()
		case "newchat":
			m.openPeoplePicker("New chat", true, "")
		case "refresh":
			return m.refresh(), true
		case "prevday":
			return m.shiftDay(-1), true
		case "nextday":
			return m.shiftDay(1), true
		case "today":
			return m.shiftDay(0), true
		case "newmeeting":
			m.openMeetingForm("")
		case "filesmode":
			m.fileStack = nil
			if m.filesKey == "root" {
				m.filesKey = ""
			} else {
				m.filesKey = "root"
			}
			m.files = nil
			return m.loadFiles(), true
		case "readall":
			m.markAllActivityRead()
		}
		return nil, true
	}
	if m.section == SecSearch && m.geo.sideList.y > 0 && msg.Y < m.geo.sideList.y && msg.X >= m.railWidth() && msg.X < m.railWidth()+m.sidebarWidth() {
		m.setFocus(FocusSidebar)
		m.searchInput.Focus()
		return nil, true
	}

	// conversation header
	for _, id := range []string{"back", "closethread", "call", "video", "people", "more", "meet", "join"} {
		if !in("hdr:" + id) {
			continue
		}
		switch id {
		case "join":
			if m.conv != nil {
				if c := m.chatByID(m.conv.ref.ChatID); c != nil && c.JoinURL != "" {
					m.setToast("Joining meeting in Teams…", false)
					return m.openLink(c.JoinURL), true
				}
			}
		case "back", "closethread":
			m.closeThread()
		case "call", "video":
			if l := m.callLink(id == "video"); l != "" {
				m.setToast("Starting call in Teams…", false)
				return m.openLink(l), true
			}
			m.setToast("No callable members found", true)
		case "people", "more":
			if m.conv != nil && m.conv.ref.Kind == model.KindChat {
				if c := m.chatByID(m.conv.ref.ChatID); c != nil {
					if id == "people" {
						m.openMembers(c)
					} else {
						m.openChatActions(c)
					}
				}
			} else if m.conv != nil {
				m.openChannelActions(m.conv.ref)
			}
		case "meet":
			m.openMeetingForm("")
		}
		return nil, true
	}
	for i := 0; i < 2; i++ {
		if in("tab:" + itoa(i)) {
			m.mainTab = i
			if i == 1 && m.conv != nil {
				return m.loadTabFiles(m.conv.ref), true
			}
			return nil, true
		}
	}
	for i := range m.tabFiles {
		if in("tabfile:" + itoa(i)) {
			f := m.tabFiles[i]
			return m.fileAction("open", &f), true
		}
	}

	// message widgets
	for _, cv := range []*convView{m.geo.msgsConv, m.geo.threadConv} {
		if cv == nil {
			continue
		}
		if in("older") {
			return m.loadOlder(cv), true
		}
		for i := range cv.msgs {
			msgItem := &cv.msgs[i]
			if in("thread:"+msgItem.ID) || in("thread:"+msgItem.ID+":open") {
				cmd := m.openThread(cv.ref, msgItem.ID)
				m.setFocus(FocusMessages)
				if msgItem.ReplyCount == 0 {
					m.setFocus(FocusCompose)
				}
				return cmd, true
			}
			if in("thread:" + msgItem.ID + ":reply") {
				cmd := m.openThread(cv.ref, msgItem.ID)
				m.setFocus(FocusCompose)
				return cmd, true
			}
			for ai, a := range msgItem.Attachments {
				if in("att:" + msgItem.ID + ":" + itoa(ai)) {
					return m.openLink(a.ContentURL), true
				}
			}
			for _, r := range msgItem.Reactions {
				if in("react:" + msgItem.ID + ":" + r.Type) {
					return m.toggleReaction(cv.ref, msgItem.ID, r.Type), true
				}
			}
		}
	}
	for _, cv := range []*convView{m.geo.msgsConv, m.geo.threadConv} {
		if cv == nil {
			continue
		}
		for id, ref := range cv.cardActs {
			if in(id) {
				return m.runCardAction(cv, ref), true
			}
		}
	}
	for _, a := range []string{"reply", "react", "edit", "delete", "copy", "open", "more", "card"} {
		if in("act:" + a) {
			return m.msgAction(a), true
		}
	}
	for _, e := range quickReactions {
		if in("quick:" + e) {
			if cv := m.activeConv(); cv != nil {
				if sel := cv.selected(); sel != nil {
					return m.toggleReaction(cv.ref, sel.ID, e), true
				}
			}
		}
	}

	// compose toolbar
	c := &m.compose
	for i := range c.mCands {
		if in("mention:" + itoa(i)) {
			c.mSel = i
			c.acceptMention()
			m.setFocus(FocusCompose)
			return nil, true
		}
	}
	if in("unquote") {
		c.quote = nil
		return nil, true
	}
	for i := range c.attachments {
		if in("unattach:" + itoa(i)) {
			c.attachments = append(c.attachments[:i], c.attachments[i+1:]...)
			return nil, true
		}
	}
	for _, id := range []string{"bold", "italic", "code", "emoji", "attach", "important", "subject", "send"} {
		if !in("tb:" + id) {
			continue
		}
		m.setFocus(FocusCompose)
		switch id {
		case "bold":
			c.ta.InsertString("****")
			c.ta.SetCursor(max(0, c.ta.LineInfo().CharOffset-2))
		case "italic":
			c.ta.InsertString("**")
			c.ta.SetCursor(max(0, c.ta.LineInfo().CharOffset-1))
		case "code":
			c.ta.InsertString("``")
			c.ta.SetCursor(max(0, c.ta.LineInfo().CharOffset-1))
		case "emoji":
			m.openEmojiPicker("Insert emoji", func(m *Model, e string) tea.Cmd {
				m.compose.ta.InsertString(e)
				m.setFocus(FocusCompose)
				return nil
			})
		case "attach":
			m.openAttachPrompt()
		case "important":
			if m.activeConv() != nil && m.activeConv().ref.Kind == model.KindChat {
				switch c.importance {
				case "":
					c.importance = "high"
				case "high":
					c.importance = "urgent"
				default:
					c.importance = ""
				}
			} else {
				c.importance = toggle(c.importance, "high")
			}
			label := map[string]string{"": "normal", "high": "important", "urgent": "urgent"}[c.importance]
			m.setToast("Importance: "+label, false)
		case "subject":
			m.openPrompt("Post subject", "Subject", c.subject, func(m *Model, s string) tea.Cmd {
				m.compose.subject = strings.TrimSpace(s)
				m.setFocus(FocusCompose)
				return nil
			})
		case "send":
			return m.submitCompose(), true
		}
		return nil, true
	}

	// calendar
	for i := 0; i < 7; i++ {
		if in("day:" + itoa(i)) {
			monday := m.calDay.AddDate(0, 0, -((int(m.calDay.Weekday()) + 6) % 7))
			m.calDay = monday.AddDate(0, 0, i)
			m.sideSel[SecCalendar] = ""
			m.sideTouched[SecCalendar] = false
			return nil, true
		}
	}
	for _, a := range []string{"join", "accept", "tentative", "decline", "web", "chat", "transcript", "recording"} {
		if !in("ev:" + a) {
			continue
		}
		e := m.selectedEvent()
		switch a {
		case "chat":
			return m.openMeetingChat(e), true
		case "transcript":
			return m.showTranscript(e), true
		case "recording":
			return m.downloadRecording(e), true
		}
		return m.eventAction(a), true
	}
	if e := m.selectedEvent(); e != nil && m.section == SecCalendar {
		for i, a := range e.Attendees {
			if in("evatt:" + itoa(i)) {
				u, ok := m.emailUsers[strings.ToLower(a.Email)]
				if !ok {
					u = model.User{DisplayName: a.Name, Email: a.Email}
				}
				m.openProfile(u)
				return nil, true
			}
		}
	}
	for _, a := range []string{"open", "download", "copy", "enter"} {
		if in("file:" + a) {
			return m.fileAction(a, m.selectedFile()), true
		}
	}
	return nil, false
}

func (m *Model) openChannelActions(ref model.ConvRef) {
	r := ref
	r.ThreadID = ""
	items := []modalItem{
		{label: "📝 New post with subject", run: func(m *Model) tea.Cmd {
			m.openPrompt("Post subject", "Subject", "", func(m *Model, s string) tea.Cmd {
				m.compose.subject = strings.TrimSpace(s)
				m.setFocus(FocusCompose)
				return nil
			})
			return nil
		}},
		{label: "📁 Files", run: func(m *Model) tea.Cmd { m.mainTab = 1; return m.loadTabFiles(r) }},
		{label: "👥 Members", run: func(m *Model) tea.Cmd {
			us := m.members[r.Key()]
			c := &model.Chat{Members: us}
			m.openMembers(c)
			return nil
		}},
		{label: "🔕 Toggle mute", run: func(m *Model) tea.Cmd {
			m.st.Muted[r.Key()] = !m.st.Muted[r.Key()]
			_ = m.st.Save()
			return nil
		}},
		{label: "📅 Schedule meeting", run: func(m *Model) tea.Cmd { m.openMeetingForm(""); return nil }},
		{label: "↻ Refresh", run: func(m *Model) tea.Cmd { return m.loadMessages(r, "", false, true) }},
	}
	m.openList(m.convTitle(r), items, false)
}

// eventSelectionChanged loads details when the selected calendar event changes.
func (m *Model) eventSelectionChanged() tea.Cmd {
	if m.section != SecCalendar {
		return nil
	}
	e := m.selectedEvent()
	if e == nil || e.ID == m.lastEventSel {
		return nil
	}
	m.lastEventSel = e.ID
	return m.onEventSelected()
}

func (m *Model) eventForChat(chatID string) *model.Event {
	var best *model.Event
	now := time.Now()
	for i := range m.events {
		e := &m.events[i]
		if e.ChatID != chatID {
			continue
		}
		// prefer the live / next occurrence
		if best == nil || (e.End.After(now) && (best.End.Before(now) || e.Start.Before(best.Start))) {
			best = e
		}
	}
	return best
}
