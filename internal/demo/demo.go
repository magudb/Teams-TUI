// Package demo provides an in-memory model.Service with sample data, so the
// UI can be explored without a Microsoft 365 account.
package demo

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magudb/Teams-TUI/internal/htmltext"
	"github.com/magudb/Teams-TUI/internal/model"
)

// Service is the demo backend.
type Service struct {
	mu        sync.Mutex
	me        model.User
	users     []model.User
	presence  map[string]model.Presence
	chats     []*model.Chat
	teams     []model.Team
	channels  map[string][]model.Channel
	msgs      map[string][]model.Message // key: ConvRef.Key() without thread
	events    []model.Event
	files     []model.DriveItem
	seq       int
	lastTick  time.Time
	ticks     int
	rng       *rand.Rand
	Simulate  bool
	FailNames map[string]bool
}

var _ model.Service = (*Service)(nil)

// New creates a populated demo service.
func New() *Service {
	s := &Service{
		presence: map[string]model.Presence{},
		channels: map[string][]model.Channel{},
		msgs:     map[string][]model.Message{},
		rng:      rand.New(rand.NewSource(42)),
		lastTick: time.Now(),
		Simulate: true,
	}
	s.seed()
	return s
}

func (s *Service) id() string { s.seq++; return "m" + strconv.Itoa(s.seq) }

