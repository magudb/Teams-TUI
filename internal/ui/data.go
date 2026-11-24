package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
	"github.com/magudb/Teams-TUI/internal/notify"
)

// ---- commands -------------------------------------------------------------------

func (m *Model) loadMe() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		u, err := svc.Me(c)
		return meMsg{u, err}
	}
}

func (m *Model) loadMyPresence() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		p, err := svc.MyPresence(c)
		return myPresenceMsg{p, err}
	}
}

func (m *Model) loadChats() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ch, err := svc.Chats(c)
		return chatsMsg{ch, err}
	}
}

func (m *Model) loadTeams() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		t, err := svc.Teams(c)
		return teamsMsg{t, err}
	}
}

func (m *Model) loadChannels(teamID string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ch, err := svc.Channels(c, teamID)
		return channelsMsg{teamID, ch, err}
	}
}

func (m *Model) loadMessages(ref model.ConvRef, cursor model.Page, older, poll bool) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ms, next, err := svc.Messages(c, ref, cursor)
		return messagesMsg{ref: ref, msgs: ms, next: next, older: older, poll: poll, err: err}
	}
}

func (m *Model) loadMembers(ref model.ConvRef) tea.Cmd {
	svc := m.svc
	ref.ThreadID = ""
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		us, err := svc.Members(c, ref)
		return membersMsg{ref, us, err}
	}
}

func (m *Model) loadPresence() tea.Cmd {
	ids := map[string]bool{}
	for _, c := range m.chats {
		for _, u := range c.Members {
			if u.ID != "" && u.ID != m.me.ID && !strings.HasPrefix(u.ID, "app:") {
				ids[u.ID] = true
			}
		}
	}
	for _, u := range m.emailUsers {
		ids[u.ID] = true
	}
	if ac := m.activeConv(); ac != nil {
		for _, u := range m.members[ac.ref.Key()] {
			if u.ID != "" {
				ids[u.ID] = true
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		p, err := svc.Presence(c, list)
		return presenceMsg{p, err}
	}
}

func (m *Model) loadEvents(day time.Time) tea.Cmd {
	svc := m.svc
	start := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7)) // monday of week
	end := start.AddDate(0, 0, 7)
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ev, err := svc.Events(c, start, end)
		return eventsMsg{day, ev, err}
	}
}

func (m *Model) loadFiles() tea.Cmd {
	svc := m.svc
	loc := m.fileLoc
	var parent *model.DriveItem
	key := loc
	if len(m.fileStack) > 0 {
		p := m.fileStack[len(m.fileStack)-1]
		parent = &p
		key = loc + "/" + p.ID
	}
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		var items []model.DriveItem
		var err error
		switch {
		case parent != nil:
			items, err = svc.Folder(c, parent)
		case loc == "loc:root":
			items, err = svc.Folder(c, nil)
		case loc == "loc:shared":
			items, err = svc.SharedFiles(c)
		case strings.HasPrefix(loc, "loc:chan:"):
			parts := strings.SplitN(strings.TrimPrefix(loc, "loc:chan:"), ":", 2)
			if len(parts) == 2 {
				items, err = svc.ChannelFiles(c, parts[0], parts[1])
			}
		default:
			items, err = svc.RecentFiles(c)
		}
		return filesMsg{key, items, err}
	}
}

func (m *Model) loadTabFiles(ref model.ConvRef) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		items, err := svc.ChannelFiles(c, ref.TeamID, ref.ChannelID)
		return filesMsg{"tab:" + ref.Key(), items, err}
	}
}

func (m *Model) runSearch(q string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		h, err := svc.SearchMessages(c, q)
		return searchMsg{q, h, err}
	}
}

func (m *Model) searchPeople(q string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		u, err := svc.SearchPeople(c, q)
		return peopleMsg{q, u, err}
	}
}

// op runs fn in the background and reports the result as an opMsg.
func (m *Model) op(fn func(svc model.Service) opMsg) tea.Cmd {
	svc := m.svc
	m.busy++
	return func() tea.Msg { return fn(svc) }
}

