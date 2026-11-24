package ui

import (
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
	"github.com/magudb/Teams-TUI/internal/notify"
)

// openConv opens a chat or channel in the main pane.
func (m *Model) openConv(ref model.ConvRef) tea.Cmd {
	return m.openConvAt(ref, "")
}

// openConvAt opens a conversation and selects msgID once loaded (if given).
func (m *Model) openConvAt(ref model.ConvRef, msgID string) tea.Cmd {
	var cmds []tea.Cmd
	root := ref
	root.ThreadID = ""
	if m.conv == nil || m.conv.ref.Key() != root.Key() {
		m.mainTab = 0
		m.tabFiles = nil
	}
	cv := m.convs[root.Key()]
	if cv == nil {
		cv = newConvView(root)
		m.convs[root.Key()] = cv
		cmds = append(cmds, m.loadMessages(root, "", false, false))
	} else {
		cv.dirty = true
		cmds = append(cmds, m.loadMessages(root, "", false, true))
	}
	m.conv = cv
	m.thread = nil
	m.touchConversation(cv)
	cv.newSince = time.Time{}
	if root.Kind == model.KindChat {
		if c := m.chatByID(root.ChatID); c != nil && c.Unread(m.me.ID) {
			cv.newSince = c.LastRead
		}
	}
	cv.dirty = true
	if _, ok := m.members[root.Key()]; !ok {
		cmds = append(cmds, m.loadMembers(root))
	}
	if ref.ThreadID != "" {
		cmds = append(cmds, m.openThread(root, ref.ThreadID))
	}
	m.compose.bind(m.activeConv().ref.Key())
	if root.Kind == model.KindChat {
		m.awaitingRead[root.ChatID] = true
	}
	target := m.activeConv()
	target.pendingSelect = msgID
	if msgID != "" {
		target.follow = false
		if i, ok := target.index[msgID]; ok {
			target.selectMsg(i)
		}
	}
	if m.section == SecCalendar || m.section == SecFiles {
		m.section = SecChat
		if root.Kind == model.KindChannel && !m.cfg.CombinedChat {
			m.section = SecTeams
		}
	}
	if m.section == SecChat || m.section == SecTeams {
		want := "chat:" + root.ChatID
		if root.Kind == model.KindChannel {
			want = "chan:" + root.ChannelID
			m.markChannelSeen(root)
		}
		if cur := m.sideSel[m.section]; cur != want && cur != "u:"+want && cur != "fav:"+want {
			m.sideSel[m.section] = want
		}
		m.sideTouched[m.section] = true
	}
	m.lastPollMsgs = timeNow()
	if m.w < 100 && m.focus == FocusSidebar {
		m.setFocus(FocusMessages)
	}
	return tea.Batch(append(cmds, m.readVisibleChat())...)
}

func (m *Model) openThread(root model.ConvRef, threadID string) tea.Cmd {
	ref := root
	ref.ThreadID = threadID
	cv := m.convs[ref.Key()]
	var cmd tea.Cmd
	if cv == nil {
		cv = newConvView(ref)
		m.convs[ref.Key()] = cv
		cmd = m.loadMessages(ref, "", false, false)
	} else {
		cv.dirty = true
		cmd = m.loadMessages(ref, "", false, true)
	}
	m.thread = cv
	m.touchConversation(cv)
	m.compose.bind(ref.Key())
	return cmd
}

func (m *Model) closeThread() {
	if m.thread == nil {
		return
	}
	m.thread = nil
	if m.conv != nil {
		m.compose.bind(m.conv.ref.Key())
		m.conv.dirty = true
	}
}

// mainHasList reports whether the main pane is a navigable list (Files
// table, Settings) rather than a conversation.
func (m *Model) mainHasList() bool {
	return m.section == SecSettings || (m.section == SecFiles && m.conv == nil)
}

