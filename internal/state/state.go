// Package state persists small pieces of local UI state (pinned chats,
// muted conversations, expanded teams, drafts).
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State is persisted to state.json in the config directory.
type State struct {
	Pinned    map[string]bool      `json:"pinned"`     // chat ids (legacy set)
	PinOrder  []string             `json:"pin_order"`  // pinned chat ids, top to bottom
	TeamOrder []string             `json:"team_order"` // custom team order (empty = server order)
	Muted     map[string]bool      `json:"muted"`      // conv keys
	Expanded  map[string]bool      `json:"expanded"`   // team ids
	Hidden    map[string]bool      `json:"hidden"`     // team ids hidden from list
	Drafts    map[string]string    `json:"drafts"`     // conv key -> draft
	Seen      map[string]time.Time `json:"seen"`       // channel key -> when you last looked at it
	Section   string               `json:"section"`

	path string
	mu   sync.Mutex
}

// Load reads state from dir (missing file is fine).
func Load(dir string) *State {
	s := &State{path: filepath.Join(dir, "state.json")}
	if dir != "" {
		if b, err := os.ReadFile(s.path); err == nil {
			_ = json.Unmarshal(b, s)
		}
	}
	if s.Pinned == nil {
		s.Pinned = map[string]bool{}
	}
	// migrate the legacy pinned set into the ordered list
	inOrder := map[string]bool{}
	for _, id := range s.PinOrder {
		inOrder[id] = true
	}
	for id, on := range s.Pinned {
		if on && !inOrder[id] {
			s.PinOrder = append(s.PinOrder, id)
		}
	}
	if s.Muted == nil {
		s.Muted = map[string]bool{}
	}
	if s.Expanded == nil {
		s.Expanded = map[string]bool{}
	}
	if s.Hidden == nil {
		s.Hidden = map[string]bool{}
	}
	if s.Seen == nil {
		s.Seen = map[string]time.Time{}
	}
	if s.Drafts == nil {
		s.Drafts = map[string]string{}
	}
	return s
}

// Save writes state to disk.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" || filepath.Dir(s.path) == "." {
		return nil
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

// IsPinned reports whether a chat is pinned.
func (s *State) IsPinned(id string) bool {
	for _, x := range s.PinOrder {
		if x == id {
			return true
		}
	}
	return false
}

// TogglePin pins (at the bottom of the pinned list) or unpins a chat.
func (s *State) TogglePin(id string) {
	for i, x := range s.PinOrder {
		if x == id {
			s.PinOrder = append(s.PinOrder[:i], s.PinOrder[i+1:]...)
			delete(s.Pinned, id)
			return
		}
	}
	s.PinOrder = append(s.PinOrder, id)
	s.Pinned[id] = true
}
