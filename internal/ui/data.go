package ui

import (
	"os"
	"slices"
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
	return m.chatPagesCmd(false)
}

func (m *Model) chatPagesCmd(more bool) tea.Cmd {
	if m.pollingChats || (more && m.nextChats == "") {
		return nil
	}
	m.pollingChats = true
	pages := m.chatPages
	var cursor model.Page
	if more {
		cursor = m.nextChats
		pages = 1
	}
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		result := chatsMsg{more: more}
		for range pages {
			chats, next, err := svc.Chats(c, cursor)
			if err != nil {
				return chatsMsg{err: err}
			}
			result.chats = append(result.chats, chats...)
			result.next = next
			if next == "" {
				break
			}
			cursor = next
		}
		return result
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
	cv := m.convs[ref.Key()]
	if cv == nil {
		return nil
	}
	if !older {
		cv.headRequest++
		cv.fetchingHead = true
	}
	request := cv.headRequest
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ms, next, err := svc.Messages(c, ref, cursor)
		return messagesMsg{target: cv, request: request, ref: ref, msgs: ms, next: next, older: older, poll: poll, err: err}
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
	m.eventsRequest++
	request := m.eventsRequest
	svc := m.svc
	start := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7)) // monday of week
	end := start.AddDate(0, 0, 7)
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ev, err := svc.Events(c, start, end)
		return eventsMsg{request: request, events: ev, err: err}
	}
}

func (m *Model) loadFiles() tea.Cmd {
	m.filesLoading = true
	m.filesRequest++
	request := m.filesRequest
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
		return filesMsg{request, key, items, err}
	}
}

func (m *Model) loadTabFiles(ref model.ConvRef) tea.Cmd {
	m.tabRequest++
	request := m.tabRequest
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		items, err := svc.ChannelFiles(c, ref.TeamID, ref.ChannelID)
		return filesMsg{request, "tab:" + ref.Key(), items, err}
	}
}

