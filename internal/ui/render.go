package ui

import (
	"slices"
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

// Conversation rendering in the "Blocks" style:
//   - chats: name + time, then the text on a filled block; your own messages
//     right-aligned in an inverse block; chips for days and "New messages"
//   - channels: every post is a filled block whose footer bar carries its
//     thread (reply count, who replied, open)
//   - threads: the original post condensed on top, a reply-count chip, then
//     the replies

var quickReactions = []string{"👍", "❤️", "😆", "😮", "😢", "😡"}

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

func (m *Model) displayName(msg *model.Message) string {
	if msg.From.ID == m.me.ID && m.me.ID != "" {
		return "You"
	}
	if msg.From.DisplayName != "" {
		return msg.From.DisplayName
	}
	return m.userName(msg.From.ID)
}

// paneFills returns the fills used inside a pane: its own background, the
// fill for message blocks on it, and the fill for chips.
func (m *Model) paneFills(side bool) (pane, block, chip lipgloss.Color) {
	t := m.s.t
	if side {
		return t.Side, t.Paper, t.Block
	}
	return t.Paper, t.Side, t.Block
}

// renderConv lays out all messages of cv for the given width.
func (m *Model) renderConv(cv *convView, width int, side bool) {
	if !cv.dirty && cv.width == width && cv.side == side {
		for _, i := range cv.changedBlocks {
			m.redrawMessage(cv, i)
		}
		cv.changedBlocks = cv.changedBlocks[:0]
		if cv.renderedSel != cv.sel {
			m.redrawMessage(cv, cv.renderedSel)
			m.redrawMessage(cv, cv.sel)
			cv.renderedSel = cv.sel
		}
		return
	}
	cv.renderedSel = cv.sel
	cv.changedBlocks = cv.changedBlocks[:0]
	cv.grouped = make([]bool, len(cv.msgs))
	cv.dirty = false
	cv.width = width
	cv.side = side
	cv.lines = cv.lines[:0]
	cv.owner = cv.owner[:0]
	cv.msgStart = make([]int, len(cv.msgs))
	cv.links = map[string][]string{}
	cv.cardActs = map[string]cardRef{}
	s := m.s
	_, blockC, chipC := m.paneFills(side)

	add := func(line string, owner int) {
		if line == "" && owner == -1 && (len(cv.lines) == 0 || cv.lines[len(cv.lines)-1] == "") {
			return // never stack blank lines
		}
		cv.lines = append(cv.lines, line)
		cv.owner = append(cv.owner, owner)
	}
	chip := func(label string, st lipgloss.Style) string {
		return centerIn(st.Render("  "+label+"  "), width)
	}
	dayChip := lipgloss.NewStyle().Background(blockC).Foreground(s.t.Muted)
	if side {
		dayChip = lipgloss.NewStyle().Background(chipC).Foreground(s.t.Text)
	}

	add("", -1)
	if cv.next != "" {
		add(zone.Mark("older", "  "+s.Button.Render("↑ Load older messages")), -2)
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
		divided := false

		if kind != "thread" && (prev == nil || !sameDay(prev.Created, msg.Created)) {
			add("", -1)
			add(chip(dayLabel(msg.Created), dayChip), -1)
			add("", -1)
			divided = true
		}
		if kind == "chat" && !newShown && !cv.newSince.IsZero() && msg.From.ID != m.me.ID && msg.Created.After(cv.newSince) {
			newShown = true
			add("", -1)
			add(chip("New messages", s.Badge), -1)
			add("", -1)
			divided = true
		}
		grouped := kind != "channel" && !isRoot && prev != nil && prev.From.ID == msg.From.ID &&
			msg.Created.Sub(prev.Created) < 5*time.Minute && sameDay(prev.Created, msg.Created) &&
			msg.MessageType == "message" && prev.MessageType == "message" && msg.Subject == "" &&
			!(kind == "thread" && prev.ID == cv.ref.ThreadID) && !divided
		if i > 0 && !grouped {
			add("", -1)
		}

		cv.msgStart[i] = len(cv.lines)
		cv.grouped[i] = grouped
		lines := m.conversationMessage(cv, i)
		for _, l := range lines {
			add(l, i)
		}
		if isRoot {
			n := len(cv.msgs) - 1
			label := itoa(n) + " replies"
			switch n {
			case 1:
				label = "1 reply"
			case 0:
				label = "No replies yet"
			}
			add("", -1)
			add(chip(label, lipgloss.NewStyle().Background(chipC).Foreground(s.t.Text)), -1)
		}
		prev = msg
	}
	switch {
	case cv.loading && len(cv.msgs) == 0:
		add("  "+s.Muted.Render("Loading messages…"), -1)
	case cv.err != "" && len(cv.msgs) == 0:
		add("  "+s.Danger.Render(cv.err), -1)
	case cv.loadedOnce && len(cv.msgs) == 0:
		if kind == "channel" {
			add("  "+s.Muted.Render("No posts yet. Start a post below."), -1)
		} else {
			add("  "+s.Muted.Render("No messages yet. Say hello."), -1)
		}
	}
}

// redrawMessage replaces just the message block. Day headers, new-message
// dividers, and the reply-count chip lie outside its owned range.
func (m *Model) redrawMessage(cv *convView, i int) {
	if i < 0 || i >= len(cv.msgStart) {
		return
	}
	start := cv.msgStart[i]
	end := start
	for end < len(cv.owner) && cv.owner[end] == i {
		end++
	}
	id := cv.msgs[i].ID
	delete(cv.links, id)
	for key, action := range cv.cardActs {
		if action.msgID == id {
			delete(cv.cardActs, key)
		}
	}
	lines := m.conversationMessage(cv, i)
	cv.lines = slices.Replace(cv.lines, start, end, lines...)
	owners := make([]int, len(lines))
	for j := range owners {
		owners[j] = i
	}
	cv.owner = slices.Replace(cv.owner, start, end, owners...)
	delta := len(lines) - (end - start)
	if delta != 0 {
		for j := i + 1; j < len(cv.msgStart); j++ {
			cv.msgStart[j] += delta
		}
	}
}

func (m *Model) conversationMessage(cv *convView, i int) []string {
	msg := &cv.msgs[i]
	switch {
	case msg.MessageType != "" && msg.MessageType != "message":
		return m.systemLines(msg, cv.width)
	case cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "":
		return m.postBlock(cv, i, cv.width, cv.side)
	case cv.ref.ThreadID != "" && msg.ID == cv.ref.ThreadID:
		return m.threadRoot(cv, i, cv.width)
	default:
		return m.chatMessage(cv, i, cv.width, cv.grouped[i], cv.ref.Kind == model.KindChat && cv.ref.ThreadID == "", cv.side)
	}
}

func (m *Model) systemLines(msg *model.Message, width int) []string {
	txt := htmltext.Plain(msg.Body)
	if txt == "" {
		txt = "System event"
	}
	return []string{centerIn(m.s.Muted.Render(trunc(txt+" · "+msg.Created.Format(m.cfg.TimeFormat), width-4)), width)}
}

// headerText is "Name 12:34 · edited  MENTION  sending…".
func (m *Model) headerText(cv *convView, msg *model.Message, withName bool) string {
	s := m.s
	var parts []string
	if withName {
		parts = append(parts, s.nameColor(msg.From.ID).Render(m.displayName(msg))+" "+s.Muted.Render(msg.Created.Format(m.cfg.TimeFormat)))
	} else {
		parts = append(parts, s.Muted.Render(msg.Created.Format(m.cfg.TimeFormat)))
	}
	if msg.Edited && !msg.Deleted {
		parts[0] += s.Muted.Render(" · edited")
	}
	if msg.ID == m.jumpMention && m.jumpMention != "" {
		parts = append(parts, s.Badge.Render(" MENTION "))
	}
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
			parts = append(parts, s.Muted.Render("sending…"))
		}
	}
	return strings.Join(parts, " ")
}

