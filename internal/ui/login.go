package ui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/magudb/Teams-TUI/internal/auth"
	"github.com/magudb/Teams-TUI/internal/config"
	"github.com/magudb/Teams-TUI/internal/notify"
)

// LoginResult is what the sign-in screen ended with.
type LoginResult int

const (
	LoginOK LoginResult = iota
	LoginDemo
	LoginQuit
)

type deviceCodeMsg struct {
	gen int
	dc  auth.DeviceCode
}

type loginDoneMsg struct {
	gen int
	err error
}

type loginModel struct {
	a       *auth.Authenticator
	cfg     *config.Config
	s       styles
	w, h    int
	send    func(tea.Msg)
	gen     int
	cancel  context.CancelFunc
	dc      *auth.DeviceCode
	started time.Time
	err     string
	toast   string
	tenants []string
	sel     int
	editing bool
	input   textinput.Model
	spin    spinner.Model
	result  LoginResult
}

func tenantLabel(t string) (string, string) {
	switch t {
	case "organizations":
		return "Any work or school account", "organizations"
	case "common":
		return "Any Microsoft account", "common"
	}
	return t, ""
}

// RunLogin shows the device-code sign-in screen until the user signs in,
// picks demo mode or quits.
func RunLogin(a *auth.Authenticator, cfg *config.Config) (LoginResult, error) {
	tenants := []string{}
	seen := map[string]bool{}
	for _, t := range append([]string{cfg.Tenant}, cfg.Tenants...) {
		if t != "" && !seen[t] {
			seen[t] = true
			tenants = append(tenants, t)
		}
	}
	if !seen["organizations"] {
		tenants = append(tenants, "organizations")
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	in := textinput.New()
	in.Placeholder = "contoso.onmicrosoft.com or tenant id"
	lm := &loginModel{a: a, cfg: cfg, s: newStyles(cfg.Theme != "dark", cfg.Icons), tenants: tenants, input: in, spin: sp, result: LoginQuit}
	p := tea.NewProgram(lm, tea.WithAltScreen())
	lm.send = p.Send
	_, err := p.Run()
	if lm.cancel != nil {
		lm.cancel()
	}
	return lm.result, err
}

func (l *loginModel) start() tea.Cmd {
	if l.cancel != nil {
		l.cancel()
	}
	l.gen++
	gen := l.gen
	l.dc = nil
	l.err = ""
	l.a.Tenant = l.tenants[l.sel]
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	a, send := l.a, l.send
	return func() tea.Msg {
		err := a.Login(ctx, func(dc auth.DeviceCode) { send(deviceCodeMsg{gen, dc}) })
		return loginDoneMsg{gen, err}
	}
}

func (l *loginModel) Init() tea.Cmd { return tea.Batch(l.start(), l.spin.Tick, tick()) }

func (l *loginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.w, l.h = msg.Width, msg.Height
	case spinner.TickMsg:
		var c tea.Cmd
		l.spin, c = l.spin.Update(msg)
		return l, c
	case tickMsg:
		return l, tick()
	case deviceCodeMsg:
		if msg.gen == l.gen {
			dc := msg.dc
			l.dc = &dc
			l.started = time.Now()
		}
	case loginDoneMsg:
		if msg.gen != l.gen {
			return l, nil
		}
		if msg.err != nil {
			if msg.err == context.Canceled {
				return l, nil
			}
			l.err = oneLineErr(msg.err)
			return l, nil
		}
		t := l.tenants[l.sel]
		if t != "organizations" && t != "common" {
			found := false
			for _, x := range l.cfg.Tenants {
				found = found || x == t
			}
			if !found {
				l.cfg.Tenants = append(l.cfg.Tenants, t)
			}
			l.cfg.Tenant = t
			_ = config.Save(*l.cfg)
		}
		l.result = LoginOK
		return l, tea.Quit
	case tea.KeyMsg:
		if l.editing {
			switch msg.String() {
			case "enter":
				v := strings.TrimSpace(l.input.Value())
				l.editing = false
				l.input.Blur()
				if v != "" {
					l.tenants = append(l.tenants, v)
					l.sel = len(l.tenants) - 1
					return l, l.start()
				}
				return l, nil
			case "esc":
				l.editing = false
				l.input.Blur()
				return l, nil
			}
			var c tea.Cmd
			l.input, c = l.input.Update(msg)
			return l, c
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			l.result = LoginQuit
			return l, tea.Quit
		case "d":
			l.result = LoginDemo
			return l, tea.Quit
		case "c":
			if l.dc != nil {
				if err := copyToClipboard(l.dc.UserCode); err != nil {
					l.toast = "copy failed: " + oneLineErr(err)
				} else {
					l.toast = "code copied"
				}
			}
		case "o":
			if l.dc != nil {
				_ = notify.Open(l.dc.VerificationURI)
				l.toast = "opened in your browser"
			}
		case "r":
			return l, l.start()
		case "up", "k":
			if l.sel > 0 {
				l.sel--
				return l, l.start()
			}
		case "down", "j":
			if l.sel < len(l.tenants) {
				l.sel++
				if l.sel == len(l.tenants) {
					l.editing = true
					l.input.SetValue("")
					l.input.Focus()
					l.sel--
					return l, nil
				}
				return l, l.start()
			}
		case "enter":
			return l, l.start()
		}
	}
	return l, nil
}

