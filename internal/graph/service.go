package graph

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
)

var _ model.Service = (*Client)(nil)

func esc(s string) string { return url.PathEscape(s) }

// Me returns the signed-in user.
func (c *Client) Me(ctx context.Context) (model.User, error) {
	var u gUser
	if err := c.do(ctx, http.MethodGet, "/me?$select=id,displayName,mail,userPrincipalName,jobTitle", nil, &u); err != nil {
		return model.User{}, err
	}
	c.me = u.ID
	return u.toModel(), nil
}

// ---- chats -----------------------------------------------------------------

// Chats lists the user's chats, most recent first.
func (c *Client) Chats(ctx context.Context) ([]model.Chat, error) {
	raw, err := list[gChat](ctx, c, "/me/chats?$expand=members,lastMessagePreview&$top=50&$orderby=lastMessagePreview/createdDateTime%20desc", 2)
	if err != nil && len(raw) == 0 {
		return nil, err
	}
	out := make([]model.Chat, 0, len(raw))
	for _, ch := range raw {
		if ch.Viewpoint != nil && ch.Viewpoint.IsHidden {
			continue
		}
		out = append(out, ch.toModel())
	}
	model.SortChats(out)
	return out, nil
}

func memberBind(id string) map[string]any {
	return map[string]any{
		"@odata.type":     "#microsoft.graph.aadUserConversationMember",
		"roles":           []string{"owner"},
		"user@odata.bind": fmt.Sprintf("https://graph.microsoft.com/v1.0/users('%s')", id),
	}
}

// CreateChat creates (or returns the existing) 1:1 or group chat.
func (c *Client) CreateChat(ctx context.Context, members []model.User, topic string) (model.Chat, error) {
	if c.me == "" {
		if _, err := c.Me(ctx); err != nil {
			return model.Chat{}, err
		}
	}
	binds := []map[string]any{memberBind(c.me)}
	for _, m := range members {
		if m.ID != c.me {
			binds = append(binds, memberBind(m.ID))
		}
	}
	body := map[string]any{"members": binds}
	if len(binds) == 2 {
		body["chatType"] = "oneOnOne"
	} else {
		body["chatType"] = "group"
		if topic != "" {
			body["topic"] = topic
		}
	}
	var ch gChat
	if err := c.do(ctx, http.MethodPost, "/chats", body, &ch); err != nil {
		return model.Chat{}, err
	}
	// re-fetch with members expanded
	var full gChat
	if err := c.do(ctx, http.MethodGet, "/chats/"+esc(ch.ID)+"?$expand=members", nil, &full); err == nil {
		ch = full
	}
	return ch.toModel(), nil
}

// RenameChat sets a group chat topic.
func (c *Client) RenameChat(ctx context.Context, chatID, topic string) error {
	return c.do(ctx, http.MethodPatch, "/chats/"+esc(chatID), map[string]any{"topic": topic}, nil)
}

// AddChatMember adds a user to a group chat (with full history).
func (c *Client) AddChatMember(ctx context.Context, chatID string, u model.User) error {
	b := memberBind(u.ID)
	b["visibleHistoryStartDateTime"] = "0001-01-01T00:00:00Z"
	return c.do(ctx, http.MethodPost, "/chats/"+esc(chatID)+"/members", b, nil)
}

// LeaveChat removes the signed-in user from a group chat.
func (c *Client) LeaveChat(ctx context.Context, chatID string) error {
	members, err := list[gMember](ctx, c, "/chats/"+esc(chatID)+"/members", 5)
	if err != nil {
		return err
	}
	for _, m := range members {
		if m.UserID == c.me {
			return c.do(ctx, http.MethodDelete, "/chats/"+esc(chatID)+"/members/"+esc(m.ID), nil, nil)
		}
	}
	return fmt.Errorf("you are not a member of this chat")
}

func (c *Client) userRef() map[string]any {
	return map[string]any{"user": map[string]any{"id": c.me, "tenantId": c.TenantID()}}
}

