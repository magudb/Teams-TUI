package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/cards"
	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
)

// Conversation rendering, modelled on the Teams desktop client:
//   - chats: avatar + name above tinted bubbles; your own messages on the right
//   - channels: every post is a card holding its thread (latest replies + Reply)
//   - threads: the original post as a card on top, replies below it

var quickReactions = []string{"👍", "❤️", "😆", "😮", "😢", "😡"}

const gutterW = 2 // selection marker column + space

// bgPrefix returns the escape sequence that switches on background c (empty
// when the terminal has no colour support).
func bgPrefix(c lipgloss.TerminalColor) string {
	out := lipgloss.NewStyle().Background(c).Render("x")
	if i := strings.Index(out, "x"); i > 0 {
		return out[:i]
	}
	return ""
}

// withBg paints line (padded to w) on a background, re-applying it after
// every reset emitted by inner styles.
func withBg(line, prefix string, w int) string {
	line = padRight(line, w)
	if prefix == "" {
		return line
	}
	return prefix + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+prefix) + "\x1b[0m"
}

func (s styles) avatar(id, name string) string {
	c := s.t.Names[0]
	if st := s.nameColor(id); st.GetForeground() != nil {
		c = st.GetForeground()
	}
	return lipgloss.NewStyle().Background(c).Foreground(lipgloss.Color("#1F1F1F")).Bold(true).Render(padRight(" "+initials(name), 4))
}

// avatarFor returns the initials chip of the message author.
func (m *Model) avatarFor(msg *model.Message) string {
	name := msg.From.DisplayName
	if msg.From.ID == m.me.ID && m.me.DisplayName != "" {
		name = m.me.DisplayName
	}
	if name == "" {
		name = m.userName(msg.From.ID)
	}
	return m.s.avatar(msg.From.ID, name)
}

func (m *Model) displayName(msg *model.Message) string {
	if msg.From.ID == m.me.ID && m.me.ID != "" {
		return "You"
	}
	if msg.From.DisplayName != "" {
		return msg.From.DisplayName
	}
	return m.userName(msg.From.ID)
}

// renderConv lays out all messages of cv for the given width.
func (m *Model) renderConv(cv *convView, width int) {
	if !cv.dirty && cv.width == width {
		return
	}
	cv.dirty = false
	cv.width = width
	cv.lines = cv.lines[:0]
	cv.owner = cv.owner[:0]
	cv.msgStart = make([]int, len(cv.msgs))
	cv.links = map[string][]string{}
	cv.cardActs = map[string]cardRef{}
	s := m.s

	add := func(line string, owner int) {
		if line == "" && owner == -1 && (len(cv.lines) == 0 || cv.lines[len(cv.lines)-1] == "") {
			return // never stack blank lines
		}
		cv.lines = append(cv.lines, line)
		cv.owner = append(cv.owner, owner)
	}
	center := func(label string, st lipgloss.Style) string {
		label = " " + label + " "
		side := max(0, (width-ansi.StringWidth(label))/2)
		return st.Render(strings.Repeat("─", side) + label + strings.Repeat("─", max(0, width-side-ansi.StringWidth(label))))
	}

	if cv.next != "" {
		add(zone.Mark("older", s.Button.Render("  ↑ Load older messages")), -2)
		add("", -1)
	} else if cv.loadedOnce && len(cv.msgs) > 0 && cv.ref.ThreadID == "" {
		add(s.Faint.Render("  Beginning of conversation"), -1)
		add("", -1)
	}

	kind := "chat"
	switch {
	case cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "":
		kind = "channel"
	case cv.ref.ThreadID != "":
		kind = "thread"
	}

	var prev *model.Message
	newShown := false
	for i := range cv.msgs {
		msg := &cv.msgs[i]
		isRoot := kind == "thread" && msg.ID == cv.ref.ThreadID

		if !isRoot && (prev == nil || !sameDay(prev.Created, msg.Created)) && kind != "thread" {
			add(center(dayLabel(msg.Created), s.Faint), -1)
			add("", -1)
		}
		if kind == "chat" && !newShown && !cv.newSince.IsZero() && msg.From.ID != m.me.ID && msg.Created.After(cv.newSince) {
			newShown = true
			add(center("New messages", s.Danger), -1)
			add("", -1)
		}
		grouped := kind != "channel" && !isRoot && prev != nil && prev.From.ID == msg.From.ID &&
			msg.Created.Sub(prev.Created) < 5*time.Minute && sameDay(prev.Created, msg.Created) &&
			msg.MessageType == "message" && prev.MessageType == "message" && msg.Subject == "" &&
			!(kind == "thread" && prev.ID == cv.ref.ThreadID)
		if i > 0 && !grouped {
			add("", -1)
		}
		// keep the divider lines tight: no blank between a divider and its message

		cv.msgStart[i] = len(cv.lines)
		var lines []string
		switch {
		case msg.MessageType != "" && msg.MessageType != "message":
			lines = m.systemLines(msg, width)
		case kind == "channel":
			lines = m.postCard(cv, i, width, false)
		case isRoot:
			lines = m.postCard(cv, i, width, true)
		default:
			lines = m.bubbleMessage(cv, i, width, grouped, kind == "chat")
		}
		for _, l := range lines {
			add(l, i)
		}
		if isRoot {
			n := len(cv.msgs) - 1
			label := itoa(n) + " replies"
			if n == 1 {
				label = "1 reply"
			} else if n == 0 {
				label = "No replies yet — be the first"
			}
			add("", -1)
			add(center(label, s.Muted), -1)
		}
		prev = msg
	}
	switch {
	case cv.loading && len(cv.msgs) == 0:
		add(s.Muted.Render("  Loading messages…"), -1)
	case cv.err != "" && len(cv.msgs) == 0:
		add(s.Danger.Render("  "+cv.err), -1)
	case cv.loadedOnce && len(cv.msgs) == 0:
		if kind == "channel" {
			add(s.Muted.Render("  No posts yet. Start a post below."), -1)
		} else {
			add(s.Muted.Render("  No messages yet. Say hello 👋"), -1)
		}
	}
}

