package htmltext

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func render(html string, w int) []string {
	r := Render(html, true, Options{Width: w, IsMe: func(id string) bool { return id == "1" }})
	out := make([]string, len(r.Lines))
	for i, l := range r.Lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func TestRenderBlocks(t *testing.T) {
	got := render(`<p>Hello <b>bold</b> &amp; <i>it</i></p><ul><li>one</li><li>two</li></ul><blockquote>quoted</blockquote><pre><code>a  b
c</code></pre>`, 40)
	want := []string{"Hello bold & it", "• one", "• two", "▎ quoted", "a  b", "c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestRenderWrapsAndLinks(t *testing.T) {
	r := Render(`<p>see <a href="https://x.example/a">this link</a> and some more words to wrap around</p><img src="https://graph/hosted" alt="image"><emoji id="smile" alt="😄"></emoji>`, true, Options{Width: 20})
	for _, l := range r.Lines {
		if ansi.StringWidth(l) > 20 {
			t.Fatalf("line too wide: %q", l)
		}
	}
	if len(r.Links) != 1 || len(r.Images) != 1 {
		t.Fatalf("links=%v images=%v", r.Links, r.Images)
	}
	if !strings.Contains(ansi.Strip(strings.Join(r.Lines, " ")), "😄") {
		t.Fatalf("emoji missing: %q", r.Lines)
	}
}

func TestMentionsMe(t *testing.T) {
	r := Render(`<p><at id="1">Me</at> and <at id="2">Bob</at></p>`, true, Options{Width: 40, IsMe: func(id string) bool { return id == "1" }})
	if !r.MentionsMe {
		t.Fatal("expected MentionsMe")
	}
}

func TestPlain(t *testing.T) {
	if got := Plain(`<p>a&nbsp;b</p><p>c<br>d</p><attachment id="x"></attachment>`); got != "a b c d Attachment" {
		t.Fatalf("got %q", got)
	}
}

func TestToHTML(t *testing.T) {
	got := ToHTML("Hi @Bob Smith **now** `x<y` :+1:\n- a\n- b\n> q\nhttps://e.example/x.", []MentionSpec{{ID: 0, Name: "Bob Smith"}})
	for _, want := range []string{`<at id="0">Bob Smith</at>`, "<b>now</b>", "<code>x&lt;y</code>", "👍", "<ul><li>a</li><li>b</li></ul>", "<blockquote>q</blockquote>", `<a href="https://e.example/x">`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	code := ToHTML("```\nfunc() {\n  *x*\n}\n```", nil)
	if !strings.Contains(code, "<pre><code>func() {\n  *x*\n}</code></pre>") {
		t.Errorf("code block: %s", code)
	}
}

func TestToMarkdownRoundTrip(t *testing.T) {
	if got := ToMarkdown(`<p><b>hi</b> <at id="0">Bob</at></p>`); got != "**hi** @Bob" {
		t.Fatalf("got %q", got)
	}
}