func (m *Model) switchSection(s Section) tea.Cmd {
	if m.liveMeeting != "" && s != SecCalendar {
		m.leaveMeetingView()
	}
	if s == m.section {
		return nil
	}
	m.section = s
	m.filtering = false
	m.chatFilter.Blur()
	m.searchInput.Blur()
	m.saveState()
	var cmd tea.Cmd
	switch s {
	case SecFiles:
		m.conv, m.thread = nil, nil
		if m.files == nil {
			cmd = m.loadFiles()
		}
	case SecCalendar:
		cmd = m.loadEvents(m.calDay)
	case SecSearch:
		m.setFocus(FocusSidebar)
		m.searchInput.Focus()
		return m.readVisibleChat()
	}
	if m.focus != FocusSidebar && (s == SecFiles || s == SecCalendar || s == SecSettings) {
		m.setFocus(FocusSidebar)
	}
	return tea.Batch(cmd, m.readVisibleChat())
}

func (m *Model) setFocus(f Focus) {
	if f == FocusCompose && m.activeConv() == nil {
		f = FocusSidebar
	}
	if f == FocusMessages && m.activeConv() == nil && !m.mainHasList() {
		f = FocusSidebar
	}
	m.focus = f
	if f == FocusCompose {
		m.compose.ta.Focus()
	} else {
		m.compose.ta.Blur()
	}
	if f != FocusSidebar {
		m.searchInput.Blur()
		m.chatFilter.Blur()
		m.filtering = false
	}
	if ac := m.activeConv(); ac != nil {
		if f == FocusMessages && ac.sel < 0 && len(ac.msgs) > 0 {
			ac.selectMsg(len(ac.msgs) - 1)
		}
		if f != FocusMessages && ac.sel >= 0 {
			ac.sel = -1
			ac.follow = true
		}
	}
}

func (m *Model) focusCompose() tea.Cmd {
	m.setFocus(FocusCompose)
	return nil
}

func (m *Model) cycleFocus(d int) {
	order := []Focus{FocusSidebar}
	switch {
	case m.mainHasList():
		order = append(order, FocusMessages)
	case m.activeConv() != nil && m.section != SecCalendar:
		order = append(order, FocusMessages, FocusCompose)
	}
	if !m.showSidebar && len(order) > 1 {
		order = order[1:]
	}
	idx := 0
	for i, f := range order {
		if f == m.focus {
			idx = i
		}
	}
	m.setFocus(order[(idx+d+len(order))%len(order)])
}

func (m *Model) doSearch() tea.Cmd {
	q := strings.TrimSpace(m.searchInput.Value())
	if q == "" {
		return nil
	}
	m.searchQ = q
	m.hits = nil
	m.busy++
	return m.runSearch(q)
}

func (m *Model) openLink(u string) tea.Cmd {
	if strings.HasPrefix(u, "https://graph.microsoft.com/") {
		// hosted content (inline images) needs auth: download, then open locally
		svc, c := m.svc, m.transferCtx
		m.busy++
		return func() tea.Msg {
			dir, err := os.MkdirTemp("", "teams-hosted-*")
			if err != nil {
				return opMsg{err: err}
			}
			dest, err := saveDownload(dir, "content", func(w io.Writer) error { return svc.FetchURL(c, u, w) })
			typed := dest + sniffExt(dest)
			if err == nil {
				err = os.Rename(dest, typed)
			}
			if err != nil {
				_ = os.RemoveAll(dir)
				return opMsg{err: err}
			}
			return opMsg{open: typed}
		}
	}
	if err := notify.Open(u); err != nil {
		m.errToast(err)
	}
	return nil
}

func sniffExt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, 12)
	n, _ := f.Read(b)
	b = b[:n]
	switch {
	case len(b) >= 8 && string(b[1:4]) == "PNG":
		return ".png"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8:
		return ".jpg"
	case len(b) >= 4 && string(b[:4]) == "GIF8":
		return ".gif"
	case len(b) >= 12 && string(b[8:12]) == "WEBP":
		return ".webp"
	}
	return ".bin"
}