func (s *Service) seed() {
	now := time.Now()
	s.me = model.User{ID: "u0", DisplayName: "Alex Morgan", Email: "alex@contoso.example", JobTitle: "Engineer"}
	s.users = []model.User{
		s.me,
		{ID: "u1", DisplayName: "Sofie Jensen", Email: "sofie@contoso.example", JobTitle: "Product Manager"},
		{ID: "u2", DisplayName: "Lars Nielsen", Email: "lars@contoso.example", JobTitle: "Lawyer"},
		{ID: "u3", DisplayName: "Emma Larsen", Email: "emma@contoso.example", JobTitle: "Designer"},
		{ID: "u4", DisplayName: "Noah Petersen", Email: "noah@contoso.example", JobTitle: "CTO"},
		{ID: "u5", DisplayName: "Ida Kristensen", Email: "ida@contoso.example", JobTitle: "Support"},
		{ID: "u6", DisplayName: "Oliver Holm", Email: "oliver@contoso.example", JobTitle: "Sales"},
	}
	avail := []string{model.Available, model.Available, model.Busy, model.Away, model.DoNotDisturb, model.BeRightBack, model.Offline}
	for i, u := range s.users {
		s.presence[u.ID] = model.Presence{UserID: u.ID, Availability: avail[i], Activity: avail[i]}
	}
	s.presence["u2"] = model.Presence{UserID: "u2", Availability: model.Busy, Activity: "InAMeeting", StatusMessage: "In court until 14:00"}
	u := func(i int) model.User { return s.users[i] }
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	add := func(ref model.ConvRef, from model.User, at time.Time, html string, mut ...func(*model.Message)) *model.Message {
		m := model.Message{ID: s.id(), MessageType: "message", From: from, Created: at, Modified: at, ContentType: "html", Body: html, Ref: ref, Importance: "normal"}
		for _, f := range mut {
			f(&m)
		}
		k := ref
		k.ThreadID = ""
		if ref.ThreadID != "" {
			list := s.msgs[k.Key()]
			for i := range list {
				if list[i].ID == ref.ThreadID {
					m.ReplyToID = ref.ThreadID
					list[i].Replies = append(list[i].Replies, m)
					list[i].ReplyCount++
					return &list[i].Replies[len(list[i].Replies)-1]
				}
			}
		}
		s.msgs[k.Key()] = append(s.msgs[k.Key()], m)
		return &s.msgs[k.Key()][len(s.msgs[k.Key()])-1]
	}

	// chats
	c1 := &model.Chat{ID: "c1", ChatType: "oneOnOne", Members: []model.User{s.me, u(1)}, LastRead: ago(3 * time.Hour)}
	c2 := &model.Chat{ID: "c2", ChatType: "group", Topic: "Launch squad 🚀", Members: []model.User{s.me, u(1), u(3), u(4)}, LastRead: ago(time.Hour)}
	c3 := &model.Chat{ID: "c3", ChatType: "oneOnOne", Members: []model.User{s.me, u(2)}, LastRead: now}
	c4 := &model.Chat{ID: "c4", ChatType: "oneOnOne", Members: []model.User{s.me, u(5)}, LastRead: now}
	c5 := &model.Chat{ID: "c5", ChatType: "meeting", Topic: "Weekly sync", Members: []model.User{s.me, u(1), u(2), u(4), u(6)}, LastRead: now}
	s.chats = []*model.Chat{c1, c2, c3, c4, c5}

	r := func(id string) model.ConvRef { return model.ConvRef{Kind: model.KindChat, ChatID: id} }
	add(r("c1"), u(1), ago(26*time.Hour), "<p>Hi Alex! Do you have a minute tomorrow to go through the roadmap?</p>")
	add(r("c1"), s.me, ago(25*time.Hour), "<p>Sure, 10:00 works for me 👍</p>", func(m *model.Message) {
		m.Reactions = []model.Reaction{{Type: "❤️", UserID: "u1"}}
	})
	add(r("c1"), u(1), ago(4*time.Hour), `<p>Here's the draft: <a href="https://contoso.example/roadmap">roadmap Q1</a></p><p>Key points:</p><ul><li>New onboarding flow</li><li><b>Pricing</b> experiment</li><li>Mobile app <i>beta</i></li></ul>`, func(m *model.Message) {
		m.Attachments = []model.Attachment{{ID: "a1", ContentType: "reference", Name: "Roadmap-Q1.pptx", ContentURL: "https://contoso.example/Roadmap-Q1.pptx"}}
		m.Body += `<attachment id="a1"></attachment>`
	})
	add(r("c1"), u(1), ago(2*time.Hour), "<p>Could you also look at the <code>billing-service</code> timeouts? Logs below:</p><pre><code>2026-11-20T10:02:11Z ERROR upstream timeout after 30s\n2026-11-20T10:02:41Z WARN retrying (2/3)</code></pre>")

	add(r("c2"), u(4), ago(30*time.Hour), "<p>Kickoff for the launch is <b>Monday</b>. Please prep your status.</p>", func(m *model.Message) { m.Importance = "high" })
	add(r("c2"), u(3), ago(5*time.Hour), `<p>New mockups are up 🎨</p><img src="https://graph.example/hosted/1" alt="image">`)
	add(r("c2"), u(1), ago(90*time.Minute), `<p><at id="0">Alex Morgan</at> can you confirm the API is ready for the beta?</p>`, func(m *model.Message) {
		m.Mentions = []model.Mention{{ID: 0, Text: "Alex Morgan", UserID: "u0"}}
	})
	add(r("c2"), u(3), ago(40*time.Minute), "<blockquote>can you confirm the API is ready</blockquote><p>+1, also need the staging URL</p>", func(m *model.Message) {
		m.Reactions = []model.Reaction{{Type: "👍", UserID: "u4"}, {Type: "👍", UserID: "u1"}, {Type: "😆", UserID: "u0"}}
	})

	add(r("c3"), u(2), ago(50*time.Hour), "<p>The contract template has been updated with the new clauses.</p>")
	add(r("c3"), s.me, ago(49*time.Hour), "<p>Thanks Lars, I'll review it this week.</p>", func(m *model.Message) { m.Edited = true })
	add(r("c4"), u(5), ago(80*time.Hour), "<p>Ticket #4521 is resolved ✅</p>")
	add(r("c4"), s.me, ago(79*time.Hour), "<p>Great work!</p>")
	add(r("c4"), u(5), ago(78*time.Hour), "", func(m *model.Message) { m.Deleted = true })
	add(r("c5"), u(4), ago(7*24*time.Hour), "<p>Agenda: 1) metrics 2) hiring 3) AOB</p>")
	add(r("c5"), u(6), ago(7*24*time.Hour-time.Hour), "<p>Sales numbers are in the deck.</p>")

	// teams & channels
	s.teams = []model.Team{
		{ID: "t1", DisplayName: "Engineering", Description: "All things engineering"},
		{ID: "t2", DisplayName: "Company", Description: "Company-wide announcements"},
		{ID: "t3", DisplayName: "Legal & Compliance"},
	}
	s.channels["t1"] = []model.Channel{
		{ID: "ch1", TeamID: "t1", DisplayName: "General"},
		{ID: "ch2", TeamID: "t1", DisplayName: "Backend"},
		{ID: "ch3", TeamID: "t1", DisplayName: "Incidents", MembershipType: "private"},
	}
	s.channels["t2"] = []model.Channel{{ID: "ch4", TeamID: "t2", DisplayName: "General"}, {ID: "ch5", TeamID: "t2", DisplayName: "Random"}}
	s.channels["t3"] = []model.Channel{{ID: "ch6", TeamID: "t3", DisplayName: "General"}}

	ch := func(t, c string) model.ConvRef {
		return model.ConvRef{Kind: model.KindChannel, TeamID: t, ChannelID: c}
	}
	p1 := add(ch("t1", "ch1"), u(4), ago(48*time.Hour), "<p>Welcome to the new Engineering team! Please introduce yourselves 👋</p>", func(m *model.Message) { m.Subject = "Welcome!" })
	thread := ch("t1", "ch1")
	thread.ThreadID = p1.ID
	add(thread, u(3), ago(47*time.Hour), "<p>Hi all, Emma here, design 🎨</p>")
	add(thread, s.me, ago(46*time.Hour), "<p>Alex, backend. Happy to be here!</p>")
	add(thread, u(1), ago(45*time.Hour), "<p>Sofie, PM 👋</p>")
	add(ch("t1", "ch1"), u(4), ago(3*time.Hour), "<p>Reminder: code freeze starts <b>Thursday 17:00</b>.</p>", func(m *model.Message) {
		m.Subject = "Code freeze"
		m.Importance = "high"
		m.Reactions = []model.Reaction{{Type: "👍", UserID: "u1"}, {Type: "😮", UserID: "u3"}}
	})
	p2 := add(ch("t1", "ch2"), s.me, ago(20*time.Hour), "<p>Proposal: migrate the job queue to Postgres <code>SKIP LOCKED</code>. Thoughts?</p>", func(m *model.Message) { m.Subject = "Job queue migration" })
	thread2 := ch("t1", "ch2")
	thread2.ThreadID = p2.ID
	add(thread2, u(4), ago(19*time.Hour), "<p>Love it. Let's benchmark first.</p>")
	card := func(typ, content string) func(*model.Message) {
		return func(m *model.Message) {
			m.Attachments = append(m.Attachments, model.Attachment{ID: "card" + m.ID, ContentType: typ, Content: content})
			m.Body += `<attachment id="card` + m.ID + `"></attachment>`
		}
	}
	approvals := model.User{ID: "app:approvals", DisplayName: "Approvals"}
	add(r("c2"), approvals, ago(20*time.Minute), "", card("application/vnd.microsoft.card.adaptive", sampleApproval))
	add(ch("t1", "ch2"), model.User{ID: "app:github", DisplayName: "GitHub"}, ago(15*time.Hour), "", card("application/vnd.microsoft.teams.card.o365connector", sampleConnector))
	add(ch("t1", "ch3"), model.User{ID: "app:pagerbot", DisplayName: "Incident Bot"}, ago(5*time.Hour), "<p>Weekly summary</p>", card("application/vnd.microsoft.card.adaptive", sampleTable))
	add(ch("t1", "ch3"), u(5), ago(6*time.Hour), "<p>🔥 Elevated error rate on checkout. Investigating.</p>", func(m *model.Message) { m.Importance = "urgent"; m.Subject = "INC-231" })
	add(ch("t2", "ch4"), u(4), ago(72*time.Hour), "<p>All-hands on Friday at 15:00 in the canteen and online.</p>", func(m *model.Message) { m.Subject = "All-hands" })
	add(ch("t2", "ch5"), u(6), ago(10*time.Hour), "<p>Who's up for lunch at the new place? 🍜</p>")
	add(ch("t3", "ch6"), u(2), ago(30*time.Hour), "<p>New GDPR guidelines are in the Files tab.</p>")

	for _, c := range s.chats {
		s.refreshPreview(c)
	}

	// calendar
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	att := func(resp map[string]string, ids ...int) []model.Attendee {
		var out []model.Attendee
		for _, i := range ids {
			u := s.users[i]
			r := resp[u.ID]
			if r == "" {
				r = "accepted"
			}
			out = append(out, model.Attendee{Name: u.DisplayName, Email: u.Email, Response: r, Type: "required"})
		}
		return out
	}
	join := func(id string) string {
		return "https://teams.microsoft.com/l/meetup-join/19%3ameeting_" + id + "%40thread.v2/0?context=%7b%7d"
	}
	soon := now.Truncate(time.Minute).Add(8 * time.Minute)
	live := now.Truncate(time.Minute).Add(-10 * time.Minute)
	s.events = []model.Event{
		{ID: "e0", Subject: "Sprint retro", Organizer: "Alex Morgan", OrganizerEmail: s.me.Email, Start: day.Add(-24*time.Hour + 15*time.Hour), End: day.Add(-24*time.Hour + 16*time.Hour), IsOnline: true, JoinURL: join("retro"), ChatID: "19:meeting_retro@thread.v2", Response: "organizer", Attendees: att(nil, 1, 3, 4)},
		{ID: "e1", Subject: "Daily stand-up", Organizer: "Noah Petersen", Start: live, End: live.Add(25 * time.Minute), IsOnline: true, JoinURL: join("standup"), ChatID: "c2", Response: "accepted", Attendees: att(map[string]string{"u3": "tentativelyAccepted"}, 4, 1, 3, 0)},
		{ID: "e2", Subject: "Roadmap review", Organizer: "Sofie Jensen", Start: soon, End: soon.Add(time.Hour), IsOnline: true, JoinURL: join("roadmap"), ChatID: "c1", Location: "Room 3.14", Response: "notResponded", Preview: "Let's go through Q1 priorities.", Attendees: att(map[string]string{"u0": "notResponded", "u2": "declined"}, 1, 0, 2, 6)},
		{ID: "e3", Subject: "Lunch", Start: day.Add(12 * time.Hour), End: day.Add(12*time.Hour + 30*time.Minute), Response: "organizer", Organizer: "Alex Morgan"},
		{ID: "e4", Subject: "Contract walkthrough", Organizer: "Lars Nielsen", Start: day.Add(16 * time.Hour), End: day.Add(17 * time.Hour), IsOnline: true, JoinURL: join("contract"), ChatID: "c3", Response: "tentativelyAccepted", Attendees: att(nil, 2, 0)},
		{ID: "e5", Subject: "Weekly sync", Organizer: "Noah Petersen", Start: day.Add(24*time.Hour + 13*time.Hour), End: day.Add(24*time.Hour + 14*time.Hour), IsOnline: true, JoinURL: join("weekly"), ChatID: "c5", Response: "accepted", Attendees: att(map[string]string{"u6": "notResponded"}, 4, 1, 2, 6, 0)},
		{ID: "e6", Subject: "Company all-hands", Organizer: "Noah Petersen", Start: day.Add(48*time.Hour + 15*time.Hour), End: day.Add(48*time.Hour + 16*time.Hour), IsOnline: true, JoinURL: join("allhands"), Response: "notResponded", Location: "Canteen"},
	}
	c5.JoinURL = join("weekly")
	c2.JoinURL = join("standup")
	s.chats = append(s.chats, &model.Chat{ID: "19:meeting_retro@thread.v2", ChatType: "meeting", Topic: "Sprint retro", Members: []model.User{s.me, u(1), u(3), u(4)}, LastRead: now, JoinURL: join("retro")})
	retro := model.ConvRef{Kind: model.KindChat, ChatID: "19:meeting_retro@thread.v2"}
	add(retro, s.me, day.Add(-24*time.Hour+15*time.Hour+2*time.Minute), "<p>Agenda: what went well, what didn't, actions.</p>")
	add(retro, u(4), day.Add(-24*time.Hour+15*time.Hour+50*time.Minute), "<p>Thanks all — action items are in the transcript 👍</p>")
	s.refreshPreview(s.chats[len(s.chats)-1])

	s.files = []model.DriveItem{
		{ID: "f0", Name: "Projects", IsFolder: true, ChildCount: 2, Modified: ago(24 * time.Hour)},
		{ID: "f1", Name: "Roadmap-Q1.pptx", Size: 2_400_000, Modified: ago(4 * time.Hour), ModifiedBy: "Sofie Jensen", WebURL: "https://contoso.example/Roadmap-Q1.pptx"},
		{ID: "f2", Name: "Contract-template-v3.docx", Size: 88_000, Modified: ago(50 * time.Hour), ModifiedBy: "Lars Nielsen", WebURL: "https://contoso.example/contract.docx"},
		{ID: "f3", Name: "Budget 2027.xlsx", Size: 410_000, Modified: ago(100 * time.Hour), ModifiedBy: "Noah Petersen", WebURL: "https://contoso.example/budget.xlsx"},
	}
}

