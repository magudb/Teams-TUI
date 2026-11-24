package ui

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/magudb/Teams-TUI/internal/cards"
	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
)

type theme struct {
	Accent, AccentDim, Text, Muted, Faint, Border, Danger, Warn, OK, SelBg, SelFg, MineBar lipgloss.TerminalColor
	BubbleMine, BubbleOther, BubbleUrgent                                                  lipgloss.TerminalColor
	Names                                                                                  []lipgloss.TerminalColor
}

func newTheme(light bool) theme {
	if light {
		return theme{
			Accent: lipgloss.Color("#5B5FC7"), AccentDim: lipgloss.Color("#C5CBFA"), Text: lipgloss.Color("#242424"),
			Muted: lipgloss.Color("#616161"), Faint: lipgloss.Color("#9E9E9E"), Border: lipgloss.Color("#D1D1D1"),
			Danger: lipgloss.Color("#C4314B"), Warn: lipgloss.Color("#C19C00"), OK: lipgloss.Color("#13A10E"),
			SelBg: lipgloss.Color("#E8EBFA"), SelFg: lipgloss.Color("#242424"), MineBar: lipgloss.Color("#5B5FC7"),
			BubbleMine: lipgloss.Color("#E8EBFA"), BubbleOther: lipgloss.Color("#F0F0F0"), BubbleUrgent: lipgloss.Color("#FDE7E9"),
			Names: []lipgloss.TerminalColor{lipgloss.Color("#B4009E"), lipgloss.Color("#0063B1"), lipgloss.Color("#CA5010"), lipgloss.Color("#498205"), lipgloss.Color("#8764B8"), lipgloss.Color("#038387"), lipgloss.Color("#986F0B")},
		}
	}
	return theme{
		Accent: lipgloss.Color("#7F85F5"), AccentDim: lipgloss.Color("#3D3E78"), Text: lipgloss.Color("#E6E6E6"),
		Muted: lipgloss.Color("#ADADAD"), Faint: lipgloss.Color("#707070"), Border: lipgloss.Color("#3D3D3D"),
		Danger: lipgloss.Color("#F1707B"), Warn: lipgloss.Color("#F8D22A"), OK: lipgloss.Color("#92C353"),
		SelBg: lipgloss.Color("#2E2F5C"), SelFg: lipgloss.Color("#FFFFFF"), MineBar: lipgloss.Color("#7F85F5"),
		BubbleMine: lipgloss.Color("#2F3056"), BubbleOther: lipgloss.Color("#2B2B2B"), BubbleUrgent: lipgloss.Color("#3E2326"),
		Names: []lipgloss.TerminalColor{lipgloss.Color("#E8A2E0"), lipgloss.Color("#8AC5FF"), lipgloss.Color("#FFB27A"), lipgloss.Color("#A8D98A"), lipgloss.Color("#C3B1F2"), lipgloss.Color("#6FD6D9"), lipgloss.Color("#E6CC7A")},
	}
}

type styles struct {
	t         theme
	Text      lipgloss.Style
	Muted     lipgloss.Style
	Faint     lipgloss.Style
	Bold      lipgloss.Style
	Accent    lipgloss.Style
	Title     lipgloss.Style
	Danger    lipgloss.Style
	Warn      lipgloss.Style
	OK        lipgloss.Style
	Sel       lipgloss.Style
	SelDim    lipgloss.Style
	Border    lipgloss.Style
	Button    lipgloss.Style
	ButtonHot lipgloss.Style
	Chip      lipgloss.Style
	ChipMine  lipgloss.Style
	Badge     lipgloss.Style
	Tab       lipgloss.Style
	TabActive lipgloss.Style
	Modal     lipgloss.Style
	Status    lipgloss.Style
	html      htmltext.Styles
	cards     cards.Styles
}

func newStyles(light bool) styles {
	t := newTheme(light)
	s := styles{t: t}
	s.Text = lipgloss.NewStyle().Foreground(t.Text)
	s.Muted = lipgloss.NewStyle().Foreground(t.Muted)
	s.Faint = lipgloss.NewStyle().Foreground(t.Faint)
	s.Bold = lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	s.Accent = lipgloss.NewStyle().Foreground(t.Accent)
	s.Title = lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	s.Danger = lipgloss.NewStyle().Foreground(t.Danger)
	s.Warn = lipgloss.NewStyle().Foreground(t.Warn)
	s.OK = lipgloss.NewStyle().Foreground(t.OK)
	s.Sel = lipgloss.NewStyle().Background(t.SelBg).Foreground(t.SelFg)
	s.SelDim = lipgloss.NewStyle().Background(t.AccentDim).Foreground(t.SelFg)
	s.Border = lipgloss.NewStyle().Foreground(t.Border)
	s.Button = lipgloss.NewStyle().Foreground(t.Accent)
	s.ButtonHot = lipgloss.NewStyle().Foreground(t.SelFg).Background(t.Accent).Bold(true)
	s.Chip = lipgloss.NewStyle().Foreground(t.Text).Background(t.Border)
	s.ChipMine = lipgloss.NewStyle().Foreground(t.SelFg).Background(t.AccentDim)
	s.Badge = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(t.Danger).Bold(true)
	s.Tab = lipgloss.NewStyle().Foreground(t.Muted)
	s.TabActive = lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Underline(true)
	s.Modal = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Accent).Padding(0, 1)
	s.Status = lipgloss.NewStyle().Foreground(t.Muted)
	s.html = htmltext.Styles{
		Text:      s.Text,
		Link:      lipgloss.NewStyle().Foreground(t.Accent).Underline(true),
		Mention:   lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		MentionMe: lipgloss.NewStyle().Foreground(t.Warn).Bold(true),
		Code:      lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A31515", Dark: "#CE9178"}),
		Quote:     s.Muted,
		Dim:       s.Faint,
	}
	s.cards = cards.Styles{
		Text: s.Text, Bold: s.Bold, Subtle: s.Muted, Heading: s.Title, Link: s.html.Link, Mention: s.html.Mention,
		Accent: s.Accent, Good: s.OK, Warning: s.Warn, Attention: s.Danger,
		Button:         lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		ButtonPositive: lipgloss.NewStyle().Foreground(t.OK).Bold(true),
		ButtonDanger:   lipgloss.NewStyle().Foreground(t.Danger).Bold(true),
		ButtonDead:     lipgloss.NewStyle().Foreground(t.Muted),
		Border:         s.Border, Input: s.Text,
	}
	return s
}

