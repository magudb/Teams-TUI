package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
)

type pendingMention struct {
	user model.User
	name string
}

// composer wraps the message input box.
type composer struct {
	m           *Model
	ta          textarea.Model
	key         string // conversation key the composer is bound to
	editingID   string
	editingRef  model.ConvRef
	quote       *model.Message
	mentions    []pendingMention
	attachments []model.Attachment
	importance  string
	subject     string

	// mention autocomplete
	mQuery string
	mCands []model.User
	mSel   int
}

func newComposer(m *Model) composer {
	ta := textarea.New()
	ta.Placeholder = "Type a message"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 28000
	ta.SetHeight(1)
	ta.MaxHeight = 8
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ta.FocusedStyle.CursorLine = ta.FocusedStyle.CursorLine.UnsetBackground()
	ta.FocusedStyle.Placeholder = m.s.Faint
	ta.BlurredStyle.Placeholder = m.s.Faint
	ta.Blur()
	return composer{m: m, ta: ta}
}

func (c *composer) resize() {
	w := c.m.composeWidth()
	if w > 0 {
		c.ta.SetWidth(w)
	}
	c.autoHeight()
}

func (c *composer) autoHeight() {
	w := max(10, c.ta.Width())
	n := 0
	for _, l := range strings.Split(c.ta.Value(), "\n") {
		n += max(1, (ansi.StringWidth(l)+w-1)/w)
	}
	c.ta.SetHeight(max(1, min(n, 8)))
}

// bind switches the composer to a conversation, stashing/restoring drafts.
func (c *composer) bind(key string) {
	if key == c.key {
		return
	}
	c.saveDraft()
	c.key = key
	c.reset()
	if d, ok := c.m.st.Drafts[key]; ok {
		c.ta.SetValue(d)
	}
	c.autoHeight()
}

func (c *composer) saveDraft() {
	if c.key == "" || c.editingID != "" || c.m.st == nil {
		return
	}
	if v := c.ta.Value(); strings.TrimSpace(v) != "" {
		c.m.st.Drafts[c.key] = v
	} else {
		delete(c.m.st.Drafts, c.key)
	}
}

func (c *composer) reset() {
	c.ta.Reset()
	c.editingID = ""
	c.quote = nil
	c.mentions = nil
	c.attachments = nil
	c.importance = ""
	c.subject = ""
	c.mCands = nil
	c.mQuery = ""
	c.autoHeight()
}

var reMentionQuery = regexp.MustCompile(`(?:^|\s)@([\p{L}\p{N}._-]*(?: [\p{L}\p{N}._-]*)?)$`)

func (c *composer) updateMentionCands() {
	v := c.ta.Value()
	mm := reMentionQuery.FindStringSubmatch(v)
	if mm == nil {
		c.mCands = nil
		c.mQuery = ""
		return
	}
	q := strings.ToLower(mm[1])
	c.mQuery = mm[1]
	ac := c.m.activeConv()
	if ac == nil {
		return
	}
	ref := ac.ref
	ref.ThreadID = ""
	var cands []model.User
	for _, u := range c.m.members[ref.Key()] {
		if u.ID == c.m.me.ID {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(u.DisplayName), q) {
			cands = append(cands, u)
		}
		if len(cands) >= 6 {
			break
		}
	}
	c.mCands = cands
	if c.mSel >= len(cands) {
		c.mSel = 0
	}
}

func (c *composer) acceptMention() {
	if len(c.mCands) == 0 {
		return
	}
	u := c.mCands[c.mSel]
	v := c.ta.Value()
	v = strings.TrimSuffix(v, "@"+c.mQuery) + "@" + u.DisplayName + " "
	c.ta.SetValue(v)
	c.mentions = append(c.mentions, pendingMention{user: u, name: u.DisplayName})
	c.mCands = nil
	c.mQuery = ""
}

// handleKey processes a key while the composer is focused.
func (m *Model) composeKey(msg tea.KeyMsg) tea.Cmd {
	c := &m.compose
	if len(c.mCands) > 0 {
		switch msg.String() {
		case "up":
			c.mSel = (c.mSel + len(c.mCands) - 1) % len(c.mCands)
			return nil
		case "down":
			c.mSel = (c.mSel + 1) % len(c.mCands)
			return nil
		case "tab", "enter":
			c.acceptMention()
			c.autoHeight()
			return nil
		case "esc":
			c.mCands = nil
			return nil
		}
	}
	switch msg.String() {
	case "enter":
		return m.submitCompose()
	case "esc":
		if c.editingID != "" || c.quote != nil {
			c.reset()
			m.restoreDraft()
			return nil
		}
		m.setFocus(FocusMessages)
		return nil
	case "up":
		if strings.TrimSpace(c.ta.Value()) == "" && c.editingID == "" {
			if ac := m.activeConv(); ac != nil {
				for i := len(ac.msgs) - 1; i >= 0; i-- {
					if ac.msgs[i].From.ID == m.me.ID && !ac.msgs[i].Deleted && !strings.HasPrefix(ac.msgs[i].ID, "tmp:") {
						m.startEdit(ac, &ac.msgs[i])
						return nil
					}
				}
			}
		}
	case "tab":
		m.cycleFocus(1)
		return nil
	case "shift+tab":
		m.cycleFocus(-1)
		return nil
	}
	var cmd tea.Cmd
	c.ta, cmd = c.ta.Update(msg)
	c.autoHeight()
	c.updateMentionCands()
	return cmd
}

