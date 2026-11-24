package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/config"
)

// Settings screen: account, notifications, polling, appearance, keys, about.
// Changes apply immediately; ctrl+s writes config.json.

var settingsSections = []string{"Account", "Notifications", "Polling", "Appearance", "Keys", "About"}

type settingRow struct {
	section string
	label   string
	value   func(sel bool) string
	change  func(m *Model) tea.Cmd // nil = not selectable
}

func (m *Model) chipOnOff(on, sel bool) string {
	s := m.s
	switch {
	case sel:
		return lipgloss.NewStyle().Background(s.t.Paper).Foreground(s.t.Accent).Bold(true).Render(map[bool]string{true: " on ", false: " off "}[on])
	case on:
		return lipgloss.NewStyle().Background(s.t.Good).Foreground(s.t.OnAccent).Bold(true).Render(" on ")
	}
	return lipgloss.NewStyle().Background(s.t.Block).Foreground(s.t.Text).Bold(true).Render(" off ")
}

func (m *Model) segmented(opts []string, cur string, sel bool) string {
	s := m.s
	var parts []string
	for _, o := range opts {
		if o == cur {
			st := s.Inverse.Bold(true)
			if sel {
				st = lipgloss.NewStyle().Background(s.t.Paper).Foreground(s.t.Accent).Bold(true)
			}
			parts = append(parts, st.Render(" "+o+" "))
		} else {
			parts = append(parts, " "+o+" ")
		}
	}
	return strings.Join(parts, "")
}

func cycle(vals []int, cur int) int {
	for i, v := range vals {
		if v == cur {
			return vals[(i+1)%len(vals)]
		}
	}
	return vals[0]
}

func (m *Model) settingsRows() []settingRow {
	c := &m.cfg
	txt := func(v string) func(bool) string { return func(bool) string { return v } }
	secs := func(n int) func(bool) string { return func(bool) string { return itoa(n) + " s" } }
	dirty := func(m *Model) { m.settingsDirty = true }
	rows := []settingRow{
		{section: "Account", label: m.me.DisplayName + " · " + m.me.Email, value: func(sel bool) string {
			if m.signOut == nil {
				return m.s.Muted.Render("demo")
			}
			return zone.Mark("set:signout", m.s.Button.Render("Sign out"))
		}, change: func(m *Model) tea.Cmd {
			if m.signOut == nil {
				m.setToast("Demo mode: nothing to sign out of", false)
				return nil
			}
			m.openConfirm("Sign out and quit?", func(m *Model) tea.Cmd {
				if err := m.signOut(); err != nil {
					m.errToast(err)
					return nil
				}
				return tea.Quit
			})
			return nil
		}},
		{section: "Account", label: "Tenant", value: txt(m.tenantLabel())},

		{section: "Notifications", label: "Desktop notifications", value: func(sel bool) string { return m.chipOnOff(c.DesktopNotifications, sel) },
			change: func(m *Model) tea.Cmd { m.cfg.DesktopNotifications = !m.cfg.DesktopNotifications; dirty(m); return nil }},
		{section: "Notifications", label: "Terminal bell", value: func(sel bool) string { return m.chipOnOff(c.Bell, sel) },
			change: func(m *Model) tea.Cmd { m.cfg.Bell = !m.cfg.Bell; dirty(m); return nil }},
		{section: "Notifications", label: "Meeting reminder", value: txt(itoa(c.MeetingReminderMinutes) + " min before"),
			change: func(m *Model) tea.Cmd {
				m.cfg.MeetingReminderMinutes = cycle([]int{1, 5, 10, 15}, m.cfg.MeetingReminderMinutes)
				dirty(m)
				return nil
			}},
		{section: "Notifications", label: "Muted chats and channels", value: txt(itoa(countTrue(m.st.Muted)))},

		{section: "Polling", label: "Open conversation", value: secs(c.PollMessagesSeconds),
			change: func(m *Model) tea.Cmd {
				m.cfg.PollMessagesSeconds = cycle([]int{2, 5, 10, 30}, m.cfg.PollMessagesSeconds)
				dirty(m)
				return nil
			}},
		{section: "Polling", label: "Chat list", value: secs(c.PollChatsSeconds),
			change: func(m *Model) tea.Cmd {
				m.cfg.PollChatsSeconds = cycle([]int{5, 15, 30, 60}, m.cfg.PollChatsSeconds)
				dirty(m)
				return nil
			}},
		{section: "Polling", label: "Presence", value: secs(c.PollPresenceSeconds),
			change: func(m *Model) tea.Cmd {
				m.cfg.PollPresenceSeconds = cycle([]int{30, 60, 120, 300}, m.cfg.PollPresenceSeconds)
				dirty(m)
				return nil
			}},
		{section: "Polling", label: "Channel unread check", value: func(bool) string {
			if c.PollChannelSeconds == 0 {
				return "off"
			}
			return itoa(c.PollChannelSeconds) + " s"
		}, change: func(m *Model) tea.Cmd {
			m.cfg.PollChannelSeconds = cycle([]int{2, 4, 10, 30, 0}, m.cfg.PollChannelSeconds)
			dirty(m)
			return nil
		}},

		{section: "Appearance", label: "Theme", value: func(sel bool) string {
			cur := "light"
			if c.Theme == "dark" {
				cur = "dark"
			}
			return m.segmented([]string{"dark", "light"}, cur, sel)
		}, change: func(m *Model) tea.Cmd {
			if m.cfg.Theme == "dark" {
				m.cfg.Theme = "light"
			} else {
				m.cfg.Theme = "dark"
			}
			m.applyAppearance()
			dirty(m)
			return nil
		}},
		{section: "Appearance", label: "Icons", value: func(sel bool) string {
			cur := "Nerd Font"
			if c.Icons == "ascii" {
				cur = "ASCII"
			}
			return m.segmented([]string{"Nerd Font", "ASCII"}, cur, sel)
		}, change: func(m *Model) tea.Cmd {
			if m.cfg.Icons == "ascii" {
				m.cfg.Icons = "nerd"
			} else {
				m.cfg.Icons = "ascii"
			}
			m.applyAppearance()
			dirty(m)
			return nil
		}},
		{section: "Appearance", label: "Channels and chats in one list", value: func(sel bool) string { return m.chipOnOff(c.CombinedChat, sel) },
			change: func(m *Model) tea.Cmd { m.cfg.CombinedChat = !m.cfg.CombinedChat; dirty(m); return nil }},
		{section: "Appearance", label: "Sidebar width", value: txt(itoa(c.SidebarWidth)),
			change: func(m *Model) tea.Cmd {
				m.cfg.SidebarWidth = cycle([]int{28, 30, 33, 36, 40}, m.cfg.SidebarWidth)
				m.invalidateRender()
				dirty(m)
				return nil
			}},
		{section: "Appearance", label: "Time format", value: txt(c.TimeFormat),
			change: func(m *Model) tea.Cmd {
				if m.cfg.TimeFormat == "15:04" {
					m.cfg.TimeFormat = "3:04 PM"
				} else {
					m.cfg.TimeFormat = "15:04"
				}
				m.invalidateRender()
				dirty(m)
				return nil
			}},

		{section: "Keys", label: "Keyboard and mouse shortcuts", value: func(bool) string { return m.s.Button.Render("? show") },
			change: func(m *Model) tea.Cmd { m.openHelp(); return nil }},

		{section: "About", label: "teams-tui", value: txt(version)},
	}
	return rows
}

