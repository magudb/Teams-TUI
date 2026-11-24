// Command teams-tui is a terminal client for Microsoft Teams.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/charmbracelet/x/term"
	"github.com/magudb/Teams-TUI/internal/auth"
	"github.com/magudb/Teams-TUI/internal/config"
	"github.com/magudb/Teams-TUI/internal/demo"
	"github.com/magudb/Teams-TUI/internal/graph"
	"github.com/magudb/Teams-TUI/internal/notify"
	"github.com/magudb/Teams-TUI/internal/state"
	"github.com/magudb/Teams-TUI/internal/ui"
)

var version = "dev"

const usage = `teams-tui — Microsoft Teams in your terminal

Usage:
  teams-tui [flags]          start the app (logs in on first run)
  teams-tui login            sign in with a device code
  teams-tui logout           forget the cached token
  teams-tui config           print the config file path
  teams-tui version

Flags:
  --demo                     run with built-in sample data (no account needed)
  --client-id <id>           Azure app registration (overrides config)
  --tenant <id|domain>       tenant (default "organizations")
`

func main() {
	fs := flag.NewFlagSet("teams-tui", flag.ExitOnError)
	demoMode := fs.Bool("demo", false, "use sample data")
	clientID := fs.String("client-id", "", "Azure AD application (client) id")
	tenant := fs.String("tenant", "", "tenant id or domain")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	_ = fs.Parse(os.Args[1:])

	cfg, err := config.Load()
	if err != nil {
		fail(fmt.Errorf("config: %w", err))
	}
	if *clientID != "" {
		cfg.ClientID = *clientID
	}
	if *tenant != "" {
		cfg.Tenant = *tenant
	}
	dir, err := config.Dir()
	if err != nil {
		fail(err)
	}

	cmd := fs.Arg(0)
	switch cmd {
	case "version":
		fmt.Println("teams-tui", version)
		return
	case "config":
		p, _ := config.Path()
		fmt.Println(p)
		return
	case "help":
		fs.Usage()
		return
	}

	ui.SetVersion(version)
	runDemo := func() {
		m := ui.New(demo.New(), cfg, state.Load(""), true)
		if err := ui.Run(m); err != nil {
			fail(err)
		}
	}
	if *demoMode {
		runDemo()
		return
	}

	a := auth.New(cfg.ClientID, cfg.Tenant, dir)
	switch cmd {
	case "logout":
		if err := a.Logout(); err != nil {
			fail(err)
		}
		fmt.Println("Signed out.")
		return
	case "login":
		switch signIn(a, &cfg) {
		case ui.LoginDemo:
			runDemo()
		case ui.LoginOK:
			fmt.Println("Signed in. Run `teams-tui` to start.")
		}
		return
	case "":
	default:
		fs.Usage()
		os.Exit(2)
	}

	if !a.LoggedIn() {
		switch signIn(a, &cfg) {
		case ui.LoginDemo:
			runDemo()
			return
		case ui.LoginQuit:
			return
		}
	}
	client := graph.New(a)
	// validate the session before taking over the screen
	ctx, cancel := context.WithTimeout(context.Background(), 30e9)
	_, err = client.Me(ctx)
	cancel()
	if err != nil {
		if errors.Is(err, auth.ErrNotLoggedIn) || graph.IsStatus(err, 401) {
			fmt.Fprintln(os.Stderr, "Session expired, signing in again…")
			switch signIn(a, &cfg) {
			case ui.LoginDemo:
				runDemo()
				return
			case ui.LoginQuit:
				return
			}
		} else {
			fail(fmt.Errorf("could not reach Microsoft Graph: %w", err))
		}
	}
	m := ui.New(client, cfg, state.Load(dir), false)
	m.SetSignOut(a.Logout)
	if err := ui.Run(m); err != nil {
		fail(err)
	}
}

// signIn shows the sign-in screen in a terminal, or prints the device code
// when there is no terminal.
func signIn(a *auth.Authenticator, cfg *config.Config) ui.LoginResult {
	if term.IsTerminal(os.Stdout.Fd()) {
		res, err := ui.RunLogin(a, cfg)
		if err != nil {
			fail(err)
		}
		return res
	}
	if err := login(a); err != nil {
		fail(err)
	}
	return ui.LoginOK
}

func login(a *auth.Authenticator) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return a.Login(ctx, func(dc auth.DeviceCode) {
		fmt.Println()
		fmt.Println("  To sign in to Microsoft Teams, open")
		fmt.Println()
		fmt.Println("      " + dc.VerificationURI)
		fmt.Println()
		fmt.Println("  and enter the code:  " + dc.UserCode)
		fmt.Println()
		fmt.Println("  Waiting for you to finish signing in… (ctrl+c to cancel)")
		_ = notify.Open(dc.VerificationURI)
	})
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "teams-tui:", err)
	os.Exit(1)
}
