// Package notify sends desktop notifications and opens URLs/files.
package notify

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
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

// Ask shows a notification with one action button and blocks until the user
// activates it, the notification closes, or ctx ends. It reports whether the
// action was chosen. Where notifications can't carry actions it shows a plain
// one and returns false.
func Ask(ctx context.Context, title, body, action string, urgent bool, expire time.Duration) bool {
	title = truncate(title, 80)
	body = truncate(body, 200)
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd":
	default:
		Desktop(title, body)
		return false
	}
	args := []string{
		"--app-name=teams-tui",
		"--action=default=" + action,
		"--expire-time=" + strconv.FormatInt(expire.Milliseconds(), 10),
	}
	if urgent {
		args = append(args, "--urgency=critical")
	}
	cmd := exec.CommandContext(ctx, "notify-send", append(args, title, body)...)
	// SIGINT makes notify-send close the notification it is waiting on
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == nil {
			Desktop(title, body) // notify-send without action support
		}
		return false
	}
	return strings.TrimSpace(string(out)) == "default"
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