// HideChat hides a chat for the signed-in user.
func (c *Client) HideChat(ctx context.Context, chatID string) error {
	return c.do(ctx, http.MethodPost, "/chats/"+esc(chatID)+"/hideForUser", c.userRef(), nil)
}

// MarkChatRead marks a chat as read.
func (c *Client) MarkChatRead(ctx context.Context, chatID string) error {
	return c.do(ctx, http.MethodPost, "/chats/"+esc(chatID)+"/markChatReadForUser", c.userRef(), nil)
}

// ---- teams & channels ------------------------------------------------------

// Teams lists joined teams.
func (c *Client) Teams(ctx context.Context) ([]model.Team, error) {
	raw, err := list[struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	}](ctx, c, "/me/joinedTeams", 5)
	out := make([]model.Team, 0, len(raw))
	for _, t := range raw {
		out = append(out, model.Team{ID: t.ID, DisplayName: t.DisplayName, Description: t.Description})
	}
	// Keep Graph's order; the UI applies the user's custom order on top.
	return out, err
}

// Channels lists channels in a team (General first).
func (c *Client) Channels(ctx context.Context, teamID string) ([]model.Channel, error) {
	raw, err := list[struct {
		ID             string `json:"id"`
		DisplayName    string `json:"displayName"`
		Description    string `json:"description"`
		MembershipType string `json:"membershipType"`
	}](ctx, c, "/teams/"+esc(teamID)+"/channels", 5)
	out := make([]model.Channel, 0, len(raw))
	for _, ch := range raw {
		out = append(out, model.Channel{ID: ch.ID, TeamID: teamID, DisplayName: ch.DisplayName, Description: ch.Description, MembershipType: ch.MembershipType})
	}
	model.SortChannels(out)
	return out, err
}

// Members lists members of a chat or channel.
func (c *Client) Members(ctx context.Context, ref model.ConvRef) ([]model.User, error) {
	var p string
	switch ref.Kind {
	case model.KindChat:
		p = "/chats/" + esc(ref.ChatID) + "/members"
	case model.KindChannel:
		p = "/teams/" + esc(ref.TeamID) + "/channels/" + esc(ref.ChannelID) + "/members"
	default:
		return nil, nil
	}
	raw, err := list[gMember](ctx, c, p, 10)
	out := make([]model.User, 0, len(raw))
	for _, m := range raw {
		out = append(out, model.User{ID: m.UserID, DisplayName: m.DisplayName, Email: m.Email})
	}
	return out, err
}

// ---- messages --------------------------------------------------------------

func msgBase(ref model.ConvRef) string {
	switch ref.Kind {
	case model.KindChat:
		return "/chats/" + esc(ref.ChatID) + "/messages"
	case model.KindChannel:
		p := "/teams/" + esc(ref.TeamID) + "/channels/" + esc(ref.ChannelID) + "/messages"
		if ref.ThreadID != "" {
			p += "/" + esc(ref.ThreadID) + "/replies"
		}
		return p
	}
	return ""
}

// msgPath returns the path of a single message, handling channel replies.
func msgPath(ref model.ConvRef, id string) string {
	if ref.Kind == model.KindChannel && ref.ThreadID != "" && ref.ThreadID == id {
		r := ref
		r.ThreadID = ""
		return msgBase(r) + "/" + esc(id)
	}
	return msgBase(ref) + "/" + esc(id)
}