func (m *Model) gutter(cv *convView, i int) string {
	if i == cv.sel {
		return m.s.Accent.Render("▌") + " "
	}
	return "  "
}

func (m *Model) systemLines(msg *model.Message, width int) []string {
	txt := htmltext.Plain(msg.Body)
	if txt == "" {
		txt = "System event"
	}
	l := m.s.Faint.Render(trunc(txt+" · "+msg.Created.Format(m.cfg.TimeFormat), width-4))
	return []string{centerIn(l, width)}
}

// headerText is "Name  12:34 · edited  ! IMPORTANT  sending…".
func (m *Model) headerText(cv *convView, msg *model.Message, withName bool) string {
	s := m.s
	var parts []string
	if withName {
		st := s.nameColor(msg.From.ID)
		if msg.From.ID == m.me.ID {
			st = s.Accent.Bold(true)
		}
		parts = append(parts, st.Render(m.displayName(msg)))
	}
	t := s.Faint.Render(msg.Created.Format(m.cfg.TimeFormat))
	if msg.Edited && !msg.Deleted {
		t += s.Faint.Render(" · edited")
	}
	parts = append(parts, t)
	switch msg.Importance {
	case "high":
		parts = append(parts, s.Danger.Bold(true).Render("! IMPORTANT"))
	case "urgent":
		parts = append(parts, s.Badge.Render(" ! URGENT "))
	}
	if strings.HasPrefix(msg.ID, "tmp:") {
		if cv.failed[msg.ID] {
			parts = append(parts, s.Danger.Render("⚠ not sent — d to discard"))
		} else {
			parts = append(parts, s.Faint.Render("sending…"))
		}
	}
	return strings.Join(parts, "  ")
}

// messageContent renders body, attachments and records links for msg.
func (m *Model) messageContent(cv *convView, msg *model.Message, w int) []string {
	lines, _ := m.messageParts(cv, msg, w, w)
	return lines
}

