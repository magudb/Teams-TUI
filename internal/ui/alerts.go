package ui

import (
	"context"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/model"
	"github.com/magudb/Teams-TUI/internal/notify"
)

// Alerts are dialogs for things that need an answer now: an incoming call, a
// meeting that someone has started, and a meeting that is about to start.
// Graph has no ringing signal, so calls are detected from the "call started"
// event Teams posts to the chat, which the chat list poll picks up. Answering
// and joining are handed off to the Teams client.

type alertKind int

const (
	alertCall alertKind = iota
	alertMeetingStarted
	alertMeetingSoon
	alertMissed
)

type alertAction struct {
	key   string
	label string
	run   func(m *Model) tea.Cmd
}

type alert struct {
	id      string
	kind    alertKind
	head    string
	title   string
	detail  string
	expires time.Time
	callID  string
	eventID string
	actions []alertAction // the first one is the primary action
	sel     int
	cancel  context.CancelFunc // closes the desktop notification
}

type alertDesktopMsg struct {
	id     string
	chosen bool
}

type agendaMsg struct {
	events []model.Event
	err    error
}

const (
	callRingFor     = 60 * time.Second // a call older than this is no longer ringing
	meetingStartFor = 5 * time.Minute
	typingPause     = 3 * time.Second
)

// loadAgenda fetches the coming day's events for reminders, so they don't
// depend on which week the calendar is showing.
func (m *Model) loadAgenda() tea.Cmd {
	svc := m.svc
	now := time.Now()
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ev, err := svc.Events(c, now.Add(-12*time.Hour), now.Add(24*time.Hour))
		return agendaMsg{ev, err}
	}
}

// checkUpcoming raises a reminder for meetings about to start.
func (m *Model) checkUpcoming() tea.Cmd {
	now := time.Now()
	src := m.events
	if m.agendaLoaded {
		src = m.agenda
	}
	window := time.Duration(m.cfg.MeetingReminderMinutes) * time.Minute
	var cmds []tea.Cmd
	for i := range src {
		e := src[i]
		if e.IsCancelled || e.IsAllDay || e.Response == "declined" {
			continue
		}
		d := e.Start.Sub(now)
		due := !m.reminded[e.ID] && d > 0 && d <= window
		again := false
		if t, ok := m.remindAt[e.ID]; ok && !now.Before(t) {
			delete(m.remindAt, e.ID)
			again = !m.joined[e.ID]
		}
		if !due && !again {
			continue
		}
		if due {
			m.reminded[e.ID] = true
			prev := "Starts " + e.Start.Format(m.cfg.TimeFormat)
			if n := len(e.Attendees); n > 0 {
				prev += " · " + itoa(n) + " attendees"
			}
			m.addActivity(activityItem{at: now, kind: "meeting", title: e.Subject, preview: prev, msgID: e.ID})
		}
		cmds = append(cmds, m.raiseAlert(m.meetingAlert(e, again)))
	}
	return tea.Batch(cmds...)
}

func (m *Model) meetingAlert(e model.Event, again bool) *alert {
	a := &alert{
		id: "meet:" + e.ID, kind: alertMeetingSoon, head: "Meeting starting soon", eventID: e.ID,
		title: e.Subject, expires: e.End,
	}
	if again {
		a.id += ":start"
		a.head = "Meeting starting now"
	}
	a.detail = e.Start.Format(m.cfg.TimeFormat) + "–" + e.End.Format(m.cfg.TimeFormat)
	if e.Organizer != "" {
		a.detail += " · " + e.Organizer
	}
	if e.Location != "" {
		a.detail += " · " + e.Location
	}
	a.actions = m.meetingActions(e.ID, e.ChatID, e.JoinURL, e.Subject)
	if !again && time.Until(e.Start) > time.Minute {
		id, at := e.ID, e.Start
		a.actions = append(a.actions, alertAction{"s", "Remind at start", func(m *Model) tea.Cmd {
			m.remindAt[id] = at
			return nil
		}})
	}
	return a
}

// meetingActions are Join and Meeting chat, as far as the meeting has them.
func (m *Model) meetingActions(eventID, chatID, joinURL, title string) []alertAction {
	var out []alertAction
	if joinURL != "" {
		out = append(out, alertAction{"j", "Join in Teams", func(m *Model) tea.Cmd {
			m.joined[eventID], m.joined[chatID] = true, true
			m.setToast("Joining “"+title+"” in Teams…", false)
			return m.openLink(joinURL)
		}})
	}
	if chatID != "" {
		out = append(out, alertAction{"c", "Meeting chat", func(m *Model) tea.Cmd {
			if e := m.eventByID(eventID); e != nil {
				return m.openMeetingChat(e)
			}
			return m.alertOpenChat(chatID, title)
		}})
	}
	return out
}

