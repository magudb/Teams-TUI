package ui

import (
	"os"
	"strings"
	"testing"
	"time"

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
		"H 7 K Q 2 M X D", "Waiting for sign-in · code expires in 1", "Any work or school account", "contoso.example",
		"Other tenant id or domain", "Microsoft Graph Command Line Tools", "d demo mode", "not signed in"} {
		if !strings.Contains(s, want) {
			t.Fatalf("login screen missing %q:\n%s", want, s)
		}
	}
	checkSize(t, s, 120, 36)
}