func (m *Model) runSearch(q string) tea.Cmd {
	m.searchRequest++
	request := m.searchRequest
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		h, err := svc.SearchMessages(c, q)
		return searchMsg{request, q, h, err}
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
	case readMsg:
		return m.finishRead(msg)
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
		m.nextChats = msg.next
		if msg.more {
			m.chatPages++
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
			isActive := m.viewingMessages(m.conv) && m.conv.ref.Kind == model.KindChat && m.conv.ref.ChatID == c.ID
			fromOther := c.LastMessage.From.ID != m.me.ID && c.LastMessage.From.ID != ""
			if first && c.Unread(m.me.ID) {
				m.addActivity(activityItem{at: c.LastMessage.Created, kind: "message", title: firstName(c.LastMessage.From.DisplayName) + " messaged you", preview: c.Title(m.me.ID) + ": " + c.LastMessage.Text, ref: model.ConvRef{Kind: model.KindChat, ChatID: c.ID}, msgID: c.LastMessage.ID})
			}
			if seen && prev != c.LastMessage.ID && fromOther && !c.LastMessage.Deleted {
				if isActive {
					cmds = append(cmds, m.loadMessages(m.conv.ref, "", false, true))
				}
				m.addActivity(activityItem{at: c.LastMessage.Created, kind: "message", title: firstName(c.LastMessage.From.DisplayName) + " messaged you", preview: c.Title(m.me.ID) + ": " + c.LastMessage.Text, ref: model.ConvRef{Kind: model.KindChat, ChatID: c.ID}, msgID: c.LastMessage.ID, read: isActive})
				if !m.st.Muted["chat:"+c.ID] && !isActive {
					m.notify(c.LastMessage.From.DisplayName, c.LastMessage.Text, c.Title(m.me.ID))
				}
			}
		}
		// keep locally-read chats read even if the server lags behind
		old := map[string]time.Time{}
		for _, c := range m.chats {
			old[c.ID] = c.LastRead
		}
		if !msg.more {
			m.chats = nil
		}
		seenChats := map[string]bool{}
		for _, chat := range m.chats {
			seenChats[chat.ID] = true
		}
		for _, chat := range msg.chats {
			if !seenChats[chat.ID] {
				m.chats = append(m.chats, chat)
				seenChats[chat.ID] = true
			}
		}
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
		if cv := m.convs[msg.ref.Key()]; cv != nil {
			cv.merge(msg.msgs, false)
		}

	case channelsMsg:
		if msg.err != nil {
			m.errToast(msg.err)
		}
		m.channels[msg.teamID] = msg.channels

	case messagesMsg:
		cv := m.convs[msg.ref.Key()]
		if cv == nil || cv != msg.target || (!msg.older && msg.request != cv.headRequest) {
			return nil
		}
		if !msg.older {
			cv.fetchingHead = false
		}
		if msg.older || !cv.loadedOnce {
			cv.loading = false
			cv.dirty = true
		}
		if msg.err != nil {
			if !msg.poll || !cv.loadedOnce {
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
			viewing := m.viewingMessages(cv)
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
		if cv == m.activeConv() && cv.pendingSelect != "" && m.viewingMessages(cv) {
			if i, ok := cv.index[cv.pendingSelect]; ok {
				cv.selectMsg(i)
				cv.pendingSelect = ""
				m.setFocus(FocusMessages)
			}
		}
		if cv.ref.Kind == model.KindChat && cv == m.conv && !msg.older {
			if changed {
				m.awaitingRead[cv.ref.ChatID] = true
			}
			return m.readVisibleChat()
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
		if msg.request != m.eventsRequest {
			return nil
		}
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
		tab := strings.HasPrefix(msg.key, "tab:")
		if tab {
			if msg.request != m.tabRequest || m.conv == nil || msg.key != "tab:"+m.conv.ref.Key() || m.mainTab != 1 {
				return nil
			}
		} else {
			cur := m.fileLoc
			if len(m.fileStack) > 0 {
				cur += "/" + m.fileStack[len(m.fileStack)-1].ID
			}
			if msg.request != m.filesRequest || msg.key != cur {
				return nil
			}
		}
		if !tab {
			m.filesLoading = false
		}
		if msg.err != nil {
			m.errToast(msg.err)
			return nil
		}
		if tab {
			m.tabFiles = msg.items
			return nil
		}
		if strings.HasPrefix(msg.key, "loc:chan:") && !strings.Contains(msg.key, "/") {
			m.fileCounts[msg.key] = len(msg.items)
		}
		m.files = msg.items
		m.fileSel = max(0, min(m.fileSel, len(m.files)-1))

	case searchMsg:
		m.busy = max(0, m.busy-1)
		if msg.request != m.searchRequest || msg.q != m.searchQ {
			return nil
		}
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
		m.finishUpload(msg)

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
	if m.readInFlight[chatID] {
		m.awaitingRead[chatID] = true
		return nil
	}
	delete(m.awaitingRead, chatID)
	m.readInFlight[chatID] = true
	at := time.Now()
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		return readMsg{chatID, at, svc.MarkChatRead(c, chatID)}
	}
}

type readMsg struct {
	chatID string
	at     time.Time
	err    error
}

func (m *Model) finishRead(msg readMsg) tea.Cmd {
	delete(m.readInFlight, msg.chatID)
	if msg.err != nil {
		m.awaitingRead[msg.chatID] = true
		m.errToast(msg.err)
		return nil
	}
	for i := range m.chats {
		c := &m.chats[i]
		if c.ID == msg.chatID && c.LastRead.Before(msg.at) {
			c.LastRead = msg.at
		}
	}
	for i := range m.activity {
		a := &m.activity[i]
		if a.ref.Kind == model.KindChat && a.ref.ChatID == msg.chatID && !a.at.After(msg.at) {
			a.read = true
		}
	}
	return m.readVisibleChat()
}

func (m *Model) viewingMessages(cv *convView) bool {
	if cv == nil || !m.focused || m.modal != nil || (m.ready && (m.w < 50 || m.h < 12)) {
		return false
	}
	if !m.meetingMode() && (m.section == SecCalendar || m.section == SecSettings) {
		return false
	}
	if cv == m.conv && cv.ref.Kind == model.KindChannel && m.mainTab == 1 {
		return false
	}
	return cv == m.activeConv() || (cv == m.conv && m.splitThread())
}

func (m *Model) readVisibleChat() tea.Cmd {
	cv := m.conv
	if m.viewingMessages(cv) && cv.ref.Kind == model.KindChat && m.awaitingRead[cv.ref.ChatID] && !m.readInFlight[cv.ref.ChatID] {
		return m.markRead(cv.ref.ChatID)
	}
	return nil
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
	for _, x := range incoming {
		i, ok := cv.index[x.ID]
		if !ok || cv.msgs[i].From.ID != m.me.ID || len(x.Reactions) == 0 {
			continue
		}
		previous := cv.msgs[i].Reactions
		for _, r := range x.Reactions {
			if r.UserID == m.me.ID || slices.ContainsFunc(previous, func(old model.Reaction) bool { return old.UserID == r.UserID && old.Type == r.Type }) {
				continue
			}
			who := firstName(m.userName(r.UserID))
			m.addActivity(activityItem{at: time.Now(), kind: "reaction", title: who + " reacted " + r.Type,
				preview: "to: " + htmltext.Plain(x.Body), ref: cv.ref, msgID: x.ID + ":r:" + r.UserID + r.Type})
		}
	}
}
