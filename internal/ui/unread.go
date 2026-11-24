package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
)

// Microsoft Graph has no unread state for channels, so it is tracked locally:
// a background poller checks one channel at a time for its newest post or
// reply, and compares it with when you last opened that channel.

type chanActivity struct {
	latest  time.Time
	from    string
	preview string
	msgID   string
}

type channelPollMsg struct {
	ref  model.ConvRef
	msgs []model.Message
	err  error
}

func chanKey(teamID, channelID string) string {
	return model.ConvRef{Kind: model.KindChannel, TeamID: teamID, ChannelID: channelID}.Key()
}

// latestActivity finds the newest post or reply not written by me.
func (m *Model) latestActivity(msgs []model.Message) (chanActivity, bool) {
	var best chanActivity
	found := false
	consider := func(x model.Message, root model.Message) {
		if x.Deleted || x.MessageType != "message" || x.From.ID == m.me.ID || x.From.ID == "" {
			return
		}
		if !found || x.Created.After(best.latest) {
			found = true
			prev := htmltext.Plain(x.Body)
			if x.Subject != "" {
				prev = x.Subject + " — " + prev
			}
			best = chanActivity{latest: x.Created, from: x.From.DisplayName, preview: prev, msgID: root.ID}
		}
	}
	for _, r := range msgs {
		consider(r, r)
		for _, rep := range r.Replies {
			consider(rep, r)
		}
	}
	return best, found
}

// pollNextChannel checks the next channel in round-robin order.
func (m *Model) pollNextChannel() tea.Cmd {
	var refs []model.ConvRef
	visible, _ := m.orderedTeams()
	for _, t := range visible {
		for _, ch := range m.channels[t.ID] {
			refs = append(refs, model.ConvRef{Kind: model.KindChannel, TeamID: t.ID, ChannelID: ch.ID})
		}
	}
	if len(refs) == 0 {
		return nil
	}
	m.chanPollIdx = (m.chanPollIdx + 1) % len(refs)
	ref := refs[m.chanPollIdx]
	if ac := m.activeConv(); ac != nil && ac.ref.TeamID == ref.TeamID && ac.ref.ChannelID == ref.ChannelID {
		return nil // the open channel is polled anyway
	}
	m.pollingChannel = true
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ms, _, err := svc.Messages(c, ref, "")
		return channelPollMsg{ref, ms, err}
	}
}

