package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/magudb/Teams-TUI/internal/auth"
	"github.com/magudb/Teams-TUI/internal/config"
	"github.com/muesli/termenv"
)

func TestLoginScreen(t *testing.T) {
	cfg := config.Default()
	cfg.Tenants = []string{"contoso.example"}
	if d := os.Getenv("TUI_DUMP"); d != "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
		defer lipgloss.SetColorProfile(termenv.Ascii)
	}
	lm := &loginModel{
		a: auth.New(config.DefaultClientID, "organizations", t.TempDir()), cfg: &cfg,
		s: newStyles(true, "nerd"), tenants: []string{"organizations", "contoso.example"},
		w: 120, h: 36, started: time.Now(),
		dc: &auth.DeviceCode{UserCode: "H7KQ2MXD", VerificationURI: "https://microsoft.com/devicelogin", ExpiresIn: 900},
	}
	out := lm.View()
	if d := os.Getenv("TUI_DUMP"); d != "" {
		_ = os.WriteFile(d+"/login.ans", []byte(out), 0o644)
	}
	s := ansi.Strip(out)
	for _, want := range []string{"teams-tui", "Sign in with your Microsoft 365 account", "microsoft.com/devicelogin",
		"H7KQ2MXD", "Waiting for sign-in · code expires in 1", "Any work or school account", "contoso.example",
		"Other tenant id or domain", "Microsoft Graph Command Line Tools", "d demo mode", "not signed in"} {
		if !strings.Contains(s, want) {
			t.Fatalf("login screen missing %q:\n%s", want, s)
		}
	}
	checkSize(t, s, 120, 36)
}

func TestLoginKeepsActiveCodeUntilExplicitRetry(t *testing.T) {
	cfg := config.Default()
	l := &loginModel{a: auth.New("test", "organizations", t.TempDir()), cfg: &cfg, s: newStyles(true, "ascii"), tenants: []string{"organizations"}, w: 120, h: 36}
	l.start()
	defer func() { l.cancel() }()
	gen := l.gen
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := l.Update(enter)
	if cmd != nil || l.gen != gen {
		t.Fatal("Enter replaced an in-flight request")
	}
	issued := auth.DeviceCode{UserCode: "TESTCODE", VerificationURI: "https://example.test/verify", ExpiresIn: 900}
	l.Update(deviceCodeMsg{gen, issued})
	_, cmd = l.Update(enter)
	if cmd != nil || l.gen != gen || !strings.Contains(ansi.Strip(l.View()), issued.UserCode) {
		t.Fatal("Enter replaced the displayed code")
	}
	l.started = time.Now().Add(-time.Hour)
	if l.codeActive() || strings.Contains(l.View(), issued.UserCode) {
		t.Fatal("expired code still offered")
	}
	l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if l.gen == gen || l.dc != nil || l.toast != "" {
		t.Fatal("retry did not clear the attempt")
	}
	l.Update(deviceCodeMsg{gen, issued})
	if l.dc != nil {
		t.Fatal("stale response restored the previous code")
	}
	l.Update(deviceCodeMsg{l.gen, issued})
	l.Update(loginDoneMsg{l.gen, errors.New("sign-in failed")})
	if l.codeActive() || strings.Contains(l.View(), issued.UserCode) {
		t.Fatal("failed attempt retained a usable code")
	}
}