func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	default:
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func (m *Model) copy(text, what string) {
	if err := copyToClipboard(text); err != nil {
		m.errToast(err)
		return
	}
	m.setToast("Copied "+what, false)
}

func (m *Model) renameChat(topic string) tea.Cmd {
	if m.conv == nil || m.conv.ref.Kind != model.KindChat {
		m.setToast("Open a group chat first", true)
		return nil
	}
	return m.renameChatID(m.conv.ref.ChatID, topic)
}

func (m *Model) renameChatID(id, topic string) tea.Cmd {
	if c := m.chatByID(id); c != nil {
		c.Topic = topic
	}
	return m.op(func(svc model.Service) opMsg {
		c, cancel := ctx()
		defer cancel()
		return opMsg{err: svc.RenameChat(c, id, topic), toast: "Chat renamed", chats: true}
	})
}

// ---- message actions ------------------------------------------------------------

func (m *Model) msgAction(action string) tea.Cmd {
	cv := m.activeConv()
	if cv == nil {
		return nil
	}
	msg := cv.selected()
	if msg == nil {
		if action == "reply" {
			m.setFocus(FocusCompose)
		}
		return nil
	}
	if strings.HasPrefix(msg.ID, "tmp:") {
		if cv.failed[msg.ID] && (action == "delete" || action == "retry") {
			for i := range cv.msgs {
				if cv.msgs[i].ID == msg.ID {
					cv.removeMessage(i)
					cv.sel = -1
					cv.dirty = true
					break
				}
			}
		}
		return nil
	}
	mine := msg.From.ID == m.me.ID
	switch action {
	case "reply":
		if cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" {
			cmd := m.openThread(cv.ref, msg.ID)
			m.setFocus(FocusCompose)
			return cmd
		}
		if cv.ref.Kind == model.KindChannel {
			m.setFocus(FocusCompose)
			return nil
		}
		m.startQuote(msg)
	case "thread":
		if cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" {
			cmd := m.openThread(cv.ref, msg.ID)
			m.setFocus(FocusMessages)
			return cmd
		}
	case "react":
		m.openReactionPicker(cv, msg)
	case "edit":
		if !mine || msg.Deleted {
			m.setToast("You can only edit your own messages", true)
			return nil
		}
		m.startEdit(cv, msg)
	case "delete":
		if !mine || msg.Deleted {
			m.setToast("You can only delete your own messages", true)
			return nil
		}
		ref, id := cv.ref, msg.ID
		m.openConfirm("Delete this message?", func(m *Model) tea.Cmd {
			for i := range cv.msgs {
				if cv.msgs[i].ID == id {
					cv.msgs[i].Deleted = true
					cv.dirty = true
				}
			}
			return m.op(func(svc model.Service) opMsg {
				c, cancel := ctx()
				defer cancel()
				return opMsg{err: svc.Delete(c, ref, id), toast: "Message deleted", reload: ref}
			})
		})
	case "copy":
		m.copy(htmltext.ToMarkdown(msg.Body), "message")
	case "open":
		links := cv.links[msg.ID]
		if len(links) == 0 {
			m.setToast("No links or attachments in this message", false)
			return nil
		}
		m.openLinkPicker(links)
	case "download":
		var items []modalItem
		for _, a := range msg.Attachments {
			a := a
			if a.ContentURL == "" {
				continue
			}
			items = append(items, modalItem{label: "⤓ " + a.Name, run: func(m *Model) tea.Cmd { return m.openLink(a.ContentURL) }})
		}
		if len(items) == 0 {
			m.setToast("No attachments", false)
			return nil
		}
		m.openList("Attachments", items, false)
	case "more":
		m.openMessageMenu(cv, msg)
	case "card":
		m.openCardActions(cv, msg)
	}
	return nil
}

