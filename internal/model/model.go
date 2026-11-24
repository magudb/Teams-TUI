// Package model holds the backend-agnostic domain types used by the UI and
// implemented by the Microsoft Graph and demo backends.
package model

import (
	"context"
	"time"
)

// User is a person in the tenant.
type User struct {
	ID          string
	DisplayName string
	Email       string
	JobTitle    string
}

// Presence availability values as used by Microsoft Graph.
const (
	Available       = "Available"
	Busy            = "Busy"
	DoNotDisturb    = "DoNotDisturb"
	BeRightBack     = "BeRightBack"
	Away            = "Away"
	Offline         = "Offline"
	PresenceUnknown = "PresenceUnknown"
)

// Presence is a user's availability.
type Presence struct {
	UserID        string
	Availability  string
	Activity      string
	StatusMessage string
}

// Team is a joined team.
type Team struct {
	ID          string
	DisplayName string
	Description string
}

// Channel is a channel in a team.
type Channel struct {
	ID             string
	TeamID         string
	DisplayName    string
	Description    string
	MembershipType string
}

// Chat is a 1:1, group, or meeting chat.
type Chat struct {
	ID          string
	Topic       string
	ChatType    string // oneOnOne, group, meeting
	Members     []User
	LastMessage *MessagePreview
	LastRead    time.Time
	Updated     time.Time // lastUpdatedDateTime
	WebURL      string
	JoinURL     string // meeting chats: the online meeting join link
}

// MessagePreview is the last message of a chat.
type MessagePreview struct {
	ID      string
	From    User
	Text    string
	Created time.Time
	Deleted bool
}

// Unread reports whether the chat has a message newer than the last read time
// that was not sent by me.
func (c Chat) Unread(meID string) bool {
	if c.LastMessage == nil {
		return false
	}
	if c.LastMessage.From.ID == meID {
		return false
	}
	return c.LastMessage.Created.After(c.LastRead)
}

// Title returns a human readable chat title.
func (c Chat) Title(meID string) string {
	if c.Topic != "" {
		return c.Topic
	}
	var names []string
	for _, m := range c.Members {
		if m.ID == meID {
			continue
		}
		names = append(names, m.DisplayName)
	}
	if len(names) == 0 {
		return "(just you)"
	}
	out := names[0]
	for i := 1; i < len(names); i++ {
		out += ", " + names[i]
	}
	return out
}

// Other returns the other member of a 1:1 chat.
func (c Chat) Other(meID string) (User, bool) {
	if c.ChatType != "oneOnOne" {
		return User{}, false
	}
	for _, m := range c.Members {
		if m.ID != meID {
			return m, true
		}
	}
	return User{}, false
}

// Reaction is an emoji reaction on a message.
type Reaction struct {
	Type    string // unicode emoji (legacy names are normalized)
	UserID  string
	Created time.Time
}

// Attachment is a file, card or quoted message attached to a message.
type Attachment struct {
	ID          string
	ContentType string // reference, messageReference, application/vnd.microsoft.card.*
	ContentURL  string
	Name        string
	Content     string
}

// Mention is an @mention inside a message.
type Mention struct {
	ID     int
	Text   string
	UserID string
}

// Message is a chat or channel message.
type Message struct {
	ID          string
	ReplyToID   string
	MessageType string // message, systemEventMessage
	From        User
	Created     time.Time
	Modified    time.Time
	Edited      bool
	Deleted     bool
	Subject     string
	Importance  string // normal, high, urgent
	ContentType string // html, text
	Body        string
	Reactions   []Reaction
	Mentions    []Mention
	Attachments []Attachment
	Replies     []Message // channel root messages only (may be partial)
	ReplyCount  int
	Ref         ConvRef
}

// ConvKind distinguishes chats from channels.
type ConvKind int

const (
	KindNone ConvKind = iota
	KindChat
	KindChannel
)

// ConvRef identifies a conversation (and optionally a thread inside a channel).
type ConvRef struct {
	Kind      ConvKind
	ChatID    string
	TeamID    string
	ChannelID string
	ThreadID  string // root message id when looking at channel replies
}

// Key returns a stable key for caching.
func (r ConvRef) Key() string {
	switch r.Kind {
	case KindChat:
		return "chat:" + r.ChatID
	case KindChannel:
		k := "chan:" + r.TeamID + ":" + r.ChannelID
		if r.ThreadID != "" {
			k += ":" + r.ThreadID
		}
		return k
	}
	return ""
}

// IsZero reports whether the ref is empty.
func (r ConvRef) IsZero() bool { return r.Kind == KindNone }

// Outgoing is a message to send or an edit.
type Outgoing struct {
	HTML        string
	Subject     string
	Importance  string
	Mentions    []Mention
	MentionUser map[int]User
	Attachments []Attachment
}