func (s *Service) refreshPreview(c *model.Chat) {
	list := s.msgs["chat:"+c.ID]
	if len(list) == 0 {
		return
	}
	m := list[len(list)-1]
	c.LastMessage = &model.MessagePreview{ID: m.ID, From: m.From, Text: htmltext.Plain(m.Body), Created: m.Created, Deleted: m.Deleted}
}

// Call posts a call started or ended event to a chat, from its first other
// member. kind is "call" or "meeting".
func (s *Service) Call(chatID, callID, kind string, started bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callLocked(chatID, callID, kind, started)
}

func (s *Service) callLocked(chatID, callID, kind string, started bool) {
	c := s.findChat(chatID)
	if c == nil {
		return
	}
	ev := &model.CallEvent{Started: started, Type: kind, CallID: callID}
	for _, u := range c.Members {
		if u.ID != s.me.ID {
			ev.Initiator = u
			break
		}
	}
	if !started {
		ev.Participants = []string{ev.Initiator.ID}
	}
	c.LastMessage = &model.MessagePreview{ID: s.id(), Text: ev.Label(), Created: time.Now(), Call: ev}
}

var chatter = []string{
	"Sounds good!", "Can we move this to tomorrow?", "I pushed a fix, can you take a look? 🙏",
	"Thanks! 🎉", "On my way", "Let me check and get back to you.", "Meeting notes are in the channel.",
	"Did anyone see the latest numbers? 📈", "Great work everyone 👏",
}

