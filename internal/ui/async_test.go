package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/magudb/Teams-TUI/internal/config"
	"github.com/magudb/Teams-TUI/internal/demo"
	"github.com/magudb/Teams-TUI/internal/model"
	"github.com/magudb/Teams-TUI/internal/state"
)

func isolatedModel(t testing.TB) *Model {
	t.Helper()
	m := New(demo.New(), config.Default(), state.Load(""), true)
	t.Cleanup(m.cancelTransfers)
	m.w, m.h, m.ready = 140, 40, true
	return m
}

func TestUploadBelongsToOriginatingDraft(t *testing.T) {
	m := isolatedModel(t)
	a := model.ConvRef{Kind: model.KindChat, ChatID: "A"}
	b := model.ConvRef{Kind: model.KindChat, ChatID: "B"}
	m.openConv(a)
	first := uploadMsg{ref: a, att: model.Attachment{Name: "meeting notes"}}
	m.openConv(b)
	m.handleData(first)
	if len(m.compose.attachments) != 0 {
		t.Fatal("attachment entered the wrong chat")
	}
	m.openConv(a)
	if len(m.compose.attachments) != 1 || m.compose.attachments[0].Name != first.att.Name {
		t.Fatal("originating draft lost its attachment")
	}
	m.compose.attachments = nil // user removes the first attachment
	m.handleData(uploadMsg{ref: a, att: model.Attachment{Name: "replacement"}})
	if len(m.compose.attachments) != 1 {
		t.Fatal("removed attachment reappeared")
	}
	m.submitCompose()
	m.handleData(first)
	if len(m.compose.attachments) != 0 {
		t.Fatal("late upload entered a new draft")
	}
}

func TestReverseOrderResults(t *testing.T) {
	m := isolatedModel(t)
	day := m.calDay
	first := m.loadEvents(day)
	second := m.loadEvents(day.AddDate(0, 0, 7))
	newer := second().(eventsMsg)
	newer.events = []model.Event{{ID: "new"}}
	m.handleData(newer)
	m.handleData(first())
	if len(m.events) != 1 || m.events[0].ID != "new" {
		t.Fatal("an older calendar request won")
	}

	a := model.ConvRef{Kind: model.KindChannel, TeamID: "t", ChannelID: "a"}
	b := a
	b.ChannelID = "b"
	m.openConv(a)
	old := m.loadTabFiles(a)
	m.openConv(b)
	m.mainTab = 1
	current := m.loadTabFiles(b)
	items := current().(filesMsg)
	items.items = []model.DriveItem{{ID: "new"}}
	items.err = nil
	m.handleData(items)
	m.handleData(old())
	if len(m.tabFiles) != 1 || m.tabFiles[0].ID != "new" {
		t.Fatal("wrong channel files")
	}

	m.searchInput.SetValue("same query")
	old = m.doSearch()
	current = m.doSearch()
	result := current().(searchMsg)
	result.hits = []model.SearchHit{{MsgID: "new"}}
	m.handleData(result)
	m.handleData(old())
	if m.busy != 0 || len(m.hits) != 1 || m.hits[0].MsgID != "new" {
		t.Fatal("search ownership or busy counter")
	}
}

func TestReadReceiptsRequireVisibleConversation(t *testing.T) {
	for _, section := range []Section{SecSettings, SecCalendar, SecChat} {
		t.Run(sectionNames[section], func(t *testing.T) {
			m := isolatedModel(t)
			ref := model.ConvRef{Kind: model.KindChat, ChatID: "A"}
			cv := newConvView(ref)
			m.conv = cv
			m.convs[ref.Key()] = cv
			m.chats = []model.Chat{{ID: "A"}}
			m.section = section
			m.focused = section != SecChat
			poll := messagesMsg{target: cv, ref: ref, poll: true, msgs: []model.Message{{ID: "new"}}}
			if m.handleData(poll) != nil || !m.chats[0].LastRead.IsZero() {
				t.Fatal("hidden chat marked read")
			}
			var cmd tea.Cmd
			if m.focused {
				cmd = m.switchSection(SecChat)
			} else {
				_, cmd = m.Update(tea.FocusMsg{})
			}
			if cmd == nil {
				t.Fatal("returning to chat did not acknowledge it")
			}
			m.handleData(readMsg{chatID: "A", err: errors.New("offline")})
			if !m.chats[0].LastRead.IsZero() || !m.toastErr {
				t.Fatal("failed receipt reported success")
			}
			if m.handleData(poll) == nil {
				t.Fatal("receipt was not retried")
			}
			now := time.Now()
			m.handleData(readMsg{chatID: "A", at: now})
			if !m.chats[0].LastRead.Equal(now) {
				t.Fatal("successful receipt lost")
			}
		})
	}
}

