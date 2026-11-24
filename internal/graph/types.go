package graph

import (
	"strings"
	"time"

	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
)

type gIdentity struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type gIdentitySet struct {
	User        *gIdentity `json:"user"`
	Application *gIdentity `json:"application"`
	Device      *gIdentity `json:"device"`
}

func (s *gIdentitySet) toUser() model.User {
	if s == nil {
		return model.User{DisplayName: "System"}
	}
	switch {
	case s.User != nil:
		return model.User{ID: s.User.ID, DisplayName: s.User.DisplayName}
	case s.Application != nil:
		return model.User{ID: "app:" + s.Application.ID, DisplayName: s.Application.DisplayName}
	case s.Device != nil:
		return model.User{ID: s.Device.ID, DisplayName: s.Device.DisplayName}
	}
	return model.User{DisplayName: "System"}
}

type gUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	JobTitle          string `json:"jobTitle"`
	// people API
	ScoredEmailAddresses []struct {
		Address string `json:"address"`
	} `json:"scoredEmailAddresses"`
}

func (u gUser) toModel() model.User {
	mail := u.Mail
	if mail == "" && len(u.ScoredEmailAddresses) > 0 {
		mail = u.ScoredEmailAddresses[0].Address
	}
	if mail == "" {
		mail = u.UserPrincipalName
	}
	return model.User{ID: u.ID, DisplayName: u.DisplayName, Email: mail, JobTitle: u.JobTitle}
}

type gMember struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

type gBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type gChat struct {
	ID                  string    `json:"id"`
	Topic               string    `json:"topic"`
	ChatType            string    `json:"chatType"`
	WebURL              string    `json:"webUrl"`
	LastUpdatedDateTime time.Time `json:"lastUpdatedDateTime"`
	Members             []gMember `json:"members"`
	Viewpoint           *struct {
		IsHidden                bool      `json:"isHidden"`
		LastMessageReadDateTime time.Time `json:"lastMessageReadDateTime"`
	} `json:"viewpoint"`
	OnlineMeetingInfo *struct {
		JoinWebURL string `json:"joinWebUrl"`
	} `json:"onlineMeetingInfo"`
	LastMessagePreview *struct {
		ID              string        `json:"id"`
		CreatedDateTime time.Time     `json:"createdDateTime"`
		IsDeleted       bool          `json:"isDeleted"`
		MessageType     string        `json:"messageType"`
		Body            gBody         `json:"body"`
		From            *gIdentitySet `json:"from"`
		EventDetail     *gEventDetail `json:"eventDetail"`
	} `json:"lastMessagePreview"`
}

// gEventDetail is the part of a system event message that describes a call.
type gEventDetail struct {
	Type          string        `json:"@odata.type"`
	CallEventType string        `json:"callEventType"`
	CallID        string        `json:"callId"`
	Initiator     *gIdentitySet `json:"initiator"`
	Participants  []struct {
		Participant *gIdentitySet `json:"participant"`
	} `json:"callParticipants"`
}

// toCall returns the call event, or nil for other kinds of system events.
func (d *gEventDetail) toCall() *model.CallEvent {
	if d == nil {
		return nil
	}
	out := &model.CallEvent{Type: d.CallEventType, CallID: d.CallID}
	switch d.Type {
	case "#microsoft.graph.callStartedEventMessageDetail":
		out.Started = true
	case "#microsoft.graph.callEndedEventMessageDetail":
	default:
		return nil
	}
	if d.Initiator != nil {
		out.Initiator = d.Initiator.toUser()
	}
	for _, p := range d.Participants {
		if p.Participant != nil && p.Participant.User != nil {
			out.Participants = append(out.Participants, p.Participant.User.ID)
		}
	}
	return out
}

