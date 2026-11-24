package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magudb/Teams-TUI/internal/auth"
	"github.com/magudb/Teams-TUI/internal/model"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	origin, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tok, _ := json.Marshal(auth.Token{ClientID: "cid", Tenant: "organizations", AccessToken: "tok", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)})
	if err := os.WriteFile(filepath.Join(dir, "token.json"), tok, 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(auth.New("cid", "organizations", dir))
	c.base = *origin
	c.me = "me"
	return c
}

func TestMessagesParsedAndSorted(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing auth header")
		}
		if !strings.HasPrefix(r.URL.Path, "/chats/c1/messages") {
			t.Errorf("path = %s", r.URL.Path)
		}
		io.WriteString(w, `{"value":[
		 {"id":"2","messageType":"message","createdDateTime":"2026-01-02T10:00:00Z","from":{"user":{"id":"u1","displayName":"Ann"}},
		  "body":{"contentType":"html","content":"<p>hi <at id=\"0\">Me</at></p>"},
		  "mentions":[{"id":0,"mentionText":"Me","mentioned":{"user":{"id":"me","displayName":"Me"}}}],
		  "reactions":[{"reactionType":"like","createdDateTime":"2026-01-02T10:01:00Z","user":{"user":{"id":"u2"}}}]},
		 {"id":"1","messageType":"message","createdDateTime":"2026-01-01T10:00:00Z","from":{"user":{"id":"me","displayName":"Me"}},
		  "body":{"contentType":"text","content":"first"},"lastEditedDateTime":"2026-01-01T11:00:00Z"}
		],"@odata.nextLink":"NEXT"}`)
	})
	msgs, next, err := c.Messages(context.Background(), model.ConvRef{Kind: model.KindChat, ChatID: "c1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if next != "NEXT" || len(msgs) != 2 || msgs[0].ID != "1" || !msgs[0].Edited {
		t.Fatalf("bad parse: %+v next=%q", msgs, next)
	}
	if msgs[1].Reactions[0].Type != "👍" || msgs[1].Mentions[0].UserID != "me" {
		t.Fatalf("bad reactions/mentions: %+v", msgs[1])
	}
}

func TestSendWithMentionAndReplyReaction(t *testing.T) {
	var got map[string]any
	var paths []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/replies") {
			_ = json.NewDecoder(r.Body).Decode(&got)
			io.WriteString(w, `{"id":"r1","createdDateTime":"2026-01-01T00:00:00Z"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	ref := model.ConvRef{Kind: model.KindChannel, TeamID: "t", ChannelID: "c", ThreadID: "root"}
	out := model.Outgoing{HTML: `<p><at id="0">Bob</at> hi</p>`, Mentions: []model.Mention{{ID: 0, Text: "Bob", UserID: "b"}},
		MentionUser: map[int]model.User{0: {ID: "b", DisplayName: "Bob"}}}
	m, err := c.Send(context.Background(), ref, out)
	if err != nil || m.ID != "r1" {
		t.Fatalf("send: %v %+v", err, m)
	}
	ms := got["mentions"].([]any)[0].(map[string]any)
	if ms["mentionText"] != "Bob" {
		t.Fatalf("mentions not sent: %v", got)
	}
	if err := c.React(context.Background(), ref, "r1", "🎉", true); err != nil {
		t.Fatal(err)
	}
	if err := c.React(context.Background(), ref, "root", "🎉", false); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /teams/t/channels/c/messages/root/replies",
		"POST /teams/t/channels/c/messages/root/replies/r1/setReaction",
		"POST /teams/t/channels/c/messages/root/unsetReaction",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
}

func TestRetryOn429(t *testing.T) {
	var n int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{"id":"me","displayName":"Me","userPrincipalName":"me@x"}`)
	})
	u, err := c.Me(context.Background())
	if err != nil || u.Email != "me@x" || n != 2 {
		t.Fatalf("got %+v %v n=%d", u, err, n)
	}
}

func TestChatsSkipHiddenAndPreview(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"value":[
		{"id":"a","chatType":"oneOnOne","members":[{"userId":"me","displayName":"Me"},{"userId":"u","displayName":"U"}],
		 "viewpoint":{"isHidden":false,"lastMessageReadDateTime":"2026-01-01T00:00:00Z"},
		 "lastMessagePreview":{"id":"m","createdDateTime":"2026-01-02T00:00:00Z","body":{"contentType":"html","content":"<p>Hello&nbsp;<b>there</b></p>"},"from":{"user":{"id":"u","displayName":"U"}}}},
		{"id":"b","chatType":"group","viewpoint":{"isHidden":true}}]}`)
	})
	chats, _, err := c.Chats(context.Background(), "")
	if err != nil || len(chats) != 1 {
		t.Fatalf("%v %+v", err, chats)
	}
	ch := chats[0]
	if ch.LastMessage.Text != "Hello there" || !ch.Unread("me") || ch.Title("me") != "U" {
		t.Fatalf("bad chat: %+v %+v", ch, ch.LastMessage)
	}
}

func TestAPIErrorForbidden(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"code":"Forbidden","message":"Missing scope"}}`)
	})
	_, err := c.Teams(context.Background())
	if !IsStatus(err, 403) || !strings.Contains(err.Error(), "admin consent") {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthStaysOnConfiguredOrigin(t *testing.T) {
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("authorization crossed origins")
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer external.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("missing credentials on Graph request")
		}
		http.Redirect(w, r, external.URL, http.StatusFound)
	})
	for _, path := range []string{"/redirect", external.URL} {
		err := c.do(context.Background(), http.MethodGet, path, nil, nil)
		if !IsStatus(err, http.StatusUnauthorized) {
			t.Fatalf("foreign 401 handling: %v", err)
		}
	}
}

func TestChatContinuation(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			io.WriteString(w, `{"value":[{"id":"b"}]}`)
			return
		}
		io.WriteString(w, `{"value":[{"id":"a"}],"@odata.nextLink":"/next"}`)
	})
	first, next, err := c.Chats(context.Background(), "")
	if err != nil || len(first) != 1 || next != "/next" {
		t.Fatal(first, next, err)
	}
	last, next, err := c.Chats(context.Background(), next)
	if err != nil || len(last) != 1 || last[0].ID != "b" || next != "" {
		t.Fatal(last, next, err)
	}
}

func TestCallEventDetail(t *testing.T) {
	var c gChat
	raw := `{"id":"19:x","chatType":"oneOnOne","lastMessagePreview":{"id":"1","messageType":"unknownFutureValue",
		"body":{"contentType":"html","content":"<systemEventMessage/>"},
		"eventDetail":{"@odata.type":"#microsoft.graph.callEndedEventMessageDetail","callEventType":"call","callId":"abc",
		"initiator":{"user":{"id":"u1","displayName":"Ann"}},
		"callParticipants":[{"participant":{"user":{"id":"u1"}}},{"participant":{"user":{"id":"u2"}}}]}}}`
	if err := jsonUnmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	p := c.toModel().LastMessage
	if p.Call == nil || p.Call.Started || p.Call.Type != "call" || p.Call.CallID != "abc" || p.Call.Initiator.ID != "u1" || len(p.Call.Participants) != 2 {
		t.Fatalf("call event not parsed: %+v", p.Call)
	}
	if p.Text != "Call ended" {
		t.Errorf("preview text = %q", p.Text)
	}
	other := gEventDetail{Type: "#microsoft.graph.callRecordingEventMessageDetail"}
	if other.toCall() != nil {
		t.Errorf("recording events are not calls")
	}
}
