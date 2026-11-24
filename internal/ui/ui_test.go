package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/magudb/Teams-TUI/internal/config"
	"github.com/magudb/Teams-TUI/internal/demo"
	"github.com/magudb/Teams-TUI/internal/model"
	"github.com/magudb/Teams-TUI/internal/state"
)

// drain runs cmd (and anything it batches) with a timeout per command,
// feeding results back into the model. Slow commands (ticks) are dropped.
func drain(t *testing.T, m *Model, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth > 6 {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(400 * time.Millisecond):
		return
	}
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			drain(t, m, c, depth+1)
		}
	case tea.QuitMsg:
	default:
		if _, ok := msg.(tickMsg); ok {
			return
		}
		_, next := m.Update(msg)
		drain(t, m, next, depth+1)
	}
}

func newTestModel(t *testing.T, w, h int) *Model {
	t.Helper()
	return newTestModelCfg(t, w, h, func(*config.Config) {})
}

func newTestModelCfg(t *testing.T, w, h int, mod func(*config.Config)) *Model {
	t.Helper()
	m, _ := newTestModelSvc(t, w, h, mod)
	return m
}

func newTestModelSvc(t *testing.T, w, h int, mod func(*config.Config)) (*Model, *demo.Service) {
	t.Helper()
	svc := demo.New()
	svc.Simulate = false
	cfg := config.Default()
	cfg.PollChannelSeconds = 0 // tests drive channel polling explicitly
	mod(&cfg)
	m := New(svc, cfg, state.Load(""), true)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	drain(t, m, m.Init(), 0)
	return m, svc
}

func classic(c *config.Config) { c.CombinedChat = false }

func press(t *testing.T, m *Model, k string) {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+k":
		msg = tea.KeyMsg{Type: tea.KeyCtrlK}
	default:
		if strings.HasPrefix(k, "alt+") {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k[4:]), Alt: true}
		} else {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
	}
	_, cmd := m.Update(msg)
	drain(t, m, cmd, 0)
}

func typeText(t *testing.T, m *Model, s string) {
	for _, r := range s {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		drain(t, m, cmd, 0)
	}
}

func screen(m *Model) string { return ansi.Strip(m.View()) }

func dump(t *testing.T, name, s string) {
	if d := os.Getenv("TUI_DUMP"); d != "" {
		_ = os.WriteFile(d+"/"+name+".txt", []byte(s), 0o644)
	}
}

func checkSize(t *testing.T, s string, w, h int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) != h {
		t.Errorf("got %d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			t.Errorf("line %d width %d > %d: %q", i, lw, w, l)
		}
	}
}

func TestChatFlow(t *testing.T) {
	m := newTestModel(t, 140, 40)
	s := screen(m)
	dump(t, "01-start", s)
	checkSize(t, s, 140, 40)
	if !strings.Contains(s, "Sofie Jensen") || !strings.Contains(s, "Launch squad") {
		t.Fatalf("chat list missing:\n%s", s)
	}

	press(t, m, "enter") // open first chat
	s = screen(m)
	dump(t, "02-chat", s)
	checkSize(t, s, 140, 40)
	if m.conv == nil {
		t.Fatal("no conversation opened")
	}
	if m.focus != FocusCompose {
		t.Fatalf("focus = %v, want compose", m.focus)
	}

	typeText(t, m, "Hello **world** :tada:")
	press(t, m, "enter")
	s = screen(m)
	dump(t, "03-sent", s)
	if !strings.Contains(s, "Hello world 🎉") {
		t.Fatalf("sent message not shown:\n%s", s)
	}

	// select & react
	press(t, m, "esc")
	if m.focus != FocusMessages {
		t.Fatalf("focus = %v, want messages", m.focus)
	}
	press(t, m, "1")
	s = screen(m)
	dump(t, "04-react", s)
	if !strings.Contains(s, "👍 1") {
		t.Fatalf("reaction not shown:\n%s", s)
	}

	// edit last message via up in compose
	press(t, m, "tab")
	press(t, m, "up")
	if m.compose.editingID == "" {
		t.Fatal("expected edit mode")
	}
	press(t, m, "esc")

	// palette
	press(t, m, "esc")
	press(t, m, "ctrl+k")
	s = screen(m)
	dump(t, "05-palette", s)
	if m.modal == nil || !strings.Contains(s, "Command palette") {
		t.Fatalf("palette not open:\n%s", s)
	}
	press(t, m, "esc")
	checkSize(t, screen(m), 140, 40)
}