func (m *Model) eventByID(id string) *model.Event {
	for i := range m.events {
		if m.events[i].ID == id && id != "" {
			return &m.events[i]
		}
	}
	return nil
}

func (m *Model) alertOpenChat(chatID, title string) tea.Cmd {
	if m.chatByID(chatID) == nil && title != "" {
		m.chatTitles[chatID] = title
	}
	if m.liveMeeting != "" {
		m.leaveMeetingView()
	}
	m.section = SecChat
	m.sideSel[SecChat] = "chat:" + chatID
	cmd := m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: chatID})
	m.setFocus(FocusCompose)
	return cmd
}

// inCall reports whether my presence says I'm already on a call.
func (m *Model) inCall() bool {
	switch m.myPresence.Activity {
	case "InACall", "InAConferenceCall", "Presenting":
		return true
	}
	return false
}

// noteCall looks at a chat's newest message for a call that started or ended.
func (m *Model) noteCall(c model.Chat, first bool) tea.Cmd {
	p := c.LastMessage
	if p == nil || p.Call == nil || p.Call.Type == "screenShare" {
		return nil
	}
	ev := p.Call
	callID := ev.CallID
	if callID == "" {
		callID = p.ID
	}
	id := "call:" + callID
	meeting := ev.Type == "meeting" || c.ChatType == "meeting" || c.JoinURL != ""
	mine := m.me.ID != "" && ev.Initiator.ID == m.me.ID
	if !ev.Started {
		return m.callEnded(c, p, callID, meeting || mine, first)
	}
	if m.alerted[id] {
		return nil
	}
	maxAge := callRingFor
	if meeting {
		maxAge = meetingStartFor
	}
	if mine || time.Since(p.Created) > maxAge || m.inCall() || m.joined[c.ID] || (meeting && m.st.Muted["chat:"+c.ID]) {
		m.alerted[id] = true
		return nil
	}
	who := ev.Initiator.DisplayName
	if who == "" && ev.Initiator.ID != "" {
		who = m.userName(ev.Initiator.ID)
	}
	title := c.Title(m.me.ID)
	if meeting {
		return m.meetingStarted(c, id, callID, who, title)
	}
	if who == "" {
		who = title
	}
	a := &alert{id: id, kind: alertCall, head: "Incoming call", callID: callID, title: who, expires: p.Created.Add(callRingFor + 30*time.Second)}
	a.detail = "is calling you"
	if c.ChatType != "oneOnOne" {
		a.detail = "started a call in " + title
	}
	link := c.WebURL
	if link == "" {
		link = "https://teams.microsoft.com/l/chat/" + url.PathEscape(c.ID) + "/0?tenantId=" + m.svc.TenantID()
	}
	chatID := c.ID
	a.actions = []alertAction{
		{"a", "Answer in Teams", func(m *Model) tea.Cmd {
			m.joined[chatID] = true
			m.setToast("Opening the call in Teams…", false)
			return m.openLink(link)
		}},
		{"m", "Reply with a message", func(m *Model) tea.Cmd { return m.alertOpenChat(chatID, title) }},
	}
	m.addActivity(activityItem{at: p.Created, kind: "call", title: firstName(who) + " called", preview: title,
		ref: model.ConvRef{Kind: model.KindChat, ChatID: c.ID}, msgID: p.ID})
	return m.raiseAlert(a)
}

// meetingStarted alerts that someone has started the meeting of a chat. A
// reminder for the same meeting that is still waiting is updated instead.
func (m *Model) meetingStarted(c model.Chat, id, callID, who, title string) tea.Cmd {
	detail := "The meeting has started"
	if who != "" {
		detail = who + " started the meeting"
	}
	eventID, joinURL := "", c.JoinURL
	src := m.events
	if m.agendaLoaded {
		src = m.agenda
	}
	now := time.Now()
	for _, e := range src {
		if e.ChatID != c.ID || e.IsCancelled || now.After(e.End) || e.Start.Sub(now) > time.Hour {
			continue
		}
		if e.Response == "declined" || m.joined[e.ID] {
			m.alerted[id] = true
			return nil
		}
		eventID, title = e.ID, e.Subject
		if joinURL == "" {
			joinURL = e.JoinURL
		}
		break
	}
	if eventID != "" {
		m.reminded[eventID] = true
		delete(m.remindAt, eventID)
		for _, a := range m.alerts {
			if a.eventID == eventID {
				m.alerted[id] = true
				a.kind, a.head, a.detail, a.callID = alertMeetingStarted, "Meeting started", detail, callID
				a.actions = m.meetingActions(eventID, c.ID, joinURL, title)
				return nil
			}
		}
	}
	a := &alert{id: id, kind: alertMeetingStarted, head: "Meeting started", callID: callID, eventID: eventID,
		title: title, detail: detail, expires: now.Add(15 * time.Minute)}
	a.actions = m.meetingActions(eventID, c.ID, joinURL, title)
	return m.raiseAlert(a)
}

