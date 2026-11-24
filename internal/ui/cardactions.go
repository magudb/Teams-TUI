package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/magudb/Teams-TUI/internal/cards"
	"github.com/magudb/Teams-TUI/internal/model"
)

func (m *Model) hasCardActions(cv *convView, msg *model.Message) bool {
	for _, r := range cv.cardActs {
		if r.msgID == msg.ID {
			return true
		}
	}
	return false
}

// runCardAction performs a card button press.
func (m *Model) runCardAction(cv *convView, ref cardRef) tea.Cmd {
	a := ref.action
	switch a.Type {
	case cards.OpenURL:
		if a.URL == "" {
			return nil
		}
		return m.openLink(a.URL)
	case cards.ShowCard, cards.Toggle:
		if st := cv.cardState[ref.msgID+":"+itoa(ref.att)]; st != nil {
			st.Apply(a)
			cv.dirty = true
		}
		return nil
	}
	// Submit / Execute go to the bot through the Bot Framework, which
	// Microsoft Graph can't do on your behalf.
	var msg *model.Message
	for i := range cv.msgs {
		if cv.msgs[i].ID == ref.msgID {
			msg = &cv.msgs[i]
		}
	}
	link := ""
	if msg != nil {
		link = messageLink(cv.ref, msg, m.svc.TenantID())
	}
	title := a.Title
	if title == "" {
		title = "This button"
	}
	if link == "" {
		m.setToast("“"+title+"” sends data to the bot — only possible in the Teams app", true)
		return nil
	}
	m.openConfirm("“"+title+"” must be sent from Teams. Open this message in Teams?", func(m *Model) tea.Cmd { return m.openLink(link) })
	return nil
}

// openCardActions lists the buttons of the selected message's cards.
func (m *Model) openCardActions(cv *convView, msg *model.Message) {
	var keys []string
	for k, r := range cv.cardActs {
		if r.msgID == msg.ID {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		m.setToast("This message has no card buttons", false)
		return
	}
	sortStrings(keys)
	var items []modalItem
	for _, k := range keys {
		ref := cv.cardActs[k]
		hint := map[cards.ActionType]string{cards.OpenURL: "open link", cards.ShowCard: "expand", cards.Toggle: "show / hide", cards.BotAction: "opens Teams"}[ref.action.Type]
		label := ref.action.Title
		if label == "" {
			label = "(untitled)"
		}
		items = append(items, modalItem{label: label, hint: hint, run: func(m *Model) tea.Cmd { return m.runCardAction(cv, ref) }})
	}
	m.openList("Card actions", items, len(items) > 8)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && strings.Compare(s[j-1], s[j]) > 0; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