// messageParts renders the text part of a message (body, file attachments)
// and, separately, its cards (cardW wide). Cards are drawn outside bubbles.
func (m *Model) messageParts(cv *convView, msg *model.Message, w, cardW int) (text, cardLines []string) {
	s := m.s
	if msg.Deleted {
		return []string{s.Faint.Italic(true).Render("This message has been deleted.")}, nil
	}
	isMe := func(atID string) bool {
		n, err := strconv.Atoi(atID)
		if err != nil {
			return false
		}
		for _, mm := range msg.Mentions {
			if mm.ID == n && mm.UserID == m.me.ID {
				return true
			}
		}
		return false
	}
	res := htmltext.Render(msg.Body, msg.ContentType != "text", htmltext.Options{Width: w, IsMe: isMe, Styles: s.html})
	out := res.Lines
	links := append([]string{}, res.Links...)
	for ai, a := range msg.Attachments {
		id := "att:" + msg.ID + ":" + itoa(ai)
		switch {
		case a.ContentType == "reference":
			out = append(out, zone.Mark(id, s.html.Link.Render(trunc("📎 "+a.Name, w))))
			links = append(links, a.ContentURL)
		case a.ContentType == "messageReference":
			out = append(out, s.Muted.Render(trunc("↪ "+quotedPreview(a.Content), w)))
		case cards.Supported(a.ContentType):
			lines, cardLinks, ok := m.renderCard(cv, msg, ai, a, cardW)
			if !ok {
				out = append(out, s.Muted.Render(trunc("▭ "+cardTitle(a), w)))
				continue
			}
			cardLines = append(cardLines, lines...)
			links = append(links, cardLinks...)
		case a.ContentURL != "":
			name := a.Name
			if name == "" {
				name = a.ContentType
			}
			out = append(out, zone.Mark(id, s.html.Link.Render(trunc("📎 "+name, w))))
			links = append(links, a.ContentURL)
		}
	}
	if len(out) == 0 && len(cardLines) == 0 {
		out = []string{""}
	}
	cv.links[msg.ID] = append(links, res.Images...)
	return out, cardLines
}

type cardRef struct {
	msgID  string
	att    int
	action cards.Action
}

// renderCard renders one card attachment and registers its clickable buttons.
func (m *Model) renderCard(cv *convView, msg *model.Message, ai int, a model.Attachment, w int) ([]string, []string, bool) {
	stateKey := msg.ID + ":" + itoa(ai)
	st := cv.cardState[stateKey]
	if st == nil {
		st = cards.NewState()
		cv.cardState[stateKey] = st
	}
	prefix := "card:" + msg.ID + ":" + itoa(ai) + ":"
	res, ok := cards.Render(a.ContentType, a.Content, cards.Options{
		Width: w, Styles: m.s.cards, State: st, Frame: true,
		Mark: func(key, label string) string { return zone.Mark(prefix+key, label) },
		IsMe: func(name string) bool { return name == m.me.DisplayName },
	})
	if !ok {
		return nil, nil, false
	}
	for _, act := range res.Actions {
		cv.cardActs[prefix+act.Key] = cardRef{msgID: msg.ID, att: ai, action: act}
	}
	return res.Lines, res.Links, true
}

// reactionChips renders reaction chips (clickable to toggle) within w cells.
func (m *Model) reactionChips(msg *model.Message, w int) string {
	if len(msg.Reactions) == 0 || msg.Deleted {
		return ""
	}
	s := m.s
	type agg struct {
		n    int
		mine bool
	}
	var order []string
	counts := map[string]*agg{}
	for _, r := range msg.Reactions {
		a := counts[r.Type]
		if a == nil {
			a = &agg{}
			counts[r.Type] = a
			order = append(order, r.Type)
		}
		a.n++
		if r.UserID == m.me.ID {
			a.mine = true
		}
	}
	var chips []string
	used := 0
	for _, e := range order {
		a := counts[e]
		st := s.Chip
		if a.mine {
			st = s.ChipMine
		}
		chip := st.Render(" " + e + " " + itoa(a.n) + " ")
		cw := ansi.StringWidth(chip) + 1
		if used+cw > w {
			break
		}
		used += cw
		chips = append(chips, zone.Mark("react:"+msg.ID+":"+e, chip))
	}
	return strings.Join(chips, " ")
}

