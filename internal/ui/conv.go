package ui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/magudb/Teams-TUI/internal/cards"
	"github.com/magudb/Teams-TUI/internal/model"
)

// convView holds the state of one open conversation (chat, channel or thread).
type convView struct {
	ref        model.ConvRef
	msgs       []model.Message
	next       model.Page
	loading    bool
	loadedOnce bool
	err        string
	sel        int // selected message index, -1 = none
	scroll     int // first visible line
	follow     bool
	dirty      bool
	tempSeq    int
	failed     map[string]bool

	// render cache
	lines    []string
	owner    []int // message index of each line (-1: none, -2: "load older")
	msgStart []int
	width    int
	links    map[string][]string // msg id -> links (attachments, hrefs, images)

	// newSince marks where the "New messages" divider goes (chats opened while unread)
	newSince time.Time

	cardState map[string]*cards.State // "<msgID>:<attachment index>" -> interactive card state
	cardActs  map[string]cardRef      // zone id -> card button (rebuilt on render)
}

func newConvView(ref model.ConvRef) *convView {
	return &convView{ref: ref, sel: -1, follow: true, dirty: true, loading: true, failed: map[string]bool{}, links: map[string][]string{},
		cardState: map[string]*cards.State{}, cardActs: map[string]cardRef{}}
}

func msgSig(m model.Message) string {
	var sb strings.Builder
	sb.WriteString(m.Modified.String())
	sb.WriteString(m.Body)
	sb.WriteString(strconv.FormatBool(m.Deleted))
	sb.WriteString(strconv.Itoa(m.ReplyCount))
	for _, r := range m.Reactions {
		sb.WriteString(r.Type + r.UserID)
	}
	return sb.String()
}

// merge merges fetched messages; returns whether anything changed.
func (cv *convView) merge(in []model.Message, older bool) bool {
	idx := make(map[string]int, len(cv.msgs))
	for i, m := range cv.msgs {
		idx[m.ID] = i
	}
	changed := false
	var selID string
	if cv.sel >= 0 && cv.sel < len(cv.msgs) {
		selID = cv.msgs[cv.sel].ID
	}
	for _, m := range in {
		if i, ok := idx[m.ID]; ok {
			if msgSig(cv.msgs[i]) != msgSig(m) {
				cv.msgs[i] = m
				changed = true
			}
			continue
		}
		// replace a pending temp message with the same body from me
		replaced := false
		for i, t := range cv.msgs {
			if strings.HasPrefix(t.ID, "tmp:") && !cv.failed[t.ID] && t.From.ID == m.From.ID && t.Body == m.Body {
				cv.msgs[i] = m
				replaced = true
				break
			}
		}
		if !replaced {
			cv.msgs = append(cv.msgs, m)
		}
		changed = true
	}
	if changed {
		sort.SliceStable(cv.msgs, func(i, j int) bool { return cv.msgs[i].Created.Before(cv.msgs[j].Created) })
		cv.sel = -1
		for i, m := range cv.msgs {
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
	for i, t := range cv.msgs {
		if t.ID == tempID {
			if m.ID == "" {
				return
			}
			for _, o := range cv.msgs {
				if o.ID == m.ID { // poll already brought it in
					cv.msgs = append(cv.msgs[:i], cv.msgs[i+1:]...)
					cv.dirty = true
					return
				}
			}
			cv.msgs[i] = m
			cv.dirty = true
			return
		}
	}
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
	if cv.sel != i {
		cv.sel = i
		cv.dirty = true
	}
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
