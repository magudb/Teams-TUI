package model

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

var reMeetup = regexp.MustCompile(`/l/meetup-join/([^/?]+)`)

// ChatIDFromJoinURL extracts the meeting chat thread id
// ("19:meeting_…@thread.v2") from a Teams join link.
func ChatIDFromJoinURL(join string) string {
	m := reMeetup.FindStringSubmatch(join)
	if m == nil {
		return ""
	}
	id, err := url.PathUnescape(m[1])
	if err != nil || !strings.HasPrefix(id, "19:") {
		return ""
	}
	return id
}

// Live reports whether the event is in progress at t.
func (e Event) Live(t time.Time) bool { return !e.IsAllDay && !t.Before(e.Start) && t.Before(e.End) }

// Joinable reports whether the event is an online meeting the user may join.
func (e Event) Joinable() bool {
	return e.JoinURL != "" && !e.IsCancelled && e.Response != "declined"
}

// TranscriptLine is one cue of a WebVTT transcript.
type TranscriptLine struct {
	Start   string // hh:mm:ss
	Speaker string
	Text    string
}

var reVoice = regexp.MustCompile(`^<v ([^>]*)>(.*?)(</v>)?$`)

// ParseVTT parses a Teams WebVTT transcript, merging consecutive cues from
// the same speaker.
func ParseVTT(vtt string) []TranscriptLine {
	var out []TranscriptLine
	var start string
	for _, raw := range strings.Split(strings.ReplaceAll(vtt, "\r\n", "\n"), "\n") {
		l := strings.TrimSpace(raw)
		switch {
		case l == "" || l == "WEBVTT" || strings.HasPrefix(l, "NOTE"):
			continue
		case strings.Contains(l, "-->"):
			start = strings.TrimSpace(strings.Split(l, "-->")[0])
			if i := strings.Index(start, "."); i > 0 {
				start = start[:i]
			}
			continue
		}
		speaker, text := "", l
		if m := reVoice.FindStringSubmatch(l); m != nil {
			speaker, text = m[1], m[2]
		}
		if strings.Contains(raw, "-->") || text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Speaker == speaker {
			out[n-1].Text += " " + text
			continue
		}
		if start == "" && len(out) == 0 && speaker == "" {
			continue // cue identifier line
		}
		out = append(out, TranscriptLine{Start: start, Speaker: speaker, Text: text})
	}
	return out
}