func (m *Model) openMessageMenu(cv *convView, msg *model.Message) {
	mine := msg.From.ID == m.me.ID
	act := func(a string) func(m *Model) tea.Cmd { return func(m *Model) tea.Cmd { return m.msgAction(a) } }
	items := []modalItem{{label: "↩ Reply", hint: "r", run: act("reply")}}
	if cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" {
		items = append(items, modalItem{label: "💬 Open thread", hint: "enter", run: act("thread")})
	}
	items = append(items, modalItem{label: "☺ React", hint: "+", run: act("react")})
	if mine && !msg.Deleted {
		items = append(items, modalItem{label: "✎ Edit", hint: "e", run: act("edit")}, modalItem{label: "⌫ Delete", hint: "d", run: act("delete")})
	}
	if m.hasCardActions(cv, msg) {
		items = append(items, modalItem{label: "⚡ Card actions", hint: "A", run: act("card")})
	}
	items = append(items, modalItem{label: "⧉ Copy text", hint: "y", run: act("copy")})
	if len(cv.links[msg.ID]) > 0 {
		items = append(items, modalItem{label: "↗ Open link / attachment", hint: "o", run: act("open")})
	}
	if msg.From.ID != "" && !mine && !strings.HasPrefix(msg.From.ID, "app:") {
		u := msg.From
		if full, ok := m.users[u.ID]; ok {
			u = full
		}
		items = append(items, modalItem{label: "👤 View " + u.DisplayName, run: func(m *Model) tea.Cmd { m.openProfile(u); return nil }})
	}
	if link := messageLink(cv.ref, msg, m.svc.TenantID()); link != "" {
		items = append(items, modalItem{label: "🔗 Copy link to message", run: func(m *Model) tea.Cmd { m.copy(link, "link"); return nil }})
	}
	m.openList("Message", items, false)
}

func messageLink(ref model.ConvRef, msg *model.Message, tenant string) string {
	switch ref.Kind {
	case model.KindChat:
		return "https://teams.microsoft.com/l/message/" + url.PathEscape(ref.ChatID) + "/" + msg.ID + "?context=" + url.QueryEscape(`{"contextType":"chat"}`)
	case model.KindChannel:
		parent := msg.ID
		if ref.ThreadID != "" {
			parent = ref.ThreadID
		}
		return "https://teams.microsoft.com/l/message/" + url.PathEscape(ref.ChannelID) + "/" + msg.ID + "?tenantId=" + tenant + "&groupId=" + ref.TeamID + "&parentMessageId=" + parent
	}
	return ""
}

// callLink builds a deep link that starts a call in the Teams client.
func (m *Model) callLink(video bool) string {
	if m.conv == nil || m.conv.ref.Kind != model.KindChat {
		return ""
	}
	return m.callLinkFor(m.chatByID(m.conv.ref.ChatID), video)
}

func (m *Model) callLinkFor(c *model.Chat, video bool) string {
	if c == nil {
		return ""
	}
	var emails []string
	for _, u := range c.Members {
		if u.ID == m.me.ID {
			continue
		}
		e := u.Email
		if e == "" {
			if full, ok := m.users[u.ID]; ok {
				e = full.Email
			}
		}
		if e != "" {
			emails = append(emails, e)
		}
	}
	if len(emails) == 0 {
		return ""
	}
	l := "https://teams.microsoft.com/l/call/0/0?users=" + url.QueryEscape(strings.Join(emails, ","))
	if video {
		l += "&withVideo=true"
	}
	return l
}

// ---- calendar & files actions --------------------------------------------------------