// callEnded clears the alert of a call that is over, and reports a 1:1 call
// that I didn't take part in as missed.
func (m *Model) callEnded(c model.Chat, p *model.MessagePreview, callID string, notMissable, first bool) tea.Cmd {
	ringing := false
	for _, a := range m.alerts {
		if a.callID == callID {
			ringing = ringing || a.kind == alertCall
			m.removeAlert(a)
		}
	}
	m.alerted["call:"+callID] = true
	ev := p.Call
	if first || notMissable || c.ChatType != "oneOnOne" || ev.Initiator.ID == "" || m.joined[c.ID] {
		return nil
	}
	if slices.Contains(ev.Participants, m.me.ID) || (len(ev.Participants) == 0 && !ringing && !m.dismissedCalls[callID]) {
		return nil
	}
	if time.Since(p.Created) > 10*time.Minute || m.alerted["missed:"+callID] {
		return nil
	}
	who := ev.Initiator.DisplayName
	if who == "" {
		who = c.Title(m.me.ID)
	}
	m.addActivity(activityItem{at: p.Created, kind: "call", title: "Missed call from " + firstName(who), preview: c.Title(m.me.ID),
		ref: model.ConvRef{Kind: model.KindChat, ChatID: c.ID}, msgID: p.ID})
	if m.dismissedCalls[callID] {
		m.alerted["missed:"+callID] = true
		return nil
	}
	chat, chatID, title := c, c.ID, c.Title(m.me.ID)
	a := &alert{id: "missed:" + callID, kind: alertMissed, head: "Missed call", title: who,
		detail: "called at " + p.Created.Local().Format(m.cfg.TimeFormat), expires: time.Now().Add(10 * time.Minute)}
	if link := m.callLinkFor(&chat, false); link != "" {
		a.actions = append(a.actions, alertAction{"c", "Call back in Teams", func(m *Model) tea.Cmd { return m.openLink(link) }})
	}
	a.actions = append(a.actions, alertAction{"m", "Send a message", func(m *Model) tea.Cmd { return m.alertOpenChat(chatID, title) }})
	return m.raiseAlert(a)
}

// ---- queue ---------------------------------------------------------------------

// typing reports whether a dialog would get in the way of what the user is
// doing right now.
func (m *Model) typing() bool {
	if m.modal != nil {
		return true
	}
	input := m.focus == FocusCompose || m.filtering || m.searchInput.Focused()
	return input && time.Since(m.lastKey) < typingPause
}

func (m *Model) alertVisible() bool { return m.alertOpen && len(m.alerts) > 0 }

func (m *Model) showAlert() {
	m.alertOpen = true
	m.alertShown = time.Time{}
}

func (m *Model) raiseAlert(a *alert) tea.Cmd {
	if m.alerted[a.id] {
		return nil
	}
	m.alerted[a.id] = true
	if m.cfg.Bell {
		_, _ = os.Stdout.WriteString("\a")
	}
	if !m.cfg.Alerts {
		if m.cfg.DesktopNotifications && !m.demo {
			notify.Desktop(a.head, a.title+" · "+a.detail)
		}
		hint := ""
		if a.kind != alertCall && a.kind != alertMissed && len(a.actions) > 0 && a.actions[0].key == "j" {
			hint = " — alt+j to join"
		}
		m.setToast(m.alertIcon(a)+" "+a.head+": "+a.title+hint, false)
		return nil
	}
	a.actions = append(a.actions, alertAction{"esc", "Dismiss", nil})
	if a.kind == alertCall {
		m.alerts = append([]*alert{a}, m.alerts...) // a ringing call goes first
	} else {
		m.alerts = append(m.alerts, a)
	}
	if !m.alertOpen && !m.typing() {
		m.alertOpen = true
		m.alertShown = time.Now()
	}
	return m.alertDesktop(a)
}

// alertDesktop shows the alert as a desktop notification whose button runs
// the primary action, for when the terminal isn't in view.
func (m *Model) alertDesktop(a *alert) tea.Cmd {
	if !m.cfg.DesktopNotifications || m.demo {
		return nil
	}
	d := min(time.Until(a.expires), 2*time.Minute)
	if d <= 0 {
		return nil
	}
	if a.actions[0].run == nil {
		notify.Desktop(a.head, a.title+" · "+a.detail)
		return nil
	}
	c, cancel := context.WithTimeout(m.transferCtx, d)
	a.cancel = cancel
	id, title, body, label, urgent := a.id, a.head, a.title+" · "+a.detail, a.actions[0].label, a.kind == alertCall
	return func() tea.Msg {
		defer cancel()
		return alertDesktopMsg{id, notify.Ask(c, title, body, label, urgent, d)}
	}
}