// Messages returns one page of messages, oldest first. cursor is an opaque
// next link from a previous call.
func (c *Client) Messages(ctx context.Context, ref model.ConvRef, cursor model.Page) ([]model.Message, model.Page, error) {
	p := string(cursor)
	if p == "" {
		switch {
		case ref.Kind == model.KindChat:
			p = msgBase(ref) + "?$top=50&$orderby=createdDateTime%20desc"
		case ref.ThreadID != "":
			p = msgBase(ref) + "?$top=50"
		default:
			p = msgBase(ref) + "?$top=50&$expand=replies"
		}
	}
	var page struct {
		Value    []gMessage `json:"value"`
		NextLink string     `json:"@odata.nextLink"`
	}
	if err := c.do(ctx, http.MethodGet, p, nil, &page); err != nil {
		return nil, "", err
	}
	out := make([]model.Message, 0, len(page.Value)+1)
	for _, m := range page.Value {
		out = append(out, m.toModel(ref))
	}
	if ref.Kind == model.KindChannel && ref.ThreadID != "" && cursor == "" {
		var root gMessage
		if err := c.do(ctx, http.MethodGet, msgPath(ref, ref.ThreadID), nil, &root); err == nil {
			rm := root.toModel(ref)
			rm.Replies = nil
			out = append(out, rm)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, model.Page(page.NextLink), nil
}

func outgoingBody(out model.Outgoing) map[string]any {
	body := map[string]any{"body": map[string]any{"contentType": "html", "content": out.HTML}}
	if out.Subject != "" {
		body["subject"] = out.Subject
	}
	if out.Importance != "" && out.Importance != "normal" {
		body["importance"] = out.Importance
	}
	if len(out.Mentions) > 0 {
		var ms []map[string]any
		for _, m := range out.Mentions {
			u := out.MentionUser[m.ID]
			ms = append(ms, map[string]any{
				"id": m.ID, "mentionText": m.Text,
				"mentioned": map[string]any{"user": map[string]any{
					"id": u.ID, "displayName": u.DisplayName, "userIdentityType": "aadUser",
				}},
			})
		}
		body["mentions"] = ms
	}
	if len(out.Attachments) > 0 {
		var as []map[string]any
		for _, a := range out.Attachments {
			as = append(as, map[string]any{"id": a.ID, "contentType": "reference", "contentUrl": a.ContentURL, "name": a.Name})
		}
		body["attachments"] = as
	}
	return body
}

// Send posts a new message (or a reply when ref.ThreadID is set).
func (c *Client) Send(ctx context.Context, ref model.ConvRef, out model.Outgoing) (model.Message, error) {
	var m gMessage
	if err := c.do(ctx, http.MethodPost, msgBase(ref), outgoingBody(out), &m); err != nil {
		return model.Message{}, err
	}
	return m.toModel(ref), nil
}

// Edit updates the body of one of my messages.
func (c *Client) Edit(ctx context.Context, ref model.ConvRef, msgID string, out model.Outgoing) error {
	body := outgoingBody(out)
	delete(body, "importance")
	return c.do(ctx, http.MethodPatch, msgPath(ref, msgID), body, nil)
}

// Delete soft-deletes one of my messages.
func (c *Client) Delete(ctx context.Context, ref model.ConvRef, msgID string) error {
	return c.do(ctx, http.MethodPost, msgPath(ref, msgID)+"/softDelete", nil, nil)
}

// React sets or unsets a reaction.
func (c *Client) React(ctx context.Context, ref model.ConvRef, msgID, emoji string, on bool) error {
	action := "/setReaction"
	if !on {
		action = "/unsetReaction"
	}
	return c.do(ctx, http.MethodPost, msgPath(ref, msgID)+action, map[string]any{"reactionType": emoji}, nil)
}

var etagGUID = regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`)

// Upload uploads a local file to the conversation's file location and
// returns an attachment to include in a message.
func (c *Client) Upload(ctx context.Context, ref model.ConvRef, localPath string) (model.Attachment, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return model.Attachment{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return model.Attachment{}, err
	}
	name := filepath.Base(localPath)

	var itemPath string // path addressing the target item, e.g. /me/drive/root:/x/y.txt:
	switch ref.Kind {
	case model.KindChannel:
		var folder gDriveItem
		if err := c.do(ctx, http.MethodGet, "/teams/"+esc(ref.TeamID)+"/channels/"+esc(ref.ChannelID)+"/filesFolder", nil, &folder); err != nil {
			return model.Attachment{}, err
		}
		itemPath = "/drives/" + esc(folder.ParentReference.DriveID) + "/items/" + esc(folder.ID) + ":/" + esc(name) + ":"
	default:
		itemPath = "/me/drive/root:/Microsoft%20Teams%20Chat%20Files/" + esc(name) + ":"
	}

	var item gDriveItem
	if st.Size() <= 4*1024*1024 {
		if err := c.do(ctx, http.MethodPut, itemPath+"/content", io.Reader(f), &item, withHeader("Content-Type", "application/octet-stream")); err != nil {
			return model.Attachment{}, err
		}
	} else {
		var sess struct {
			UploadURL string `json:"uploadUrl"`
		}
		if err := c.do(ctx, http.MethodPost, itemPath+"/createUploadSession", map[string]any{"item": map[string]any{"@microsoft.graph.conflictBehavior": "rename"}}, &sess); err != nil {
			return model.Attachment{}, err
		}
		const chunk = 320 * 1024 * 16 // 5 MiB, multiple of 320 KiB
		buf := make([]byte, chunk)
		var off int64
		for off < st.Size() {
			n, rerr := io.ReadFull(f, buf)
			if n == 0 {
				if rerr != nil {
					return model.Attachment{}, rerr
				}
				break
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPut, sess.UploadURL, bytes.NewReader(buf[:n]))
			if err != nil {
				return model.Attachment{}, err
			}
			req.ContentLength = int64(n)
			req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, off+int64(n)-1, st.Size()))
			resp, err := c.http.Do(req)
			if err != nil {
				return model.Attachment{}, err
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				return model.Attachment{}, fmt.Errorf("upload failed: %s", b)
			}
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
				_ = jsonUnmarshal(b, &item)
			}
			off += int64(n)
		}
	}
	if ref.Kind == model.KindChat && item.ID != "" {
		// Make the file viewable by people in the organisation so chat members can open it.
		drive := item.ParentReference.DriveID
		p := "/me/drive/items/" + esc(item.ID) + "/createLink"
		if drive != "" {
			p = "/drives/" + esc(drive) + "/items/" + esc(item.ID) + "/createLink"
		}
		_ = c.do(ctx, http.MethodPost, p, map[string]any{"type": "view", "scope": "organization"}, nil)
	}
	guid := etagGUID.FindString(item.ETag)
	if guid == "" {
		return model.Attachment{}, fmt.Errorf("upload succeeded but no eTag returned")
	}
	return model.Attachment{ID: strings.ToLower(guid), ContentType: "reference", ContentURL: item.WebURL, Name: item.Name}, nil
}

// ---- presence --------------------------------------------------------------

type gPresence struct {
	ID            string `json:"id"`
	Availability  string `json:"availability"`
	Activity      string `json:"activity"`
	StatusMessage *struct {
		Message *struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"statusMessage"`
}

func (p gPresence) toModel() model.Presence {
	out := model.Presence{UserID: p.ID, Availability: p.Availability, Activity: p.Activity}
	if p.StatusMessage != nil && p.StatusMessage.Message != nil {
		out.StatusMessage = htmltext.Plain(p.StatusMessage.Message.Content)
	}
	return out
}

// Presence fetches presence for up to 650 users.
func (c *Client) Presence(ctx context.Context, ids []string) (map[string]model.Presence, error) {
	out := map[string]model.Presence{}
	for start := 0; start < len(ids); start += 600 {
		end := min(start+600, len(ids))
		var resp struct {
			Value []gPresence `json:"value"`
		}
		if err := c.do(ctx, http.MethodPost, "/communications/getPresencesByUserId", map[string]any{"ids": ids[start:end]}, &resp); err != nil {
			return out, err
		}
		for _, p := range resp.Value {
			out[p.ID] = p.toModel()
		}
	}
	return out, nil
}

// MyPresence returns the signed-in user's presence.
func (c *Client) MyPresence(ctx context.Context) (model.Presence, error) {
	var p gPresence
	err := c.do(ctx, http.MethodGet, "/me/presence", nil, &p)
	return p.toModel(), err
}

// SetPresence sets the user's preferred presence (like the status picker).
func (c *Client) SetPresence(ctx context.Context, availability, activity string) error {
	body := map[string]any{"availability": availability, "activity": activity, "expirationDuration": "P1D"}
	return c.do(ctx, http.MethodPost, "/me/presence/setUserPreferredPresence", body, nil)
}

// ClearPresence resets presence to automatic.
func (c *Client) ClearPresence(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/me/presence/clearUserPreferredPresence", map[string]any{}, nil)
}

// SetStatusMessage sets (or clears, if empty) the status message.
func (c *Client) SetStatusMessage(ctx context.Context, text string) error {
	body := map[string]any{"statusMessage": map[string]any{"message": map[string]any{"content": text, "contentType": "text"}}}
	return c.do(ctx, http.MethodPost, "/me/presence/setStatusMessage", body, nil)
}

// ---- calendar --------------------------------------------------------------

// Events returns calendar events between start and end.
func (c *Client) Events(ctx context.Context, start, end time.Time) ([]model.Event, error) {
	q := url.Values{
		"startDateTime": {start.UTC().Format(time.RFC3339)},
		"endDateTime":   {end.UTC().Format(time.RFC3339)},
		"$orderby":      {"start/dateTime"},
		"$top":          {"100"},
		"$select":       {"id,subject,bodyPreview,start,end,isAllDay,isCancelled,isOnlineMeeting,onlineMeeting,webLink,location,organizer,responseStatus,attendees"},
	}
	raw, err := list[gEvent](ctx, c, "/me/calendarView?"+q.Encode(), 5, withHeader("Prefer", `outlook.timezone="UTC"`))
	out := make([]model.Event, 0, len(raw))
	for _, e := range raw {
		out = append(out, e.toModel())
	}
	return out, err
}

// RespondEvent accepts, tentatively accepts or declines an invitation.
func (c *Client) RespondEvent(ctx context.Context, id, response string) error {
	action := map[string]string{"accept": "accept", "tentative": "tentativelyAccept", "decline": "decline"}[response]
	if action == "" {
		return fmt.Errorf("unknown response %q", response)
	}
	return c.do(ctx, http.MethodPost, "/me/events/"+esc(id)+"/"+action, map[string]any{"sendResponse": true}, nil)
}

// CreateEvent schedules a meeting (as a Teams meeting when Online is set).
func (c *Client) CreateEvent(ctx context.Context, ev model.NewEvent) (model.Event, error) {
	var att []map[string]any
	for _, a := range ev.Attendees {
		att = append(att, map[string]any{"emailAddress": map[string]any{"address": a}, "type": "required"})
	}
	body := map[string]any{
		"subject":   ev.Subject,
		"body":      map[string]any{"contentType": "text", "content": ev.Body},
		"start":     map[string]any{"dateTime": ev.Start.UTC().Format("2006-01-02T15:04:05"), "timeZone": "UTC"},
		"end":       map[string]any{"dateTime": ev.End.UTC().Format("2006-01-02T15:04:05"), "timeZone": "UTC"},
		"attendees": att,
	}
	if ev.Online {
		body["isOnlineMeeting"] = true
		body["onlineMeetingProvider"] = "teamsForBusiness"
	}
	var e gEvent
	err := c.do(ctx, http.MethodPost, "/me/events", body, &e, withHeader("Prefer", `outlook.timezone="UTC"`))
	return e.toModel(), err
}

// ---- meetings --------------------------------------------------------------

// ResolveUsers looks up directory users by email / UPN.
func (c *Client) ResolveUsers(ctx context.Context, emails []string) (map[string]model.User, error) {
	out := map[string]model.User{}
	var firstErr error
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || out[e].ID != "" {
			continue
		}
		var u gUser
		err := c.do(ctx, http.MethodGet, "/users/"+esc(e)+"?$select=id,displayName,mail,userPrincipalName,jobTitle", nil, &u)
		if err != nil {
			if !IsStatus(err, http.StatusNotFound) && firstErr == nil {
				firstErr = err // guests and external attendees are simply skipped
			}
			continue
		}
		out[e] = u.toModel()
	}
	return out, firstErr
}