func countTrue(m map[string]bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}

func (m *Model) tenantLabel() string {
	if t := m.cfg.Tenant; t != "" && t != "organizations" && t != "common" {
		return t
	}
	if id := m.svc.TenantID(); id != "" {
		return id
	}
	return "your work account"
}

// applyAppearance rebuilds styles after a theme or icon change.
func (m *Model) applyAppearance() {
	m.s = newStyles(m.cfg.Theme != "dark", m.cfg.Icons)
	m.invalidateRender()
}

func (m *Model) settingsJump(section string) {
	for i, r := range m.settingsRows() {
		if r.section == section && r.change != nil {
			m.setSel = i
			break
		}
	}
	m.sideSel[SecSettings] = "set:" + section
	m.sideTouched[SecSettings] = true
	m.focus = FocusMessages
	m.compose.ta.Blur()
}

func (m *Model) settingsMove(d int) {
	rows := m.settingsRows()
	i := m.setSel
	for {
		i += d
		if i < 0 || i >= len(rows) {
			return
		}
		if rows[i].change != nil {
			m.setSel = i
			m.sideSel[SecSettings] = "set:" + rows[i].section
			return
		}
	}
}

func (m *Model) saveSettings() {
	if m.demo {
		m.settingsDirty = false
		m.setToast("Demo mode: settings apply for this session only", false)
		return
	}
	if err := config.Save(m.cfg); err != nil {
		m.errToast(err)
		return
	}
	m.settingsDirty = false
	m.setToast("Settings saved", false)
}

func (m *Model) settingsKey(k string) tea.Cmd {
	switch k {
	case "up", "k":
		m.settingsMove(-1)
	case "down", "j":
		m.settingsMove(1)
	case "enter", " ":
		rows := m.settingsRows()
		if m.setSel >= 0 && m.setSel < len(rows) && rows[m.setSel].change != nil {
			return rows[m.setSel].change(m)
		}
	case "ctrl+s":
		m.saveSettings()
	case "esc", "left", "h":
		m.setFocus(FocusSidebar)
	}
	return nil
}

func (m *Model) settingsView(w, h int) []string {
	s := m.s
	bar := lipgloss.NewStyle().Background(s.t.Block).Foreground(s.t.Text)
	lines := []string{withBg(spread(" "+s.Title.Render("SETTINGS"), s.Button.Render("enter change · ctrl+s save")+" ", w), styleOn(bar), w)}
	rw := min(w-2, 66)
	rows := m.settingsRows()
	if m.setSel < 0 || m.setSel >= len(rows) || rows[m.setSel].change == nil {
		m.settingsMove(1)
	}
	section := ""
	for i, r := range rows {
		if r.section != section {
			section = r.section
			lines = append(lines, "", "  "+s.Muted.Bold(true).Render(section))
		}
		sel := i == m.setSel && m.focus == FocusMessages
		val := r.value(sel)
		label := r.label
		if sel {
			if !strings.Contains(val, "\x1b[48") {
				val = s.Sel.Render(ansi.Strip(val))
			}
			label = s.Sel.Render(label)
			row := spread(s.Sel.Render("  ")+label, val, rw)
			lines = append(lines, zone.Mark("setrow:"+itoa(i), withBg(row, s.bgAccent, rw)))
			continue
		}
		lines = append(lines, zone.Mark("setrow:"+itoa(i), "  "+spread(label, val, rw-2)))
	}
	p, _ := config.Path()
	status := "Saved to " + tildePath(p)
	if m.settingsDirty {
		status = "Unsaved changes · ctrl+s save"
	}
	lines = append(lines, "", "  "+s.Muted.Render(status))
	_ = ansi.StringWidth
	return lines
}

func tildePath(p string) string {
	if home, err := userHome(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
