package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/model"
)

func responseIcon(s styles, r string) string {
	switch r {
	case "accepted":
		return s.OK.Render(s.ic.Check)
	case "tentativelyAccepted":
		return s.Warn.Render("~")
	case "declined":
		return s.Danger.Render("✗")
	case "organizer":
		return s.Danger.Render("★")
	}
	return s.Muted.Render("?")
}

// calendarView is the main pane of the Calendar: week strip and the
// selected meeting's details.
func (m *Model) calendarView(w, h int) []string {
	s := m.s
	bar := lipgloss.NewStyle().Background(s.t.Block).Foreground(s.t.Text)
	lines := []string{withBg(spread(" "+s.Title.Render(strings.ToUpper(m.calDay.Format("January 2006"))),
		zone.Mark("hdr:newmeeting", s.Button.Render("n new meeting"))+" ", w), styleOn(bar), w), ""}

	// week strip
	monday := m.calDay.AddDate(0, 0, -((int(m.calDay.Weekday()) + 6) % 7))
	var cells []string
	cw := max(6, min(11, (w-4)/7-1))
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i)
		n := 0
		for _, e := range m.events {
			if overlapsDay(e, d) && !e.IsCancelled {
				n++
			}
		}
		label := d.Format("Mon 02")
		if cw < 10 {
			label = d.Format("Mon")
		}
		if n > 0 && cw >= 10 {
			label += " ·" + itoa(n)
		}
		st := lipgloss.NewStyle().Background(s.t.Side).Foreground(s.t.Text)
		if sameDay(d, m.calDay) {
			st = s.Sel
		} else if sameDay(d, time.Now()) {
			st = st.Foreground(s.t.Link).Bold(true)
		}
		cells = append(cells, zone.Mark("day:"+itoa(i), st.Render(padRight(" "+label, cw))))
	}
	lines = append(lines, "  "+strings.Join(cells, " "), "")

	e := m.selectedEvent()
	if e == nil {
		return append(lines, "  "+s.Muted.Render("Select a meeting in the list to see details."))
	}
	return append(lines, m.eventDetailLines(e, w)...)
}

