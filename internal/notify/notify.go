// Package notify sends desktop notifications and opens URLs/files.
package notify

import (
	"os/exec"
	"runtime"
	"strings"
)

// Desktop shows a native notification (best effort, non-blocking).
func Desktop(title, body string) {
	title = truncate(title, 80)
	body = truncate(body, 200)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		script := `display notification "` + escAS(body) + `" with title "` + escAS(title) + `" subtitle "Teams TUI"`
		cmd = exec.Command("osascript", "-e", script)
	case "linux", "freebsd", "openbsd":
		cmd = exec.Command("notify-send", "--app-name=teams-tui", title, body)
	case "windows":
		ps := `[reflection.assembly]::loadwithpartialname('System.Windows.Forms') | Out-Null;` +
			`$n=New-Object System.Windows.Forms.NotifyIcon;$n.Icon=[System.Drawing.SystemIcons]::Information;$n.Visible=$true;` +
			`$n.ShowBalloonTip(5000,'` + strings.ReplaceAll(title, "'", "''") + `','` + strings.ReplaceAll(body, "'", "''") + `','Info')`
		cmd = exec.Command("powershell", "-NoProfile", "-Command", ps)
	default:
		return
	}
	_ = cmd.Start()
	go func() { _ = cmd.Wait() }()
}

// Open opens a URL or file with the OS default handler.
func Open(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func escAS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
