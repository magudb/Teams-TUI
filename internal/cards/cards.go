// Package cards renders Microsoft Teams cards (Adaptive Cards, hero /
// thumbnail cards, Office 365 connector cards, list and receipt cards) as
// styled terminal text.
package cards

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Content types handled by Render.
const (
	TypeAdaptive  = "application/vnd.microsoft.card.adaptive"
	TypeHero      = "application/vnd.microsoft.card.hero"
	TypeThumbnail = "application/vnd.microsoft.card.thumbnail"
	TypeConnector = "application/vnd.microsoft.teams.card.o365connector"
	TypeList      = "application/vnd.microsoft.teams.card.list"
	TypeReceipt   = "application/vnd.microsoft.card.receipt"
	TypeSignin    = "application/vnd.microsoft.card.signin"
)

// Supported reports whether contentType is a card this package renders.
func Supported(contentType string) bool {
	switch contentType {
	case TypeAdaptive, TypeHero, TypeThumbnail, TypeConnector, TypeList, TypeReceipt, TypeSignin:
		return true
	}
	return strings.HasPrefix(contentType, "application/vnd.microsoft.card.")
}

// Styles used for rendering.
type Styles struct {
	Text, Bold, Subtle, Heading, Link, Mention       lipgloss.Style
	Accent, Good, Warning, Attention                 lipgloss.Style
	Button, ButtonPositive, ButtonDanger, ButtonDead lipgloss.Style
	Border, Input                                    lipgloss.Style
	TitleBar, TableHeader, InputBox                  lipgloss.Style // bar fills
	Lock, External                                   string         // glyphs for bot-only and link buttons
}

// ActionType classifies what a card button does.
type ActionType int

const (
	OpenURL   ActionType = iota // open a link
	ShowCard                    // expand / collapse an inline card
	Toggle                      // toggle visibility of elements
	BotAction                   // Submit / Execute / messageBack: needs the bot
)

// Action is a clickable card button.
type Action struct {
	Key     string // stable key, unique within the card
	Type    ActionType
	Title   string
	URL     string
	Targets []ToggleTarget
}

// ToggleTarget is one target of Action.ToggleVisibility.
type ToggleTarget struct {
	ID      string
	Visible *bool // nil = toggle
}

// State keeps interactive card state (expanded show-cards, toggled elements).
type State struct {
	Expanded map[string]bool // show-card action key -> expanded
	Vis      map[string]bool // element id -> visibility override
	defaults map[string]bool // element id -> isVisible from the card (recorded while rendering)
}

// NewState returns an empty state.
func NewState() *State {
	return &State{Expanded: map[string]bool{}, Vis: map[string]bool{}, defaults: map[string]bool{}}
}

func (s *State) visible(id string, def bool) bool {
	if id == "" {
		return def
	}
	s.defaults[id] = def
	if v, ok := s.Vis[id]; ok {
		return v
	}
	return def
}

// Apply performs a local action (ShowCard / Toggle) on the state.
func (s *State) Apply(a Action) {
	switch a.Type {
	case ShowCard:
		s.Expanded[a.Key] = !s.Expanded[a.Key]
	case Toggle:
		for _, t := range a.Targets {
			if t.Visible != nil {
				s.Vis[t.ID] = *t.Visible
				continue
			}
			cur, ok := s.Vis[t.ID]
			if !ok {
				cur, ok = s.defaults[t.ID]
				if !ok {
					cur = true
				}
			}
			s.Vis[t.ID] = !cur
		}
	}
}

// Options control rendering.
type Options struct {
	Width  int
	Styles Styles
	State  *State
	// Mark wraps a button label so the caller can make it clickable.
	Mark func(key, label string) string
	// Frame draws the card as a block: an inverse title bar followed by the
	// body (the caller paints the body background).
	Frame bool
	// Label is shown on the right of the title bar (e.g. the app name).
	Label string
	// IsMe reports whether a mentioned name refers to the current user.
	IsMe func(name string) bool
}

// Result is a rendered card.
type Result struct {
	Lines   []string
	Actions []Action
	Links   []string
	Title   string // best-effort plain title (for previews)
}

type renderer struct {
	opt       Options
	s         Styles
	res       Result
	barTitle  string
	botTitles []string
}

