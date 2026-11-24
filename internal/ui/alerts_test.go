package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/magudb/Teams-TUI/internal/config"
	"github.com/magudb/Teams-TUI/internal/demo"
	"github.com/magudb/Teams-TUI/internal/model"
)

func alertModel(t *testing.T, mod func(*config.Config)) (*Model, *demo.Service) {
	t.Helper()
	m, svc := newTestModelSvc(t, 120, 36, mod)
	m.alertGuard = 0
	return m, svc
}

func TestIncomingCallAlert(t *testing.T) {
	m, svc := alertModel(t, func(*config.Config) {})
	svc.Call("c3", "call-1", "call", true)
	drain(t, m, m.loadChats())
	if !m.alertVisible() || m.alerts[0].kind != alertCall {
		t.Fatalf("expected an incoming call alert, got %+v", m.alerts)
	}
	s := screen(m)
	checkSize(t, s, 120, 36)
	for _, want := range []string{"INCOMING CALL", "Lars Nielsen", "is calling you", "Answer in Teams", "Reply with a message", "Dismiss"} {
		if !strings.Contains(s, want) {
			t.Errorf("alert is missing %q", want)
		}
	}
	// the same event on the next poll must not ring again
	drain(t, m, m.loadChats())
	if len(m.alerts) != 1 {
		t.Fatalf("call alerted twice: %d alerts", len(m.alerts))
	}
	press(t, m, "m")
	if m.alertVisible() || m.conv == nil || m.conv.ref.ChatID != "c3" || m.focus != FocusCompose {
		t.Fatalf("reply should open the chat with the compose box focused")
	}
}

func TestMissedCallAlert(t *testing.T) {
	m, svc := alertModel(t, func(*config.Config) {})
	svc.Call("c3", "call-1", "call", true)
	drain(t, m, m.loadChats())
	svc.Call("c3", "call-1", "call", false)
	drain(t, m, m.loadChats())
	if len(m.alerts) != 1 || m.alerts[0].kind != alertMissed {
		t.Fatalf("expected the ringing alert to become a missed call, got %+v", m.alerts)
	}
	if s := screen(m); !strings.Contains(s, "MISSED CALL") || !strings.Contains(s, "Call back in Teams") {
		t.Errorf("missed call dialog not shown")
	}
	press(t, m, "esc")
	if len(m.alerts) != 0 || m.alertVisible() {
		t.Fatalf("esc should dismiss the alert")
	}
}

func TestDismissedCallDoesNotComeBackAsMissed(t *testing.T) {
	m, svc := alertModel(t, func(*config.Config) {})
	svc.Call("c3", "call-1", "call", true)
	drain(t, m, m.loadChats())
	press(t, m, "esc")
	svc.Call("c3", "call-1", "call", false)
	drain(t, m, m.loadChats())
	if len(m.alerts) != 0 {
		t.Fatalf("a dismissed call should not raise a missed-call dialog")
	}
	found := false
	for _, a := range m.activity {
		found = found || strings.HasPrefix(a.title, "Missed call from")
	}
	if !found {
		t.Errorf("missed call should still be listed in Activity")
	}
}

func TestStaleAndOwnCallsDoNotAlert(t *testing.T) {
	m, _ := alertModel(t, func(*config.Config) {})
	chat := *m.chatByID("c3")
	old := &model.CallEvent{Started: true, Type: "call", CallID: "old", Initiator: chat.Members[1]}
	chat.LastMessage = &model.MessagePreview{ID: "x1", Created: time.Now().Add(-5 * time.Minute), Call: old}
	drain(t, m, m.noteCall(chat, false))
	mine := &model.CallEvent{Started: true, Type: "call", CallID: "mine", Initiator: m.me}
	chat.LastMessage = &model.MessagePreview{ID: "x2", Created: time.Now(), Call: mine}
	drain(t, m, m.noteCall(chat, false))
	if len(m.alerts) != 0 {
		t.Fatalf("stale or own calls should not alert: %+v", m.alerts[0])
	}
}

