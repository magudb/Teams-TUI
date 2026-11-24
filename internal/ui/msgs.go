package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/magudb/Teams-TUI/internal/model"
)

// async result messages

type meMsg struct {
	me  model.User
	err error
}
type myPresenceMsg struct {
	p   model.Presence
	err error
}
type chatsMsg struct {
	next  model.Page
	more  bool
	chats []model.Chat
	err   error
}
type teamsMsg struct {
	teams []model.Team
	err   error
}
type channelsMsg struct {
	teamID   string
	channels []model.Channel
	err      error
}
type messagesMsg struct {
	target  *convView
	request uint64
	ref     model.ConvRef
	msgs    []model.Message
	next    model.Page
	older   bool
	poll    bool
	err     error
}
type sentMsg struct {
	ref     model.ConvRef
	msg     model.Message
	err     error
	tempID  string
	editing bool
}
type presenceMsg struct {
	p   map[string]model.Presence
	err error
}
type eventsMsg struct {
	request uint64
	events  []model.Event
	err     error
}
type filesMsg struct {
	request uint64
	key     string
	items   []model.DriveItem
	err     error
}
type searchMsg struct {
	request uint64
	q       string
	hits    []model.SearchHit
	err     error
}
type peopleMsg struct {
	q     string
	users []model.User
	err   error
}
type membersMsg struct {
	ref   model.ConvRef
	users []model.User
	err   error
}
type chatCreatedMsg struct {
	chat model.Chat
	err  error
}
type uploadMsg struct {
	editSession uint64
	draft       uint64
	ref         model.ConvRef
	att         model.Attachment
	err         error
}

// opMsg is a generic "operation finished" message.
type opMsg struct {
	toast       string
	err         error
	reload      model.ConvRef // reload this conversation afterwards
	chats       bool          // reload chat list
	events      bool          // reload calendar
	presenc     bool          // reload my presence
	open        string        // open this file/url
	reloadFiles bool
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

const callTimeout = 45 * time.Second

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), callTimeout)
}
