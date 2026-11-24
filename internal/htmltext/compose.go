package htmltext

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Shortcodes maps :name: to emoji for composing.
var Shortcodes = map[string]string{
	"smile": "😄", "grin": "😁", "joy": "😂", "laugh": "😆", "wink": "😉", "blush": "😊",
	"heart": "❤️", "+1": "👍", "thumbsup": "👍", "like": "👍", "-1": "👎", "thumbsdown": "👎",
	"clap": "👏", "pray": "🙏", "tada": "🎉", "fire": "🔥", "rocket": "🚀", "eyes": "👀",
	"thinking": "🤔", "cry": "😢", "sad": "😢", "angry": "😡", "surprised": "😮", "wow": "😮",
	"ok": "👌", "wave": "👋", "coffee": "☕", "100": "💯", "check": "✅", "x": "❌",
	"warning": "⚠️", "star": "⭐", "sun": "☀️", "beer": "🍺", "cake": "🎂", "sweat_smile": "😅",
	"rofl": "🤣", "facepalm": "🤦", "shrug": "🤷", "muscle": "💪", "bulb": "💡", "point_up": "☝️",
}

var (
	reShortcode = regexp.MustCompile(`:([a-z0-9_+\-]+):`)
	reBold      = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	reItalic    = regexp.MustCompile(`(^|[\s(])[*_]([^*_\n]+)[*_]`)
	reStrike    = regexp.MustCompile(`~~([^~\n]+)~~`)
	reCode      = regexp.MustCompile("`([^`\n]+)`")
	reURL       = regexp.MustCompile(`https?://[^\s<]+[^\s<.,;:!?)\]'"]`)
)

// ExpandShortcodes replaces :name: with emoji.
func ExpandShortcodes(s string) string {
	return reShortcode.ReplaceAllStringFunc(s, func(m string) string {
		if e, ok := Shortcodes[strings.Trim(m, ":")]; ok {
			return e
		}
		return m
	})
}

// MentionSpec is a mention to embed: occurrences of "@"+Name become <at>.
type MentionSpec struct {
	ID   int
	Name string
}

func inline(s string) string {
	// protect code spans first
	var codes []string
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, "<code>"+m[1:len(m)-1]+"</code>")
		return "\x00" + strconv.Itoa(len(codes)-1) + "\x00"
	})
	s = reURL.ReplaceAllStringFunc(s, func(u string) string {
		return `<a href="` + u + `">` + u + `</a>`
	})
	s = reBold.ReplaceAllString(s, "<b>$1</b>")
	s = reStrike.ReplaceAllString(s, "<s>$1</s>")
	s = reItalic.ReplaceAllString(s, "$1<i>$2</i>")
	for i, c := range codes {
		s = strings.ReplaceAll(s, "\x00"+strconv.Itoa(i)+"\x00", c)
	}
	return s
}

// ToHTML converts composed text with light markdown to Teams HTML.
func ToHTML(text string, mentions []MentionSpec) string {
	text = ExpandShortcodes(strings.TrimRight(text, "\n "))
	lines := strings.Split(text, "\n")
	var out strings.Builder
	inCode, inList, inQuote := false, false, false
	closeBlocks := func() {
		if inList {
			out.WriteString("</ul>")
			inList = false
		}
		if inQuote {
			out.WriteString("</blockquote>")
			inQuote = false
		}
	}
	first := true
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			if inCode {
				out.WriteString("</code></pre>")
				inCode = false
			} else {
				closeBlocks()
				out.WriteString("<pre><code>")
				inCode = true
				first = true
			}
			continue
		}
		if inCode {
			if !first {
				out.WriteString("\n")
			}
			out.WriteString(html.EscapeString(l))
			first = false
			continue
		}
		esc := html.EscapeString(l)
		switch {
		case strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* "):
			if inQuote {
				out.WriteString("</blockquote>")
				inQuote = false
			}
			if !inList {
				out.WriteString("<ul>")
				inList = true
			}
			out.WriteString("<li>" + inline(esc[2:]) + "</li>")
		case strings.HasPrefix(l, "> ") || l == ">":
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			if !inQuote {
				out.WriteString("<blockquote>")
				inQuote = true
			} else {
				out.WriteString("<br>")
			}
			out.WriteString(inline(strings.TrimPrefix(strings.TrimPrefix(esc, "&gt;"), " ")))
		default:
			closeBlocks()
			out.WriteString("<p>" + inline(esc) + "</p>")
		}
	}
	if inCode {
		out.WriteString("</code></pre>")
	}
	closeBlocks()
	s := out.String()
	s = strings.ReplaceAll(s, "<p></p>", "<p>&nbsp;</p>")
	for _, m := range mentions {
		name := html.EscapeString(m.Name)
		tag := `<at id="` + strconv.Itoa(m.ID) + `">` + name + `</at>`
		s = strings.Replace(s, "@"+name, tag, 1)
	}
	return s
}

// ToMarkdown is a best-effort inverse used when editing a message.
func ToMarkdown(body string) string {
	r := strings.NewReplacer(
		"<b>", "**", "</b>", "**", "<strong>", "**", "</strong>", "**",
		"<i>", "*", "</i>", "*", "<em>", "*", "</em>", "*",
		"<s>", "~~", "</s>", "~~", "<code>", "`", "</code>", "`",
		"<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p><p>", "\n", "</div><div>", "\n",
	)
	s := r.Replace(body)
	s = regexp.MustCompile(`<at id="\d+">([^<]*)</at>`).ReplaceAllString(s, "@$1")
	return Plainish(s)
}

// Plainish strips tags but keeps newlines.
func Plainish(s string) string {
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(strings.ReplaceAll(s, " ", " "))
}