type pagedChats struct {
	model.Service
	fail bool
}

func (s *pagedChats) Chats(_ context.Context, cursor model.Page) ([]model.Chat, model.Page, error) {
	switch cursor {
	case "":
		return []model.Chat{{ID: "first"}}, "page2", nil
	case "page2":
		if s.fail {
			return nil, "", errors.New("offline")
		}
		return []model.Chat{{ID: "second"}}, "page3", nil
	default:
		return []model.Chat{{ID: "third"}}, "", nil
	}
}
func TestChatPaginationKeepsLastCompleteRefresh(t *testing.T) {
	m := isolatedModel(t)
	svc := &pagedChats{Service: m.svc}
	m.svc = svc
	m.handleData(m.loadChats()())
	if len(m.chats) != 2 || m.nextChats != "page3" {
		t.Fatal("missing continuation")
	}
	var more tea.Cmd
	for _, item := range m.sideItems(33) {
		if item.key == "more:chats" {
			more = item.open(m)
		}
	}
	if more == nil {
		t.Fatal("no sidebar action to load the remaining chats")
	}
	m.handleData(more())
	if len(m.chats) != 3 || m.nextChats != "" {
		t.Fatal("missing final page")
	}
	svc.fail = true
	m.handleData(m.loadChats()())
	if len(m.chats) != 3 || !m.toastErr {
		t.Fatal("partial refresh replaced complete list")
	}
	svc.fail = false
	m.handleData(m.loadChats()())
	if len(m.chats) != 3 {
		t.Fatal("refresh lost expanded range")
	}
}

func TestSpinnerStopsAndRestarts(t *testing.T) {
	m := isolatedModel(t)
	m.chatsLoaded = true
	m.busy = 1
	_, start := m.Update(nil)
	if start == nil {
		t.Fatal("spinner did not start")
	}
	_, tick := m.Update(start())
	if tick == nil {
		t.Fatal("spinner did not advance")
	}
	m.busy = 0
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil || m.spinning {
		t.Fatal("idle spinner rescheduled")
	}
	m.busy = 1
	if _, cmd := m.Update(nil); cmd == nil {
		t.Fatal("spinner did not restart")
	}
}

func TestHistorySelectionMatchesFullLayout(t *testing.T) {
	m := isolatedModel(t)
	for _, ref := range []model.ConvRef{{Kind: model.KindChat, ChatID: "c"}, {Kind: model.KindChannel, ChannelID: "c"}, {Kind: model.KindChannel, ChannelID: "c", ThreadID: "0"}} {
		cv := newConvView(ref)
		cv.merge(historyMessages(12), false)
		for _, i := range []int{0, 11, 3, 4, 0, -1} {
			cv.sel = i
			if i == 3 {
				msg := cv.msgs[2]
				msg.Body = "updated with a much longer body that may wrap to a new line"
				cv.merge([]model.Message{msg}, false)
			}
			m.renderConv(cv, 60, false)
			lines := slices.Clone(cv.lines)
			owners := slices.Clone(cv.owner)
			starts := slices.Clone(cv.msgStart)
			cv.dirty = true
			m.renderConv(cv, 60, false)
			if !slices.Equal(lines, cv.lines) || !slices.Equal(owners, cv.owner) || !slices.Equal(starts, cv.msgStart) {
				t.Fatalf("incremental layout differed for %v selection %d", ref, i)
			}
		}
	}
}

func TestHistoryEvictionProtectsPendingSend(t *testing.T) {
	m := isolatedModel(t)
	first := model.ConvRef{Kind: model.KindChat, ChatID: "pending"}
	m.openConv(first)
	m.conv.addTemp(model.User{}, model.Outgoing{})
	for i := range 32 {
		m.openConv(model.ConvRef{Kind: model.KindChat, ChatID: fmt.Sprint(i)})
	}
	if len(m.convs) != 16 || m.convs[first.Key()] == nil || m.convs["chat:0"] != nil {
		t.Fatal("history eviction did not preserve the expected conversations")
	}
}
