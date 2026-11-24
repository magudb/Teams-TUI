package ui

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/magudb/Teams-TUI/internal/cards"
	"github.com/magudb/Teams-TUI/internal/model"
)

// convView holds the state of one open conversation (chat, channel or thread).
type convView struct {
	changedBlocks []int
	pendingSelect string
	headRequest   uint64
	fetchingHead  bool
	ref           model.ConvRef
	msgs          []model.Message
	next          model.Page
	loading       bool
	loadedOnce    bool
	err           string
	sel           int // selected message index, -1 = none
	scroll        int // first visible line
	follow        bool
	dirty         bool
	index         map[string]int
	lastUsed      uint64
	tempSeq       int
	failed        map[string]bool

	// render cache
	lines       []string
	owner       []int // message index of each line (-1: none, -2: "load older")
	grouped     []bool
	renderedSel int
	msgStart    []int
	width       int
	links       map[string][]string // msg id -> links (attachments, hrefs, images)

	// newSince marks where the "New messages" divider goes (chats opened while unread)
	newSince time.Time

	side bool // rendered for a side pane (different fills)

	cardState map[string]*cards.State // "<msgID>:<attachment index>" -> interactive card state
	cardActs  map[string]cardRef      // zone id -> card button (rebuilt on render)
}

func newConvView(ref model.ConvRef) *convView {
	return &convView{index: map[string]int{}, ref: ref, sel: -1, follow: true, dirty: true, loading: true, failed: map[string]bool{}, links: map[string][]string{},
		cardState: map[string]*cards.State{}, cardActs: map[string]cardRef{}}
}

func sameMessage(a, b model.Message) bool {
	return a.ID == b.ID && a.ReplyToID == b.ReplyToID && a.MessageType == b.MessageType && a.From == b.From &&
		a.Created.Equal(b.Created) && a.Modified.Equal(b.Modified) && a.Edited == b.Edited && a.Deleted == b.Deleted &&
		a.Subject == b.Subject && a.Importance == b.Importance && a.ContentType == b.ContentType && a.Body == b.Body &&
		a.ReplyCount == b.ReplyCount && a.Ref == b.Ref && slices.Equal(a.Mentions, b.Mentions) && slices.Equal(a.Attachments, b.Attachments) &&
		slices.Equal(a.Reactions, b.Reactions) && slices.EqualFunc(a.Replies, b.Replies, sameMessage)
}

// merge merges fetched messages; returns whether anything changed.
func (cv *convView) merge(in []model.Message, older bool) bool {
	changed := false
	reorder := false
	var selID string
	if cv.sel >= 0 && cv.sel < len(cv.msgs) {
		selID = cv.msgs[cv.sel].ID
	}
	for _, m := range in {
		if i, ok := cv.index[m.ID]; ok {
			old := cv.msgs[i]
			if !sameMessage(old, m) {
				if !old.Created.Equal(m.Created) {
					reorder = true
				}
				if reorder || old.MessageType != m.MessageType || old.Subject != m.Subject || old.From.ID != m.From.ID {
					cv.dirty = true // grouping and dividers may change
				} else if !cv.dirty {
					cv.changedBlocks = append(cv.changedBlocks, i)
				}
				cv.msgs[i] = m
				changed = true
			}
			continue
		}
		// The send acknowledgement correlates server IDs with optimistic sends.
		// Equal text alone cannot identify two concurrent messages.
		reorder = true
		cv.dirty = true
		cv.index[m.ID] = len(cv.msgs)
		cv.msgs = append(cv.msgs, m)
		changed = true
	}
	if reorder {
		sort.SliceStable(cv.msgs, func(i, j int) bool { return cv.msgs[i].Created.Before(cv.msgs[j].Created) })
		cv.sel = -1
		for i, m := range cv.msgs {
			cv.index[m.ID] = i
			if m.ID == selID {
				cv.sel = i
			}
		}
		if older {
			cv.follow = false
		}
	}
	return changed
}

func (cv *convView) addTemp(me model.User, out model.Outgoing) string {
	cv.tempSeq++
	id := "tmp:" + strconv.Itoa(cv.tempSeq)
	cv.index[id] = len(cv.msgs)
	cv.msgs = append(cv.msgs, model.Message{
		ID: id, MessageType: "message", From: me, Created: time.Now(), Modified: time.Now(),
		ContentType: "html", Body: out.HTML, Subject: out.Subject, Importance: out.Importance,
		Attachments: out.Attachments, Ref: cv.ref,
	})
	cv.follow = true
	cv.dirty = true
	return id
}