func (m *Model) eventAction(action string) tea.Cmd {
	e := m.selectedEvent()
	if e == nil {
		return nil
	}
	switch action {
	case "join":
		return m.joinEvent(e)
	case "web":
		if e.WebLink != "" {
			return m.openLink(e.WebLink)
		}
	case "accept", "tentative", "decline":
		id := e.ID
		e.Response = map[string]string{"accept": "accepted", "tentative": "tentativelyAccepted", "decline": "declined"}[action]
		return m.op(func(svc model.Service) opMsg {
			c, cancel := ctx()
			defer cancel()
			return opMsg{err: svc.RespondEvent(c, id, action), toast: "Response sent", events: true}
		})
	}
	return nil
}

func (m *Model) shiftDay(d int) tea.Cmd {
	old := m.calDay
	m.calDay = m.calDay.AddDate(0, 0, d)
	if d == 0 {
		m.calDay = today()
	}
	m.sideSel[SecCalendar] = ""
	m.sideTouched[SecCalendar] = false
	oldMon := old.AddDate(0, 0, -((int(old.Weekday()) + 6) % 7))
	newMon := m.calDay.AddDate(0, 0, -((int(m.calDay.Weekday()) + 6) % 7))
	if !sameDay(oldMon, newMon) {
		return m.loadEvents(m.calDay)
	}
	return nil
}

func (m *Model) fileAction(action string, f *model.DriveItem) tea.Cmd {
	if f == nil {
		return nil
	}
	switch action {
	case "open":
		if f.IsFolder {
			m.fileStack = append(m.fileStack, *f)
			m.files = nil
			return m.loadFiles()
		}
		if f.WebURL != "" {
			return m.openLink(f.WebURL)
		}
	case "enter":
		if f.IsFolder {
			return m.fileAction("open", f)
		}
	case "copy":
		if f.WebURL != "" {
			m.copy(f.WebURL, "link")
		}
	case "download":
		if f.IsFolder {
			m.setToast("Folders can't be downloaded", true)
			return nil
		}
		item := *f
		dir, c := m.cfg.DownloadDir, m.transferCtx
		m.setToast("Downloading "+f.Name+"…", false)
		return m.op(func(svc model.Service) opMsg {
			dest, err := saveDownload(dir, item.Name, func(w io.Writer) error { return svc.Download(c, item, w) })
			return opMsg{err: err, toast: "Saved to " + dest}
		})
	}
	return nil
}

// ---- palette & help ---------------------------------------------------------------