// ---- data handling ------------------------------------------------------------

func (m *Model) handleData(msg tea.Msg) tea.Cmd {
	if c, ok := m.handleMeetingData(msg); ok {
		return c
	}
	switch msg := msg.(type) {
	case meMsg:
		if msg.err != nil {
			m.errToast(msg.err)
			return nil
		}
		m.me = msg.me
		m.rememberUsers(msg.me)
		m.invalidateRender()

	case myPresenceMsg:
		if msg.err == nil {
			m.myPresence = msg.p
		}

	case chatsMsg:
		m.pollingChats = false
		if msg.err != nil {
			m.errToast(msg.err)
			return nil
		}
		first := !m.chatsLoaded
		m.chatsLoaded = true
		var cmds []tea.Cmd
		for _, c := range msg.chats {
			m.rememberUsers(c.Members...)
			if c.LastMessage == nil {
				continue
			}
			prev, seen := m.lastPreview[c.ID]
			m.lastPreview[c.ID] = c.LastMessage.ID
			isActive := m.conv != nil && m.conv.ref.Kind == model.KindChat && m.conv.ref.ChatID == c.ID
			fromOther := c.LastMessage.From.ID != m.me.ID && c.LastMessage.From.ID != ""
			if first && c.Unread(m.me.ID) {
				m.addActivity(activityItem{at: c.LastMessage.Created, kind: "message", title: firstName(c.LastMessage.From.DisplayName) + " messaged you", preview: c.Title(m.me.ID) + ": " + c.LastMessage.Text, ref: model.ConvRef{Kind: model.KindChat, ChatID: c.ID}, msgID: c.LastMessage.ID})
			}
			if seen && prev != c.LastMessage.ID && fromOther && !c.LastMessage.Deleted {
				if isActive {
					cmds = append(cmds, m.loadMessages(m.conv.ref, "", false, true))
				}
				m.addActivity(activityItem{at: c.LastMessage.Created, kind: "message", title: firstName(c.LastMessage.From.DisplayName) + " messaged you", preview: c.Title(m.me.ID) + ": " + c.LastMessage.Text, ref: model.ConvRef{Kind: model.KindChat, ChatID: c.ID}, msgID: c.LastMessage.ID, read: isActive && m.focused})
				if !m.st.Muted["chat:"+c.ID] && (!isActive || !m.focused) {
					m.notify(c.LastMessage.From.DisplayName, c.LastMessage.Text, c.Title(m.me.ID))
				}
			}
		}
		// keep locally-read chats read even if the server lags behind
		old := map[string]time.Time{}
		for _, c := range m.chats {
			old[c.ID] = c.LastRead
		}
		m.chats = msg.chats
		for i := range m.chats {
			if t, ok := old[m.chats[i].ID]; ok && t.After(m.chats[i].LastRead) {
				m.chats[i].LastRead = t
			}
		}
		m.updateTitle()
		if first {
			cmds = append(cmds, m.loadPresence())
			m.lastPollPresence = time.Now()
		}
		return tea.Batch(cmds...)

	case teamsMsg:
		if msg.err != nil {
			m.errToast(msg.err)
		}
		m.teams = msg.teams
		var cmds []tea.Cmd
		for _, t := range m.teams {
			// all channels are needed for the combined view and unread tracking
			if _, ok := m.channels[t.ID]; !ok || m.st.Expanded[t.ID] {
				cmds = append(cmds, m.loadChannels(t.ID))
			}
		}
		return tea.Batch(cmds...)

	case channelPollMsg:
		m.pollingChannel = false
		if msg.err != nil {
			if strings.Contains(msg.err.Error(), "403") {
				m.chanPollDisabled = true
				m.setToast("Channel unread tracking disabled: no permission to read channel messages", true)
			}
			return nil
		}
		m.noteChannelMessages(msg.ref, msg.msgs, false)
		if cv := m.convs[msg.ref.Key()]; cv != nil && cv.merge(msg.msgs, false) {
			cv.dirty = true
		}

	case channelsMsg:
		if msg.err != nil {
			m.errToast(msg.err)
		}
		m.channels[msg.teamID] = msg.channels

	case messagesMsg:
		if msg.poll {
			m.pollingMsgs = false
		}
		cv := m.convs[msg.ref.Key()]
		if cv == nil {
			return nil
		}
		cv.loading = false
		if msg.err != nil {
			if !msg.poll {
				cv.err = oneLineErr(msg.err)
				m.errToast(msg.err)
			}
			return nil
		}
		cv.err = ""
		for _, mm := range msg.msgs {
			m.rememberUsers(mm.From)
			m.scanMentions(cv, mm)
		}
		if cv.ref.Kind == model.KindChannel && !msg.older {
			viewing := cv == m.conv || cv == m.thread
			if cv.ref.ThreadID == "" {
				m.noteChannelMessages(cv.ref, msg.msgs, viewing)
			} else if viewing {
				m.markChannelSeen(cv.ref)
			}
		}
		if cv.loadedOnce && !msg.older {
			m.noteReactions(cv, msg.msgs)
		}
		changed := cv.merge(msg.msgs, msg.older)
		if msg.older || !cv.loadedOnce {
			cv.next = msg.next
		}
		cv.loadedOnce = true
		if changed {
			cv.dirty = true
		}
		if m.pendingSelect != "" {
			for i := range cv.msgs {
				if cv.msgs[i].ID == m.pendingSelect {
					cv.selectMsg(i)
					m.pendingSelect = ""
					m.focus = FocusMessages
					m.compose.ta.Blur()
				}
			}
		}
		if cv.ref.Kind == model.KindChat && cv == m.conv && changed {
			return m.markRead(cv.ref.ChatID)
		}

	case sentMsg:
		cv := m.convs[msg.ref.Key()]
		if msg.err != nil {
			m.errToast(msg.err)
			if cv != nil {
				cv.markFailed(msg.tempID)
			}
			return nil
		}
		if cv != nil {
			cv.replaceTemp(msg.tempID, msg.msg)
		}
		if msg.ref.ThreadID != "" {
			parent := msg.ref
			parent.ThreadID = ""
			if pc := m.convs[parent.Key()]; pc != nil {
				return m.loadMessages(parent, "", false, true)
			}
		}
		if msg.ref.Kind == model.KindChat {
			return m.loadChats()
		}

	case presenceMsg:
		if msg.err == nil {
			for k, v := range msg.p {
				m.presence[k] = v
			}
		}

	case eventsMsg:
		if msg.err != nil {
			m.errToast(msg.err)
			return nil
		}
		m.events = msg.events
		m.checkUpcoming()
		if m.section == SecCalendar {
			return m.onEventSelected()
		}

	case filesMsg:
		if msg.err != nil {
			m.errToast(msg.err)
			return nil
		}
		if strings.HasPrefix(msg.key, "tab:") {
			m.tabFiles = msg.items
			return nil
		}
		if strings.HasPrefix(msg.key, "loc:chan:") && !strings.Contains(msg.key, "/") {
			m.fileCounts[msg.key] = len(msg.items)
		}
		cur := m.fileLoc
		if len(m.fileStack) > 0 {
			cur += "/" + m.fileStack[len(m.fileStack)-1].ID
		}
		if msg.key != cur {
			return nil // stale
		}
		m.files = msg.items
		m.fileSel = max(0, min(m.fileSel, len(m.files)-1))

	case searchMsg:
		if msg.q != m.searchQ {
			return nil
		}
		m.busy = max(0, m.busy-1)
		if msg.err != nil {
			m.errToast(msg.err)
			return nil
		}
		m.hits = msg.hits
		if len(m.hits) == 0 {
			m.setToast("No results for "+msg.q, false)
		}

	case debounceMsg:
		if m.modal != nil && m.modal.kind == mPeople && m.modal.debounce == msg.id {
			return m.searchPeople(msg.q)
		}

	case peopleMsg:
		if m.modal != nil {
			m.modal.setPeople(msg)
		}

	case membersMsg:
		if msg.err == nil {
			m.members[msg.ref.Key()] = msg.users
			m.rememberUsers(msg.users...)
		}

	case chatCreatedMsg:
		m.busy = max(0, m.busy-1)
		if msg.err != nil {
			m.errToast(msg.err)
			return nil
		}
		found := false
		for _, c := range m.chats {
			if c.ID == msg.chat.ID {
				found = true
			}
		}
		if !found {
			m.chats = append([]model.Chat{msg.chat}, m.chats...)
		}
		m.rememberUsers(msg.chat.Members...)
		m.section = SecChat
		m.sideSel[SecChat] = "chat:" + msg.chat.ID
		return tea.Batch(m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: msg.chat.ID}), m.focusCompose())

	case uploadMsg:
		m.busy = max(0, m.busy-1)
		if msg.err != nil {
			m.errToast(msg.err)
			return nil
		}
		m.compose.attachments = append(m.compose.attachments, msg.att)
		m.setToast("Attached "+msg.att.Name, false)

	case opMsg:
		m.busy = max(0, m.busy-1)
		if msg.err != nil {
			m.errToast(msg.err)
		} else if msg.toast != "" {
			m.setToast(msg.toast, false)
		}
		var cmds []tea.Cmd
		if !msg.reload.IsZero() {
			cmds = append(cmds, m.loadMessages(msg.reload, "", false, true))
			if msg.reload.ThreadID != "" {
				p := msg.reload
				p.ThreadID = ""
				if m.convs[p.Key()] != nil {
					cmds = append(cmds, m.loadMessages(p, "", false, true))
				}
			}
		}
		if msg.chats {
			cmds = append(cmds, m.loadChats())
		}
		if msg.events {
			cmds = append(cmds, m.loadEvents(m.calDay))
		}
		if msg.presenc {
			cmds = append(cmds, m.loadMyPresence())
		}
		if msg.reloadFiles {
			cmds = append(cmds, m.loadFiles())
		}
		if msg.open != "" && msg.err == nil {
			if err := notify.Open(msg.open); err != nil {
				m.errToast(err)
			}
		}
		return tea.Batch(cmds...)
	}
	return nil
}