func (s styles) nameColor(id string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return lipgloss.NewStyle().Foreground(s.t.Names[int(h.Sum32())%len(s.t.Names)]).Bold(true)
}

func (s styles) presenceDot(p model.Presence, known bool) string {
	if !known {
		return s.Faint.Render("◌")
	}
	switch p.Availability {
	case model.Available, "AvailableIdle":
		return s.OK.Render("●")
	case model.Busy, "BusyIdle":
		return s.Danger.Render("●")
	case model.DoNotDisturb:
		return s.Danger.Render("⊖")
	case model.Away, model.BeRightBack:
		return s.Warn.Render("◷")
	case model.Offline:
		return s.Faint.Render("○")
	}
	return s.Faint.Render("◌")
}

func presenceLabel(p model.Presence) string {
	switch p.Availability {
	case model.Available, "AvailableIdle":
		return "Available"
	case model.Busy, "BusyIdle":
		switch p.Activity {
		case "InAMeeting":
			return "In a meeting"
		case "InACall", "InAConferenceCall":
			return "In a call"
		case "Presenting":
			return "Presenting"
		}
		return "Busy"
	case model.DoNotDisturb:
		if p.Activity == "Presenting" {
			return "Presenting"
		}
		return "Do not disturb"
	case model.BeRightBack:
		return "Be right back"
	case model.Away:
		return "Away"
	case model.Offline:
		return "Offline"
	}
	return "Unknown"
}

// ---- small helpers ------------------------------------------------------------

func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// padRight pads/truncates s to exactly w cells.
func padRight(s string, w int) string {
	s = trunc(s, w)
	if d := w - ansi.StringWidth(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// spread places left and right text on one line of width w.
func spread(left, right string, w int) string {
	rw := ansi.StringWidth(right)
	if rw >= w {
		return trunc(right, w)
	}
	left = trunc(left, w-rw-1)
	gap := w - ansi.StringWidth(left) - rw
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// box pads/truncates a list of lines into exactly w x h.
func box(lines []string, w, h int) string {
	out := make([]string, h)
	for i := 0; i < h; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = padRight(l, w)
	}
	return strings.Join(out, "\n")
}

func plainOneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func relTime(t time.Time, tf string) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return t.Format(tf)
	case now.Sub(t) < 6*24*time.Hour:
		return t.Format("Mon")
	case y1 == y2:
		return t.Format("02/01")
	}
	return t.Format("02/01/06")
}

func dayLabel(t time.Time) string {
	now := time.Now()
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch diff := int(today.Sub(d).Hours() / 24); {
	case diff == 0:
		return "Today"
	case diff == 1:
		return "Yesterday"
	case diff == -1:
		return "Tomorrow"
	case diff > 0 && diff < 7:
		return t.Format("Monday")
	}
	return t.Format("Monday, 2 January 2006")
}

func sameDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func humanSize(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return itoa(int(n)) + " B"
	case n < k*k:
		return itoa(int(n/k)) + " KB"
	case n < k*k*k:
		return ftoa(float64(n)/(k*k)) + " MB"
	}
	return ftoa(float64(n)/(k*k*k)) + " GB"
}

func itoa(i int) string { return strconv.Itoa(i) }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

func initials(name string) string {
	var parts []string
	for _, f := range strings.Fields(name) {
		r := []rune(f)
		if unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]) {
			parts = append(parts, f)
		}
	}
	switch len(parts) {
	case 0:
		return "?"
	case 1:
		return strings.ToUpper(string([]rune(parts[0])[0]))
	}
	return strings.ToUpper(string([]rune(parts[0])[0]) + string([]rune(parts[len(parts)-1])[0]))
}

// overlapsDay reports whether an event touches the given day (meetings that
// run past midnight show on both days).
func overlapsDay(e model.Event, day time.Time) bool {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	end := start.AddDate(0, 0, 1)
	if e.End.Equal(e.Start) {
		return !e.Start.Before(start) && e.Start.Before(end)
	}
	return e.Start.Before(end) && e.End.After(start)
}