// tick simulates incoming activity.
func (s *Service) tick() {
	if !s.Simulate || time.Since(s.lastTick) < 25*time.Second {
		return
	}
	s.lastTick = time.Now()
	s.ticks++
	switch s.ticks {
	case 2: // an incoming call that nobody answers
		s.callLocked("c3", "demo-call", "call", true)
		return
	case 3:
		s.callLocked("c3", "demo-call", "call", false)
		return
	}
	if s.ticks%3 == 0 {
		// every third tick: activity in a channel
		var refs []model.ConvRef
		for _, t := range s.teams {
			for _, ch := range s.channels[t.ID] {
				refs = append(refs, model.ConvRef{Kind: model.KindChannel, TeamID: t.ID, ChannelID: ch.ID})
			}
		}
		ref := refs[s.rng.Intn(len(refs))]
		s.postAsLocked(s.users[1+s.rng.Intn(len(s.users)-1)].ID, ref, "<p>"+chatter[s.rng.Intn(len(chatter))]+"</p>")
		return
	}
	c := s.chats[s.rng.Intn(len(s.chats))]
	var from model.User
	for _, m := range c.Members {
		if m.ID != s.me.ID {
			from = m
			break
		}
	}
	m := model.Message{
		ID: s.id(), MessageType: "message", From: from, Created: time.Now(), Modified: time.Now(),
		ContentType: "html", Body: "<p>" + chatter[s.rng.Intn(len(chatter))] + "</p>",
		Ref: model.ConvRef{Kind: model.KindChat, ChatID: c.ID}, Importance: "normal",
	}
	s.msgs["chat:"+c.ID] = append(s.msgs["chat:"+c.ID], m)
	s.refreshPreview(c)
	// shuffle presence a bit
	u := s.users[1+s.rng.Intn(len(s.users)-1)]
	states := []string{model.Available, model.Busy, model.Away, model.BeRightBack, model.DoNotDisturb}
	p := s.presence[u.ID]
	p.Availability = states[s.rng.Intn(len(states))]
	p.Activity = p.Availability
	s.presence[u.ID] = p
}