func (m *Model) markRead(chatID string) tea.Cmd {
	for i := range m.chats {
		if m.chats[i].ID == chatID {
			m.chats[i].LastRead = time.Now()
		}
	}
	for i := range m.activity {
		if m.activity[i].ref.Kind == model.KindChat && m.activity[i].ref.ChatID == chatID {
			m.activity[i].read = true
		}
	}
	m.updateTitle()
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		_ = svc.MarkChatRead(c, chatID)
		return nil
	}
}

func (m *Model) unreadCount() int {
	n := 0
	if m.cfg.CombinedChat {
		n += len(m.unreadChannels())
	}
	for _, c := range m.chats {
		if c.Unread(m.me.ID) && !m.st.Muted["chat:"+c.ID] {
			n++
		}
	}
	return n
}

func (m *Model) unreadActivity() int {
	n := 0
	for _, a := range m.activity {
		if !a.read {
			n++
		}
	}
	return n
}

func (m *Model) updateTitle() {}

func (m *Model) addActivity(a activityItem) {
	for _, x := range m.activity {
		if x.msgID == a.msgID && a.msgID != "" {
			return
		}
	}
	m.activity = append(m.activity, a)
	sort.SliceStable(m.activity, func(i, j int) bool { return m.activity[i].at.After(m.activity[j].at) })
	if len(m.activity) > 200 {
		m.activity = m.activity[:200]
	}
}