func (c gChat) toModel() model.Chat {
	out := model.Chat{ID: c.ID, Topic: c.Topic, ChatType: c.ChatType, WebURL: c.WebURL, Updated: c.LastUpdatedDateTime}
	for _, m := range c.Members {
		out.Members = append(out.Members, model.User{ID: m.UserID, DisplayName: m.DisplayName, Email: m.Email})
	}
	if c.OnlineMeetingInfo != nil {
		out.JoinURL = c.OnlineMeetingInfo.JoinWebURL
	}
	if c.Viewpoint != nil {
		out.LastRead = c.Viewpoint.LastMessageReadDateTime
	}
	if p := c.LastMessagePreview; p != nil {
		text := p.Body.Content
		if p.Body.ContentType == "html" {
			text = htmltext.Plain(text)
		}
		out.LastMessage = &model.MessagePreview{
			ID: p.ID, From: p.From.toUser(), Text: strings.TrimSpace(text),
			Created: p.CreatedDateTime, Deleted: p.IsDeleted, Call: p.EventDetail.toCall(),
		}
		if c := out.LastMessage.Call; c != nil && out.LastMessage.Text == "" {
			out.LastMessage.Text = c.Label()
		}
	}
	return out
}

type gMessage struct {
	ID                   string        `json:"id"`
	ReplyToID            string        `json:"replyToId"`
	MessageType          string        `json:"messageType"`
	CreatedDateTime      time.Time     `json:"createdDateTime"`
	LastModifiedDateTime time.Time     `json:"lastModifiedDateTime"`
	LastEditedDateTime   *time.Time    `json:"lastEditedDateTime"`
	DeletedDateTime      *time.Time    `json:"deletedDateTime"`
	Subject              string        `json:"subject"`
	Importance           string        `json:"importance"`
	ChatID               string        `json:"chatId"`
	From                 *gIdentitySet `json:"from"`
	Body                 gBody         `json:"body"`
	ChannelIdentity      *struct {
		TeamID    string `json:"teamId"`
		ChannelID string `json:"channelId"`
	} `json:"channelIdentity"`
	Attachments []struct {
		ID          string `json:"id"`
		ContentType string `json:"contentType"`
		ContentURL  string `json:"contentUrl"`
		Content     string `json:"content"`
		Name        string `json:"name"`
	} `json:"attachments"`
	Mentions []struct {
		ID          int           `json:"id"`
		MentionText string        `json:"mentionText"`
		Mentioned   *gIdentitySet `json:"mentioned"`
	} `json:"mentions"`
	Reactions []struct {
		ReactionType    string    `json:"reactionType"`
		DisplayName     string    `json:"displayName"`
		CreatedDateTime time.Time `json:"createdDateTime"`
		User            struct {
			User *gIdentity `json:"user"`
		} `json:"user"`
	} `json:"reactions"`
	Replies []gMessage `json:"replies"`
	// Graph returns "replies@odata.count" when expanding.
	RepliesCount int `json:"replies@odata.count"`
}

var legacyReactions = map[string]string{
	"like": "👍", "heart": "❤️", "laugh": "😆", "surprised": "😮", "sad": "😢", "angry": "😡",
}

// NormalizeReaction maps legacy reaction names to emoji.
func NormalizeReaction(s string) string {
	if e, ok := legacyReactions[s]; ok {
		return e
	}
	return s
}

func (m gMessage) toModel(ref model.ConvRef) model.Message {
	out := model.Message{
		ID: m.ID, ReplyToID: m.ReplyToID, MessageType: m.MessageType,
		From: m.From.toUser(), Created: m.CreatedDateTime, Modified: m.LastModifiedDateTime,
		Edited: m.LastEditedDateTime != nil, Deleted: m.DeletedDateTime != nil,
		Subject: m.Subject, Importance: m.Importance,
		ContentType: m.Body.ContentType, Body: m.Body.Content, Ref: ref,
	}
	if m.ChatID != "" && out.Ref.Kind == model.KindNone {
		out.Ref = model.ConvRef{Kind: model.KindChat, ChatID: m.ChatID}
	}
	for _, a := range m.Attachments {
		out.Attachments = append(out.Attachments, model.Attachment{ID: a.ID, ContentType: a.ContentType, ContentURL: a.ContentURL, Name: a.Name, Content: a.Content})
	}
	for _, mm := range m.Mentions {
		u := mm.Mentioned.toUser()
		out.Mentions = append(out.Mentions, model.Mention{ID: mm.ID, Text: mm.MentionText, UserID: u.ID})
	}
	for _, r := range m.Reactions {
		uid := ""
		if r.User.User != nil {
			uid = r.User.User.ID
		}
		t := r.ReactionType
		if r.DisplayName != "" && !isEmojiish(t) {
			t = r.DisplayName
		}
		out.Reactions = append(out.Reactions, model.Reaction{Type: NormalizeReaction(t), UserID: uid, Created: r.CreatedDateTime})
	}
	for _, r := range m.Replies {
		rr := ref
		rr.ThreadID = m.ID
		out.Replies = append(out.Replies, r.toModel(rr))
	}
	out.ReplyCount = len(m.Replies)
	if m.RepliesCount > out.ReplyCount {
		out.ReplyCount = m.RepliesCount
	}
	return out
}