// actionBar is shown under the selected message.
func (m *Model) actionBar(cv *convView, msg *model.Message, w int) string {
	s := m.s
	mine := msg.From.ID == m.me.ID
	type act struct{ id, label string }
	reply := "↩ Reply"
	if cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" {
		reply = "↩ Reply in thread"
	}
	acts := []act{{"reply", reply}, {"react", "☺ React"}}
	if mine && !msg.Deleted {
		acts = append(acts, act{"edit", "✎ Edit"}, act{"delete", "⌫ Delete"})
	}
	if m.hasCardActions(cv, msg) {
		acts = append(acts, act{"card", "⚡ Card actions"})
	}
	acts = append(acts, act{"copy", "⧉ Copy"})
	if len(cv.links[msg.ID]) > 0 {
		acts = append(acts, act{"open", "↗ Open"})
	}
	acts = append(acts, act{"more", "⋯"})
	var parts []string
	used := 0
	for _, a := range acts {
		b := s.Button.Render("[" + a.label + "]")
		if used+ansi.StringWidth(b)+1 > w {
			break
		}
		used += ansi.StringWidth(b) + 1
		parts = append(parts, zone.Mark("act:"+a.id, b))
	}
	for _, e := range quickReactions {
		if used+3 > w {
			break
		}
		used += 3
		parts = append(parts, zone.Mark("quick:"+e, e))
	}
	return strings.Join(parts, " ")
}

// bubbleMessage renders a chat-style message. In chats (rightAlign) your own
// messages are right-aligned like in Teams.
func (m *Model) bubbleMessage(cv *convView, i, width int, grouped, rightAlign bool) []string {
	s := m.s
	msg := &cv.msgs[i]
	mine := msg.From.ID == m.me.ID && m.me.ID != ""
	right := mine && rightAlign
	g := m.gutter(cv, i)
	avail := width - gutterW
	indent := 5 // avatar (4) + space
	if right {
		indent = 0
	}
	maxBubble := max(20, min(avail-indent-2, max(40, avail*3/4)))

	bg := bgPrefix(s.t.BubbleOther)
	if mine {
		bg = bgPrefix(s.t.BubbleMine)
	}
	if msg.Importance == "urgent" || msg.Importance == "high" {
		bg = bgPrefix(s.t.BubbleUrgent)
	}
	cardW := max(20, min(avail-indent, 84))
	content, cardLines := m.messageParts(cv, msg, maxBubble-2, cardW)
	if len(cardLines) > 0 && len(content) == 1 && strings.TrimSpace(ansi.Strip(content[0])) == "" {
		content = nil // card-only message: no empty bubble
	}
	bw := 0
	for _, l := range content {
		bw = max(bw, ansi.StringWidth(l))
	}
	bw = min(bw, maxBubble-2) + 2

	place := func(l string, lw int) string {
		if right {
			return g + strings.Repeat(" ", max(0, avail-lw)) + l
		}
		return g + strings.Repeat(" ", indent) + l
	}

	var out []string
	if !grouped {
		if right {
			h := m.headerText(cv, msg, false)
			out = append(out, place(h, ansi.StringWidth(h)))
		} else {
			out = append(out, g+m.avatarFor(msg)+" "+trunc(m.headerText(cv, msg, true), avail-indent))
		}
	} else if strings.HasPrefix(msg.ID, "tmp:") || msg.Importance != "normal" && msg.Importance != "" {
		h := m.headerText(cv, msg, false)
		out = append(out, place(h, ansi.StringWidth(h)))
	}
	if msg.Subject != "" {
		out = append(out, place(s.Title.Render(trunc(msg.Subject, maxBubble)), min(ansi.StringWidth(msg.Subject), maxBubble)))
	}
	for _, l := range content {
		out = append(out, place(withBg(" "+l, bg, bw), bw))
	}
	for _, l := range cardLines {
		out = append(out, place(l, ansi.StringWidth(l)))
	}
	if chips := m.reactionChips(msg, avail-indent); chips != "" {
		out = append(out, place(chips, ansi.StringWidth(chips)))
	}
	if i == cv.sel {
		bar := m.actionBar(cv, msg, avail-indent)
		out = append(out, place(bar, ansi.StringWidth(bar)))
	}
	return out
}