func (m *Model) scanMentions(cv *convView, mm model.Message) {
	if m.me.ID == "" || mm.From.ID == m.me.ID {
		return
	}
	for _, mention := range mm.Mentions {
		if mention.UserID == m.me.ID {
			title := firstName(mm.From.DisplayName) + " mentioned you"
			preview := htmltext.Plain(mm.Body)
			if t := m.convTitle(cv.ref); t != "" {
				preview = t + ": " + preview
			}
			m.addActivity(activityItem{
				at: mm.Created, kind: "mention", title: title, preview: preview,
				ref: cv.ref, msgID: mm.ID, read: time.Since(mm.Created) > 7*24*time.Hour,
			})
			return
		}
	}
}

func (m *Model) notify(from, text, where string) {
	if m.cfg.Bell {
		_, _ = os.Stdout.WriteString("\a")
	}
	if m.cfg.DesktopNotifications && !m.demo {
		title := from
		if where != "" && where != from {
			title += " · " + where
		}
		notify.Desktop(title, text)
	}
	m.setToast("💬 "+from+": "+plainOneLine(text), false)
}

var notifiedEvents = map[string]bool{}

// checkUpcoming notifies about meetings starting within the next 5 minutes.
func (m *Model) checkUpcoming() {
	now := time.Now()
	for _, e := range m.events {
		if e.IsCancelled || e.Response == "declined" || notifiedEvents[e.ID] {
			continue
		}
		if d := e.Start.Sub(now); d > 0 && d <= time.Duration(m.cfg.MeetingReminderMinutes)*time.Minute {
			notifiedEvents[e.ID] = true
			prev := "Starts " + e.Start.Format(m.cfg.TimeFormat)
			if n := len(e.Attendees); n > 0 {
				prev += " · " + itoa(n) + " attendees"
			}
			m.addActivity(activityItem{at: now, kind: "meeting", title: e.Subject, preview: prev, msgID: e.ID})
			hint := ""
			if e.JoinURL != "" {
				hint = " — alt+j to join"
			}
			m.notify("Meeting starting soon", e.Subject+" at "+e.Start.Format(m.cfg.TimeFormat)+hint, "")
		}
	}
}

