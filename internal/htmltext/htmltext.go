// Package htmltext converts Teams message HTML into styled, wrapped terminal
// text, and converts composed text (light markdown) back to HTML.
package htmltext

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Styles used when rendering.
type Styles struct {
	Text      lipgloss.Style
	Link      lipgloss.Style
	Mention   lipgloss.Style
	MentionMe lipgloss.Style
	Code      lipgloss.Style
	Quote     lipgloss.Style
	Dim       lipgloss.Style
}

// Options control rendering.
type Options struct {
	Width int
	// IsMe reports whether the <at id="N"> mention refers to the current user.
	IsMe   func(atID string) bool
	Styles Styles
}

// Result is a rendered message body.
type Result struct {
	Lines  []string
	Links  []string // hrefs in order of appearance
	Images []string // img srcs (hosted content URLs)
	// MentionsMe is set when any mention refers to the current user.
	MentionsMe bool
}

type state struct {
	bold, italic, underline, strike, code, link, mention, mentionMe, heading bool
}

type block struct {
	first, rest string
	pre         bool
	text        strings.Builder
}

type listCtx struct {
	ordered bool
	n       int
}

type renderer struct {
	opt     Options
	blocks  []*block
	cur     *block
	quote   int
	lists   []listCtx
	st      state
	res     Result
	inPre   bool
	lastWS  bool
	linkBuf string
}

func (r *renderer) prefix() string {
	p := strings.Repeat(r.opt.Styles.Quote.Render("▎")+" ", r.quote)
	if n := len(r.lists); n > 0 {
		p += strings.Repeat("  ", n)
	}
	return p
}

func (r *renderer) newBlock(first string) {
	if r.cur != nil && r.cur.text.Len() == 0 && first == "" {
		r.cur.first, r.cur.rest = r.prefix(), r.prefix()
		r.cur.pre = r.inPre
		return
	}
	p := r.prefix()
	if first == "" {
		first = p
	}
	r.cur = &block{first: first, rest: p, pre: r.inPre}
	r.blocks = append(r.blocks, r.cur)
	r.lastWS = true
}

func (r *renderer) style() lipgloss.Style {
	s := r.opt.Styles.Text
	switch {
	case r.st.code:
		s = r.opt.Styles.Code
	case r.st.mentionMe:
		s = r.opt.Styles.MentionMe
	case r.st.mention:
		s = r.opt.Styles.Mention
	case r.st.link:
		s = r.opt.Styles.Link
	}
	if r.quote > 0 && !r.st.code && !r.st.mention && !r.st.link {
		s = r.opt.Styles.Quote
	}
	if r.st.bold || r.st.heading {
		s = s.Bold(true)
	}
	if r.st.italic {
		s = s.Italic(true)
	}
	if r.st.underline {
		s = s.Underline(true)
	}
	if r.st.strike {
		s = s.Strikethrough(true)
	}
	return s
}

var wsRun = regexp.MustCompile(`[ \t\r\n\f]+`)

func (r *renderer) text(t string) {
	if r.cur == nil {
		r.newBlock("")
	}
	if !r.inPre {
		t = wsRun.ReplaceAllString(t, " ")
		if r.lastWS {
			t = strings.TrimLeft(t, " ")
		}
		if t == "" {
			return
		}
		r.lastWS = strings.HasSuffix(t, " ")
	}
	t = strings.ReplaceAll(t, " ", " ")
	st := r.style()
	// render line by line so styles never span newlines
	parts := strings.Split(t, "\n")
	for i, p := range parts {
		if i > 0 {
			r.cur.text.WriteString("\n")
		}
		if p != "" {
			r.cur.text.WriteString(st.Render(p))
		}
	}
}

func attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func (r *renderer) walk(n *xhtml.Node) {
	switch n.Type {
	case xhtml.TextNode:
		r.text(n.Data)
		return
	case xhtml.ElementNode:
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			r.walk(c)
		}
		return
	}

	saved := r.st
	children := true
	after := func() {}

	switch n.DataAtom {
	case atom.P, atom.Div, atom.Section, atom.Article, atom.Header, atom.Footer, atom.Tr:
		r.newBlock("")
		after = func() { r.newBlock("") }
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		r.newBlock("")
		r.st.heading = true
		after = func() { r.newBlock("") }
	case atom.Br:
		if r.cur == nil {
			r.newBlock("")
		}
		r.cur.text.WriteString("\n")
		r.lastWS = true
	case atom.Hr:
		r.newBlock("")
		r.cur.text.WriteString(r.opt.Styles.Dim.Render(strings.Repeat("─", max(3, min(r.opt.Width-2, 40)))))
		r.newBlock("")
	case atom.B, atom.Strong:
		r.st.bold = true
	case atom.I, atom.Em:
		r.st.italic = true
	case atom.U, atom.Ins:
		r.st.underline = true
	case atom.S, atom.Strike, atom.Del:
		r.st.strike = true
	case atom.Code:
		r.st.code = true
	case atom.Pre:
		r.inPre = true
		r.newBlock("")
		r.st.code = true
		after = func() { r.inPre = false; r.newBlock("") }
	case atom.Blockquote:
		r.quote++
		r.newBlock("")
		after = func() { r.quote--; r.newBlock("") }
	case atom.Ul, atom.Ol:
		r.lists = append(r.lists, listCtx{ordered: n.DataAtom == atom.Ol})
		after = func() { r.lists = r.lists[:len(r.lists)-1]; r.newBlock("") }
	case atom.Li:
		bullet := "• "
		if l := len(r.lists); l > 0 {
			r.lists[l-1].n++
			if r.lists[l-1].ordered {
				bullet = strconv.Itoa(r.lists[l-1].n) + ". "
			}
		}
		p := r.prefix()
		// hanging indent: replace the last 2 indentation columns with the bullet
		first := strings.TrimSuffix(p, "  ") + bullet
		r.cur = nil
		r.newBlock(first)
		r.cur.rest = p + strings.Repeat(" ", max(0, ansi.StringWidth(bullet)-2))
		after = func() { r.newBlock("") }
	case atom.Td, atom.Th:
		if r.cur != nil && r.cur.text.Len() > 0 {
			r.text(" │ ")
		}
		if n.DataAtom == atom.Th {
			r.st.bold = true
		}
	case atom.A:
		href := attr(n, "href")
		if href != "" {
			r.res.Links = append(r.res.Links, href)
		}
		r.st.link = true
	case atom.Img:
		alt := attr(n, "alt")
		if strings.Contains(attr(n, "itemtype"), "Emoji") || (alt != "" && isEmoji(alt)) {
			r.text(alt)
		} else {
			src := attr(n, "src")
			if src != "" {
				r.res.Images = append(r.res.Images, src)
			}
			label := "image"
			if alt != "" && alt != "image" {
				label = alt
			}
			saved2 := r.st
			r.st = state{link: true}
			r.text("[📷 " + label + "]")
			r.st = saved2
		}
		children = false
	case atom.Script, atom.Style, atom.Head, atom.Title:
		children = false
	default:
		switch n.Data {
		case "at":
			r.st.mention = true
			if r.opt.IsMe != nil && r.opt.IsMe(attr(n, "id")) {
				r.st.mentionMe = true
				r.res.MentionsMe = true
			}
			if t := strings.TrimSpace(textOf(n)); t != "" && !strings.HasPrefix(t, "@") {
				r.text("@")
				r.lastWS = false
			}
		case "emoji":
			alt := attr(n, "alt")
			if alt == "" {
				alt = ":" + attr(n, "id") + ":"
			}
			r.text(alt)
			children = false
		case "attachment", "systemeventmessage":
			children = false
		}
	}

	if children {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			r.walk(c)
		}
	}
	r.st = saved
	after()
}

func isEmoji(s string) bool {
	for _, r := range s {
		if r < 0x2000 {
			return false
		}
	}
	return s != ""
}

// Render renders an HTML (or plain text) body.
func Render(body string, isHTML bool, opt Options) Result {
	if opt.Width < 10 {
		opt.Width = 10
	}
	r := &renderer{opt: opt}
	if !isHTML {
		body = "<p>" + strings.ReplaceAll(html.EscapeString(body), "\n", "<br>") + "</p>"
	}
	nodes, err := xhtml.ParseFragment(strings.NewReader(body), &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		r.newBlock("")
		r.text(body)
	} else {
		for _, n := range nodes {
			r.walk(n)
		}
	}

	var lines []string
	for _, b := range r.blocks {
		content := b.text.String()
		if strings.TrimSpace(ansi.Strip(content)) == "" {
			continue
		}
		// trim trailing newlines from <br> at end of block
		content = strings.TrimRight(content, "\n")
		fw := ansi.StringWidth(b.first)
		rw := ansi.StringWidth(b.rest)
		w := max(5, opt.Width-max(fw, rw))
		first := true
		for _, src := range strings.Split(content, "\n") {
			var wrapped string
			if b.pre {
				// wrap the plain text, then style each piece so the
				// continuation lines keep the code colour
				var parts []string
				for _, p := range strings.Split(ansi.Hardwrap(ansi.Strip(src), w, true), "\n") {
					parts = append(parts, opt.Styles.Code.Render(p))
				}
				wrapped = strings.Join(parts, "\n")
			} else {
				wrapped = ansi.Wrap(src, w, "-/")
			}
			for _, l := range strings.Split(wrapped, "\n") {
				p := b.rest
				if first {
					p = b.first
					first = false
				}
				if b.pre {
					l = opt.Styles.Code.Render("") + l
				}
				lines = append(lines, p+strings.TrimRight(l, " "))
			}
		}
	}
	r.res.Lines = lines
	return r.res
}

// Plain converts HTML to a single plain-text string (for previews).
func Plain(body string) string {
	nodes, err := xhtml.ParseFragment(strings.NewReader(body), &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return body
	}
	var sb strings.Builder
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		switch {
		case n.Type == xhtml.TextNode:
			sb.WriteString(n.Data)
		case n.Type == xhtml.ElementNode && (n.DataAtom == atom.Img || n.Data == "emoji"):
			if a := attr(n, "alt"); a != "" {
				sb.WriteString(a)
			} else if n.DataAtom == atom.Img {
				sb.WriteString("[image]")
			}
			return
		case n.Type == xhtml.ElementNode && (n.DataAtom == atom.Br || n.DataAtom == atom.P || n.DataAtom == atom.Div || n.DataAtom == atom.Li):
			sb.WriteString(" ")
		case n.Type == xhtml.ElementNode && n.Data == "attachment":
			sb.WriteString(" Attachment ")
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return strings.TrimSpace(wsRun.ReplaceAllString(strings.ReplaceAll(sb.String(), " ", " "), " "))
}

func textOf(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
