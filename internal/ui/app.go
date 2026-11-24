// Package ui implements the bubbletea terminal user interface.
package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/config"
	"github.com/magudb/Teams-TUI/internal/model"
	"github.com/magudb/Teams-TUI/internal/state"
)

// Section is a left-rail app.
type Section int

const (
	SecActivity Section = iota
	SecChat
	SecTeams
	SecCalendar
	SecFiles
	SecSearch
	numSections
)

var sectionNames = [...]string{"Activity", "Chat", "Teams", "Calendar", "Files", "Search"}
var sectionIcons = [...]string{"🔔", "💬", "👥", "📅", "📁", "🔍"}
var sectionASCII = [...]string{"!", "#", "@", "=", "&", "?"}

// Focus is the focused pane.
type Focus int

const (
	FocusSidebar Focus = iota
	FocusMessages
	FocusCompose
)

type activityItem struct {
	at      time.Time
	kind    string // mention, message, reply
	title   string
	preview string
	ref     model.ConvRef
	msgID   string
	read    bool
}

// Model is the root bubbletea model.
type Model struct {
	svc  model.Service
	cfg  config.Config
	st   *state.State
	s    styles
	demo bool

	w, h       int
	ready      bool
	focused    bool // terminal focus
	me         model.User
	myPresence model.Presence
	presence   map[string]model.Presence
	users      map[string]model.User

	section     Section
	focus       Focus
	showSidebar bool

	// data
	chats       []model.Chat
	chatsLoaded bool
	lastPreview map[string]string // chat id -> last message id (for notifications)
	teams       []model.Team
	channels    map[string][]model.Channel
	activity    []activityItem
	events      []model.Event
	calDay      time.Time
	selEvent    string
	files       []model.DriveItem
	fileStack   []model.DriveItem
	filesKey    string
	selFile     string
	searchInput textinput.Model
	searchQ     string
	hits        []model.SearchHit
	chatFilter  textinput.Model
	filtering   bool

	// sidebar selection per section (by item key) and scroll offsets
	sideSel    map[Section]string
	sideScroll map[Section]int

	// conversations
	conv     *convView
	thread   *convView
	convs    map[string]*convView // cache by ref key
	members  map[string][]model.User
	mainTab  int // 0 = messages, 1 = files
	tabFiles []model.DriveItem

	compose composer

	modal *modal

	toast      string
	toastErr   bool
	toastUntil time.Time
	spinner    spinner.Model
	busy       int

	geo           geometry
	pendingCmd    tea.Cmd
	pendingSelect string
	dragKey       string           // sidebar item being pressed/dragged
	sideTouched   map[Section]bool // user has chosen a selection (otherwise it follows the first item)

	// meetings
	emailUsers   map[string]model.User // lower-cased email -> directory user
	resolving    map[string]bool
	artifacts    map[string]*artifactState // event id -> transcripts/recordings
	chatTitles   map[string]string         // titles for chats not in the chat list (meeting chats)
	lastEventSel string

	// channel unread tracking
	chanAct          map[string]chanActivity
	chanPollIdx      int
	pollingChannel   bool
	lastChanPoll     time.Time
	chanPollDisabled bool

	lastPollMsgs, lastPollChats, lastPollPresence, lastPollEvents time.Time
	pollingMsgs, pollingChats                                     bool
}

// New creates the root model.
func New(svc model.Service, cfg config.Config, st *state.State, demo bool) *Model {
	zone.NewGlobal()
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	si := textinput.New()
	si.Placeholder = "Search messages…"
	si.Prompt = "🔍 "
	cf := textinput.New()
	cf.Placeholder = "Filter chats"
	cf.Prompt = "› "
	m := &Model{
		svc: svc, cfg: cfg, st: st, demo: demo,
		s:           newStyles(cfg.Theme == "light"),
		presence:    map[string]model.Presence{},
		users:       map[string]model.User{},
		channels:    map[string][]model.Channel{},
		convs:       map[string]*convView{},
		members:     map[string][]model.User{},
		lastPreview: map[string]string{},
		sideSel:     map[Section]string{},
		sideScroll:  map[Section]int{},
		emailUsers:  map[string]model.User{},
		resolving:   map[string]bool{},
		artifacts:   map[string]*artifactState{},
		chatTitles:  map[string]string{},
		chanAct:     map[string]chanActivity{},
		sideTouched: map[Section]bool{},
		section:     SecChat,
		showSidebar: true,
		focused:     true,
		calDay:      today(),
		spinner:     sp,
		searchInput: si,
		chatFilter:  cf,
	}
	if st != nil {
		for i, n := range sectionNames {
			if n == st.Section && Section(i) != SecSearch {
				m.section = Section(i)
			}
		}
	}
	m.compose = newComposer(m)
	return m
}