func (m *Model) convTitle(ref model.ConvRef) string {
	switch ref.Kind {
	case model.KindChat:
		for _, c := range m.chats {
			if c.ID == ref.ChatID {
				return c.Title(m.me.ID)
			}
		}
		if t := m.chatTitles[ref.ChatID]; t != "" {
			return t
		}
		return "Chat"
	case model.KindChannel:
		team, ch := "", ""
		for _, t := range m.teams {
			if t.ID == ref.TeamID {
				team = t.DisplayName
			}
		}
		for _, c := range m.channels[ref.TeamID] {
			if c.ID == ref.ChannelID {
				ch = c.DisplayName
			}
		}
		if ch == "" {
			ch = "channel"
		}
		if team != "" {
			return team + " › " + ch
		}
		return ch
	}
	return ""
}

func (m *Model) downloadPath(name string) string {
	dir := m.cfg.DownloadDir
	if dir == "" {
		dir = os.TempDir()
	}
	p := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
		p = filepath.Join(dir, base+" ("+itoa(i)+")"+ext)
	}
}

func firstName(n string) string {
	if f := strings.Fields(n); len(f) > 0 {
		return f[0]
	}
	return n
}

// noteReactions adds Activity items for new reactions by others on my messages.
func (m *Model) noteReactions(cv *convView, incoming []model.Message) {
	if m.me.ID == "" {
		return
	}
	old := map[string]map[string]bool{}
	for _, x := range cv.msgs {
		if x.From.ID != m.me.ID {
			continue
		}
		set := map[string]bool{}
		for _, r := range x.Reactions {
			set[r.UserID+r.Type] = true
		}
		old[x.ID] = set
	}
	for _, x := range incoming {
		prev, ok := old[x.ID]
		if !ok {
			continue
		}
		for _, r := range x.Reactions {
			if r.UserID == m.me.ID || prev[r.UserID+r.Type] {
				continue
			}
			who := firstName(m.userName(r.UserID))
			m.addActivity(activityItem{at: time.Now(), kind: "reaction", title: who + " reacted " + r.Type,
				preview: "to: " + htmltext.Plain(x.Body), ref: cv.ref, msgID: x.ID + ":r:" + r.UserID + r.Type})
		}
	}
}