func TestMeetingReminderAlert(t *testing.T) {
	m, _ := alertModel(t, func(c *config.Config) { c.MeetingReminderMinutes = 10 })
	if !m.alertVisible() || m.alerts[0].kind != alertMeetingSoon || m.alerts[0].title != "Roadmap review" {
		t.Fatalf("expected a reminder for the meeting starting soon, got %+v", m.alerts)
	}
	s := screen(m)
	for _, want := range []string{"MEETING STARTING SOON", "Join in Teams", "Meeting chat", "Remind at start"} {
		if !strings.Contains(s, want) {
			t.Errorf("reminder is missing %q", want)
		}
	}
	press(t, m, "s")
	if len(m.alerts) != 0 || m.remindAt["e2"].IsZero() {
		t.Fatalf("snooze should close the dialog and schedule a second reminder")
	}
	m.remindAt["e2"] = time.Now().Add(-time.Second)
	drain(t, m, m.checkUpcoming())
	if !m.alertVisible() || m.alerts[0].head != "Meeting starting now" {
		t.Fatalf("expected the reminder to come back at the start time")
	}
	press(t, m, "c")
	if m.alertVisible() || m.conv == nil || m.conv.ref.ChatID != "c1" {
		t.Fatalf("meeting chat action should open the meeting chat")
	}
}

func TestMeetingStartedUpdatesReminder(t *testing.T) {
	m, svc := alertModel(t, func(c *config.Config) { c.MeetingReminderMinutes = 10 })
	svc.Call("c1", "meet-1", "meeting", true)
	drain(t, m, m.loadChats())
	if len(m.alerts) != 1 || m.alerts[0].kind != alertMeetingStarted {
		t.Fatalf("expected the reminder to turn into a meeting-started alert, got %d alerts", len(m.alerts))
	}
	if s := screen(m); !strings.Contains(s, "MEETING STARTED") || !strings.Contains(s, "Sofie Jensen started the meeting") {
		t.Errorf("meeting started dialog not shown")
	}
	svc.Call("c1", "meet-1", "meeting", false)
	drain(t, m, m.loadChats())
	if len(m.alerts) != 0 {
		t.Fatalf("the alert should close when the meeting ends")
	}
}

func TestAlertWaitsWhileTyping(t *testing.T) {
	m, svc := alertModel(t, func(*config.Config) {})
	drain(t, m, m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: "c1"}))
	m.setFocus(FocusCompose)
	typeText(t, m, "hello")
	svc.Call("c3", "call-1", "call", true)
	drain(t, m, m.loadChats())
	if m.alertVisible() || len(m.alerts) != 1 {
		t.Fatalf("the dialog must not take over while typing")
	}
	if s := screen(m); !strings.Contains(s, "Incoming call · Lars Nielsen — alt+a") {
		t.Errorf("waiting alert should show in the status bar")
	}
	press(t, m, "alt+a")
	if !m.alertVisible() {
		t.Fatalf("alt+a should show the waiting alert")
	}
	press(t, m, "esc")
	m.lastKey = time.Now().Add(-time.Minute)
	svc.Call("c4", "call-2", "call", true)
	drain(t, m, m.loadChats())
	if !m.alertVisible() {
		t.Fatalf("the dialog should open once typing has paused")
	}
}

func TestAlertKeyGuard(t *testing.T) {
	m, svc := alertModel(t, func(*config.Config) {})
	m.alertGuard = time.Minute
	svc.Call("c3", "call-1", "call", true)
	drain(t, m, m.loadChats())
	press(t, m, "enter")
	if !m.alertVisible() {
		t.Fatalf("a key pressed as the dialog appears must not trigger an action")
	}
}

func TestAlertsDisabled(t *testing.T) {
	m, svc := alertModel(t, func(c *config.Config) { c.Alerts = false })
	svc.Call("c3", "call-1", "call", true)
	drain(t, m, m.loadChats())
	if len(m.alerts) != 0 || !strings.Contains(m.toast, "Incoming call: Lars Nielsen") {
		t.Fatalf("with dialogs off a call should only show a toast, got %q", m.toast)
	}
}
