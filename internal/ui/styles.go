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

// theme holds the "Blocks" palette from the Terminal Chat Client design:
// light paper, panes separated by fills, inverse headers, one red accent.
type theme struct {
	Paper, Side, Block, Card  lipgloss.Color // pane fills: main, sidebar, blocks/headers, card bodies
	Ink, OnInk                lipgloss.Color // inverse bars (headers, own messages) and text on them
	Text, Muted, Faint        lipgloss.Color
	Accent, OnAccent, Link    lipgloss.Color // selection / badges, text on accent, accent-coloured text
	Good, Warn, Danger        lipgloss.Color
	RailBg, RailFg, RailBadge lipgloss.Color
	RailDot                   lipgloss.Color
	Names                     []lipgloss.Color
}

func newTheme(light bool) theme {
	if light {
		return theme{
			Paper: "#F4F1EA", Side: "#E9E5DB", Block: "#DCD7CA", Card: "#FFFDF8",
			Ink: "#23222A", OnInk: "#F4F1EA",
			Text: "#23222A", Muted: "#6B6875", Faint: "#A9A6B3",
			Accent: "#C8452D", OnAccent: "#FFFFFF", Link: "#A3371F",
			Good: "#3F7D2C", Warn: "#8A6A00", Danger: "#B3261E",
			RailBg: "#23222A", RailFg: "#A9A6B3", RailBadge: "#FF9D85", RailDot: "#9FD08A",
			Names: []lipgloss.Color{"#8B2F84", "#1F5F99", "#9A4D00", "#3F7D2C", "#A3371F", "#5B4B8A", "#00796B"},
		}
	}
	// dark variant of Blocks: same structure, inverted values
	return theme{
		Paper: "#1E1D24", Side: "#26252D", Block: "#33313B", Card: "#2B2A32",
		Ink: "#E9E5DB", OnInk: "#1E1D24",
		Text: "#E9E5DB", Muted: "#A9A6B3", Faint: "#6B6875",
		Accent: "#C8452D", OnAccent: "#FFFFFF", Link: "#FF9D85",
		Good: "#8CC97A", Warn: "#D9B44A", Danger: "#F06A5E",
		RailBg: "#141318", RailFg: "#8F8C99", RailBadge: "#FF9D85", RailDot: "#9FD08A",
		Names: []lipgloss.Color{"#D88BD0", "#7FB2E5", "#E3A15F", "#8CC97A", "#FF9D85", "#B5A6E0", "#5FC4B8"},
	}
}

type styles struct {
	t         theme
	ic        icons
	Text      lipgloss.Style
	Muted     lipgloss.Style
	Faint     lipgloss.Style
	Bold      lipgloss.Style
	Accent    lipgloss.Style // accent-coloured text (links, buttons)
	Title     lipgloss.Style
	Danger    lipgloss.Style
	Warn      lipgloss.Style
	OK        lipgloss.Style
	Sel       lipgloss.Style // selected row: accent fill
	SelDim    lipgloss.Style
	Border    lipgloss.Style
	Button    lipgloss.Style
	ButtonHot lipgloss.Style
	Chip      lipgloss.Style // block-filled chip
	ChipMine  lipgloss.Style // inverse chip
	Badge     lipgloss.Style // accent count badge
	Inverse   lipgloss.Style // inverse bar
	Tab       lipgloss.Style
	TabActive lipgloss.Style
	Modal     lipgloss.Style
	Status    lipgloss.Style
	html      htmltext.Styles
	cards     cards.Styles

	// background prefixes for painting pane fills
	bgPaper, bgSide, bgBlock, bgCard, bgInk, bgAccent, bgRail string
}

func newStyles(light bool, iconSet string) styles {
	t := newTheme(light)
	s := styles{t: t, ic: newIcons(iconSet)}
	fg := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	s.Text = fg(t.Text)
	s.Muted = fg(t.Muted)
	s.Faint = fg(t.Faint)
	s.Bold = fg(t.Text).Bold(true)
	s.Accent = fg(t.Link)
	s.Title = fg(t.Text).Bold(true)
	s.Danger = fg(t.Danger)
	s.Warn = fg(t.Warn)
	s.OK = fg(t.Good)
	s.Sel = lipgloss.NewStyle().Background(t.Accent).Foreground(t.OnAccent).Bold(true)
	s.SelDim = lipgloss.NewStyle().Background(t.Block).Foreground(t.Text)
	s.Border = fg(t.Faint)
	s.Button = fg(t.Link).Bold(true)
	s.ButtonHot = lipgloss.NewStyle().Foreground(t.OnAccent).Background(t.Accent).Bold(true)
	s.Chip = lipgloss.NewStyle().Foreground(t.Text).Background(t.Block)
	s.ChipMine = lipgloss.NewStyle().Foreground(t.OnInk).Background(t.Ink)
	s.Badge = lipgloss.NewStyle().Foreground(t.OnAccent).Background(t.Accent).Bold(true)
	s.Inverse = lipgloss.NewStyle().Foreground(t.OnInk).Background(t.Ink)
	s.Tab = fg(t.Link).Bold(true)
	s.TabActive = s.Inverse.Bold(true)
	s.Modal = lipgloss.NewStyle()
	s.Status = fg(t.RailFg)
	s.html = htmltext.Styles{
		Text:      s.Text,
		Link:      fg(t.Link).Underline(true),
		Mention:   fg(t.Link).Bold(true),
		MentionMe: s.Inverse.Bold(true),
		Code:      fg(t.Link),
		Quote:     s.Muted,
		Dim:       s.Faint,
	}
	s.cards = cards.Styles{
		Text: s.Text, Bold: s.Bold, Subtle: s.Muted, Heading: s.Title, Link: s.html.Link, Mention: s.html.Mention,
		Accent: s.Accent, Good: s.OK.Bold(true), Warning: s.Warn.Bold(true), Attention: s.Danger.Bold(true),
		Button:         s.Button,
		ButtonPositive: s.Button,
		ButtonDanger:   s.Button,
		ButtonDead:     s.Muted,
		Border:         s.Border, Input: s.Muted,
		TitleBar:    s.Inverse.Bold(true),
		TableHeader: lipgloss.NewStyle().Background(t.Block).Foreground(t.Text).Bold(true),
		InputBox:    lipgloss.NewStyle().Background(t.Side).Foreground(t.Muted),
		Lock:        s.ic.Lock,
		External:    s.ic.External,
	}
	s.bgPaper, s.bgSide, s.bgBlock = bgPrefix(t.Paper), bgPrefix(t.Side), bgPrefix(t.Block)
	s.bgCard, s.bgInk, s.bgAccent, s.bgRail = bgPrefix(t.Card), bgPrefix(t.Ink), bgPrefix(t.Accent), bgPrefix(t.RailBg)
	return s
}

func (s styles) nameColor(id string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return lipgloss.NewStyle().Foreground(s.t.Names[int(h.Sum32())%len(s.t.Names)]).Bold(true)
}

// paint fills each line to width w on the background prefix bg.
func paint(lines []string, w int, bg string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = withBg(l, bg, w)
	}
	return out
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
		return s.Danger.Render("●")
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
