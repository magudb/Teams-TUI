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
	count   int // posts and replies from others since you last looked
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
	seenAt := m.st.Seen[key]
	for _, r := range msgs {
		for _, x := range append([]model.Message{r}, r.Replies...) {
			if x.From.ID != m.me.ID && x.MessageType == "message" && !x.Deleted && x.Created.After(seenAt) {
				act.count++
			}
		}
	}
	m.chanAct[key] = act
	if _, seen := m.st.Seen[key]; !seen || viewing {
		// first time we see this channel (don't flood Unread), or it's open
		m.st.Seen[key] = maxTime(act.latest, time.Now())
		if viewing {
			return
		}
	}
	if had && act.latest.After(prev.latest) && !viewing {
		replied := false
		for _, r := range msgs {
			if r.From.ID != m.me.ID {
				continue
			}
			for _, x := range r.Replies {
				if x.From.ID != m.me.ID && x.Created.After(prev.latest) {
					th := ref
					th.ThreadID = r.ID
					m.addActivity(activityItem{at: x.Created, kind: "reply", title: firstName(x.From.DisplayName) + " replied",
						preview: m.convTitle(ref) + ": " + htmltext.Plain(x.Body), ref: th, msgID: x.ID})
					replied = true
				}
			}
		}
		if !replied {
			m.addActivity(activityItem{at: act.latest, kind: "message", title: firstName(act.from) + " posted",
				preview: m.convTitle(ref) + ": " + act.preview, ref: ref, msgID: act.msgID})
		}
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
	if ac := m.conv; m.viewingMessages(ac) && ac.ref.Kind == model.KindChannel && ac.ref.ChannelID == ch.ID {
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

func (m *Model) chatMatchesFilter(c model.Chat) bool {
	q := strings.ToLower(strings.TrimSpace(m.chatFilter.Value()))
	return q == "" || strings.Contains(strings.ToLower(c.Title(m.me.ID)), q)
}