func (m *Model) openPalette() {
	ic := m.s.ic
	sec := func(s Section) func(m *Model) tea.Cmd { return func(m *Model) tea.Cmd { return m.switchSection(s) } }
	const acts, goTo = "Actions", "Go to"
	var items []modalItem
	if e := m.upcomingMeeting(); e != nil {
		ev := e
		items = append(items,
			modalItem{section: acts, label: ic.Video + " Join " + ev.Subject, hint: "alt+j", run: func(m *Model) tea.Cmd { return m.joinEvent(ev) }},
			modalItem{section: acts, label: ic.Chat + " Open meeting chat: " + ev.Subject, run: func(m *Model) tea.Cmd { return m.openMeetingChat(ev) }})
	}
	items = append(items,
		modalItem{section: acts, label: ic.Chat + " New chat", hint: "ctrl+n", run: func(m *Model) tea.Cmd { m.openPeoplePicker("New chat", true, ""); return m.pendingTake() }},
		modalItem{section: acts, label: ic.Calendar + " New meeting…", hint: "/meeting", run: func(m *Model) tea.Cmd { m.openMeetingForm(""); return nil }},
		modalItem{section: acts, label: "● Set status: Available", hint: "/available", run: func(m *Model) tea.Cmd { return m.setPresenceCmd("available") }},
		modalItem{section: acts, label: "● Set status: In a meeting", hint: "/busy", run: func(m *Model) tea.Cmd { return m.setPresenceCmd("busy") }},
		modalItem{section: acts, label: "● Set status: Do not disturb", hint: "/dnd", run: func(m *Model) tea.Cmd { return m.setPresenceCmd("dnd") }},
		modalItem{section: acts, label: ic.Pencil + " Set status message…", hint: "/status", run: func(m *Model) tea.Cmd {
			m.openPrompt("Status message", "What's happening?", m.myPresence.StatusMessage, func(m *Model, s string) tea.Cmd { return m.setStatusMessage(s) })
			return nil
		}},
		modalItem{section: acts, label: "↻ Refresh", hint: "ctrl+r", run: func(m *Model) tea.Cmd { return m.refresh() }},
		modalItem{section: acts, label: "◧ Toggle sidebar", hint: "ctrl+b", run: func(m *Model) tea.Cmd { m.showSidebar = !m.showSidebar; m.invalidateRender(); return nil }},
		modalItem{section: acts, label: ic.Help + " Keyboard shortcuts", hint: "?", run: func(m *Model) tea.Cmd { m.openHelp(); return nil }},
		modalItem{section: acts, label: "Quit", hint: "ctrl+c", run: func(m *Model) tea.Cmd { return tea.Quit }},
		modalItem{section: goTo, label: ic.Bell + " Activity", hint: "alt+1", run: sec(SecActivity)},
		modalItem{section: goTo, label: ic.Chat + " Chat", hint: "alt+2", run: sec(SecChat)},
		modalItem{section: goTo, label: ic.Teams + " Teams", hint: "alt+3", run: sec(SecTeams)},
		modalItem{section: goTo, label: ic.Calendar + " Calendar", hint: "alt+4", run: sec(SecCalendar)},
		modalItem{section: goTo, label: ic.Files + " Files", hint: "alt+5", run: sec(SecFiles)},
		modalItem{section: goTo, label: ic.Search + " Search", hint: "ctrl+e", run: sec(SecSearch)},
		modalItem{section: goTo, label: ic.Settings + " Settings", run: sec(SecSettings)},
	)
	// jump to chats and channels
	for _, c := range m.chats {
		c := c
		icon := ic.Users
		if c.ChatType == "oneOnOne" {
			icon = "●"
		} else if c.ChatType == "meeting" {
			icon = ic.Video
		}
		items = append(items, modalItem{section: goTo, label: icon + " " + c.Title(m.me.ID), hint: "chat", run: func(m *Model) tea.Cmd {
			m.section = SecChat
			return m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: c.ID})
		}})
	}
	for _, t := range m.teams {
		for _, ch := range m.channels[t.ID] {
			ch, team := ch, t.DisplayName
			items = append(items, modalItem{section: goTo, label: ic.Hash + " " + ch.DisplayName + " " + m.s.Muted.Render(team), hint: "channel", run: func(m *Model) tea.Cmd {
				m.section = SecTeams
				if m.cfg.CombinedChat {
					m.section = SecChat
				}
				return m.openConv(model.ConvRef{Kind: model.KindChannel, TeamID: ch.TeamID, ChannelID: ch.ID})
			}})
		}
	}
	m.openList("Command palette", items, true).width = 64
}

func (m *Model) pendingTake() tea.Cmd {
	c := m.pendingCmd
	m.pendingCmd = nil
	return c
}

func (m *Model) refresh() tea.Cmd {
	cmds := []tea.Cmd{m.loadChats(), m.loadTeams(), m.loadPresence(), m.loadMyPresence(), m.loadEvents(m.calDay)}
	if ac := m.activeConv(); ac != nil {
		cmds = append(cmds, m.loadMessages(ac.ref, "", false, true))
	}
	if m.section == SecFiles {
		cmds = append(cmds, m.loadFiles())
	}
	m.setToast("Refreshing…", false)
	return tea.Batch(cmds...)
}