func (m *Model) alertFromDesktop(msg alertDesktopMsg) tea.Cmd {
	if !msg.chosen {
		return nil
	}
	for _, a := range m.alerts {
		if a.id == msg.id {
			return m.runAlert(a, 0)
		}
	}
	return nil
}

func (m *Model) removeAlert(a *alert) {
	if a.cancel != nil {
		a.cancel()
	}
	m.alerts = slices.DeleteFunc(m.alerts, func(x *alert) bool { return x == a })
	if len(m.alerts) == 0 {
		m.alertOpen = false
	}
}

func (m *Model) runAlert(a *alert, i int) tea.Cmd {
	m.removeAlert(a)
	if i < 0 || i >= len(a.actions) || a.actions[i].run == nil {
		if a.kind == alertCall {
			m.dismissedCalls[a.callID] = true
		}
		return nil
	}
	return a.actions[i].run(m)
}

// alertTick drops alerts that have run out and shows waiting ones once the
// user has stopped typing.
func (m *Model) alertTick(now time.Time) tea.Cmd {
	for _, a := range slices.Clone(m.alerts) {
		if now.After(a.expires) {
			m.removeAlert(a)
		}
	}
	if len(m.alerts) > 0 && !m.alertOpen && !m.typing() {
		m.alertOpen = true
		m.alertShown = now
	}
	return nil
}

// ---- input ---------------------------------------------------------------------

func (m *Model) alertKey(msg tea.KeyMsg) tea.Cmd {
	a := m.alerts[0]
	k := msg.String()
	switch k {
	case "left", "up", "shift+tab":
		a.sel = (a.sel + len(a.actions) - 1) % len(a.actions)
		return nil
	case "right", "down", "tab":
		a.sel = (a.sel + 1) % len(a.actions)
		return nil
	}
	// keys typed just as the dialog popped up were meant for something else
	if time.Since(m.alertShown) < m.alertGuard {
		return nil
	}
	if k == "enter" {
		return m.runAlert(a, a.sel)
	}
	for i, act := range a.actions {
		if k == act.key {
			return m.runAlert(a, i)
		}
	}
	return nil
}

func (m *Model) alertMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	a := m.alerts[0]
	for i := range a.actions {
		if zone.Get("al:" + itoa(i)).InBounds(msg) {
			return m.runAlert(a, i)
		}
	}
	if !zone.Get("alert").InBounds(msg) {
		m.alertOpen = false // keeps waiting in the status bar
		m.lastKey = time.Now()
	}
	return nil
}

// ---- view ----------------------------------------------------------------------

func (m *Model) alertIcon(a *alert) string {
	ic := m.s.ic
	switch a.kind {
	case alertCall, alertMissed:
		return ic.Phone
	case alertMeetingStarted:
		return ic.Video
	}
	return ic.Calendar
}

func (m *Model) alertView() string {
	a := m.alerts[0]
	s := m.s
	w := min(60, m.w-4)
	inner := w - 2
	bar := s.Inverse
	if a.kind == alertCall || a.kind == alertMeetingStarted {
		bar = lipgloss.NewStyle().Background(s.t.Accent).Foreground(s.t.OnAccent)
	}
	right := ""
	if n := len(m.alerts) - 1; n > 0 {
		right = "+" + itoa(n) + " more"
	}
	out := []string{headerBar(bar, bar.Bold(true).Render(m.alertIcon(a)+" "+strings.ToUpper(a.head)), right, w)}
	lines := []string{"", s.Bold.Render(trunc(a.title, inner-1))}
	for _, l := range strings.Split(ansi.Wrap(a.detail, inner-1, " "), "\n") {
		lines = append(lines, s.Muted.Render(l))
	}
	lines = append(lines, "")
	row, rowW := "", 0
	for i, act := range a.actions {
		st := s.Button
		if i == a.sel {
			st = s.ButtonHot
		}
		b := zone.Mark("al:"+itoa(i), st.Render(" "+act.key+" "+act.label+" "))
		bw := ansi.StringWidth(b)
		if rowW > 0 && rowW+2+bw > inner-1 {
			lines = append(lines, row, "")
			row, rowW = "", 0
		}
		if rowW > 0 {
			row += "  "
			rowW += 2
		}
		row += b
		rowW += bw
	}
	lines = append(lines, row, "", s.Muted.Render("←→ choose · enter select · esc dismiss"))
	for _, l := range lines {
		out = append(out, withBg(" "+l, s.bgCard, w))
	}
	return zone.Mark("alert", strings.Join(out, "\n"))
}