func (l *loginModel) View() string {
	if l.w == 0 {
		return ""
	}
	s := l.s
	bw := min(70, l.w-4)
	var b []string
	spin := "·"
	if len(l.spin.Spinner.Frames) > 0 {
		spin = l.spin.View()
	}
	b = append(b, s.Inverse.Bold(true).Render(" teams-tui ")+" "+s.Muted.Render(version), "",
		s.Bold.Render("Sign in with your Microsoft 365 account"), "")
	uri := "microsoft.com/devicelogin"
	code := "········"
	if l.dc != nil {
		uri = strings.TrimPrefix(strings.TrimPrefix(l.dc.VerificationURI, "https://"), "www.")
		code = l.dc.UserCode
	}
	b = append(b, s.Bold.Render("1")+" Open "+s.html.Link.Render(uri)+" "+s.Muted.Render("o opens it in your browser"),
		s.Bold.Render("2")+" Enter this code "+s.Muted.Render("c copies it"), "")
	spaced := strings.Join(strings.Split(code, ""), " ")
	boxW := ansi.StringWidth(spaced) + 8
	codeSt := s.Inverse.Bold(true)
	b = append(b, "   "+codeSt.Render(strings.Repeat(" ", boxW)), "   "+codeSt.Render(padRight("    "+spaced, boxW)), "   "+codeSt.Render(strings.Repeat(" ", boxW)), "")
	switch {
	case l.err != "":
		for _, e := range strings.Split(ansi.Wrap(l.err, bw-2, " "), "\n") {
			b = append(b, s.Danger.Render(e))
		}
		b = append(b, s.Muted.Render("r tries again"))
	case l.dc == nil:
		b = append(b, s.Accent.Render(spin)+" Requesting a sign-in code…")
	default:
		left := time.Until(l.started.Add(time.Duration(l.dc.ExpiresIn) * time.Second)).Round(time.Second)
		mins, secs := int(left.Minutes()), int(left.Seconds())%60
		ss := itoa(secs)
		if secs < 10 {
			ss = "0" + ss
		}
		b = append(b, s.Accent.Render(spin)+" Waiting for sign-in · code expires in "+itoa(max(0, mins))+":"+ss)
	}
	if l.toast != "" {
		b = append(b, s.Muted.Render(l.toast))
	}
	b = append(b, "", s.Muted.Bold(true).Render("Tenant"))
	for i, t := range l.tenants {
		name, dom := tenantLabel(t)
		row := spread(" "+name, s.Muted.Render(dom)+" ", bw)
		if i == l.sel {
			row = withBg(s.Sel.Render(spread(" "+name, dom+" ", bw)), s.bgAccent, bw)
		}
		b = append(b, row)
	}
	if l.editing {
		l.input.Width = bw - 4
		l.input.Prompt = s.Accent.Render("› ")
		b = append(b, " "+l.input.View())
	} else {
		b = append(b, " "+s.Text.Render("Other tenant id or domain…"))
	}
	client := "Microsoft Graph Command Line Tools"
	if l.a.ClientID != config.DefaultClientID {
		client = l.a.ClientID
	}
	b = append(b, "", s.Muted.Render(trunc("Client: "+client+" · change with --client-id", bw)))

	// center the block
	top := max(0, (l.h-1-len(b))/2)
	left := max(0, (l.w-bw)/2)
	lines := make([]string, l.h-1)
	for i := range lines {
		if j := i - top; j >= 0 && j < len(b) {
			lines[i] = strings.Repeat(" ", left) + b[j]
		}
	}
	body := paint(lines, l.w, s.bgPaper)
	ink := lipgloss.NewStyle().Background(s.t.Ink).Foreground(s.t.OnInk)
	status := ink.Render(spread(" c copy code · o open browser · ↑↓ tenant · d demo mode · q quit", "not signed in ", l.w))
	return strings.Join(body, "\n") + "\n" + status
}