// Render renders a card attachment. content is the attachment's JSON content.
func Render(contentType, content string, opt Options) (Result, bool) {
	if opt.State == nil {
		opt.State = NewState()
	}
	if opt.Mark == nil {
		opt.Mark = func(_, l string) string { return l }
	}
	inner := opt.Width
	if opt.Frame {
		inner -= 2 // one column of padding on each side
	}
	if inner < 12 {
		return Result{}, false
	}
	var card map[string]any
	if err := json.Unmarshal([]byte(content), &card); err != nil {
		// some producers double-encode the content
		var s string
		if json.Unmarshal([]byte(content), &s) != nil || json.Unmarshal([]byte(s), &card) != nil {
			return Result{}, false
		}
	}
	r := &renderer{opt: opt, s: opt.Styles}
	var lines []string
	switch {
	case contentType == TypeAdaptive || str(card, "type") == "AdaptiveCard":
		if opt.Frame {
			r.barTitle = takeHeading(card)
		}
		lines = r.adaptive(card, inner, "")
	case contentType == TypeHero || contentType == TypeThumbnail || contentType == TypeSignin:
		lines = r.hero(card, inner)
	case contentType == TypeConnector:
		lines = r.connector(card, inner)
	case contentType == TypeList:
		lines = r.list(card, inner)
	case contentType == TypeReceipt:
		lines = r.receipt(card, inner)
	default:
		lines = r.hero(card, inner)
	}
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return Result{}, false
	}
	if len(r.botTitles) > 0 {
		lines = append(lines, r.s.Subtle.Render(ansi.Truncate(strings.Join(r.botTitles, "/")+" go to the bot. Press T to respond in Teams.", inner, "…")))
	}
	if opt.Frame {
		title := r.barTitle
		if title == "" {
			title = r.res.Title
		}
		if title == "" {
			title = "card"
		}
		title = strings.ToUpper(reTags.ReplaceAllString(title, ""))
		label := opt.Label
		lw := ansi.StringWidth(label)
		bar := " " + ansi.Truncate(title, max(4, opt.Width-lw-4), "…")
		bar = fit(bar, opt.Width-lw-1) + label + " "
		out := []string{r.s.TitleBar.Render(fit(bar, opt.Width))}
		for _, l := range lines {
			out = append(out, " "+fit(l, inner)+" ")
		}
		lines = out
	}
	r.res.Lines = lines
	return r.res, true
}

// takeHeading removes a leading heading TextBlock from the card body and
// returns its text, so it can be shown in the title bar.
func takeHeading(card map[string]any) string {
	body, _ := card["body"].([]any)
	if len(body) == 0 {
		return ""
	}
	first, ok := body[0].(map[string]any)
	if !ok || str(first, "type") != "TextBlock" {
		return ""
	}
	size := strings.ToLower(str(first, "size"))
	if strings.ToLower(str(first, "style")) != "heading" && size != "large" && size != "extralarge" && strings.ToLower(str(first, "weight")) != "bolder" {
		return ""
	}
	card["body"] = body[1:]
	return str(first, "text")
}

// ---- JSON helpers ---------------------------------------------------------------

func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	switch v := m[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func boolv(m map[string]any, k string, def bool) bool {
	if v, ok := m[k].(bool); ok {
		return v
	}
	return def
}

func arr(m map[string]any, k string) []map[string]any {
	raw, _ := m[k].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		if o, ok := x.(map[string]any); ok {
			out = append(out, o)
		}
	}
	return out
}

func obj(m map[string]any, k string) map[string]any {
	o, _ := m[k].(map[string]any)
	return o
}

// ---- layout helpers ---------------------------------------------------------------