func delay(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(60 * time.Millisecond):
		return nil
	}
}

// Me returns the demo user.
func (s *Service) Me(ctx context.Context) (model.User, error) { return s.me, nil }

// TenantID returns a fake tenant.
func (s *Service) TenantID() string { return "demo-tenant" }

// Chats lists chats.
func (s *Service) Chats(ctx context.Context, cursor model.Page) ([]model.Chat, model.Page, error) {
	if err := delay(ctx); err != nil {
		return nil, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tick()
	out := make([]model.Chat, 0, len(s.chats))
	for _, c := range s.chats {
		cc := *c
		cc.Members = append([]model.User(nil), c.Members...)
		if c.LastMessage != nil {
			lm := *c.LastMessage
			cc.LastMessage = &lm
		}
		out = append(out, cc)
	}
	sort.SliceStable(out, func(i, j int) bool {
		var a, b time.Time
		if out[i].LastMessage != nil {
			a = out[i].LastMessage.Created
		}
		if out[j].LastMessage != nil {
			b = out[j].LastMessage.Created
		}
		return a.After(b)
	})
	return out, "", nil
}

func (s *Service) findChat(id string) *model.Chat {
	for _, c := range s.chats {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// CreateChat creates a chat.
func (s *Service) CreateChat(ctx context.Context, members []model.User, topic string) (model.Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(members) == 1 {
		for _, c := range s.chats {
			if o, ok := c.Other(s.me.ID); ok && o.ID == members[0].ID {
				return *c, nil
			}
		}
	}
	c := &model.Chat{ID: "c" + s.id(), Topic: topic, ChatType: "group", Members: append([]model.User{s.me}, members...), LastRead: time.Now()}
	if len(members) == 1 {
		c.ChatType = "oneOnOne"
		c.Topic = ""
	}
	s.chats = append([]*model.Chat{c}, s.chats...)
	return *c, nil
}

// RenameChat renames a chat.
func (s *Service) RenameChat(ctx context.Context, chatID, topic string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.findChat(chatID); c != nil {
		c.Topic = topic
	}
	return nil
}

// AddChatMember adds a member.
func (s *Service) AddChatMember(ctx context.Context, chatID string, u model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.findChat(chatID); c != nil {
		c.Members = append(c.Members, u)
		if c.ChatType == "oneOnOne" {
			c.ChatType = "group"
		}
	}
	return nil
}

// LeaveChat leaves a chat.
func (s *Service) LeaveChat(ctx context.Context, chatID string) error { return s.HideChat(ctx, chatID) }

// HideChat hides a chat.
func (s *Service) HideChat(ctx context.Context, chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.chats {
		if c.ID == chatID {
			s.chats = append(s.chats[:i], s.chats[i+1:]...)
			break
		}
	}
	return nil
}

// MarkChatRead marks a chat read.
func (s *Service) MarkChatRead(ctx context.Context, chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.findChat(chatID); c != nil {
		c.LastRead = time.Now()
	}
	return nil
}

// Teams lists teams.
func (s *Service) Teams(ctx context.Context) ([]model.Team, error) {
	return append([]model.Team(nil), s.teams...), delay(ctx)
}

// Channels lists channels.
func (s *Service) Channels(ctx context.Context, teamID string) ([]model.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]model.Channel(nil), s.channels[teamID]...)
	model.SortChannels(out)
	return out, nil
}

// Members lists conversation members.
func (s *Service) Members(ctx context.Context, ref model.ConvRef) ([]model.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref.Kind == model.KindChat {
		if c := s.findChat(ref.ChatID); c != nil {
			return append([]model.User(nil), c.Members...), nil
		}
	}
	return append([]model.User(nil), s.users...), nil
}

func baseKey(ref model.ConvRef) string {
	ref.ThreadID = ""
	return ref.Key()
}

// Messages returns messages, oldest first.
func (s *Service) Messages(ctx context.Context, ref model.ConvRef, cursor model.Page) ([]model.Message, model.Page, error) {
	if err := delay(ctx); err != nil {
		return nil, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tick()
	list := s.msgs[baseKey(ref)]
	if ref.ThreadID != "" {
		for _, m := range list {
			if m.ID == ref.ThreadID {
				root := m
				root.Replies = nil
				out := append([]model.Message{root}, m.Replies...)
				for i := range out {
					out[i].Ref = ref
				}
				return out, "", nil
			}
		}
		return nil, "", fmt.Errorf("thread not found")
	}
	out := make([]model.Message, len(list))
	for i, m := range list {
		out[i] = m
		out[i].Reactions = append([]model.Reaction(nil), m.Reactions...)
		out[i].Replies = append([]model.Message(nil), m.Replies...)
		out[i].Ref = ref
	}
	return out, "", nil
}

func (s *Service) find(ref model.ConvRef, id string) *model.Message {
	list := s.msgs[baseKey(ref)]
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
		for j := range list[i].Replies {
			if list[i].Replies[j].ID == id {
				return &list[i].Replies[j]
			}
		}
	}
	return nil
}

// Send sends a message.
func (s *Service) Send(ctx context.Context, ref model.ConvRef, out model.Outgoing) (model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := model.Message{
		ID: s.id(), MessageType: "message", From: s.me, Created: time.Now(), Modified: time.Now(),
		ContentType: "html", Body: out.HTML, Subject: out.Subject, Importance: out.Importance,
		Attachments: out.Attachments, Ref: ref,
	}
	for _, a := range out.Attachments {
		m.Body += `<attachment id="` + a.ID + `"></attachment>`
	}
	for _, mm := range out.Mentions {
		m.Mentions = append(m.Mentions, model.Mention{ID: mm.ID, Text: mm.Text, UserID: out.MentionUser[mm.ID].ID})
	}
	key := baseKey(ref)
	if ref.ThreadID != "" {
		root := s.find(ref, ref.ThreadID)
		if root == nil {
			return m, fmt.Errorf("thread not found")
		}
		m.ReplyToID = root.ID
		root.Replies = append(root.Replies, m)
		root.ReplyCount++
		return m, nil
	}
	s.msgs[key] = append(s.msgs[key], m)
	if ref.Kind == model.KindChat {
		if c := s.findChat(ref.ChatID); c != nil {
			s.refreshPreview(c)
			c.LastRead = time.Now()
		}
		// simulate a reply
		go func() {
			time.Sleep(3 * time.Second)
			s.mu.Lock()
			defer s.mu.Unlock()
			c := s.findChat(ref.ChatID)
			if c == nil || !s.Simulate {
				return
			}
			for _, u := range c.Members {
				if u.ID != s.me.ID {
					r := model.Message{ID: s.id(), MessageType: "message", From: u, Created: time.Now(), Modified: time.Now(), ContentType: "html", Body: "<p>" + chatter[s.rng.Intn(len(chatter))] + "</p>", Ref: ref, Importance: "normal"}
					s.msgs[key] = append(s.msgs[key], r)
					s.refreshPreview(c)
					return
				}
			}
		}()
	}
	return m, nil
}

// Edit edits a message.
func (s *Service) Edit(ctx context.Context, ref model.ConvRef, msgID string, out model.Outgoing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.find(ref, msgID)
	if m == nil {
		return fmt.Errorf("message not found")
	}
	m.Body = out.HTML
	m.Edited = true
	return nil
}

// Delete deletes a message.
func (s *Service) Delete(ctx context.Context, ref model.ConvRef, msgID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.find(ref, msgID)
	if m == nil {
		return fmt.Errorf("message not found")
	}
	m.Deleted = true
	m.Body = ""
	return nil
}

// React toggles a reaction.
func (s *Service) React(ctx context.Context, ref model.ConvRef, msgID, emoji string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.find(ref, msgID)
	if m == nil {
		return fmt.Errorf("message not found")
	}
	var rs []model.Reaction
	for _, r := range m.Reactions {
		if r.UserID == s.me.ID && r.Type == emoji {
			continue
		}
		rs = append(rs, r)
	}
	if on {
		rs = append(rs, model.Reaction{Type: emoji, UserID: s.me.ID, Created: time.Now()})
	}
	m.Reactions = rs
	return nil
}

// Upload pretends to upload a file.
func (s *Service) Upload(ctx context.Context, ref model.ConvRef, localPath string) (model.Attachment, error) {
	st, err := os.Stat(localPath)
	if err != nil {
		return model.Attachment{}, err
	}
	s.mu.Lock()
	id := s.id()
	s.mu.Unlock()
	return model.Attachment{ID: id, ContentType: "reference", Name: filepath.Base(localPath), ContentURL: "file://" + localPath + "?size=" + strconv.FormatInt(st.Size(), 10)}, nil
}

// FetchURL writes a placeholder file.
func (s *Service) FetchURL(ctx context.Context, url string, dest io.Writer) error {
	_, err := fmt.Fprintln(dest, "demo content from "+url)
	return err
}

// Presence returns presence.
func (s *Service) Presence(ctx context.Context, ids []string) (map[string]model.Presence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]model.Presence{}
	for _, id := range ids {
		if p, ok := s.presence[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

// MyPresence returns my presence.
func (s *Service) MyPresence(ctx context.Context) (model.Presence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.presence[s.me.ID], nil
}

// SetPresence sets my presence.
func (s *Service) SetPresence(ctx context.Context, availability, activity string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.presence[s.me.ID]
	p.Availability, p.Activity = availability, activity
	s.presence[s.me.ID] = p
	return nil
}

// ClearPresence resets presence.
func (s *Service) ClearPresence(ctx context.Context) error {
	return s.SetPresence(ctx, model.Available, model.Available)
}

// SetStatusMessage sets the status message.
func (s *Service) SetStatusMessage(ctx context.Context, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.presence[s.me.ID]
	p.StatusMessage = text
	s.presence[s.me.ID] = p
	return nil
}

// Events lists events.
func (s *Service) Events(ctx context.Context, start, end time.Time) ([]model.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Event
	for _, e := range s.events {
		if e.End.After(start) && e.Start.Before(end) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// RespondEvent responds to an invite.
func (s *Service) RespondEvent(ctx context.Context, id, response string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]string{"accept": "accepted", "tentative": "tentativelyAccepted", "decline": "declined"}
	for i := range s.events {
		if s.events[i].ID == id {
			s.events[i].Response = m[response]
		}
	}
	return nil
}

// CreateEvent creates an event.
func (s *Service) CreateEvent(ctx context.Context, ev model.NewEvent) (model.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := model.Event{ID: s.id(), Subject: ev.Subject, Start: ev.Start, End: ev.End, Organizer: s.me.DisplayName, Response: "organizer", IsOnline: ev.Online, Preview: ev.Body}
	for _, a := range ev.Attendees {
		e.Attendees = append(e.Attendees, model.Attendee{Name: a, Email: a, Response: "none", Type: "required"})
	}
	if ev.Online {
		e.JoinURL = "https://teams.microsoft.com/l/meetup-join/" + e.ID
	}
	s.events = append(s.events, e)
	return e, nil
}

// SearchMessages does a substring search.
func (s *Service) SearchMessages(ctx context.Context, q string) ([]model.SearchHit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q = strings.ToLower(q)
	var out []model.SearchHit
	for _, list := range s.msgs {
		for _, m := range list {
			all := append([]model.Message{m}, m.Replies...)
			for _, mm := range all {
				txt := htmltext.Plain(mm.Body)
				if strings.Contains(strings.ToLower(txt+" "+mm.Subject), q) {
					ref := m.Ref
					if mm.ReplyToID != "" {
						ref.ThreadID = mm.ReplyToID
					}
					out = append(out, model.SearchHit{Summary: txt, From: mm.From.DisplayName, Created: mm.Created, Ref: ref, MsgID: mm.ID})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// SearchPeople searches users.
func (s *Service) SearchPeople(ctx context.Context, q string) ([]model.User, error) {
	q = strings.ToLower(q)
	var out []model.User
	for _, u := range s.users {
		if u.ID == s.me.ID {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(u.DisplayName+" "+u.Email), q) {
			out = append(out, u)
		}
	}
	return out, nil
}

// RecentFiles lists files.
func (s *Service) RecentFiles(ctx context.Context) ([]model.DriveItem, error) {
	var out []model.DriveItem
	for _, f := range s.files {
		if !f.IsFolder {
			out = append(out, f)
		}
	}
	return out, nil
}

// Folder lists a folder.
func (s *Service) Folder(ctx context.Context, item *model.DriveItem) ([]model.DriveItem, error) {
	if item == nil {
		return append([]model.DriveItem(nil), s.files...), nil
	}
	return []model.DriveItem{
		{ID: "f10", Name: "Spec.md", Size: 4_200, Modified: time.Now().Add(-time.Hour), WebURL: "https://contoso.example/spec"},
		{ID: "f11", Name: "Diagram.png", Size: 120_000, Modified: time.Now().Add(-2 * time.Hour), WebURL: "https://contoso.example/diagram"},
	}, nil
}

// ChannelFiles lists channel files.
func (s *Service) ChannelFiles(ctx context.Context, teamID, channelID string) ([]model.DriveItem, error) {
	return []model.DriveItem{
		{ID: "cf1", Name: "Meeting notes.docx", Size: 34_000, Modified: time.Now().Add(-26 * time.Hour), ModifiedBy: "Noah Petersen", WebURL: "https://contoso.example/notes"},
		{ID: "cf2", Name: "Architecture.vsdx", Size: 980_000, Modified: time.Now().Add(-200 * time.Hour), ModifiedBy: "Alex Morgan", WebURL: "https://contoso.example/arch"},
	}, nil
}

// Download writes a placeholder file.
func (s *Service) Download(ctx context.Context, item model.DriveItem, dest io.Writer) error {
	return s.FetchURL(ctx, item.WebURL, dest)
}

// ResolveUsers maps emails to demo users.
func (s *Service) ResolveUsers(ctx context.Context, emails []string) (map[string]model.User, error) {
	out := map[string]model.User{}
	for _, e := range emails {
		for _, u := range s.users {
			if strings.EqualFold(u.Email, e) {
				out[strings.ToLower(e)] = u
			}
		}
	}
	return out, nil
}

const demoVTT = `WEBVTT

00:00:03.000 --> 00:00:07.500
<v Alex Morgan>Welcome to the retro. Let's start with what went well.</v>

00:00:08.000 --> 00:00:14.000
<v Sofie Jensen>The new onboarding flow shipped on time.</v>

00:00:14.500 --> 00:00:18.000
<v Sofie Jensen>And support tickets are down twenty percent.</v>

00:00:19.000 --> 00:00:25.000
<v Noah Petersen>What didn't go well: the billing timeouts. We need better alerting.</v>

00:00:26.000 --> 00:00:31.000
<v Emma Larsen>I'll own the dashboard for that. Action item for me.</v>
`

// MeetingArtifacts returns sample artifacts for past meetings.
func (s *Service) MeetingArtifacts(ctx context.Context, joinURL string) (model.MeetingArtifacts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if e.JoinURL != joinURL {
			continue
		}
		if e.End.After(time.Now()) {
			return model.MeetingArtifacts{MeetingID: e.ID}, nil
		}
		return model.MeetingArtifacts{
			MeetingID:   e.ID,
			Transcripts: []model.Transcript{{ID: "t-" + e.ID, MeetingID: e.ID, Created: e.End, ContentURL: "demo://transcript/" + e.ID}},
			Recordings:  []model.Recording{{ID: "r-" + e.ID, MeetingID: e.ID, Created: e.End, ContentURL: "demo://recording/" + e.ID}},
		}, nil
	}
	return model.MeetingArtifacts{}, fmt.Errorf("meeting not found")
}

// TranscriptText returns a sample transcript.
func (s *Service) TranscriptText(ctx context.Context, t model.Transcript) (string, error) {
	return demoVTT, nil
}

// PostAs adds a message from another user (for simulation and tests).
func (s *Service) PostAs(userID string, ref model.ConvRef, html string) model.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.postAsLocked(userID, ref, html)
}

func (s *Service) postAsLocked(userID string, ref model.ConvRef, html string) model.Message {
	var from model.User
	for _, u := range s.users {
		if u.ID == userID {
			from = u
		}
	}
	m := model.Message{ID: s.id(), MessageType: "message", From: from, Created: time.Now(), Modified: time.Now(), ContentType: "html", Body: html, Ref: ref, Importance: "normal"}
	key := baseKey(ref)
	if ref.ThreadID != "" {
		if root := s.find(ref, ref.ThreadID); root != nil {
			m.ReplyToID = root.ID
			root.Replies = append(root.Replies, m)
			root.ReplyCount++
		}
		return m
	}
	s.msgs[key] = append(s.msgs[key], m)
	if ref.Kind == model.KindChat {
		if c := s.findChat(ref.ChatID); c != nil {
			s.refreshPreview(c)
		}
	}
	return m
}

//go:embed samples/approval.json
var sampleApproval string

//go:embed samples/connector.json
var sampleConnector string

//go:embed samples/table.json
var sampleTable string

// SharedFiles lists files shared with the demo user.
func (s *Service) SharedFiles(ctx context.Context) ([]model.DriveItem, error) {
	return []model.DriveItem{
		{ID: "sh1", Name: "Brand guidelines.pdf", Size: 3_100_000, Modified: time.Now().Add(-30 * time.Hour), ModifiedBy: "Emma Larsen", WebURL: "https://contoso.example/brand"},
		{ID: "sh2", Name: "Team offsite.xlsx", Size: 54_000, Modified: time.Now().Add(-75 * time.Hour), ModifiedBy: "Noah Petersen", WebURL: "https://contoso.example/offsite"},
	}, nil
}

// UploadFile pretends to upload a file to OneDrive.
func (s *Service) UploadFile(ctx context.Context, folder *model.DriveItem, localPath string) (model.DriveItem, error) {
	st, err := os.Stat(localPath)
	if err != nil {
		return model.DriveItem{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it := model.DriveItem{ID: s.id(), Name: filepath.Base(localPath), Size: st.Size(), Modified: time.Now(), ModifiedBy: s.me.DisplayName, WebURL: "file://" + localPath}
	if folder == nil {
		s.files = append(s.files, it)
	}
	return it, nil
}