// postCard renders a channel post as a card containing its thread summary.
// inThread renders the original post at the top of a thread (no footer).
func (m *Model) postCard(cv *convView, i, width int, inThread bool) []string {
	s := m.s
	msg := &cv.msgs[i]
	g := m.gutter(cv, i)
	cw := min(width-gutterW, 120) // card width (capped for readability)
	inner := cw - 4

	border := s.Border
	if i == cv.sel || (m.thread != nil && m.thread.ref.ThreadID == msg.ID && !inThread) || inThread {
		border = s.Accent
	}
	if msg.Importance == "urgent" || msg.Importance == "high" {
		border = s.Danger
	}

	var body []string
	body = append(body, m.avatarFor(msg)+" "+trunc(m.headerText(cv, msg, true), inner-5))
	if msg.Subject != "" {
		body = append(body, s.Title.Render(trunc(msg.Subject, inner)))
	}
	text, cardLines := m.messageParts(cv, msg, inner, min(inner, 84))
	if len(cardLines) > 0 && len(text) == 1 && strings.TrimSpace(ansi.Strip(text[0])) == "" {
		text = nil
	}
	body = append(body, text...)
	body = append(body, cardLines...)
	if chips := m.reactionChips(msg, inner); chips != "" {
		body = append(body, "", chips)
	}
	if i == cv.sel {
		body = append(body, "", m.actionBar(cv, msg, inner))
	}

	var footer []string
	if !inThread && !msg.Deleted {
		if msg.ReplyCount > 0 {
			names := []string{}
			seen := map[string]bool{}
			for k := len(msg.Replies) - 1; k >= 0 && len(names) < 3; k-- {
				n := m.displayName(&msg.Replies[k])
				if n != "" && !seen[n] {
					seen[n] = true
					names = append(names, strings.Fields(n)[0])
				}
			}
			label := "💬 " + itoa(msg.ReplyCount) + " replies"
			if msg.ReplyCount == 1 {
				label = "💬 1 reply"
			}
			if len(names) > 0 {
				label += " from " + strings.Join(names, ", ")
			}
			footer = append(footer, zone.Mark("thread:"+msg.ID, s.Accent.Bold(true).Render(trunc(label, inner-14)))+"  "+zone.Mark("thread:"+msg.ID+":open", s.Button.Render("Open thread ›")))
			// latest replies, one line each
			start := max(0, len(msg.Replies)-2)
			if start > 0 || msg.ReplyCount > len(msg.Replies) {
				more := msg.ReplyCount - 2
				if more == 1 {
					footer = append(footer, s.Faint.Render("   ⋮ 1 earlier reply"))
				} else if more > 1 {
					footer = append(footer, s.Faint.Render("   ⋮ "+itoa(more)+" earlier replies"))
				}
			}
			for _, r := range msg.Replies[start:] {
				r := r
				txt := plainOneLine(htmltext.Plain(r.Body))
				if r.Deleted {
					txt = "This message has been deleted."
				}
				who := strings.Fields(m.displayName(&r) + " ")[0]
				left := "   " + m.avatarFor(&r) + " " + s.nameColor(r.From.ID).Render(who) + "  " + s.Muted.Render(trunc(txt, inner-20-ansi.StringWidth(who)))
				footer = append(footer, spread(left, s.Faint.Render(relTime(r.Created, m.cfg.TimeFormat)), inner))
			}
			footer = append(footer, zone.Mark("thread:"+msg.ID+":reply", s.Button.Render("↩ Reply")))
		} else {
			footer = append(footer, zone.Mark("thread:"+msg.ID, s.Button.Render("↩ Reply")))
		}
	}

	var out []string
	out = append(out, g+border.Render("╭"+strings.Repeat("─", cw-2)+"╮"))
	for _, l := range body {
		out = append(out, g+border.Render("│")+" "+padRight(l, inner)+" "+border.Render("│"))
	}
	if len(footer) > 0 {
		out = append(out, g+border.Render("├"+strings.Repeat("─", cw-2)+"┤"))
		for _, l := range footer {
			out = append(out, g+border.Render("│")+" "+padRight(l, inner)+" "+border.Render("│"))
		}
	}
	out = append(out, g+border.Render("╰"+strings.Repeat("─", cw-2)+"╯"))
	return out
}