// fit pads/truncates s to exactly w cells.
func fit(s string, w int) string {
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if d := w - ansi.StringWidth(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

func maxWidth(lines []string) int {
	n := 0
	for _, l := range lines {
		n = max(n, ansi.StringWidth(l))
	}
	return n
}

func align(lines []string, how string, w int) []string {
	if how != "center" && how != "right" {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		pad := w - ansi.StringWidth(l)
		if how == "center" {
			pad /= 2
		}
		out[i] = strings.Repeat(" ", max(0, pad)) + l
	}
	return out
}

// ---- text ---------------------------------------------------------------------

var (
	reDateFn = regexp.MustCompile(`\{\{(DATE|TIME)\(([^,)]+)(?:,\s*(SHORT|LONG|COMPACT))?\)\}\}`)
	reLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reBold   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reItal   = regexp.MustCompile(`(^|[\s(])[_*]([^_*]+)[_*]`)
	reAt     = regexp.MustCompile(`<at>([^<]*)</at>`)
	reTags   = regexp.MustCompile(`<[^>]+>`)
)

func expandDates(s string) string {
	return reDateFn.ReplaceAllStringFunc(s, func(m string) string {
		p := reDateFn.FindStringSubmatch(m)
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(p[2]))
		if err != nil {
			return p[2]
		}
		t = t.Local()
		if p[1] == "TIME" {
			return t.Format("15:04")
		}
		switch p[3] {
		case "LONG":
			return t.Format("Monday, 2 January 2006")
		case "SHORT":
			return t.Format("Mon, 2 Jan 2006")
		}
		return t.Format("02/01/2006")
	})
}

// text renders a TextBlock-style markdown string in base style, wrapped to w.
func (r *renderer) text(src string, base lipgloss.Style, w int, wrap bool, maxLines int) []string {
	src = expandDates(strings.ReplaceAll(src, "\r\n", "\n"))
	var out []string
	for _, para := range strings.Split(src, "\n") {
		prefix := ""
		trim := strings.TrimSpace(para)
		switch {
		case strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* "):
			prefix, para = "• ", trim[2:]
		case len(trim) > 2 && trim[0] >= '0' && trim[0] <= '9' && strings.Contains(trim[:min(4, len(trim))], ". "):
			i := strings.Index(trim, ". ")
			prefix, para = trim[:i+2], trim[i+2:]
		}
		styled := r.inline(para, base)
		pw := ansi.StringWidth(prefix)
		var wrapped []string
		if wrap {
			wrapped = strings.Split(ansi.Wrap(styled, max(4, w-pw), " -/"), "\n")
		} else {
			wrapped = []string{ansi.Truncate(styled, max(4, w-pw), "…")}
		}
		for i, l := range wrapped {
			p := strings.Repeat(" ", pw)
			if i == 0 {
				p = prefix
			}
			out = append(out, p+strings.TrimRight(l, " "))
		}
	}
	if maxLines > 0 && len(out) > maxLines {
		out = out[:maxLines]
		out[maxLines-1] = ansi.Truncate(out[maxLines-1], max(4, w-1), "") + "…"
	}
	return out
}

// inline applies markdown links, bold, italics and <at> mentions.
func (r *renderer) inline(s string, base lipgloss.Style) string {
	type span struct {
		start, end int
		out        string
	}
	var spans []span
	for _, m := range reLink.FindAllStringSubmatchIndex(s, -1) {
		title, url := s[m[2]:m[3]], s[m[4]:m[5]]
		r.res.Links = append(r.res.Links, url)
		spans = append(spans, span{m[0], m[1], r.s.Link.Render(title)})
	}
	for _, m := range reAt.FindAllStringSubmatchIndex(s, -1) {
		name := s[m[2]:m[3]]
		st := r.s.Mention
		if r.opt.IsMe != nil && r.opt.IsMe(name) {
			st = r.s.Warning.Bold(true)
		}
		spans = append(spans, span{m[0], m[1], st.Render("@" + name)})
	}
	var b strings.Builder
	pos := 0
	plain := func(t string) {
		if t == "" {
			return
		}
		t = reTags.ReplaceAllString(t, "")
		// bold / italics within plain runs
		t = reBold.ReplaceAllStringFunc(t, func(m string) string { return "\x00B" + m[2:len(m)-2] + "\x00b" })
		t = reItal.ReplaceAllString(t, "$1\x00I$2\x00i")
		for t != "" {
			i := strings.IndexByte(t, 0)
			if i < 0 {
				b.WriteString(base.Render(t))
				break
			}
			if i > 0 {
				b.WriteString(base.Render(t[:i]))
			}
			if i+1 >= len(t) {
				break
			}
			tag := t[i+1]
			rest := t[i+2:]
			end := strings.IndexByte(rest, 0)
			if end < 0 || (tag != 'B' && tag != 'I') {
				t = rest
				continue
			}
			st := base.Bold(true)
			if tag == 'I' {
				st = base.Italic(true)
			}
			b.WriteString(st.Render(rest[:end]))
			t = rest[min(len(rest), end+2):]
		}
	}
	// apply spans in order
	for len(spans) > 0 {
		first := 0
		for i := range spans {
			if spans[i].start < spans[first].start {
				first = i
			}
		}
		sp := spans[first]
		spans = append(spans[:first], spans[first+1:]...)
		if sp.start < pos {
			continue
		}
		plain(s[pos:sp.start])
		b.WriteString(sp.out)
		pos = sp.end
	}
	plain(s[pos:])
	return b.String()
}

func (r *renderer) textStyle(el map[string]any) lipgloss.Style {
	st := r.s.Text
	switch strings.ToLower(str(el, "color")) {
	case "accent":
		st = r.s.Accent
	case "good":
		st = r.s.Good
	case "warning":
		st = r.s.Warning
	case "attention":
		st = r.s.Attention
	case "light":
		st = r.s.Subtle
	}
	if boolv(el, "isSubtle", false) {
		st = r.s.Subtle
	}
	switch strings.ToLower(str(el, "weight")) {
	case "bolder":
		st = st.Bold(true)
	case "lighter":
		st = st.Faint(true)
	}
	switch strings.ToLower(str(el, "size")) {
	case "large", "extralarge":
		st = st.Bold(true)
	}
	if strings.ToLower(str(el, "style")) == "heading" {
		st = r.s.Heading
	}
	return st
}