var helpText = []string{
	"GLOBAL",
	"  alt+1 … alt+6     Activity · Chat · Teams · Calendar · Files · Search",
	"                    (Teams lives inside Chat when combined_chat is on)",
	"  ctrl+k            Command palette (jump to any chat / channel / action)",
	"  ctrl+n            New chat",
	"  ctrl+e  or  /     Search messages",
	"  ctrl+b            Toggle sidebar",
	"  ctrl+r            Refresh",
	"  alt+j             Join the meeting that is live or starts within 10 min",
	"  alt+a             Show the waiting call / meeting alert",
	"  Settings          click the gear in the rail, or ctrl+k → Settings",
	"  tab / shift+tab   Cycle focus: sidebar → messages → compose",
	"  esc               Back (close modal / thread / leave compose)",
	"  ?  or  F1         This help",
	"  q / ctrl+c        Quit (q only outside text inputs)",
	"",
	"SIDEBAR",
	"  ↑/↓  j/k          Move          g/G   first / last",
	"  enter  →  l       Open chat / channel, expand team",
	"  ←  h              Collapse team",
	"  /                 Filter chats",
	"  .  or  space      Chat actions (pin, mute, rename, add people, leave…)",
	"  p / m             Add to / remove from Favorites · mute (chats and channels)",
	"  alt+↑/↓  or  K/J  Move a favorite or a team up / down",
	"  H                 Hide / show team (hidden teams collapse at the bottom)",
	"  m (on a channel)  Mute channel (keeps it out of Unread)",
	"  enter on a section header (UNREAD / CHANNELS / CHATS) collapses it",
	"",
	"MESSAGES",
	"  ↑/↓  j/k          Select message        pgup/pgdn  scroll",
	"  g / G             Oldest / newest       u  load older",
	"  enter             Open thread (channels) / start typing",
	"  r                 Reply (thread in channels, quote in chats)",
	"  +                 React…               1-6  👍 ❤️ 😆 😮 😢 😡",
	"  e / d             Edit / delete your message",
	"  y                 Copy text            o  open links & attachments",
	"  A                 Card buttons (Adaptive Cards); or click a button",
	"  T                 Open the selected message in Teams (bot buttons, calls)",
	"  .                 More actions",
	"",
	"COMPOSE",
	"  enter             Send                  alt+enter / ctrl+j  newline",
	"  ↑ (empty box)     Edit your last message",
	"  @name             Mention (tab/enter to pick)",
	"  **bold** *italic* ~~strike~~ `code`  ```code block```  > quote  - list",
	"  :smile: :+1: …    Emoji shortcodes",
	"  /attach <path>    Upload & attach a file",
	"  /subject <text>   Subject for a channel post",
	"  /important /urgent  Mark message importance",
	"  /available /busy /dnd /brb /away /offline /reset   Set presence",
	"  /status <text>    Set status message",
	"  /meeting <title>  Schedule a Teams meeting",
	"  /rename <name>    Rename group chat",
	"",
	"CALENDAR",
	"  [ / ]  t          Previous / next day, today",
	"  j                 Join meeting (opens Teams)",
	"  c                 Open the meeting chat (live meetings open the meeting view:",
	"                    chat + people; p toggles people, esc leaves)",
	"  a / T / D         Accept / tentative / decline",
	"  x / R             Transcript / download recording (after the meeting)",
	"  n                 New meeting",
	"",
	"FILES",
	"  enter             Open folder / open file in browser",
	"  sidebar           Recent · My files · Shared with me · channel files",
	"  tab → table       ↑↓ select · enter open · d download · y copy link",
	"  u                 Upload a file      / filter      backspace  up a folder",
	"",
	"MOUSE",
	"  Click rail, chats, channels, messages, reactions, buttons and tabs.",
	"  Wheel scrolls lists and messages. Click a reaction chip to toggle it.",
	"  Drag a favorite or a team onto another to reorder. Right-click for actions.",
	"  Click your status (bottom left) to change presence.",
}

func (m *Model) openHelp() {
	md := &modal{kind: mText, title: "Keyboard & mouse", text: helpText, width: 86}
	m.modal = md
}
