package ui

import (
	"fmt"
	"github.com/magudb/Teams-TUI/internal/model"
	"testing"
	"time"
)

func BenchmarkIdleView(b *testing.B) {
	for _, n := range []int{10, 100} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			m := isolatedModel(b)
			m.chatsLoaded = true
			for i := range n {
				m.chats = append(m.chats, model.Chat{ID: fmt.Sprint(i), Topic: "Project discussion", ChatType: "group"})
			}
			m.View()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				m.View()
			}
		})
	}
}

func historyMessages(n int) []model.Message {
	at := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	msgs := make([]model.Message, n)
	for i := range msgs {
		msgs[i] = model.Message{ID: fmt.Sprint(i), MessageType: "message", Body: "Status update", ContentType: "text", From: model.User{ID: "a", DisplayName: "Ann"}, Created: at.Add(time.Duration(i) * time.Minute)}
	}
	return msgs
}
func BenchmarkConversationHistory(b *testing.B) {
	m := isolatedModel(b)
	cv := newConvView(model.ConvRef{Kind: model.KindChat, ChatID: "c"})
	msgs := historyMessages(3000)
	cv.merge(msgs, false)
	m.renderConv(cv, 80, false)
	b.Run("Select", func(b *testing.B) {
		b.ReportAllocs()
		for i := range b.N {
			cv.selectMsg(i % len(msgs))
			m.renderConv(cv, 80, false)
		}
	})
	b.Run("UnchangedPoll", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			cv.merge(msgs[len(msgs)-50:], false)
		}
	})
}