// Event is a calendar event.
type Event struct {
	ID             string
	Subject        string
	Organizer      string
	OrganizerEmail string
	Location       string
	Start          time.Time
	End            time.Time
	IsAllDay       bool
	IsOnline       bool
	JoinURL        string
	WebLink        string
	Response       string // none, organizer, accepted, declined, tentativelyAccepted, notResponded
	Attendees      []Attendee
	Preview        string
	IsCancelled    bool
	ChatID         string // meeting chat thread, derived from the join URL
}

// Attendee is an invitee of a calendar event.
type Attendee struct {
	Name     string
	Email    string
	Response string // none, accepted, declined, tentativelyAccepted, notResponded, organizer
	Type     string // required, optional, resource
}

// Transcript is a meeting transcript.
type Transcript struct {
	ID         string
	MeetingID  string
	Created    time.Time
	ContentURL string
}

// Recording is a meeting recording.
type Recording struct {
	ID         string
	MeetingID  string
	Created    time.Time
	ContentURL string
}

// MeetingArtifacts are the post-meeting transcripts and recordings.
type MeetingArtifacts struct {
	MeetingID   string
	Transcripts []Transcript
	Recordings  []Recording
}

// NewEvent describes a meeting to create.
type NewEvent struct {
	Subject   string
	Start     time.Time
	End       time.Time
	Attendees []string // email addresses
	Body      string
	Online    bool
}

// DriveItem is a file or folder in OneDrive / SharePoint.
type DriveItem struct {
	ID          string
	DriveID     string
	Name        string
	WebURL      string
	Size        int64
	Modified    time.Time
	ModifiedBy  string
	IsFolder    bool
	ChildCount  int
	DownloadURL string
}

// SearchHit is a message search result.
type SearchHit struct {
	Summary string
	From    string
	Created time.Time
	Ref     ConvRef
	MsgID   string
}

// Page is a cursor for paging messages; empty means no more.
type Page string

// Service is implemented by the Graph backend and the demo backend.
type Service interface {
	Me(ctx context.Context) (User, error)
	TenantID() string

	Chats(ctx context.Context) ([]Chat, error)
	CreateChat(ctx context.Context, members []User, topic string) (Chat, error)
	RenameChat(ctx context.Context, chatID, topic string) error
	AddChatMember(ctx context.Context, chatID string, u User) error
	LeaveChat(ctx context.Context, chatID string) error
	HideChat(ctx context.Context, chatID string) error
	MarkChatRead(ctx context.Context, chatID string) error

	Teams(ctx context.Context) ([]Team, error)
	Channels(ctx context.Context, teamID string) ([]Channel, error)
	Members(ctx context.Context, ref ConvRef) ([]User, error)

	Messages(ctx context.Context, ref ConvRef, cursor Page) ([]Message, Page, error)
	Send(ctx context.Context, ref ConvRef, out Outgoing) (Message, error)
	Edit(ctx context.Context, ref ConvRef, msgID string, out Outgoing) error
	Delete(ctx context.Context, ref ConvRef, msgID string) error
	React(ctx context.Context, ref ConvRef, msgID, emoji string, on bool) error
	Upload(ctx context.Context, ref ConvRef, localPath string) (Attachment, error)
	FetchURL(ctx context.Context, url, dest string) error

	Presence(ctx context.Context, userIDs []string) (map[string]Presence, error)
	MyPresence(ctx context.Context) (Presence, error)
	SetPresence(ctx context.Context, availability, activity string) error
	ClearPresence(ctx context.Context) error
	SetStatusMessage(ctx context.Context, text string) error

	Events(ctx context.Context, start, end time.Time) ([]Event, error)
	RespondEvent(ctx context.Context, eventID, response string) error
	CreateEvent(ctx context.Context, ev NewEvent) (Event, error)

	// Meetings
	ResolveUsers(ctx context.Context, emails []string) (map[string]User, error) // lower-cased email -> user
	MeetingArtifacts(ctx context.Context, joinURL string) (MeetingArtifacts, error)
	TranscriptText(ctx context.Context, t Transcript) (string, error) // WebVTT

	SearchMessages(ctx context.Context, q string) ([]SearchHit, error)
	SearchPeople(ctx context.Context, q string) ([]User, error)

	RecentFiles(ctx context.Context) ([]DriveItem, error)
	Folder(ctx context.Context, item *DriveItem) ([]DriveItem, error) // nil = OneDrive root
	ChannelFiles(ctx context.Context, teamID, channelID string) ([]DriveItem, error)
	SharedFiles(ctx context.Context) ([]DriveItem, error)
	UploadFile(ctx context.Context, folder *DriveItem, localPath string) (DriveItem, error)
	Download(ctx context.Context, item DriveItem, dest string) error
}