func (m *Model) restoreDraft() {
	c := &m.compose
	if d, ok := m.st.Drafts[c.key]; ok {
		c.ta.SetValue(d)
	}
	c.autoHeight()
}

func (m *Model) startEdit(cv *convView, msg *model.Message) {
	c := &m.compose
	c.saveDraft()
	c.reset()
	c.editingID = msg.ID
	c.editingRef = cv.ref
	body := htmltext.ToMarkdown(msg.Body)
	c.ta.SetValue(body)
	for _, mm := range msg.Mentions {
		c.mentions = append(c.mentions, pendingMention{user: model.User{ID: mm.UserID, DisplayName: mm.Text}, name: mm.Text})
	}
	c.autoHeight()
	m.setFocus(FocusCompose)
}

func (m *Model) startQuote(msg *model.Message) {
	c := &m.compose
	cp := *msg
	c.quote = &cp
	m.setFocus(FocusCompose)
}

var reAttach = regexp.MustCompile(`^/attach\s+(.+)$`)

// submitCompose sends (or saves an edit of) the composed message.
func (m *Model) submitCompose() tea.Cmd {
	c := &m.compose
	text := strings.TrimSpace(c.ta.Value())
	ac := m.activeConv()

	if strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "//") && c.editingID == "" {
		return m.slashCommand(text)
	}
	if text == "" && len(c.attachments) == 0 {
		return nil
	}
	if ac == nil {
		m.setToast("Open a chat or channel first", true)
		return nil
	}
	text = strings.TrimPrefix(text, "/")

	var specs []htmltext.MentionSpec
	out := model.Outgoing{Subject: c.subject, Importance: c.importance, MentionUser: map[int]model.User{}, Attachments: c.attachments}
	for i, pm := range c.mentions {
		if !strings.Contains(text, "@"+pm.name) {
			continue
		}
		specs = append(specs, htmltext.MentionSpec{ID: i, Name: pm.name})
		out.Mentions = append(out.Mentions, model.Mention{ID: i, Text: pm.name, UserID: pm.user.ID})
		out.MentionUser[i] = pm.user
	}
	html := htmltext.ToHTML(text, specs)
	if c.quote != nil {
		q := c.quote
		html = "<blockquote><b>" + escapeHTML(q.From.DisplayName) + "</b><br>" +
			escapeHTML(trunc(htmltext.Plain(q.Body), 200)) + "</blockquote>" + html
	}
	for _, a := range c.attachments {
		html += `<attachment id="` + a.ID + `"></attachment>`
	}
	out.HTML = html

	svc := m.svc
	if c.editingID != "" {
		ref, id := c.editingRef, c.editingID
		for i := range ac.msgs {
			if ac.msgs[i].ID == id {
				ac.msgs[i].Body = html
				ac.msgs[i].Edited = true
				ac.dirty = true
			}
		}
		c.reset()
		m.restoreDraft()
		return m.op(func(_ model.Service) opMsg {
			cx, cancel := ctx()
			defer cancel()
			err := svc.Edit(cx, ref, id, out)
			return opMsg{err: err, reload: ref}
		})
	}

	ref := ac.ref
	tempID := ac.addTemp(m.me, out)
	delete(m.st.Drafts, c.key)
	c.reset()
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		msg, err := svc.Send(cx, ref, out)
		return sentMsg{ref: ref, msg: msg, err: err, tempID: tempID}
	}
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func expandHome(p string) string {
	p = strings.Trim(strings.TrimSpace(p), `"'`)
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[2:])
		}
	}
	return p
}

func (m *Model) attachFile(path string) tea.Cmd {
	ac := m.activeConv()
	if ac == nil {
		m.setToast("Open a conversation first", true)
		return nil
	}
	path = expandHome(path)
	if _, err := os.Stat(path); err != nil {
		m.errToast(err)
		return nil
	}
	ref := ac.ref
	svc := m.svc
	m.busy++
	m.setToast("Uploading "+filepath.Base(path)+"…", false)
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		a, err := svc.Upload(cx, ref, path)
		return uploadMsg{ref, a, err}
	}
}