func (c *Client) meetingScopeHint(err error) error {
	if IsStatus(err, http.StatusForbidden) || IsStatus(err, http.StatusUnauthorized) {
		for _, s := range []string{"OnlineMeetings.Read", "OnlineMeetingTranscript.Read.All", "OnlineMeetingRecording.Read.All"} {
			if !c.auth.HasScope(s) {
				return fmt.Errorf("%w — run `teams-tui login` again to grant %s", err, s)
			}
		}
		return fmt.Errorf("%w — transcripts/recordings are usually only available to the meeting organizer, and your admin may have disabled API access", err)
	}
	return err
}

// MeetingArtifacts finds the online meeting for a join link and lists its
// transcripts and recordings.
func (c *Client) MeetingArtifacts(ctx context.Context, joinURL string) (model.MeetingArtifacts, error) {
	var out model.MeetingArtifacts
	if c.me == "" {
		if _, err := c.Me(ctx); err != nil {
			return out, err
		}
	}
	filter := "JoinWebUrl eq '" + strings.ReplaceAll(joinURL, "'", "''") + "'"
	var mt struct {
		Value []struct {
			ID string `json:"id"`
		} `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, "/me/onlineMeetings?$filter="+url.QueryEscape(filter), nil, &mt); err != nil {
		return out, c.meetingScopeHint(err)
	}
	if len(mt.Value) == 0 {
		return out, fmt.Errorf("meeting not found (only Teams meetings you organized or were invited to can be looked up)")
	}
	out.MeetingID = mt.Value[0].ID
	p := "/users/" + esc(c.me) + "/onlineMeetings/" + esc(out.MeetingID)

	type artifact struct {
		ID                   string    `json:"id"`
		MeetingID            string    `json:"meetingId"`
		CreatedDateTime      time.Time `json:"createdDateTime"`
		TranscriptContentURL string    `json:"transcriptContentUrl"`
		RecordingContentURL  string    `json:"recordingContentUrl"`
	}
	ts, terr := list[artifact](ctx, c, p+"/transcripts", 3)
	for _, t := range ts {
		u := t.TranscriptContentURL
		if u == "" {
			u = base + p + "/transcripts/" + esc(t.ID) + "/content"
		}
		out.Transcripts = append(out.Transcripts, model.Transcript{ID: t.ID, MeetingID: out.MeetingID, Created: t.CreatedDateTime, ContentURL: u})
	}
	rs, rerr := list[artifact](ctx, c, p+"/recordings", 3)
	for _, r := range rs {
		u := r.RecordingContentURL
		if u == "" {
			u = base + p + "/recordings/" + esc(r.ID) + "/content"
		}
		out.Recordings = append(out.Recordings, model.Recording{ID: r.ID, MeetingID: out.MeetingID, Created: r.CreatedDateTime, ContentURL: u})
	}
	if terr != nil && rerr != nil {
		return out, c.meetingScopeHint(terr)
	}
	return out, nil
}

// TranscriptText downloads a transcript as WebVTT.
func (c *Client) TranscriptText(ctx context.Context, t model.Transcript) (string, error) {
	u := t.ContentURL
	if strings.Contains(u, "?") {
		u += "&$format=text/vtt"
	} else {
		u += "?$format=text/vtt"
	}
	resp, err := c.raw(ctx, http.MethodGet, u, nil, withHeader("Accept", "text/vtt"))
	if err != nil {
		return "", c.meetingScopeHint(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// ---- search ----------------------------------------------------------------

// SearchMessages searches chat and channel messages.
func (c *Client) SearchMessages(ctx context.Context, q string) ([]model.SearchHit, error) {
	body := map[string]any{"requests": []map[string]any{{
		"entityTypes": []string{"chatMessage"},
		"query":       map[string]any{"queryString": q},
		"from":        0, "size": 25,
	}}}
	var resp struct {
		Value []struct {
			HitsContainers []struct {
				Hits []struct {
					Summary  string   `json:"summary"`
					Resource gMessage `json:"resource"`
				} `json:"hits"`
			} `json:"hitsContainers"`
		} `json:"value"`
	}
	if err := c.do(ctx, http.MethodPost, "/search/query", body, &resp); err != nil {
		return nil, err
	}
	var out []model.SearchHit
	for _, v := range resp.Value {
		for _, hc := range v.HitsContainers {
			for _, h := range hc.Hits {
				r := h.Resource
				hit := model.SearchHit{
					Summary: htmltext.Plain(h.Summary), From: r.From.toUser().DisplayName,
					Created: r.CreatedDateTime, MsgID: r.ID,
				}
				switch {
				case r.ChannelIdentity != nil && r.ChannelIdentity.ChannelID != "":
					hit.Ref = model.ConvRef{Kind: model.KindChannel, TeamID: r.ChannelIdentity.TeamID, ChannelID: r.ChannelIdentity.ChannelID}
					if r.ReplyToID != "" {
						hit.Ref.ThreadID = r.ReplyToID
					}
				case r.ChatID != "":
					hit.Ref = model.ConvRef{Kind: model.KindChat, ChatID: r.ChatID}
				}
				out = append(out, hit)
			}
		}
	}
	return out, nil
}

// SearchPeople searches users in the directory.
func (c *Client) SearchPeople(ctx context.Context, q string) ([]model.User, error) {
	q = strings.ReplaceAll(q, `"`, "")
	if strings.TrimSpace(q) == "" {
		raw, err := list[gUser](ctx, c, "/me/people?$top=20", 1)
		return usersFrom(raw), err
	}
	search := fmt.Sprintf(`"displayName:%s" OR "mail:%s"`, q, q)
	p := "/users?$top=25&$select=id,displayName,mail,userPrincipalName,jobTitle&$search=" + url.QueryEscape(search)
	raw, err := list[gUser](ctx, c, p, 1, withHeader("ConsistencyLevel", "eventual"))
	if err != nil {
		raw, err = list[gUser](ctx, c, "/me/people?$top=20&$search="+url.QueryEscape(`"`+q+`"`), 1)
	}
	return usersFrom(raw), err
}

func usersFrom(raw []gUser) []model.User {
	out := make([]model.User, 0, len(raw))
	for _, u := range raw {
		if u.ID == "" {
			continue
		}
		out = append(out, u.toModel())
	}
	return out
}

// ---- files -----------------------------------------------------------------

func items(raw []gDriveItem) []model.DriveItem {
	out := make([]model.DriveItem, 0, len(raw))
	for _, d := range raw {
		out = append(out, d.toModel())
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].IsFolder && !out[j].IsFolder })
	return out
}