func isEmojiish(s string) bool {
	for _, r := range s {
		if r > 0x2000 {
			return true
		}
	}
	return false
}

type gDriveItem struct {
	ID                   string        `json:"id"`
	Name                 string        `json:"name"`
	WebURL               string        `json:"webUrl"`
	Size                 int64         `json:"size"`
	ETag                 string        `json:"eTag"`
	LastModifiedDateTime time.Time     `json:"lastModifiedDateTime"`
	LastModifiedBy       *gIdentitySet `json:"lastModifiedBy"`
	DownloadURL          string        `json:"@microsoft.graph.downloadUrl"`
	Folder               *struct {
		ChildCount int `json:"childCount"`
	} `json:"folder"`
	ParentReference struct {
		DriveID string `json:"driveId"`
	} `json:"parentReference"`
	RemoteItem *gDriveItem `json:"remoteItem"`
}

func (d gDriveItem) toModel() model.DriveItem {
	if d.RemoteItem != nil && d.RemoteItem.ID != "" {
		r := d.RemoteItem.toModel()
		if r.Name == "" {
			r.Name = d.Name
		}
		return r
	}
	out := model.DriveItem{
		ID: d.ID, DriveID: d.ParentReference.DriveID, Name: d.Name, WebURL: d.WebURL,
		Size: d.Size, Modified: d.LastModifiedDateTime, DownloadURL: d.DownloadURL,
	}
	if d.LastModifiedBy != nil {
		out.ModifiedBy = d.LastModifiedBy.toUser().DisplayName
	}
	if d.Folder != nil {
		out.IsFolder = true
		out.ChildCount = d.Folder.ChildCount
	}
	return out
}

type gDateTimeTZ struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

func (d gDateTimeTZ) time() time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.0000000", "2006-01-02T15:04:05", time.RFC3339Nano} {
		if t, err := time.ParseInLocation(layout, d.DateTime, time.UTC); err == nil {
			return t.Local()
		}
	}
	return time.Time{}
}

type gEvent struct {
	ID          string      `json:"id"`
	Subject     string      `json:"subject"`
	BodyPreview string      `json:"bodyPreview"`
	Start       gDateTimeTZ `json:"start"`
	End         gDateTimeTZ `json:"end"`
	IsAllDay    bool        `json:"isAllDay"`
	IsCancelled bool        `json:"isCancelled"`
	IsOnline    bool        `json:"isOnlineMeeting"`
	WebLink     string      `json:"webLink"`
	Location    struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	Organizer struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"organizer"`
	OnlineMeeting *struct {
		JoinURL string `json:"joinUrl"`
	} `json:"onlineMeeting"`
	ResponseStatus struct {
		Response string `json:"response"`
	} `json:"responseStatus"`
	Attendees []struct {
		Type         string `json:"type"`
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
		Status struct {
			Response string `json:"response"`
		} `json:"status"`
	} `json:"attendees"`
}

func (e gEvent) toModel() model.Event {
	out := model.Event{
		ID: e.ID, Subject: e.Subject, Preview: e.BodyPreview, Start: e.Start.time(), End: e.End.time(),
		IsAllDay: e.IsAllDay, IsCancelled: e.IsCancelled, IsOnline: e.IsOnline, WebLink: e.WebLink,
		Location: e.Location.DisplayName, Organizer: e.Organizer.EmailAddress.Name,
		OrganizerEmail: e.Organizer.EmailAddress.Address, Response: e.ResponseStatus.Response,
	}
	if e.OnlineMeeting != nil {
		out.JoinURL = e.OnlineMeeting.JoinURL
		out.ChatID = model.ChatIDFromJoinURL(out.JoinURL)
	}
	for _, a := range e.Attendees {
		n := a.EmailAddress.Name
		if n == "" {
			n = a.EmailAddress.Address
		}
		out.Attendees = append(out.Attendees, model.Attendee{Name: n, Email: a.EmailAddress.Address, Response: a.Status.Response, Type: a.Type})
	}
	return out
}