// slashCommand handles /commands typed in the compose box.
func (m *Model) slashCommand(text string) tea.Cmd {
	c := &m.compose
	fields := strings.Fields(text)
	cmd := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	done := func() { c.ta.Reset(); c.autoHeight() }
	switch cmd {
	case "/attach", "/file", "/upload":
		done()
		if arg == "" {
			m.openAttachPrompt()
			return nil
		}
		return m.attachFile(arg)
	case "/subject":
		done()
		c.subject = arg
		m.setToast("Subject set: "+arg, false)
	case "/important":
		done()
		c.importance = toggle(c.importance, "high")
	case "/urgent":
		done()
		c.importance = toggle(c.importance, "urgent")
	case "/status":
		done()
		return m.setStatusMessage(arg)
	case "/available", "/busy", "/dnd", "/brb", "/away", "/offline", "/reset":
		done()
		return m.setPresenceCmd(strings.TrimPrefix(cmd, "/"))
	case "/search":
		done()
		m.switchSection(SecSearch)
		m.searchInput.SetValue(arg)
		return m.doSearch()
	case "/chat", "/new":
		done()
		m.openPeoplePicker("New chat", true, arg)
	case "/meeting", "/meet":
		done()
		m.openMeetingForm(arg)
	case "/rename":
		done()
		return m.renameChat(arg)
	case "/help":
		done()
		m.openHelp()
	case "/shrug":
		c.ta.SetValue(strings.TrimSpace(arg + ` ¯\\_(ツ)_/¯`))
		return m.submitCompose()
	case "/me":
		done()
		m.setToast("Signed in as "+m.me.DisplayName+" <"+m.me.Email+">", false)
	default:
		m.setToast("Unknown command "+cmd+" — prefix with // to send literally", true)
	}
	return nil
}

func toggle(cur, v string) string {
	if cur == v {
		return ""
	}
	return v
}

// ---- view ------------------------------------------------------------------------

// composeLines renders the compose area: optional mention candidates and
// status line, the "› message" input, and a hint line with clickable tools.
func (m *Model) composeLines(cv *convView, width int, side bool) []string {
	c := &m.compose
	s := m.s
	ic := s.ic
	var lines []string

	for i, u := range c.mCands {
		p, ok := m.presence[u.ID]
		l := " " + s.presenceDot(p, ok) + " " + u.DisplayName
		if i == c.mSel {
			l = s.Sel.Render(padRight(" ▸ "+u.DisplayName, width))
		}
		lines = append(lines, zone.Mark("mention:"+itoa(i), padRight(l, width)))
	}
	switch {
	case c.editingID != "":
		lines = append(lines, " "+s.Warn.Bold(true).Render(trunc(ic.Pencil+" Editing message · enter save · esc cancel", width-2)))
	case c.quote != nil:
		lines = append(lines, " "+s.Muted.Render(trunc(ic.Reply+" Replying to "+c.quote.From.DisplayName+": "+htmltext.Plain(c.quote.Body), width-6))+" "+zone.Mark("unquote", s.Accent.Render("✕")))
	case c.subject != "":
		lines = append(lines, " "+s.Bold.Render(trunc("Subject: "+c.subject, width-2)))
	}

	switch {
	case cv == nil:
		c.ta.Placeholder = "Select a conversation"
	case cv.ref.Kind == model.KindChannel && cv.ref.ThreadID != "":
		c.ta.Placeholder = "Reply in thread"
	case cv.ref.Kind == model.KindChannel:
		name := "channel"
		for _, ch := range m.channels[cv.ref.TeamID] {
			if ch.ID == cv.ref.ChannelID {
				name = ch.DisplayName
			}
		}
		c.ta.Placeholder = "Start a post in " + name
	default:
		c.ta.Placeholder = "Message " + m.convTitle(cv.ref)
	}
	c.ta.FocusedStyle.Placeholder = s.Muted
	c.ta.BlurredStyle.Placeholder = s.Muted
	c.ta.FocusedStyle.Text = s.Text
	c.ta.BlurredStyle.Text = s.Text
	for i, l := range strings.Split(c.ta.View(), "\n") {
		p := "  "
		if i == 0 {
			p = " " + s.Accent.Bold(true).Render(ic.Prompt) + " "
		}
		lines = append(lines, p+l)
	}

	if !m.narrow() || len(c.attachments) > 0 || c.importance != "" {
		tool := func(id, label string, on bool) string {
			st := s.Muted
			if on {
				st = s.Accent.Bold(true)
			}
			return zone.Mark("tb:"+id, st.Render(label))
		}
		parts := []string{tool("attach", ic.Clip+" attach", false), tool("mention", "@ mention", false), tool("commands", "/ commands", false)}
		if c.importance != "" {
			parts = append(parts, tool("important", "! "+c.importance, true))
		}
		for i, a := range c.attachments {
			parts = append(parts, zone.Mark("unattach:"+itoa(i), s.ChipMine.Render(" "+ic.Clip+" "+trunc(a.Name, 18)+" ✕ ")))
		}
		lines = append(lines, " "+trunc(strings.Join(parts, s.Muted.Render(" ")), width-2))
	}
	return lines
}
