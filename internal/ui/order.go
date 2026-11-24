package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/magudb/Teams-TUI/internal/model"
)

const hiddenTeamsKey = "__hidden_teams"

// orderedTeams applies the user's custom order (like dragging teams in the
// Teams client) on top of the server order, and splits off hidden teams.
func (m *Model) orderedTeams() (visible, hidden []model.Team) {
	byID := make(map[string]model.Team, len(m.teams))
	ids := make([]string, 0, len(m.teams))
	for _, t := range m.teams {
		byID[t.ID] = t
		ids = append(ids, t.ID)
	}
	for _, id := range model.ApplyOrder(ids, m.st.TeamOrder) {
		if m.st.Hidden[id] {
			hidden = append(hidden, byID[id])
		} else {
			visible = append(visible, byID[id])
		}
	}
	return
}

// reorderGroup returns the reorderable group a sidebar key belongs to:
// "pinned" for pinned chats, "teams" for visible teams (channel rows count as
// their team). ok is false for items that can't be reordered.
func (m *Model) reorderGroup(key string) (group, id string, ok bool) {
	switch {
	case strings.HasPrefix(key, "chat:"):
		id = strings.TrimPrefix(key, "chat:")
		if m.st.IsPinned(id) {
			return "pinned", id, true
		}
	case strings.HasPrefix(key, "team:"):
		id = strings.TrimPrefix(key, "team:")
		if !m.st.Hidden[id] {
			return "teams", id, true
		}
	case strings.HasPrefix(key, "chan:"):
		chID := strings.TrimPrefix(key, "chan:")
		for teamID, chs := range m.channels {
			for _, ch := range chs {
				if ch.ID == chID && !m.st.Hidden[teamID] {
					return "teams", teamID, true
				}
			}
		}
	}
	return "", "", false
}

func (m *Model) groupOrder(group string) []string {
	switch group {
	case "pinned":
		return append([]string(nil), m.st.PinOrder...)
	case "teams":
		visible, _ := m.orderedTeams()
		ids := make([]string, len(visible))
		for i, t := range visible {
			ids[i] = t.ID
		}
		return ids
	}
	return nil
}

func (m *Model) setGroupOrder(group string, order []string) {
	switch group {
	case "pinned":
		m.st.PinOrder = order
	case "teams":
		// keep hidden teams' relative positions after the visible ones
		_, hidden := m.orderedTeams()
		for _, t := range hidden {
			order = append(order, t.ID)
		}
		m.st.TeamOrder = order
	}
	_ = m.st.Save()
}

// moveBy moves the item for key up (-1) or down (+1) within its group.
func (m *Model) moveBy(key string, delta int) {
	group, id, ok := m.reorderGroup(key)
	if !ok {
		if strings.HasPrefix(key, "chat:") {
			m.setToast("Pin a chat to reorder it (p)", false)
		}
		return
	}
	order := m.groupOrder(group)
	for i, x := range order {
		if x == id {
			m.setGroupOrder(group, model.MoveInOrder(order, id, i+delta))
			break
		}
	}
	if group == "teams" {
		m.sideSel[m.section] = "team:" + id
	}
}

// dropOn moves dragKey to targetKey's position. It reports whether a move happened.
func (m *Model) dropOn(dragKey, targetKey string) bool {
	g1, id1, ok1 := m.reorderGroup(dragKey)
	g2, id2, ok2 := m.reorderGroup(targetKey)
	if !ok1 || !ok2 || g1 != g2 || id1 == id2 {
		if ok1 && targetKey != dragKey {
			m.setToast("Can only drop within the same list", true)
		}
		return false
	}
	order := m.groupOrder(g1)
	for i, x := range order {
		if x == id2 {
			m.setGroupOrder(g1, model.MoveInOrder(order, id1, i))
			if g1 == "teams" {
				m.sideSel[m.section] = "team:" + id1
			}
			return true
		}
	}
	return false
}

func (m *Model) toggleHiddenTeam(id string) {
	m.st.Hidden[id] = !m.st.Hidden[id]
	if !m.st.Hidden[id] {
		delete(m.st.Hidden, id)
	}
	_ = m.st.Save()
}

func (m *Model) openTeamActions(t model.Team) {
	id := t.ID
	key := "team:" + id
	hideLabel := "⊘ Hide"
	if m.st.Hidden[id] {
		hideLabel = "◉ Show"
	}
	items := []modalItem{}
	if !m.st.Hidden[id] {
		items = append(items,
			modalItem{label: "↑ Move up", hint: "alt+↑", run: func(m *Model) tea.Cmd { m.moveBy(key, -1); return nil }},
			modalItem{label: "↓ Move down", hint: "alt+↓", run: func(m *Model) tea.Cmd { m.moveBy(key, 1); return nil }},
		)
	}
	items = append(items,
		modalItem{label: hideLabel, hint: "H", run: func(m *Model) tea.Cmd { m.toggleHiddenTeam(id); return nil }},
		modalItem{label: "↺ Reset team order", run: func(m *Model) tea.Cmd {
			m.st.TeamOrder = nil
			_ = m.st.Save()
			return nil
		}},
	)
	m.openList(t.DisplayName, items, false)
}

func (m *Model) teamByID(id string) *model.Team {
	for i := range m.teams {
		if m.teams[i].ID == id {
			return &m.teams[i]
		}
	}
	return nil
}