func (cv *convView) replaceTemp(tempID string, m model.Message) {
	i, ok := cv.index[tempID]
	if !ok || m.ID == "" {
		return
	}
	if _, ok := cv.index[m.ID]; ok {
		cv.removeMessage(i)
		return
	}
	delete(cv.index, tempID)
	cv.index[m.ID] = i
	cv.msgs[i] = m
	cv.dirty = true
}

func (cv *convView) removeMessage(i int) {
	id := cv.msgs[i].ID
	delete(cv.index, id)
	delete(cv.failed, id)
	cv.msgs = slices.Delete(cv.msgs, i, i+1)
	for j := i; j < len(cv.msgs); j++ {
		cv.index[cv.msgs[j].ID] = j
	}
	if cv.sel == i {
		cv.sel = -1
	} else if cv.sel > i {
		cv.sel--
	}
	cv.dirty = true
}

func (cv *convView) markFailed(tempID string) {
	cv.failed[tempID] = true
	cv.dirty = true
}

func (cv *convView) selected() *model.Message {
	if cv.sel >= 0 && cv.sel < len(cv.msgs) {
		return &cv.msgs[cv.sel]
	}
	return nil
}

func (cv *convView) selectMsg(i int) {
	if len(cv.msgs) == 0 {
		cv.sel = -1
		return
	}
	i = max(0, min(i, len(cv.msgs)-1))
	cv.sel = i
	cv.follow = false
}

func quotedPreview(content string) string {
	// messageReference content is JSON with messagePreview / messageSender.
	i := strings.Index(content, `"messagePreview":"`)
	if i < 0 {
		return "quoted message"
	}
	rest := content[i+len(`"messagePreview":"`):]
	if j := strings.Index(rest, `"`); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func cardTitle(a model.Attachment) string {
	for _, key := range []string{`"title":"`, `"text":"`} {
		if i := strings.Index(a.Content, key); i >= 0 {
			rest := a.Content[i+len(key):]
			if j := strings.Index(rest, `"`); j >= 0 {
				return "Card: " + rest[:j]
			}
		}
	}
	if a.Name != "" {
		return "Card: " + a.Name
	}
	return "Card"
}

// ensureVisible adjusts scroll so the selected message is visible.
func (cv *convView) ensureVisible(height int) {
	if cv.sel < 0 || cv.sel >= len(cv.msgStart) {
		return
	}
	start := cv.msgStart[cv.sel]
	end := start
	for end+1 < len(cv.owner) && cv.owner[end+1] == cv.sel {
		end++
	}
	if end-start+1 > height || start < cv.scroll {
		cv.scroll = start
	} else if end >= cv.scroll+height {
		cv.scroll = end - height + 1
	}
}

func (cv *convView) clampScroll(height int) {
	maxScroll := max(0, len(cv.lines)-height)
	if cv.follow {
		cv.scroll = maxScroll
	}
	cv.scroll = max(0, min(cv.scroll, maxScroll))
	if cv.scroll == maxScroll && cv.sel < 0 {
		cv.follow = true
	}
}

func (cv *convView) scrollBy(n, height int) {
	cv.follow = false
	cv.scroll += n
	cv.clampScroll(height)
	if cv.scroll >= len(cv.lines)-height {
		cv.follow = cv.sel < 0
	}
}

func (cv *convView) view(height int) []string {
	cv.clampScroll(height)
	end := min(len(cv.lines), cv.scroll+height)
	out := make([]string, 0, height)
	out = append(out, cv.lines[cv.scroll:end]...)
	return out
}

// Keep recently used histories and preserve optimistic or failed sends until
// the server acknowledges them or the user discards them.
func (m *Model) touchConversation(cv *convView) {
	m.conversationUse++
	cv.lastUsed = m.conversationUse
	const historyLimit = 16
	for len(m.convs) > historyLimit {
		var oldest *convView
		for _, candidate := range m.convs {
			if candidate == m.conv || candidate == m.thread || (oldest != nil && candidate.lastUsed >= oldest.lastUsed) {
				continue
			}
			if candidate.tempSeq > 0 && slices.ContainsFunc(candidate.msgs, func(msg model.Message) bool { return strings.HasPrefix(msg.ID, "tmp:") }) {
				continue
			}
			oldest = candidate
		}
		if oldest == nil {
			return
		}
		delete(m.convs, oldest.ref.Key())
	}
}