func today() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
}

// Init starts initial loads.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadMe(), m.loadMyPresence(), m.loadChats(), m.loadTeams(),
		m.loadEvents(m.calDay), tick(), m.spinner.Tick, tea.SetWindowTitle("Teams"),
	)
}

func (m *Model) setToast(s string, isErr bool) {
	m.toast = s
	m.toastErr = isErr
	d := 4 * time.Second
	if isErr {
		d = 8 * time.Second
	}
	m.toastUntil = time.Now().Add(d)
}

func (m *Model) errToast(err error) {
	if err != nil {
		m.setToast(oneLineErr(err), true)
	}
}

func oneLineErr(err error) string {
	s := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func (m *Model) rememberUsers(us ...model.User) {
	for _, u := range us {
		if u.ID == "" {
			continue
		}
		old, ok := m.users[u.ID]
		if !ok || old.DisplayName == "" || (old.Email == "" && u.Email != "") {
			if u.DisplayName == "" && ok {
				u.DisplayName = old.DisplayName
			}
			m.users[u.ID] = u
		}
	}
}

func (m *Model) userName(id string) string {
	if id == m.me.ID {
		return "You"
	}
	if u, ok := m.users[id]; ok && u.DisplayName != "" {
		return u.DisplayName
	}
	return "Someone"
}

// activeConv is the conversation the compose box and message keys target.
func (m *Model) activeConv() *convView {
	if m.thread != nil {
		return m.thread
	}
	return m.conv
}

func (m *Model) saveState() {
	if m.st == nil {
		return
	}
	m.st.Section = sectionNames[m.section]
	_ = m.st.Save()
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ready = true
		m.invalidateRender()
		m.compose.resize()
		return m, nil

	case tea.FocusMsg:
		m.focused = true
		return m, nil
	case tea.BlurMsg:
		m.focused = false
		return m, nil

	case spinner.TickMsg:
		var c tea.Cmd
		m.spinner, c = m.spinner.Update(msg)
		return m, c

	case tickMsg:
		return m, tea.Batch(m.onTick(time.Time(msg)), tick())

	case tea.KeyMsg:
		return m, m.handleKey(msg)

	case tea.MouseMsg:
		return m, m.handleMouse(msg)
	}

	if c := m.handleData(msg); c != nil {
		cmds = append(cmds, c)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) onTick(now time.Time) tea.Cmd {
	var cmds []tea.Cmd
	m.checkUpcoming()
	if !m.toastUntil.IsZero() && now.After(m.toastUntil) {
		m.toast = ""
		m.toastUntil = time.Time{}
	}
	if ac := m.activeConv(); ac != nil && !m.pollingMsgs && now.Sub(m.lastPollMsgs) >= time.Duration(m.cfg.PollMessagesSeconds)*time.Second {
		m.lastPollMsgs = now
		m.pollingMsgs = true
		cmds = append(cmds, m.loadMessages(ac.ref, "", false, true))
		if m.thread != nil && m.conv != nil {
			cmds = append(cmds, m.loadMessages(m.conv.ref, "", false, true))
		}
	}
	if !m.pollingChats && now.Sub(m.lastPollChats) >= time.Duration(m.cfg.PollChatsSeconds)*time.Second {
		m.lastPollChats = now
		m.pollingChats = true
		cmds = append(cmds, m.loadChats())
	}
	if now.Sub(m.lastPollPresence) >= time.Duration(m.cfg.PollPresenceSeconds)*time.Second {
		m.lastPollPresence = now
		cmds = append(cmds, m.loadPresence(), m.loadMyPresence())
	}
	if iv := time.Duration(m.cfg.PollChannelSeconds) * time.Second; iv > 0 && !m.chanPollDisabled && !m.pollingChannel && now.Sub(m.lastChanPoll) >= iv {
		m.lastChanPoll = now
		cmds = append(cmds, m.pollNextChannel())
	}
	if now.Sub(m.lastPollEvents) >= 5*time.Minute {
		m.lastPollEvents = now
		cmds = append(cmds, m.loadEvents(m.calDay))
	}
	return tea.Batch(cmds...)
}

// Run starts the program.
func Run(m *Model) error {
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus())
	_, err := p.Run()
	m.compose.saveDraft()
	m.saveState()
	return err
}
