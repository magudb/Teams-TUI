// Package config loads user configuration for teams-tui.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// DefaultClientID is the public "Microsoft Graph Command Line Tools" client,
// which supports the device-code flow with delegated permissions. Set your own
// app registration's client id in the config file or TEAMS_TUI_CLIENT_ID to
// override it (recommended for organisations).
const DefaultClientID = "14d82eec-204b-4c2f-b7e8-296a70dab67e"

// Config is the persisted configuration.
type Config struct {
	ClientID             string `json:"client_id"`
	Tenant               string `json:"tenant"`
	DesktopNotifications bool   `json:"desktop_notifications"`
	Bell                 bool   `json:"bell"`
	PollMessagesSeconds  int    `json:"poll_messages_seconds"`
	PollChatsSeconds     int    `json:"poll_chats_seconds"`
	PollPresenceSeconds  int    `json:"poll_presence_seconds"`
	DownloadDir          string `json:"download_dir"`
	TimeFormat           string `json:"time_format"`
	DateFormat           string `json:"date_format"`
	SidebarWidth         int    `json:"sidebar_width"`
	Theme                string `json:"theme"`                 // "dark" (default) or "light"
	ASCIIIcons           bool   `json:"ascii_icons,omitempty"` // deprecated: use icons
	// Icons is "nerd" (Nerd Font glyphs, as in the design) or "ascii".
	Icons string `json:"icons"`
	// MeetingReminderMinutes is how long before a meeting the reminder fires.
	MeetingReminderMinutes int `json:"meeting_reminder_minutes"`
	// Tenants remembers tenants signed in to, for the login tenant picker.
	Tenants []string `json:"tenants,omitempty"`
	// CombinedChat shows channels and chats in one Chat tab (Unread, Channels, Chats).
	CombinedChat bool `json:"combined_chat"`
	// PollChannelSeconds is the pause between background checks of single
	// channels for new posts (used for channel unread markers). 0 disables.
	PollChannelSeconds int `json:"poll_channel_seconds"`
}

// Default returns the default configuration.
func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		ClientID:               DefaultClientID,
		Tenant:                 "organizations",
		DesktopNotifications:   true,
		Bell:                   false,
		PollMessagesSeconds:    5,
		PollChatsSeconds:       15,
		PollPresenceSeconds:    60,
		DownloadDir:            filepath.Join(home, "Downloads"),
		TimeFormat:             "15:04",
		DateFormat:             "Mon 02 Jan",
		SidebarWidth:           33,
		Theme:                  "light",
		Icons:                  "nerd",
		MeetingReminderMinutes: 5,
		CombinedChat:           true,
		PollChannelSeconds:     4,
	}
}

// Dir returns the configuration directory, creating it if needed.
func Dir() (string, error) {
	if d := os.Getenv("TEAMS_TUI_HOME"); d != "" {
		return d, os.MkdirAll(d, 0o700)
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	d := filepath.Join(base, "teams-tui")
	return d, os.MkdirAll(d, 0o700)
}

// Path returns the config file path.
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Load reads config.json (writing a default one on first run) and applies
// environment overrides.
func Load() (Config, error) {
	cfg := Default()
	p, err := Path()
	if err != nil {
		return cfg, err
	}
	b, err := os.ReadFile(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if out, err := json.MarshalIndent(cfg, "", "  "); err == nil {
			_ = os.WriteFile(p, out, 0o600)
		}
	case err != nil:
		return cfg, err
	default:
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, err
		}
	}
	if v := os.Getenv("TEAMS_TUI_CLIENT_ID"); v != "" {
		cfg.ClientID = v
	}
	if v := os.Getenv("TEAMS_TUI_TENANT"); v != "" {
		cfg.Tenant = v
	}
	if cfg.ClientID == "" {
		cfg.ClientID = DefaultClientID
	}
	if cfg.Tenant == "" {
		cfg.Tenant = "organizations"
	}
	if cfg.PollMessagesSeconds < 2 {
		cfg.PollMessagesSeconds = 2
	}
	if cfg.PollChatsSeconds < 5 {
		cfg.PollChatsSeconds = 5
	}
	if cfg.PollPresenceSeconds < 15 {
		cfg.PollPresenceSeconds = 15
	}
	if cfg.PollChannelSeconds < 0 {
		cfg.PollChannelSeconds = 0
	} else if cfg.PollChannelSeconds > 0 && cfg.PollChannelSeconds < 2 {
		cfg.PollChannelSeconds = 2
	}
	if cfg.ASCIIIcons && cfg.Icons == "" {
		cfg.Icons = "ascii"
	}
	if cfg.Icons != "ascii" {
		cfg.Icons = "nerd"
	}
	if cfg.MeetingReminderMinutes <= 0 {
		cfg.MeetingReminderMinutes = 5
	}
	if cfg.SidebarWidth < 24 {
		cfg.SidebarWidth = 24
	}
	if cfg.TimeFormat == "" {
		cfg.TimeFormat = "15:04"
	}
	if cfg.DateFormat == "" {
		cfg.DateFormat = "Mon 02 Jan"
	}
	return cfg, nil
}

// Save writes cfg to config.json.
func Save(cfg Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	cfg.ASCIIIcons = false
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}