// RecentFiles lists recently used files.
func (c *Client) RecentFiles(ctx context.Context) ([]model.DriveItem, error) {
	raw, err := list[gDriveItem](ctx, c, "/me/drive/recent?$top=50", 1)
	out := make([]model.DriveItem, 0, len(raw))
	for _, d := range raw {
		out = append(out, d.toModel())
	}
	return out, err
}

// Folder lists the children of a folder (nil = OneDrive root).
func (c *Client) Folder(ctx context.Context, item *model.DriveItem) ([]model.DriveItem, error) {
	p := "/me/drive/root/children?$top=200"
	if item != nil {
		p = "/drives/" + esc(item.DriveID) + "/items/" + esc(item.ID) + "/children?$top=200"
	}
	raw, err := list[gDriveItem](ctx, c, p, 3)
	return items(raw), err
}

// ChannelFiles lists the files tab of a channel.
func (c *Client) ChannelFiles(ctx context.Context, teamID, channelID string) ([]model.DriveItem, error) {
	var folder gDriveItem
	if err := c.do(ctx, http.MethodGet, "/teams/"+esc(teamID)+"/channels/"+esc(channelID)+"/filesFolder", nil, &folder); err != nil {
		return nil, err
	}
	f := folder.toModel()
	return c.Folder(ctx, &f)
}

// Download saves a drive item to dest.
func (c *Client) Download(ctx context.Context, item model.DriveItem, dest string) error {
	p := "/me/drive/items/" + esc(item.ID) + "/content"
	if item.DriveID != "" {
		p = "/drives/" + esc(item.DriveID) + "/items/" + esc(item.ID) + "/content"
	}
	return c.FetchURL(ctx, base+p, dest)
}