func TestTeamsAndThread(t *testing.T) {
	m := newTestModelCfg(t, 160, 45, classic)
	press(t, m, "alt+3")
	press(t, m, "enter") // expand Engineering
	s := screen(m)
	dump(t, "10-teams", s)
	if !strings.Contains(s, "General") {
		t.Fatalf("channels not shown:\n%s", s)
	}
	press(t, m, "down")
	press(t, m, "l") // open General without focusing compose
	s = screen(m)
	dump(t, "11-channel", s)
	if !strings.Contains(s, "Welcome!") || !strings.Contains(s, "3 replies") {
		t.Fatalf("channel posts missing:\n%s", s)
	}
	m.setFocus(FocusMessages)
	m.conv.selectMsg(0)
	press(t, m, "enter")
	s = screen(m)
	dump(t, "12-thread", s)
	if m.thread == nil || !strings.Contains(s, "Thread") || !strings.Contains(s, "Sofie, PM") {
		t.Fatalf("thread not open:\n%s", s)
	}
	checkSize(t, s, 160, 45)
	press(t, m, "r")
	typeText(t, m, "nice")
	press(t, m, "enter")
	if !strings.Contains(screen(m), "nice") {
		t.Fatal("reply not shown")
	}
}

func TestSectionsRender(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {200, 50}} {
		m := newTestModel(t, size[0], size[1])
		for i := 1; i <= 6; i++ {
			press(t, m, "alt+"+itoa(i))
			s := screen(m)
			dump(t, "20-section-"+itoa(i)+"-"+itoa(size[0]), s)
			checkSize(t, s, size[0], size[1])
		}
	}
}

func TestCalendarAndSearch(t *testing.T) {
	m := newTestModel(t, 140, 40)
	press(t, m, "alt+4")
	s := screen(m)
	dump(t, "30-calendar", s)
	if !strings.Contains(s, "Daily stand-up") {
		t.Fatalf("calendar missing events:\n%s", s)
	}
	press(t, m, "down")
	press(t, m, "a")
	press(t, m, "alt+6")
	typeText(t, m, "contract")
	press(t, m, "enter")
	s = screen(m)
	dump(t, "31-search", s)
	if len(m.hits) == 0 {
		t.Fatalf("no search hits:\n%s", s)
	}
	press(t, m, "enter")
	if m.conv == nil {
		t.Fatal("search result did not open conversation")
	}
}

func click(t *testing.T, m *Model, x, y int) {
	t.Helper()
	_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	drain(t, m, cmd, 0)
	_, cmd = m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	drain(t, m, cmd, 0)
}

func TestMouse(t *testing.T) {
	m := newTestModelCfg(t, 140, 40, classic)
	m.View()
	time.Sleep(50 * time.Millisecond) // zone manager is async
	// click second chat in the sidebar
	y := m.geo.sideList.y + 3
	click(t, m, m.geo.sideList.x+5, y)
	if m.conv == nil {
		t.Fatal("click did not open chat")
	}
	m.View()
	time.Sleep(50 * time.Millisecond)
	// click the Teams rail entry
	click(t, m, 3, 8)
	if m.section != SecTeams {
		t.Fatalf("section = %v, want Teams", m.section)
	}
	// wheel over messages should not panic
	m.View()
	_, _ = m.Update(tea.MouseMsg{X: m.geo.msgs.x + 5, Y: m.geo.msgs.y + 2, Button: tea.MouseButtonWheelUp})
}

// rowOf returns the screen row of the sidebar item with the given key.
func rowOf(m *Model, key string) int {
	for i, idx := range m.geo.sideOwner {
		if m.geo.sideItems[idx].key == key {
			return m.geo.sideList.y + i
		}
	}
	return -1
}