func (m *Model) eventDetailLines(e *model.Event, w int) []string {
	s := m.s
	ic := s.ic
	now := time.Now()
	in := w - 4
	pad := "  "
	var lines []string
	title := s.Title.Render(trunc(e.Subject, in-20))
	switch {
	case e.IsCancelled:
		title += " " + lipgloss.NewStyle().Background(s.t.Danger).Foreground(s.t.OnAccent).Bold(true).Render(" cancelled ")
	case e.Live(now) && e.IsOnline:
		title += " " + lipgloss.NewStyle().Background(s.t.Danger).Foreground(s.t.OnAccent).Bold(true).Render(" ● live ")
	case e.Start.After(now) && e.Start.Sub(now) <= 60*time.Minute:
		title += " " + s.Inverse.Bold(true).Render(" starts "+untilLabel(e)+" ")
	}
	lines = append(lines, pad+title)
	when := e.Start.Format("Monday 2 January · ") + e.Start.Format(m.cfg.TimeFormat) + "–" + e.End.Format(m.cfg.TimeFormat)
	if e.IsAllDay {
		when = e.Start.Format("Monday 2 January") + " · all day"
	}
	lines = append(lines, pad+s.Muted.Render(when))
	org := ""
	if e.Organizer != "" {
		org = "Organizer: " + e.Organizer
	}
	switch {
	case e.IsOnline && e.Location != "":
		org += " · online and " + e.Location
	case e.IsOnline:
		org += " · online meeting"
	case e.Location != "":
		org += " · " + e.Location
	}
	if org != "" {
		lines = append(lines, pad+s.Muted.Render(trunc(strings.TrimPrefix(org, " · "), in)))
	}
	switch e.Response {
	case "accepted":
		lines = append(lines, pad+s.OK.Bold(true).Render(ic.Check+" You accepted"))
	case "tentativelyAccepted":
		lines = append(lines, pad+s.Warn.Bold(true).Render("~ You said maybe"))
	case "declined":
		lines = append(lines, pad+s.Danger.Bold(true).Render("✗ You declined"))
	case "organizer":
		lines = append(lines, pad+s.Accent.Bold(true).Render("★ You're the organizer"))
	case "notResponded", "none":
		lines = append(lines, pad+s.Warn.Bold(true).Render("? You haven't responded"))
	}
	lines = append(lines, "")

	act := func(id, label string) string { return zone.Mark("ev:"+id, s.Button.Render(label)) }
	var bs []string
	if e.JoinURL != "" && !e.IsCancelled {
		bs = append(bs, zone.Mark("ev:join", s.ButtonHot.Render(" "+ic.Video+" Join ")))
	}
	if e.ChatID != "" {
		bs = append(bs, act("chat", ic.Chat+" Chat"))
	}
	if e.Response != "organizer" && !e.IsCancelled && now.Before(e.End) {
		bs = append(bs, act("accept", "a Accept"), act("tentative", "T Maybe"), act("decline", "D Decline"))
	}
	if e.WebLink != "" {
		bs = append(bs, act("web", "Outlook "+ic.External))
	}
	lines = append(lines, wrapButtons(bs, in, pad)...)

	if len(e.Attendees) > 0 {
		counts := map[string]int{}
		for _, a := range e.Attendees {
			counts[a.Response]++
		}
		var sum []string
		if n := counts["accepted"]; n > 0 {
			sum = append(sum, itoa(n)+" accepted")
		}
		if n := counts["tentativelyAccepted"]; n > 0 {
			sum = append(sum, itoa(n)+" maybe")
		}
		if n := counts["declined"]; n > 0 {
			sum = append(sum, itoa(n)+" declined")
		}
		if n := counts["none"] + counts["notResponded"]; n > 0 {
			sum = append(sum, itoa(n)+" no response")
		}
		lines = append(lines, "", pad+s.Bold.Render("Attendees ("+itoa(len(e.Attendees))+")")+" "+s.Muted.Render(strings.Join(sum, " · ")))
		rw := min(in, 62)
		for i, a := range e.Attendees {
			if i >= 16 {
				lines = append(lines, pad+s.Muted.Render("+"+itoa(len(e.Attendees)-16)+" more"))
				break
			}
			dot := s.Muted.Render("◌")
			status := ""
			if u, ok := m.emailUsers[strings.ToLower(a.Email)]; ok {
				p, known := m.presence[u.ID]
				dot = s.presenceDot(p, known)
				if known {
					status = presenceLabel(p)
				}
			}
			name := a.Name
			if strings.EqualFold(a.Email, m.me.Email) && m.me.Email != "" {
				name = "You"
			}
			role := ""
			switch {
			case strings.EqualFold(a.Email, e.OrganizerEmail) && e.OrganizerEmail != "":
				role = " organizer"
			case a.Type == "optional":
				role = " optional"
			}
			icon := responseIcon(s, a.Response)
			if role == " organizer" {
				icon = responseIcon(s, "organizer")
			}
			l := dot + " " + icon + " " + name + s.Muted.Render(role)
			lines = append(lines, pad+zone.Mark("evatt:"+itoa(i), spread(l, s.Muted.Render(status), rw)))
		}
	}
	if e.Preview != "" {
		lines = append(lines, "", pad+s.Bold.Render("Agenda"))
		for _, l := range strings.Split(ansi.Wrap(e.Preview, in, " "), "\n") {
			lines = append(lines, pad+l)
		}
	}
	if e.JoinURL != "" {
		lines = append(lines, "", pad+s.Bold.Render("Recap"))
		if now.Before(e.End) {
			lines = append(lines, pad+s.Muted.Render("Transcript and recording appear here after the meeting."))
		} else {
			st := m.artifacts[e.ID]
			switch {
			case st == nil || st.loading:
				lines = append(lines, pad+s.Muted.Render(m.spinner.View()+" Looking for transcript and recording…"))
			case st.err != "" && len(st.a.Transcripts) == 0 && len(st.a.Recordings) == 0:
				for _, l := range strings.Split(ansi.Wrap(st.err, in, " "), "\n") {
					lines = append(lines, pad+s.Muted.Render(l))
				}
			default:
				var rb []string
				if len(st.a.Transcripts) > 0 {
					rb = append(rb, act("transcript", "x Transcript"))
				}
				if len(st.a.Recordings) > 0 {
					rb = append(rb, act("recording", ic.Download+" R Download recording"))
				}
				if len(rb) == 0 {
					lines = append(lines, pad+s.Muted.Render("No transcript or recording was made."))
				} else {
					lines = append(lines, wrapButtons(rb, in, pad)...)
				}
			}
		}
	}
	return lines
}

func wrapButtons(bs []string, w int, pad string) []string {
	var lines []string
	cur := ""
	for _, b := range bs {
		if cur != "" && ansi.StringWidth(cur)+2+ansi.StringWidth(b) > w {
			lines = append(lines, pad+cur)
			cur = ""
		}
		if cur != "" {
			cur += "  "
		}
		cur += b
	}
	if cur != "" {
		lines = append(lines, pad+cur)
	}
	return lines
}
