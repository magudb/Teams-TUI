package model

import (
	"strings"
	"sync"
	"testing"
)

func names(chs []Channel) string {
	var out []string
	for _, c := range chs {
		out = append(out, c.DisplayName)
	}
	return strings.Join(out, ",")
}

func TestSortChannelsDanish(t *testing.T) {
	t.Setenv("LANG", "da_DK.UTF-8")
	collatorOnce = sync.Once{}
	chs := []Channel{{DisplayName: "Økonomi"}, {DisplayName: "arv"}, {DisplayName: "Ægtepagter"}, {DisplayName: "General"}, {DisplayName: "Zebra"}, {DisplayName: "Ånden"}, {DisplayName: "Bolig"}}
	SortChannels(chs)
	if got := names(chs); got != "General,arv,Bolig,Zebra,Ægtepagter,Økonomi,Ånden" {
		t.Fatalf("got %s", got)
	}
}

func TestApplyAndMoveOrder(t *testing.T) {
	if got := strings.Join(ApplyOrder([]string{"a", "b", "c", "d"}, []string{"c", "x", "a"}), ","); got != "c,a,b,d" {
		t.Fatalf("ApplyOrder: %s", got)
	}
	if got := strings.Join(MoveInOrder([]string{"a", "b", "c", "d"}, "d", 1), ","); got != "a,d,b,c" {
		t.Fatalf("MoveInOrder up: %s", got)
	}
	if got := strings.Join(MoveInOrder([]string{"a", "b", "c", "d"}, "a", 2), ","); got != "b,c,a,d" {
		t.Fatalf("MoveInOrder down: %s", got)
	}
	if got := strings.Join(MoveInOrder([]string{"a", "b"}, "a", 5), ","); got != "a,b" {
		t.Fatalf("out of range: %s", got)
	}
}

func TestChatIDFromJoinURL(t *testing.T) {
	u := "https://teams.microsoft.com/l/meetup-join/19%3ameeting_NzA5YTk2%40thread.v2/0?context=%7b%22Tid%22%3a%22x%22%7d"
	if got := ChatIDFromJoinURL(u); got != "19:meeting_NzA5YTk2@thread.v2" {
		t.Fatalf("got %q", got)
	}
	if ChatIDFromJoinURL("https://example.com/x") != "" {
		t.Fatal("expected empty")
	}
}

func TestParseVTT(t *testing.T) {
	vtt := "WEBVTT\n\nabc-123/1\n00:00:01.000 --> 00:00:02.000\n<v Ann>Hello</v>\n\n00:00:02.500 --> 00:00:03.000\n<v Ann>there.</v>\n\n00:01:00.000 --> 00:01:02.000\n<v Bob>Hi Ann</v>\n"
	got := ParseVTT(vtt)
	if len(got) != 2 || got[0].Speaker != "Ann" || got[0].Text != "Hello there." || got[0].Start != "00:00:01" || got[1].Speaker != "Bob" {
		t.Fatalf("got %+v", got)
	}
}