func TestPinnedOrderAndDrag(t *testing.T) {
	m := newTestModel(t, 140, 40)
	// pin three chats: c3, c1, c4 (pin order = top-to-bottom)
	for _, id := range []string{"c3", "c1", "c4"} {
		m.st.TogglePin(id)
	}
	pinned, recent := m.sortedChats()
	if got := ids(pinned); got != "c3,c1,c4" {
		t.Fatalf("pinned order %s", got)
	}
	// recent sorted by last activity: c2 (40m ago) before c5 (a week ago)
	if got := ids(recent); got != "c2,19:meeting_retro@thread.v2,c5" {
		t.Fatalf("recent order %s", got)
	}
	// keyboard: move c4 up
	m.sideSel[SecChat] = "chat:c4"
	m.sideTouched[SecChat] = true
	press(t, m, "K")
	if got := strings.Join(m.st.PinOrder, ","); got != "c3,c4,c1" {
		t.Fatalf("after K: %s", got)
	}
	// mouse: drag c1 onto c3
	m.View()
	from, to := rowOf(m, "chat:c1"), rowOf(m, "chat:c3")
	x := m.geo.sideList.x + 5
	_, _ = m.Update(tea.MouseMsg{X: x, Y: from, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	_, _ = m.Update(tea.MouseMsg{X: x, Y: to, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	_, _ = m.Update(tea.MouseMsg{X: x, Y: to, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if got := strings.Join(m.st.PinOrder, ","); got != "c1,c3,c4" {
		t.Fatalf("after drag: %s", got)
	}
	if m.conv != nil {
		t.Fatal("drag should not open a chat")
	}
}

func ids(cs []model.Chat) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return strings.Join(out, ",")
}

func TestTeamOrderAndHidden(t *testing.T) {
	m := newTestModelCfg(t, 140, 40, classic)
	press(t, m, "alt+3")
	vis, _ := m.orderedTeams()
	if vis[0].DisplayName != "Engineering" {
		t.Fatalf("default order should follow server order, got %s", vis[0].DisplayName)
	}
	m.sideSel[SecTeams] = "team:t3"
	m.sideTouched[SecTeams] = true
	press(t, m, "K")
	press(t, m, "K")
	vis, _ = m.orderedTeams()
	if vis[0].ID != "t3" {
		t.Fatalf("t3 not moved to top: %+v", vis)
	}
	press(t, m, "H")
	vis, hidden := m.orderedTeams()
	if len(hidden) != 1 || hidden[0].ID != "t3" || len(vis) != 2 {
		t.Fatalf("hide failed: %v %v", vis, hidden)
	}
	s := screen(m)
	if !strings.Contains(s, "Hidden teams (1)") {
		t.Fatalf("hidden section missing:\n%s", s)
	}
}

func TestMeetingCompanion(t *testing.T) {
	m := newTestModel(t, 150, 45)
	// live meeting banner in the status bar
	s := screen(m)
	if !strings.Contains(s, "Daily stand-up · live now") {
		t.Fatalf("no live meeting banner:\n%s", s)
	}
	press(t, m, "alt+4")
	m.sideSel[SecCalendar] = "ev:e1"
	m.sideTouched[SecCalendar] = true
	press(t, m, "down")
	press(t, m, "up") // triggers selection change -> attendee resolution
	s = screen(m)
	dump(t, "40-meeting", s)
	if !strings.Contains(s, "LIVE") || !strings.Contains(s, "Attendees (4)") || !strings.Contains(s, "Noah Petersen") {
		t.Fatalf("meeting details missing:\n%s", s)
	}
	if _, ok := m.emailUsers["noah@contoso.example"]; !ok {
		t.Fatal("attendees not resolved")
	}
	// meeting chat
	press(t, m, "c")
	if m.conv == nil || m.conv.ref.ChatID != "c2" || m.section != SecChat {
		t.Fatalf("meeting chat not opened: %+v", m.conv)
	}
	// past meeting: transcript
	press(t, m, "esc")
	press(t, m, "alt+4")
	press(t, m, "[") // yesterday
	m.sideSel[SecCalendar] = "ev:e0"
	m.sideTouched[SecCalendar] = true
	press(t, m, "down")
	press(t, m, "up")
	s = screen(m)
	dump(t, "41-recap", s)
	if !strings.Contains(s, "Transcript") || !strings.Contains(s, "Download recording") {
		t.Fatalf("recap buttons missing:\n%s", s)
	}
	press(t, m, "x")
	s = screen(m)
	dump(t, "42-transcript", s)
	if m.modal == nil || !strings.Contains(s, "support tickets are down") {
		t.Fatalf("transcript not shown:\n%s", s)
	}
}

func pollAllChannels(t *testing.T, m *Model) {
	n := 0
	for _, chs := range m.channels {
		n += len(chs)
	}
	for i := 0; i < n; i++ {
		drain(t, m, m.pollNextChannel(), 0)
	}
}

func TestCombinedUnread(t *testing.T) {
	m, svc := newTestModelSvc(t, 140, 40, func(*config.Config) {})
	pollAllChannels(t, m) // baseline: nothing unread yet
	if n := len(m.unreadChannels()); n != 0 {
		t.Fatalf("expected no unread channels on first sight, got %d", n)
	}
	s := screen(m)
	dump(t, "50-combined", s)
	for _, want := range []string{"UNREAD (2)", "CHANNELS", "CHATS", "Engineering", "Launch squad"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}
	if strings.Index(s, "UNREAD") > strings.Index(s, "CHANNELS") || strings.Index(s, "CHANNELS") > strings.Index(s, "CHATS") {
		t.Fatalf("sections out of order:\n%s", s)
	}
	if strings.Contains(s, "Teams\n") && strings.Contains(s, "  Teams  ") {
		t.Fatal("Teams rail entry should be hidden in combined mode")
	}

	// someone posts in Engineering › Backend
	svc.PostAs("u4", model.ConvRef{Kind: model.KindChannel, TeamID: "t1", ChannelID: "ch2"}, "<p>Deploy is green ✅</p>")
	pollAllChannels(t, m)
	s = screen(m)
	dump(t, "51-combined-unread", s)
	if len(m.unreadChannels()) != 1 || !strings.Contains(s, "UNREAD (3)") || !strings.Contains(s, "Deploy is green") {
		t.Fatalf("channel not unread:\n%s", s)
	}
	// open it from the Unread section
	m.sideSel[SecChat] = "u:chan:ch2"
	m.sideTouched[SecChat] = true
	press(t, m, "l")
	if m.conv == nil || m.conv.ref.ChannelID != "ch2" {
		t.Fatal("unread channel did not open")
	}
	if len(m.unreadChannels()) != 0 {
		t.Fatal("channel still unread after opening")
	}
	if m.sideSel[SecChat] != "chan:ch2" {
		t.Fatalf("selection should move to the channel in the tree, got %s", m.sideSel[SecChat])
	}
}

func lineWith(s, needle string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	return ""
}

func TestConversationLayout(t *testing.T) {
	m := newTestModelCfg(t, 140, 45, classic)
	drain(t, m, m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: "c1"}), 0)
	s := screen(m)
	dump(t, "60-bubbles", s)
	mine, theirs := lineWith(s, "Sure, 10:00 works"), lineWith(s, "Hi Alex! Do you have")
	if mine == "" || theirs == "" {
		t.Fatalf("messages missing:\n%s", s)
	}
	if strings.Index(mine, "Sure") <= strings.Index(theirs, "Hi Alex")+20 {
		t.Fatalf("own message should be right-aligned:\n%q\n%q", mine, theirs)
	}
	if !strings.Contains(s, "New messages") || !strings.Contains(s, "SJ") {
		t.Fatalf("expected unread divider and avatar:\n%s", s)
	}

	drain(t, m, m.openConv(model.ConvRef{Kind: model.KindChannel, TeamID: "t1", ChannelID: "ch1"}), 0)
	s = screen(m)
	dump(t, "61-cards", s)
	for _, want := range []string{"╭", "💬 3 replies from", "Open thread ›", "↩ Reply"} {
		if !strings.Contains(s, want) {
			t.Fatalf("card missing %q:\n%s", want, s)
		}
	}
	m.View()
	time.Sleep(50 * time.Millisecond)
	// click "Open thread ›"
	var x, y int
	for i, l := range strings.Split(screen(m), "\n") {
		if j := strings.Index(l, "Open thread"); j >= 0 {
			x, y = ansi.StringWidth(l[:j])+2, i
		}
	}
	click(t, m, x, y)
	s = screen(m)
	dump(t, "62-thread", s)
	if m.thread == nil || !strings.Contains(s, "← # General") || !strings.Contains(s, "3 replies") {
		t.Fatalf("thread not opened by click:\n%s", s)
	}
}

func clickText(t *testing.T, m *Model, text string) {
	t.Helper()
	m.View()
	time.Sleep(60 * time.Millisecond)
	for i, l := range strings.Split(screen(m), "\n") {
		if j := strings.Index(l, text); j >= 0 {
			click(t, m, ansi.StringWidth(l[:j])+2, i)
			return
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, screen(m))
}

func TestAdaptiveCardsInChat(t *testing.T) {
	m := newTestModelCfg(t, 150, 60, classic)
	drain(t, m, m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: "c2"}), 0)
	s := screen(m)
	dump(t, "70-card-chat", s)
	for _, want := range []string{"Approvals", "Approval requested: Contract renewal", "Contoso A/S", "[ Approve ]", "[ ▸ Delegate ]", "╭"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}
	clickText(t, m, "▸ Delegate")
	s = screen(m)
	if !strings.Contains(s, "Delegate to") {
		t.Fatalf("show card did not expand:\n%s", s)
	}
	clickText(t, m, "[ Approve ]")
	if m.modal == nil || !strings.Contains(screen(m), "must be sent from Teams") {
		t.Fatalf("bot action should explain it needs Teams:\n%s", screen(m))
	}
	press(t, m, "esc")
	// keyboard: card actions menu
	m.setFocus(FocusMessages)
	for i, msg := range m.conv.msgs {
		if msg.From.ID == "app:approvals" {
			m.conv.selectMsg(i)
		}
	}
	press(t, m, "A")
	if m.modal == nil || !strings.Contains(screen(m), "Card actions") {
		t.Fatalf("card actions menu missing:\n%s", screen(m))
	}
	press(t, m, "esc")

	// connector card in a channel post
	drain(t, m, m.openConv(model.ConvRef{Kind: model.KindChannel, TeamID: "t1", ChannelID: "ch2"}), 0)
	s = screen(m)
	dump(t, "71-card-channel", s)
	if !strings.Contains(s, "Pull request opened") || !strings.Contains(s, "fix/billing-retry") {
		t.Fatalf("connector card missing:\n%s", s)
	}
	checkSize(t, s, 150, 60)
}