// noteChannelMessages records activity for a channel from freshly loaded messages.
func (m *Model) noteChannelMessages(ref model.ConvRef, msgs []model.Message, viewing bool) {
	ref.ThreadID = ""
	key := ref.Key()
	act, ok := m.latestActivity(msgs)
	if !ok {
		if viewing {
			m.st.Seen[key] = time.Now()
		}
		return
	}
	prev, had := m.chanAct[key]
	m.chanAct[key] = act
	if _, seen := m.st.Seen[key]; !seen || viewing {
		// first time we see this channel (don't flood Unread), or it's open
		m.st.Seen[key] = maxTime(act.latest, time.Now())
		if viewing {
			return
		}
	}
	if had && act.latest.After(prev.latest) && !viewing {
		title := act.from + " in " + m.convTitle(ref)
		m.addActivity(activityItem{at: act.latest, kind: "message", title: title, preview: act.preview, ref: ref, msgID: act.msgID})
	}
	// notify on new mentions of me in channels
	for _, r := range msgs {
		all := append([]model.Message{r}, r.Replies...)
		for _, x := range all {
			if !had || !x.Created.After(prev.latest) || x.From.ID == m.me.ID {
				continue
			}
			for _, mm := range x.Mentions {
				if mm.UserID == m.me.ID && !m.st.Muted[key] {
					m.notify(x.From.DisplayName+" mentioned you", htmltext.Plain(x.Body), m.convTitle(ref))
				}
			}
		}
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// channelUnread reports whether a channel has activity you haven't seen.
func (m *Model) channelUnread(ch model.Channel) bool {
	key := chanKey(ch.TeamID, ch.ID)
	act, ok := m.chanAct[key]
	if !ok {
		return false
	}
	if ac := m.conv; ac != nil && ac.ref.Kind == model.KindChannel && ac.ref.ChannelID == ch.ID {
		return false
	}
	return act.latest.After(m.st.Seen[key])
}

func (m *Model) markChannelSeen(ref model.ConvRef) {
	ref.ThreadID = ""
	m.st.Seen[ref.Key()] = time.Now()
	_ = m.st.Save()
}

func (m *Model) unreadChannels() []model.Channel {
	var out []model.Channel
	visible, _ := m.orderedTeams()
	for _, t := range visible {
		for _, ch := range m.channels[t.ID] {
			if m.channelUnread(ch) && !m.st.Muted[chanKey(t.ID, ch.ID)] {
				out = append(out, ch)
			}
		}
	}
	return out
}

func (m *Model) teamName(id string) string {
	if t := m.teamByID(id); t != nil {
		return t.DisplayName
	}
	return ""
}

// combinedItems builds the combined Chat tab: Unread, Channels, Chats.
func (m *Model) combinedItems(w int) []sideItem {
	s := m.s
	var items []sideItem
	section := func(id, title string, count int) bool {
		collapsed := m.st.Expanded["collapsed:"+id]
		arrow := "▾"
		if collapsed {
			arrow = "▸"
		}
		label := strings.ToUpper(title)
		if count > 0 {
			label += " (" + itoa(count) + ")"
		}
		if len(items) > 0 {
			items = append(items, sideItem{lines: []string{""}})
		}
		items = append(items, sideItem{key: "sec:" + id, selectable: true,
			lines: []string{s.Faint.Render(trunc(" "+arrow+" "+label, w))},
			open: func(m *Model) tea.Cmd {
				m.st.Expanded["collapsed:"+id] = !m.st.Expanded["collapsed:"+id]
				_ = m.st.Save()
				return nil
			}})
		return !collapsed
	}

	// Unread: chats and channels with new messages
	var unreadChats []model.Chat
	for _, c := range m.chats {
		if c.Unread(m.me.ID) && !m.st.Muted["chat:"+c.ID] && m.chatMatchesFilter(c) {
			unreadChats = append(unreadChats, c)
		}
	}
	model.SortChats(unreadChats)
	unreadChs := m.unreadChannels()
	if n := len(unreadChats) + len(unreadChs); n > 0 {
		if section("unread", "Unread", n) {
			for _, ch := range unreadChs {
				items = append(items, m.unreadChannelItem(ch, w))
			}
			for _, c := range unreadChats {
				it := m.chatItem(c, w)
				id := c.ID
				it.key = "u:" + it.key
				it.open = func(m *Model) tea.Cmd {
					m.sideSel[SecChat] = "chat:" + id
					return m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: id})
				}
				items = append(items, it)
			}
		}
	}

	if section("channels", "Channels", 0) {
		items = append(items, m.teamTreeItems(w)...)
	}
	if section("chats", "Chats", 0) {
		items = append(items, m.chatListItems(w, true)...)
	}
	return items
}

func (m *Model) chatMatchesFilter(c model.Chat) bool {
	q := strings.ToLower(strings.TrimSpace(m.chatFilter.Value()))
	return q == "" || strings.Contains(strings.ToLower(c.Title(m.me.ID)), q)
}

func (m *Model) unreadChannelItem(ch model.Channel, w int) sideItem {
	s := m.s
	act := m.chanAct[chanKey(ch.TeamID, ch.ID)]
	icon := "#"
	if ch.MembershipType == "private" {
		icon = "🔒"
	}
	name := s.Bold.Render(trunc(ch.DisplayName, w/2)) + s.Faint.Render(" · "+trunc(m.teamName(ch.TeamID), w/3))
	l1 := spread(" "+s.Faint.Render(icon)+" "+name, s.Accent.Bold(true).Render("● ")+s.Faint.Render(relTime(act.latest, m.cfg.TimeFormat))+" ", w)
	who := ""
	if act.from != "" {
		who = strings.Fields(act.from)[0] + ": "
	}
	l2 := "   " + s.Text.Render(trunc(plainOneLine(who+act.preview), w-4))
	teamID, chID := ch.TeamID, ch.ID
	return sideItem{key: "u:chan:" + ch.ID, selectable: true, lines: []string{l1, l2}, open: func(m *Model) tea.Cmd {
		m.sideSel[SecChat] = "chan:" + chID
		m.st.Expanded[teamID] = true
		return m.openConv(model.ConvRef{Kind: model.KindChannel, TeamID: teamID, ChannelID: chID})
	}}
}
