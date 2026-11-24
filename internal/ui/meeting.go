package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
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

// ---- rendering --------------------------------------------------------------------

func responseIcon(s styles, r string) string {
	switch r {
	case "accepted":
		return s.OK.Render("✓")
	case "tentativelyAccepted":
		return s.Warn.Render("~")
	case "declined":
		return s.Danger.Render("✗")
	case "organizer":
		return s.Accent.Render("★")
	}
	return s.Faint.Render("?")
}

func (m *Model) eventDetailLines(e *model.Event, w int) []string {
	s := m.s
	now := time.Now()
	var lines []string
	title := " " + s.Title.Render(trunc(e.Subject, w-20))
	switch {
	case e.IsCancelled:
		title += "  " + s.Danger.Render("Cancelled")
	case e.Live(now) && e.IsOnline:
		title += "  " + s.Badge.Render(" ● LIVE ")
	case e.Start.After(now) && e.Start.Sub(now) <= 15*time.Minute:
		title += "  " + s.Warn.Render("starts "+untilLabel(e))
	}
	lines = append(lines, "", title)
	when := e.Start.Format("Monday 2 January · ") + e.Start.Format(m.cfg.TimeFormat) + "–" + e.End.Format(m.cfg.TimeFormat)
	if e.IsAllDay {
		when = e.Start.Format("Monday 2 January") + " · all day"
	}
	lines = append(lines, " "+s.Muted.Render(when))
	if e.Location != "" {
		lines = append(lines, " "+s.Muted.Render("📍 "+e.Location))
	}
	if e.Organizer != "" {
		lines = append(lines, " "+s.Muted.Render("Organizer: "+e.Organizer))
	}
	resp := map[string]string{"accepted": "✓ You accepted", "tentativelyAccepted": "~ You said maybe", "declined": "✗ You declined", "organizer": "★ You're the organizer", "notResponded": "? You haven't responded", "none": "? You haven't responded"}[e.Response]
	if resp != "" {
		lines = append(lines, " "+s.Accent.Render(resp))
	}
	lines = append(lines, "")

	btn := func(id, label string, hot bool) string {
		st := s.Button
		if hot {
			st = s.ButtonHot
		}
		return zone.Mark("ev:"+id, st.Render(label))
	}
	var bs []string
	if e.JoinURL != "" && !e.IsCancelled {
		hot := e.Live(now) || (e.Start.After(now) && e.Start.Sub(now) <= 15*time.Minute)
		bs = append(bs, btn("join", " 📹 Join ", hot))
	}
	if e.ChatID != "" {
		bs = append(bs, btn("chat", "[💬 Chat]", false))
	}
	if e.Response != "organizer" && !e.IsCancelled && now.Before(e.End) {
		bs = append(bs, btn("accept", "[✓ Accept]", false), btn("tentative", "[~ Maybe]", false), btn("decline", "[✗ Decline]", false))
	}
	if e.WebLink != "" {
		bs = append(bs, btn("web", "[↗ Outlook]", false))
	}
	lines = append(lines, wrapButtons(bs, w-2)...)

	// post-meeting recap
	if e.JoinURL != "" && now.After(e.End) {
		lines = append(lines, "", " "+s.Bold.Render("Recap"))
		st := m.artifacts[e.ID]
		switch {
		case st == nil || st.loading:
			lines = append(lines, "  "+s.Faint.Render(m.spinner.View()+" Looking for transcript and recording…"))
		case st.err != "" && len(st.a.Transcripts) == 0 && len(st.a.Recordings) == 0:
			for _, l := range strings.Split(ansi.Wrap(st.err, w-4, " "), "\n") {
				lines = append(lines, "  "+s.Faint.Render(l))
			}
		default:
			var rb []string
			if n := len(st.a.Transcripts); n > 0 {
				rb = append(rb, btn("transcript", "[📝 Transcript]", false))
			}
			if n := len(st.a.Recordings); n > 0 {
				rb = append(rb, btn("recording", "[⏺ Download recording]", false))
			}
			if len(rb) == 0 {
				lines = append(lines, "  "+s.Faint.Render("No transcript or recording was made."))
			} else {
				lines = append(lines, wrapButtons(rb, w-2)...)
			}
		}
	}

	if len(e.Attendees) > 0 {
		counts := map[string]int{}
		for _, a := range e.Attendees {
			counts[a.Response]++
		}
		summary := itoa(counts["accepted"]) + " accepted"
		if n := counts["tentativelyAccepted"]; n > 0 {
			summary += " · " + itoa(n) + " maybe"
		}
		if n := counts["declined"]; n > 0 {
			summary += " · " + itoa(n) + " declined"
		}
		if n := counts["none"] + counts["notResponded"]; n > 0 {
			summary += " · " + itoa(n) + " no response"
		}
		lines = append(lines, "", " "+s.Bold.Render("Attendees ("+itoa(len(e.Attendees))+")")+"  "+s.Faint.Render(summary))
		for i, a := range e.Attendees {
			if i >= 20 {
				lines = append(lines, "   "+s.Faint.Render("+"+itoa(len(e.Attendees)-20)+" more"))
				break
			}
			dot := s.Faint.Render("◌")
			status := ""
			if u, ok := m.emailUsers[strings.ToLower(a.Email)]; ok {
				p, known := m.presence[u.ID]
				dot = s.presenceDot(p, known)
				if known {
					status = presenceLabel(p)
				}
			}
			name := a.Name
			if a.Type == "optional" {
				name += s.Faint.Render(" (optional)")
			}
			l := "  " + dot + " " + responseIcon(s, a.Response) + " " + name
			lines = append(lines, zone.Mark("evatt:"+itoa(i), spread(l, s.Faint.Render(status)+" ", w)))
		}
	}
	if e.Preview != "" {
		lines = append(lines, "")
		for _, l := range strings.Split(ansi.Wrap(e.Preview, w-4, " "), "\n") {
			lines = append(lines, "  "+s.Muted.Render(l))
		}
	}
	lines = append(lines, "", s.Faint.Render(" j join · c chat · a/T/D respond · x transcript · R recording · o Outlook"))
	return lines
}

func wrapButtons(bs []string, w int) []string {
	var lines []string
	cur := ""
	for _, b := range bs {
		if cur != "" && ansi.StringWidth(cur)+1+ansi.StringWidth(b) > w {
			lines = append(lines, " "+cur)
			cur = ""
		}
		if cur != "" {
			cur += " "
		}
		cur += b
	}
	if cur != "" {
		lines = append(lines, " "+cur)
	}
	return lines
}

// meetingBanner is shown in the status bar when a meeting is live or about to start.
func (m *Model) meetingBanner() string {
	e := m.upcomingMeeting()
	if e == nil {
		return ""
	}
	st := m.s.Warn
	if e.Live(time.Now()) {
		st = m.s.OK
	}
	return st.Render("📅 "+trunc(e.Subject, 28)+" · "+untilLabel(e)) + " " + zone.Mark("status:join", m.s.ButtonHot.Render(" Join "))
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
