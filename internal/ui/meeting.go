package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/magudb/Teams-TUI/internal/model"
)

// Meeting companion: attendees with presence, meeting chat, join banner,
// and post-meeting transcripts / recordings. Audio and video are handed off
// to the Teams client.

type resolvedMsg struct {
	users map[string]model.User
	err   error
}

type artifactsMsg struct {
	eventID string
	a       model.MeetingArtifacts
	err     error
}

type transcriptMsg struct {
	title string
	vtt   string
	err   error
}

type artifactState struct {
	loading bool
	a       model.MeetingArtifacts
	err     string
}

// resolveAttendees looks up directory users for the selected event's
// attendees so their presence can be shown.
func (m *Model) resolveAttendees(e *model.Event) tea.Cmd {
	var emails []string
	for _, a := range e.Attendees {
		k := strings.ToLower(a.Email)
		if k == "" || m.resolving[k] {
			continue
		}
		m.resolving[k] = true
		emails = append(emails, k)
	}
	if e.OrganizerEmail != "" && !m.resolving[strings.ToLower(e.OrganizerEmail)] {
		k := strings.ToLower(e.OrganizerEmail)
		m.resolving[k] = true
		emails = append(emails, k)
	}
	if len(emails) == 0 {
		return nil
	}
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		us, err := svc.ResolveUsers(c, emails)
		return resolvedMsg{us, err}
	}
}

func (m *Model) loadArtifacts(e *model.Event) tea.Cmd {
	if e == nil || e.JoinURL == "" || m.artifacts[e.ID] != nil {
		return nil
	}
	m.artifacts[e.ID] = &artifactState{loading: true}
	id, join := e.ID, e.JoinURL
	svc := m.svc
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		a, err := svc.MeetingArtifacts(c, join)
		return artifactsMsg{id, a, err}
	}
}

// onEventSelected fetches what the detail pane needs for the selected event.
func (m *Model) onEventSelected() tea.Cmd {
	e := m.selectedEvent()
	if e == nil {
		return nil
	}
	cmds := []tea.Cmd{m.resolveAttendees(e)}
	if e.JoinURL != "" && time.Now().After(e.End) {
		cmds = append(cmds, m.loadArtifacts(e))
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleMeetingData(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case resolvedMsg:
		var ids []string
		for email, u := range msg.users {
			m.emailUsers[email] = u
			m.rememberUsers(u)
			ids = append(ids, u.ID)
		}
		if len(ids) == 0 {
			return nil, true
		}
		svc := m.svc
		return func() tea.Msg {
			c, cancel := ctx()
			defer cancel()
			p, err := svc.Presence(c, ids)
			return presenceMsg{p, err}
		}, true
	case artifactsMsg:
		st := &artifactState{a: msg.a}
		if msg.err != nil {
			st.err = oneLineErr(msg.err)
		}
		m.artifacts[msg.eventID] = st
		return nil, true
	case transcriptMsg:
		m.busy = max(0, m.busy-1)
		if msg.err != nil {
			m.errToast(msg.err)
			return nil, true
		}
		m.openTranscript(msg.title, msg.vtt)
		return nil, true
	}
	return nil, false
}

// upcomingMeeting returns the joinable meeting that is live now or starts
// within the next 10 minutes (earliest first).
func (m *Model) upcomingMeeting() *model.Event {
	now := time.Now()
	var best *model.Event
	for i := range m.events {
		e := &m.events[i]
		if !e.Joinable() || e.IsAllDay {
			continue
		}
		if e.Live(now) || (e.Start.After(now) && e.Start.Sub(now) <= 10*time.Minute) {
			if best == nil || e.Start.Before(best.Start) {
				best = e
			}
		}
	}
	return best
}

func untilLabel(e *model.Event) string {
	now := time.Now()
	if e.Live(now) {
		return "live now"
	}
	d := e.Start.Sub(now).Round(time.Minute)
	if d < time.Minute {
		return "starting now"
	}
	return "in " + itoa(int(d.Minutes())) + " min"
}

func (m *Model) joinEvent(e *model.Event) tea.Cmd {
	if e == nil {
		m.setToast("No meeting to join right now", false)
		return nil
	}
	if e.JoinURL == "" {
		m.setToast("This meeting has no online join link", true)
		return nil
	}
	m.setToast("Joining “"+e.Subject+"” in Teams…", false)
	return m.openLink(e.JoinURL)
}

func (m *Model) openMeetingChat(e *model.Event) tea.Cmd {
	if e == nil {
		return nil
	}
	if e.ChatID == "" {
		m.setToast("No meeting chat for this event", true)
		return nil
	}
	m.chatTitles[e.ChatID] = e.Subject
	// a meeting that is live (or about to start) opens in the meeting view
	if e.Live(time.Now()) || (e.Start.After(time.Now()) && time.Until(e.Start) <= 10*time.Minute) {
		m.liveMeeting = e.ID
		m.section = SecCalendar
		cmd := tea.Batch(m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: e.ChatID}), m.resolveAttendees(e))
		m.section = SecCalendar
		m.setFocus(FocusCompose)
		return cmd
	}
	m.section = SecChat
	cmd := m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: e.ChatID})
	m.setFocus(FocusCompose)
	return cmd
}

