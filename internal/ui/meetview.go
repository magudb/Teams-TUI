package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/model"
)

// The live meeting view: the meeting chat with a People pane. Audio and
// video stay in the Teams app (alt+j hands off).

func (m *Model) meetingEvent() *model.Event {
	if m.liveMeeting == "" {
		return nil
	}
	for i := range m.events {
		if m.events[i].ID == m.liveMeeting {
			return &m.events[i]
		}
	}
	return nil
}

// meetingMode reports whether the meeting view is showing.
func (m *Model) meetingMode() bool {
	e := m.meetingEvent()
	return e != nil && m.conv != nil && m.conv.ref.Kind == model.KindChat && m.conv.ref.ChatID == e.ChatID && m.thread == nil
}

func (m *Model) peopleWidth() int {
	if !m.showPeople {
		return 0
	}
	return min(40, m.mainWidth()/3)
}

func (m *Model) leaveMeetingView() {
	m.liveMeeting = ""
	m.invalidateRender()
}

func elapsed(since time.Time) string {
	d := time.Since(since)
	if d < 0 {
		return "0:00"
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	sec := itoa(secs)
	if secs < 10 {
		sec = "0" + sec
	}
	return itoa(mins) + ":" + sec
}

func (m *Model) meetingView(x, w, h int) string {
	s := m.s
	ic := s.ic
	e := m.meetingEvent()
	bar := lipgloss.NewStyle().Background(s.t.Block).Foreground(s.t.Text)
	live := ""
	if e.Live(time.Now()) {
		live = " " + lipgloss.NewStyle().Background(s.t.Danger).Foreground(s.t.OnAccent).Bold(true).Render(" ● LIVE "+elapsed(e.Start)+" ")
	} else {
		live = " " + s.Muted.Render(untilLabel(e))
	}
	join := zone.Mark("status:join", s.ButtonHot.Render(" "+ic.Video+" alt+j Join in Teams "))
	header := withBg(spread(" "+s.Title.Render(strings.ToUpper(trunc(e.Subject, w/2)))+live, join, w), styleOn(bar), w)
	info := s.Inverse.Render(padRight(" Audio and video run in the Teams app. This view follows the meeting chat, people and recap.", w))

	pw := m.peopleWidth()
	cw := w - pw
	cv := m.conv
	compose := m.composeLines(cv, cw, false)
	msgH := max(1, h-2-len(compose))
	m.renderConv(cv, cw, false)
	if m.focus == FocusMessages {
		cv.ensureVisible(msgH)
	}
	m.geo.msgs, m.geo.msgsConv = rect{x, 2, cw, msgH}, cv
	m.geo.compose = rect{x, 2 + msgH, cw, len(compose)}
	left := paint(padLines(cv.view(msgH), msgH), cw, s.bgPaper)
	left = append(left, paint(compose, cw, s.bgSide)...)

	lines := []string{header, info}
	if pw == 0 {
		lines = append(lines, left...)
		return strings.Join(padLines(lines, h), "\n")
	}
	right := paint(padLines(m.peopleLines(e, pw), h-2), pw, s.bgSide)
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		lines = append(lines, l+right[i])
	}
	return strings.Join(lines, "\n")
}

// peopleLines lists attendees with what their presence says they are doing.
// Graph doesn't expose who has joined a meeting, so presence is used.
func (m *Model) peopleLines(e *model.Event, w int) []string {
	s := m.s
	in := w - 4
	type row struct {
		name, role, status string
		dot                string
		inCall             bool
	}
	var rows []row
	inCall := 0
	for _, a := range e.Attendees {
		r := row{name: a.Name, dot: s.Muted.Render("◌"), status: "no response"}
		if strings.EqualFold(a.Email, m.me.Email) && m.me.Email != "" {
			r.name = "You"
		}
		if strings.EqualFold(a.Email, e.OrganizerEmail) && e.OrganizerEmail != "" {
			r.role = " organizer"
		}
		if u, ok := m.emailUsers[strings.ToLower(a.Email)]; ok {
			p, known := m.presence[u.ID]
			if known {
				r.dot = s.presenceDot(p, true)
				switch p.Activity {
				case "Presenting":
					r.status, r.inCall = "Presenting", true
				case "InACall", "InAConferenceCall":
					r.status, r.inCall = "In a call", true
				case "InAMeeting":
					r.status, r.inCall = "In a meeting", true
				default:
					r.status = "not in a call"
					if p.Availability == model.Offline {
						r.status = "Offline"
					}
				}
			}
		}
		if a.Response == "declined" {
			r.status = "declined"
		}
		if r.inCall {
			inCall++
		}
		rows = append(rows, r)
	}
	lines := []string{"", "  " + s.Bold.Render("People") + " " + s.Muted.Render(itoa(inCall)+" of "+itoa(len(rows))+" in a call")}
	for _, r := range rows {
		lines = append(lines, "  "+spread(r.dot+" "+r.name+s.Muted.Render(r.role), s.Muted.Render(r.status), in))
	}
	lines = append(lines, "", "  "+s.Bold.Render("After the meeting"),
		"  "+zone.Mark("ev:transcript", s.Button.Render("x transcript"))+s.Muted.Render(" · ")+zone.Mark("ev:recording", s.Button.Render("R recording")),
		"  "+s.Muted.Render("Available to the organizer when the"), "  "+s.Muted.Render("meeting ends."))
	return lines
}