func (m *Model) messageContent(cv *convView, msg *model.Message, w int) []string {
	lines, _ := m.messageParts(cv, msg, w, w)
	return lines
}

// messageParts renders the text part of a message (body, file attachments)
// and, separately, its cards (cardW wide). Cards are drawn outside blocks.
func (m *Model) messageParts(cv *convView, msg *model.Message, w, cardW int) (text, cardLines []string) {
	s := m.s
	if msg.Deleted {
		return []string{s.Muted.Italic(true).Render("This message has been deleted.")}, nil
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
	if msg.ID == m.searchTarget && m.searchQ != "" {
		for i, l := range out {
			out[i] = highlight(ansi.Strip(l), m.searchQ, s.Text, s.Inverse.Bold(true))
		}
	}
	links := append([]string{}, res.Links...)
	for ai, a := range msg.Attachments {
		id := "att:" + msg.ID + ":" + itoa(ai)
		switch {
		case a.ContentType == "reference":
			out = append(out, zone.Mark(id, s.Accent.Render(s.ic.Clip)+" "+s.html.Link.Render(trunc(a.Name, w-12))+attachmentSize(s, a)))
			links = append(links, a.ContentURL)
		case a.ContentType == "messageReference":
			out = append(out, s.Muted.Render(trunc(s.ic.Reply+" "+quotedPreview(a.Content), w)))
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
			out = append(out, zone.Mark(id, s.Accent.Render(s.ic.Clip)+" "+s.html.Link.Render(trunc(name, w-4))))
			links = append(links, a.ContentURL)
		}
	}
	if len(out) == 0 && len(cardLines) == 0 {
		out = []string{""}
	}
	cv.links[msg.ID] = append(links, res.Images...)
	return out, cardLines
}

func attachmentSize(s styles, a model.Attachment) string {
	if i := strings.Index(a.ContentURL, "size="); i >= 0 {
		if n, err := strconv.ParseInt(a.ContentURL[i+5:], 10, 64); err == nil {
			return " " + s.Muted.Render(humanSize(n))
		}
	}
	return ""
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
	label := msg.From.DisplayName
	res, ok := cards.Render(a.ContentType, a.Content, cards.Options{
		Width: w, Styles: m.s.cards, State: st, Frame: true, Label: label,
		Mark: func(key, label string) string { return zone.Mark(prefix+key, label) },
		IsMe: func(name string) bool { return name == m.me.DisplayName },
	})
	if !ok {
		return nil, nil, false
	}
	for _, act := range res.Actions {
		cv.cardActs[prefix+act.Key] = cardRef{msgID: msg.ID, att: ai, action: act}
	}
	// card body on the card fill, title bar keeps its own
	lines := make([]string, len(res.Lines))
	for i, l := range res.Lines {
		if i == 0 {
			lines[i] = l
			continue
		}
		lines[i] = withBg(l, m.s.bgCard, w)
	}
	return lines, res.Links, true
}

// reactionIcon maps common reactions to the icon set (others stay emoji).
func (m *Model) reactionIcon(e string) string {
	if m.cfg.Icons == "ascii" {
		return e
	}
	switch e {
	case "👍":
		return m.s.ic.ThumbsUp
	case "✅", "✔️", "✔":
		return m.s.ic.Check
	}
	return e
}

// reactionChips renders reaction chips (clickable to toggle) within w cells.
func (m *Model) reactionChips(msg *model.Message, w int, chipC lipgloss.Color) string {
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
		st := lipgloss.NewStyle().Background(chipC).Foreground(s.t.Text)
		if a.mine {
			st = s.ChipMine
		}
		chip := st.Render(" " + m.reactionIcon(e) + " " + itoa(a.n) + " ")
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
	ic := s.ic
	mine := msg.From.ID == m.me.ID
	type act struct{ id, label string }
	reply := ic.Reply + " Reply"
	if cv.ref.Kind == model.KindChannel && cv.ref.ThreadID == "" {
		reply = ic.Reply + " Reply in thread"
	}
	var acts []act
	if m.hasCardActions(cv, msg) {
		acts = append(acts, act{"card", ic.Bolt + " Card actions"})
	} else {
		acts = append(acts, act{"reply", reply})
	}
	acts = append(acts, act{"react", "+ React"})
	if mine && !msg.Deleted {
		acts = append(acts, act{"edit", ic.Pencil + " Edit"}, act{"delete", "Delete"})
	}
	acts = append(acts, act{"copy", "Copy"})
	if len(cv.links[msg.ID]) > 0 && !m.hasCardActions(cv, msg) {
		acts = append(acts, act{"open", "Open " + ic.External})
	}
	acts = append(acts, act{"more", ic.More})
	var parts []string
	used := 0
	for _, a := range acts {
		b := s.Button.Render(a.label)
		if used+ansi.StringWidth(b)+1 > w {
			break
		}
		used += ansi.StringWidth(b) + 1
		parts = append(parts, zone.Mark("act:"+a.id, b))
	}
	return strings.Join(parts, " ")
}

// chatMessage renders a chat-style message: header, filled text block,
// cards, reactions and (when selected) the action row. In chats your own
// messages are right-aligned on an inverse block.
func (m *Model) chatMessage(cv *convView, i, width int, grouped, rightAlign, side bool) []string {
	s := m.s
	msg := &cv.msgs[i]
	mine := msg.From.ID == m.me.ID && m.me.ID != ""
	right := mine && rightAlign
	_, blockC, chipC := m.paneFills(side)
	const indent = 2
	avail := width - 2*indent
	maxBlock := max(16, avail-max(2, width/8))

	bg := bgPrefix(blockC)
	textSt := lipgloss.NewStyle()
	if mine {
		bg = s.bgInk
		textSt = lipgloss.NewStyle().Foreground(s.t.OnInk)
	}
	cardW := max(20, min(avail, 72))
	content, cardLines := m.messageParts(cv, msg, maxBlock-2, cardW)
	if len(cardLines) > 0 && len(content) == 1 && strings.TrimSpace(ansi.Strip(content[0])) == "" {
		content = nil // card-only message: no empty block
	}
	bw := 0
	for _, l := range content {
		bw = max(bw, ansi.StringWidth(l))
	}
	bw = min(bw, maxBlock-2) + 2

	place := func(l string, lw int) string {
		if right {
			return strings.Repeat(" ", max(0, width-indent-lw)) + l
		}
		return strings.Repeat(" ", indent) + l
	}

	var out []string
	if !grouped || msg.Importance == "high" || msg.Importance == "urgent" || strings.HasPrefix(msg.ID, "tmp:") {
		h := m.headerText(cv, msg, !right)
		out = append(out, place(h, ansi.StringWidth(h)))
	}
	if msg.Subject != "" {
		sub := s.Title.Render(trunc(msg.Subject, maxBlock))
		out = append(out, place(sub, ansi.StringWidth(sub)))
	}
	for _, l := range content {
		if mine {
			l = textSt.Render(ansi.Strip(l))
		}
		out = append(out, place(withBg(" "+l, bg, bw), bw))
	}
	for _, l := range cardLines {
		out = append(out, place(l, ansi.StringWidth(l)))
	}
	if chips := m.reactionChips(msg, avail, chipC); chips != "" {
		out = append(out, place(chips, ansi.StringWidth(chips)))
	}
	if i == cv.sel {
		bar := m.actionBar(cv, msg, avail)
		out = append(out, place(bar, ansi.StringWidth(bar)))
	}
	return out
}

// threadRoot is the condensed original post at the top of a thread.
func (m *Model) threadRoot(cv *convView, i, width int) []string {
	s := m.s
	msg := &cv.msgs[i]
	w := width - 4
	out := []string{"  " + m.headerText(cv, msg, true)}
	if msg.Subject != "" {
		out = append(out, "  "+s.Title.Render(trunc(msg.Subject, w)))
	}
	text, cardLines := m.messageParts(cv, msg, w, w)
	if len(text) > 3 && i != cv.sel {
		text = append(text[:2], s.Muted.Render(trunc(ansi.Strip(text[2]), w-1)+"…"))
	}
	for _, l := range text {
		out = append(out, "  "+s.Muted.Render(ansi.Strip(l)))
	}
	for _, l := range cardLines {
		out = append(out, "  "+l)
	}
	if i == cv.sel {
		out = append(out, "  "+m.actionBar(cv, msg, w))
	}
	return out
}

// postBlock renders a channel post as a filled block whose footer bar holds
// its thread: reply count and repliers (accent), or a Reply button.
func (m *Model) postBlock(cv *convView, i, width int, side bool) []string {
	s := m.s
	ic := s.ic
	msg := &cv.msgs[i]
	const indent = 2
	bw := min(width-2*indent, 110)
	inner := bw - 2
	_, blockC, chipC := m.paneFills(side)
	fill := bgPrefix(blockC)
	if i == cv.sel {
		fill = s.bgBlock
		chipC = s.t.Side
	}

	var body []string
	body = append(body, m.headerText(cv, msg, true))
	if msg.Subject != "" {
		body = append(body, s.Title.Render(trunc(msg.Subject, inner)))
	}
	text, cardLines := m.messageParts(cv, msg, inner, inner)
	if len(cardLines) > 0 && len(text) == 1 && strings.TrimSpace(ansi.Strip(text[0])) == "" {
		text = nil
	}
	body = append(body, text...)
	if chips := m.reactionChips(msg, inner, chipC); chips != "" {
		body = append(body, chips)
	}
	if i == cv.sel {
		body = append(body, m.actionBar(cv, msg, inner))
	}

	pad := strings.Repeat(" ", indent)
	var out []string
	for _, l := range body {
		out = append(out, pad+withBg(" "+l, fill, bw))
	}
	for _, l := range cardLines {
		// cards sit inside the block, one column in
		out = append(out, pad+withBg(" "+l, fill, bw))
	}

	if msg.Deleted {
		return out
	}
	if msg.ReplyCount > 0 {
		var names []string
		seen := map[string]bool{}
		for k := len(msg.Replies) - 1; k >= 0 && len(names) < 3; k-- {
			n := m.displayName(&msg.Replies[k])
			if n == "You" {
				n = "you"
			}
			if n != "" && !seen[n] {
				seen[n] = true
				names = append(names, strings.Fields(n)[0])
			}
		}
		label := ic.Reply + " " + itoa(msg.ReplyCount) + " replies"
		if msg.ReplyCount == 1 {
			label = ic.Reply + " 1 reply"
		}
		if len(names) > 0 {
			label += " · " + strings.Join(names, ", ")
		}
		bar := s.Sel.Render(spread(" "+trunc(label, bw-10), "open › ", bw))
		out = append(out, pad+zone.Mark("thread:"+msg.ID+":open", bar))
	} else {
		bar := lipgloss.NewStyle().Background(s.t.Block).Foreground(s.t.Link).Bold(true).Render(padRight(" "+ic.Reply+" Reply", bw))
		out = append(out, pad+zone.Mark("thread:"+msg.ID+":reply", bar))
	}
	return out
}
