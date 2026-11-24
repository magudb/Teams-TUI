// Package state persists small pieces of local UI state (favorites,
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
	Pinned    map[string]bool      `json:"pinned,omitempty"`    // legacy: pinned chat ids
	PinOrder  []string             `json:"pin_order,omitempty"` // legacy: pinned chat ids in order
	Favorites []string             `json:"favorites"`           // "chat:<id>" / "chan:<team>:<channel>", top to bottom
	TeamOrder []string             `json:"team_order"`          // custom team order (empty = server order)
	Muted     map[string]bool      `json:"muted"`               // conv keys
	Expanded  map[string]bool      `json:"expanded"`            // team ids
	Hidden    map[string]bool      `json:"hidden"`              // team ids hidden from list
	Drafts    map[string]string    `json:"drafts"`              // conv key -> draft
	Seen      map[string]time.Time `json:"seen"`                // channel key -> when you last looked at it
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
	// pinned chats became favorites
	if len(s.PinOrder) > 0 {
		for _, id := range s.PinOrder {
			if !s.IsFavorite("chat:" + id) {
				s.Favorites = append(s.Favorites, "chat:"+id)
			}
		}
		s.PinOrder = nil
		s.Pinned = map[string]bool{}
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

// IsFavorite reports whether a chat ("chat:<id>") or channel
// ("chan:<team>:<channel>") is a favorite.
func (s *State) IsFavorite(key string) bool {
	for _, x := range s.Favorites {
		if x == key {
			return true
		}
	}
	return false
}

// ToggleFavorite adds key at the bottom of Favorites, or removes it.
func (s *State) ToggleFavorite(key string) {
	for i, x := range s.Favorites {
		if x == key {
			s.Favorites = append(s.Favorites[:i], s.Favorites[i+1:]...)
			return
		}
	}
	s.Favorites = append(s.Favorites, key)
}

// IsPinned reports whether a chat is a favorite.
func (s *State) IsPinned(id string) bool { return s.IsFavorite("chat:" + id) }

// TogglePin toggles a chat as favorite.
func (s *State) TogglePin(id string) { s.ToggleFavorite("chat:" + id) }