func (m *Model) showTranscript(e *model.Event) tea.Cmd {
	if e == nil {
		return nil
	}
	st := m.artifacts[e.ID]
	if st == nil {
		return m.loadArtifacts(e)
	}
	if st.loading {
		m.setToast("Looking up the meeting…", false)
		return nil
	}
	if len(st.a.Transcripts) == 0 {
		msg := "No transcript for this meeting"
		if st.err != "" {
			msg = st.err
		}
		m.setToast(msg, st.err != "")
		return nil
	}
	t := st.a.Transcripts[len(st.a.Transcripts)-1]
	title := e.Subject + " — transcript"
	svc := m.svc
	m.busy++
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		vtt, err := svc.TranscriptText(c, t)
		return transcriptMsg{title, vtt, err}
	}
}

func (m *Model) downloadRecording(e *model.Event) tea.Cmd {
	if e == nil {
		return nil
	}
	st := m.artifacts[e.ID]
	if st == nil {
		return m.loadArtifacts(e)
	}
	if st.loading || len(st.a.Recordings) == 0 {
		msg := "No recording for this meeting"
		if st.err != "" {
			msg = st.err
		}
		m.setToast(msg, st.err != "")
		return nil
	}
	r := st.a.Recordings[len(st.a.Recordings)-1]
	name := safeFileName(e.Subject+" "+e.Start.Format("2006-01-02")) + ".mp4"
	dest := m.downloadPath(name)
	m.setToast("Downloading recording…", false)
	return m.op(func(svc model.Service) opMsg {
		c, cancel := ctx()
		defer cancel()
		if err := svc.FetchURL(c, r.ContentURL, dest); err != nil {
			return opMsg{err: err}
		}
		return opMsg{toast: "Saved " + filepath.Base(dest), open: dest}
	})
}

func safeFileName(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "", "?", "", `"`, "", "<", "", ">", "", "|", "")
	return strings.TrimSpace(r.Replace(s))
}

func (m *Model) openTranscript(title, vtt string) {
	lines := model.ParseVTT(vtt)
	width := min(100, m.w-8) - 4
	var text []string
	var plain strings.Builder
	for _, l := range lines {
		who := l.Speaker
		if who == "" {
			who = "Unknown"
		}
		text = append(text, m.s.Faint.Render(l.Start)+"  "+m.s.nameColor(who).Render(who))
		for _, w := range strings.Split(ansi.Wrap(l.Text, width-2, " "), "\n") {
			text = append(text, "  "+w)
		}
		text = append(text, "")
		plain.WriteString("[" + l.Start + "] " + who + ": " + l.Text + "\n")
	}
	if len(lines) == 0 {
		text = []string{m.s.Muted.Render("The transcript is empty.")}
	}
	m.modal = &modal{kind: mText, title: title, text: text, width: min(100, m.w-8), raw: plain.String(), saveName: safeFileName(title) + ".txt"}
}

func (m *Model) saveTranscript(md *modal) {
	if md.raw == "" {
		return
	}
	dest := m.downloadPath(md.saveName)
	if err := os.WriteFile(dest, []byte(md.raw), 0o644); err != nil {
		m.errToast(err)
		return
	}
	m.setToast("Saved "+dest, false)
}
