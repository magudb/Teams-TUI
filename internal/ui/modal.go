package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/magudb/Teams-TUI/internal/model"
)

type modalKind int

const (
	mList modalKind = iota
	mPeople
	mPrompt
	mConfirm
	mText
	mForm
)

type modalItem struct {
	section string // optional group heading (command palette)
	label   string
	hint    string
	run     func(m *Model) tea.Cmd
	user    *model.User
}

type modal struct {
	kind     modalKind
	title    string
	input    textinput.Model
	hasInput bool
	all      []modalItem
	items    []modalItem
	sel      int
	scroll   int
	width    int
	grid     bool

	onText func(m *Model, s string) tea.Cmd

	multi    bool
	chosen   []model.User
	onPeople func(m *Model, us []model.User) tea.Cmd
	lastQ    string
	debounce int

	text     []string
	raw      string // plain text for copy / save (transcripts)
	saveName string

	labels   []string
	fields   []textinput.Model
	fieldSel int
	onForm   func(m *Model, vals []string) tea.Cmd
}

func newInput(placeholder, value string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Prompt = "› "
	ti.SetValue(value)
	ti.CharLimit = 2000
	ti.Focus()
	return ti
}

func (m *Model) openList(title string, items []modalItem, filter bool) *modal {
	md := &modal{kind: mList, title: title, all: items, items: items, width: 52}
	if filter {
		md.hasInput = true
		md.input = newInput("Type to filter", "")
	}
	m.modal = md
	return md
}

func (md *modal) applyFilter() {
	if !md.hasInput || md.kind != mList {
		return
	}
	q := strings.ToLower(strings.TrimSpace(md.input.Value()))
	if q == "" {
		md.items = md.all
	} else {
		md.items = nil
		for _, it := range md.all {
			if fuzzy(strings.ToLower(it.label+" "+it.hint), q) {
				md.items = append(md.items, it)
			}
		}
	}
	if md.sel >= len(md.items) {
		md.sel = max(0, len(md.items)-1)
	}
}

// fuzzy reports whether all words of q appear in s.
func fuzzy(s, q string) bool {
	for _, w := range strings.Fields(q) {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

func (md *modal) setPeople(msg peopleMsg) {
	if md.kind != mPeople || msg.q != md.lastQ {
		return
	}
	md.items = nil
	for _, u := range msg.users {
		u := u
		md.items = append(md.items, modalItem{label: u.DisplayName, hint: u.Email, user: &u})
	}
	if msg.err != nil {
		md.items = append(md.items, modalItem{label: "Error: " + oneLineErr(msg.err)})
	}
	md.sel = 0
	md.scroll = 0
}

func (m *Model) openPrompt(title, placeholder, value string, fn func(m *Model, s string) tea.Cmd) {
	md := &modal{kind: mPrompt, title: title, hasInput: true, input: newInput(placeholder, value), onText: fn, width: 60}
	m.modal = md
}

func (m *Model) openConfirm(title string, fn func(m *Model) tea.Cmd) {
	m.openList("Confirm\n"+title, []modalItem{
		{label: "Yes", run: fn},
		{label: "Cancel", run: func(m *Model) tea.Cmd { return nil }},
	}, false).width = min(70, max(36, ansi.StringWidth(title)+6))
}

// ---- concrete modals ----------------------------------------------------------

func (m *Model) openPresencePicker() {
	set := func(key string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.setPresenceCmd(key) }
	}
	items := []modalItem{
		{label: m.s.OK.Render("●") + " Available", run: set("available")},
		{label: m.s.Danger.Render("●") + " Busy", run: set("busy")},
		{label: m.s.Danger.Render("⊖") + " Do not disturb", run: set("dnd")},
		{label: m.s.Warn.Render("◷") + " Be right back", run: set("brb")},
		{label: m.s.Warn.Render("◷") + " Appear away", run: set("away")},
		{label: m.s.Faint.Render("○") + " Appear offline", run: set("offline")},
		{label: "↺ Reset status", run: set("reset")},
		{label: "✎ Set status message…", hint: m.myPresence.StatusMessage, run: func(m *Model) tea.Cmd {
			m.openPrompt("Status message", "What's happening?", m.myPresence.StatusMessage, func(m *Model, s string) tea.Cmd { return m.setStatusMessage(s) })
			return nil
		}},
	}
	m.openList("Set status — "+presenceLabel(m.myPresence), items, false)
}

func (m *Model) setPresenceCmd(key string) tea.Cmd {
	type ps struct{ a, act string }
	v, ok := map[string]ps{
		"available": {model.Available, model.Available}, "busy": {model.Busy, model.Busy},
		"dnd": {model.DoNotDisturb, model.DoNotDisturb}, "brb": {model.BeRightBack, model.BeRightBack},
		"away": {model.Away, model.Away}, "offline": {model.Offline, "OffWork"},
	}[key]
	if key != "reset" && !ok {
		return nil
	}
	if ok {
		m.myPresence.Availability, m.myPresence.Activity = v.a, v.act
	}
	return m.op(func(svc model.Service) opMsg {
		c, cancel := ctx()
		defer cancel()
		var err error
		if key == "reset" {
			err = svc.ClearPresence(c)
		} else {
			err = svc.SetPresence(c, v.a, v.act)
		}
		return opMsg{err: err, toast: "Status updated", presenc: true}
	})
}

func (m *Model) setStatusMessage(text string) tea.Cmd {
	m.myPresence.StatusMessage = text
	return m.op(func(svc model.Service) opMsg {
		c, cancel := ctx()
		defer cancel()
		return opMsg{err: svc.SetStatusMessage(c, text), toast: "Status message updated", presenc: true}
	})
}

var allEmoji = []string{
	"👍", "❤️", "😆", "😮", "😢", "😡", "🎉", "👏", "🙏", "🔥", "🚀", "👀", "✅", "❌", "💯", "🤔",
	"😄", "😁", "😂", "🤣", "😊", "😉", "😍", "😘", "😎", "🤩", "😅", "🙂", "🙃", "😐", "😴", "🤯",
	"🥳", "😬", "🤗", "🤝", "💪", "👋", "👌", "✌️", "🤞", "👎", "💡", "⚠️", "⭐", "☕", "🍕", "🍺",
}

var emojiNames = map[string]string{
	"👍": "like thumbs up", "❤️": "heart love", "😆": "laugh", "😮": "surprised wow", "😢": "sad cry", "😡": "angry",
	"🎉": "party tada", "👏": "clap", "🙏": "pray thanks", "🔥": "fire", "🚀": "rocket", "👀": "eyes", "✅": "check done",
	"❌": "cross no", "💯": "hundred", "🤔": "thinking", "😄": "smile", "😂": "joy", "😎": "cool", "💪": "strong",
	"👋": "wave hi", "👌": "ok", "👎": "dislike", "💡": "idea", "⚠️": "warning", "⭐": "star", "☕": "coffee",
}

func (m *Model) openEmojiPicker(title string, fn func(m *Model, e string) tea.Cmd) {
	var items []modalItem
	for _, e := range allEmoji {
		e := e
		items = append(items, modalItem{label: e, hint: emojiNames[e], run: func(m *Model) tea.Cmd { return fn(m, e) }})
	}
	md := m.openList(title, items, true)
	md.grid = true
	md.width = 44
}

func (m *Model) openReactionPicker(cv *convView, msg *model.Message) {
	ref, id := cv.ref, msg.ID
	m.openEmojiPicker("React", func(m *Model, e string) tea.Cmd { return m.toggleReaction(ref, id, e) })
}

func (m *Model) toggleReaction(ref model.ConvRef, msgID, emoji string) tea.Cmd {
	cv := m.convs[ref.Key()]
	on := true
	if cv != nil {
		for i := range cv.msgs {
			if cv.msgs[i].ID != msgID {
				continue
			}
			msg := &cv.msgs[i]
			var rs []model.Reaction
			for _, r := range msg.Reactions {
				if r.UserID == m.me.ID && r.Type == emoji {
					on = false
					continue
				}
				rs = append(rs, r)
			}
			if on {
				rs = append(rs, model.Reaction{Type: emoji, UserID: m.me.ID, Created: time.Now()})
			}
			msg.Reactions = rs
			cv.dirty = true
		}
	}
	return m.op(func(svc model.Service) opMsg {
		c, cancel := ctx()
		defer cancel()
		return opMsg{err: svc.React(c, ref, msgID, emoji, on), reload: ref}
	})
}

func (m *Model) openPeoplePicker(title string, multi bool, initial string) {
	md := &modal{kind: mPeople, title: title, hasInput: true, input: newInput("Search people by name or email", initial), multi: multi, width: 60}
	md.onPeople = func(m *Model, us []model.User) tea.Cmd {
		if len(us) == 0 {
			return nil
		}
		topic := ""
		if len(us) > 1 {
			names := []string{}
			for _, u := range us {
				names = append(names, strings.Fields(u.DisplayName)[0])
			}
			topic = ""
			_ = names
		}
		svc := m.svc
		m.busy++
		return func() tea.Msg {
			c, cancel := ctx()
			defer cancel()
			ch, err := svc.CreateChat(c, us, topic)
			return chatCreatedMsg{ch, err}
		}
	}
	m.modal = md
	md.lastQ = initial
	m.pendingCmd = m.searchPeople(initial)
}

func (m *Model) openAddPeople(chatID string) {
	m.openPeoplePicker("Add people", true, "")
	m.modal.onPeople = func(m *Model, us []model.User) tea.Cmd {
		var cmds []tea.Cmd
		for _, u := range us {
			u := u
			cmds = append(cmds, m.op(func(svc model.Service) opMsg {
				c, cancel := ctx()
				defer cancel()
				return opMsg{err: svc.AddChatMember(c, chatID, u), toast: "Added " + u.DisplayName, chats: true}
			}))
		}
		return tea.Batch(cmds...)
	}
}

func (m *Model) openAttachPrompt() {
	m.openPrompt("Attach file", "Path to file, e.g. ~/Documents/report.pdf", "", func(m *Model, s string) tea.Cmd {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return m.attachFile(s)
	})
}

func (m *Model) openLinkPicker(links []string) {
	if len(links) == 1 {
		m.pendingCmd = m.openLink(links[0])
		return
	}
	var items []modalItem
	for _, l := range links {
		l := l
		items = append(items, modalItem{label: l, run: func(m *Model) tea.Cmd { return m.openLink(l) }})
	}
	m.openList("Open link", items, true).width = 80
}

func (m *Model) openMeetingForm(subject string) {
	start := time.Now().Truncate(30 * time.Minute).Add(30 * time.Minute)
	md := &modal{kind: mForm, title: "New meeting", width: 64}
	md.labels = []string{"Title", "Start (YYYY-MM-DD HH:MM)", "Duration (minutes)", "Attendees (emails, comma-separated)", "Details"}
	vals := []string{subject, start.Format("2006-01-02 15:04"), "30", "", ""}
	for i, l := range md.labels {
		ti := newInput(l, vals[i])
		if i > 0 {
			ti.Blur()
		}
		md.fields = append(md.fields, ti)
	}
	md.onForm = func(m *Model, v []string) tea.Cmd {
		st, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(v[1]), time.Local)
		if err != nil {
			m.setToast("Invalid start time: "+v[1], true)
			return nil
		}
		dur, err := time.ParseDuration(strings.TrimSpace(v[2]) + "m")
		if err != nil || dur <= 0 {
			dur = 30 * time.Minute
		}
		var att []string
		for _, a := range strings.Split(v[3], ",") {
			if a = strings.TrimSpace(a); a != "" {
				att = append(att, a)
			}
		}
		ev := model.NewEvent{Subject: v[0], Start: st, End: st.Add(dur), Attendees: att, Body: v[4], Online: true}
		return m.op(func(svc model.Service) opMsg {
			c, cancel := ctx()
			defer cancel()
			_, err := svc.CreateEvent(c, ev)
			return opMsg{err: err, toast: "Meeting scheduled", events: true}
		})
	}
	m.modal = md
}

// ---- key / mouse handling -------------------------------------------------------

func (m *Model) modalKey(msg tea.KeyMsg) tea.Cmd {
	md := m.modal
	k := msg.String()
	if k == "esc" || (k == "q" && !md.hasInput && md.kind != mForm) {
		m.modal = nil
		return nil
	}
	switch md.kind {
	case mText:
		switch k {
		case "up", "k":
			md.scroll = max(0, md.scroll-1)
		case "down", "j":
			md.scroll++
		case "pgup":
			md.scroll = max(0, md.scroll-10)
		case "pgdown", " ":
			md.scroll += 10
		case "enter":
			m.modal = nil
		case "y":
			if md.raw != "" {
				m.copy(md.raw, "transcript")
			}
		case "s":
			return m.saveTranscript(md)
		}
		return nil
	case mPrompt:
		if k == "enter" {
			v := md.input.Value()
			m.modal = nil
			return md.onText(m, v)
		}
		var c tea.Cmd
		md.input, c = md.input.Update(msg)
		return c
	case mForm:
		switch k {
		case "tab", "down":
			md.focusField(md.fieldSel + 1)
			return nil
		case "shift+tab", "up":
			md.focusField(md.fieldSel - 1)
			return nil
		case "enter":
			if md.fieldSel < len(md.fields)-1 {
				md.focusField(md.fieldSel + 1)
				return nil
			}
			fallthrough
		case "ctrl+s":
			vals := make([]string, len(md.fields))
			for i, f := range md.fields {
				vals[i] = f.Value()
			}
			m.modal = nil
			return md.onForm(m, vals)
		}
		var c tea.Cmd
		md.fields[md.fieldSel], c = md.fields[md.fieldSel].Update(msg)
		return c
	}

	// list & people
	cols := 1
	if md.grid {
		cols = md.gridCols()
	}
	switch k {
	case "up", "ctrl+p":
		md.sel = max(0, md.sel-cols)
		return nil
	case "down", "ctrl+n":
		md.sel = min(len(md.items)-1, md.sel+cols)
		return nil
	case "left":
		if md.grid {
			md.sel = max(0, md.sel-1)
			return nil
		}
	case "right":
		if md.grid {
			md.sel = min(len(md.items)-1, md.sel+1)
			return nil
		}
	case "k":
		if !md.hasInput {
			md.sel = max(0, md.sel-cols)
			return nil
		}
	case "j":
		if !md.hasInput {
			md.sel = min(len(md.items)-1, md.sel+cols)
			return nil
		}
	case "pgup":
		md.sel = max(0, md.sel-10)
		return nil
	case "pgdown":
		md.sel = min(len(md.items)-1, md.sel+10)
		return nil
	case "enter":
		return m.modalActivate(md.sel)
	case "ctrl+s":
		if md.kind == mPeople {
			return m.modalFinishPeople()
		}
	case "backspace":
		if md.kind == mPeople && md.input.Value() == "" && len(md.chosen) > 0 {
			md.chosen = md.chosen[:len(md.chosen)-1]
			return nil
		}
	}
	if md.hasInput {
		var c tea.Cmd
		md.input, c = md.input.Update(msg)
		if md.kind == mPeople {
			q := md.input.Value()
			if q != md.lastQ {
				md.lastQ = q
				md.debounce++
				id := md.debounce
				return tea.Batch(c, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return debounceMsg{id: id, q: q} }))
			}
		} else {
			md.applyFilter()
		}
		return c
	}
	return nil
}

type debounceMsg struct {
	id int
	q  string
}

func (md *modal) focusField(i int) {
	if i < 0 || i >= len(md.fields) {
		return
	}
	md.fields[md.fieldSel].Blur()
	md.fieldSel = i
	md.fields[i].Focus()
}

func (md *modal) gridCols() int { return max(1, (md.width-4)/5) }

func (m *Model) modalActivate(i int) tea.Cmd {
	md := m.modal
	if md == nil || i < 0 || i >= len(md.items) {
		if md != nil && md.kind == mPeople && len(md.chosen) > 0 {
			return m.modalFinishPeople()
		}
		return nil
	}
	it := md.items[i]
	if md.kind == mPeople {
		if it.user == nil {
			return nil
		}
		if !md.multi {
			m.modal = nil
			return md.onPeople(m, []model.User{*it.user})
		}
		for _, u := range md.chosen {
			if u.ID == it.user.ID {
				return m.modalFinishPeople()
			}
		}
		md.chosen = append(md.chosen, *it.user)
		md.input.SetValue("")
		md.lastQ = ""
		return m.searchPeople("")
	}
	m.modal = nil
	if it.run != nil {
		return it.run(m)
	}
	return nil
}

func (m *Model) modalFinishPeople() tea.Cmd {
	md := m.modal
	m.modal = nil
	if len(md.chosen) == 0 {
		return nil
	}
	return md.onPeople(m, md.chosen)
}

func (m *Model) modalMouse(msg tea.MouseMsg) tea.Cmd {
	md := m.modal
	if msg.Button == tea.MouseButtonWheelUp {
		if md.kind == mText {
			md.scroll = max(0, md.scroll-3)
		} else {
			md.sel = max(0, md.sel-1)
		}
		return nil
	}
	if msg.Button == tea.MouseButtonWheelDown {
		if md.kind == mText {
			md.scroll += 3
		} else {
			md.sel = min(len(md.items)-1, md.sel+1)
		}
		return nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	for i := range md.items {
		if zone.Get("mi:" + itoa(i)).InBounds(msg) {
			md.sel = i
			return m.modalActivate(i)
		}
	}
	for i := range md.fields {
		if zone.Get("mf:" + itoa(i)).InBounds(msg) {
			md.focusField(i)
			return nil
		}
	}
	for i := range md.chosen {
		if zone.Get("mc:" + itoa(i)).InBounds(msg) {
			md.chosen = append(md.chosen[:i], md.chosen[i+1:]...)
			return nil
		}
	}
	if zone.Get("m:ok").InBounds(msg) {
		switch md.kind {
		case mPeople:
			return m.modalFinishPeople()
		case mForm:
			return m.modalKey(tea.KeyMsg{Type: tea.KeyCtrlS})
		case mPrompt:
			return m.modalKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	if zone.Get("m:cancel").InBounds(msg) || !zone.Get("modal").InBounds(msg) {
		m.modal = nil
	}
	return nil
}

// ---- view ----------------------------------------------------------------------

func (m *Model) modalView() string {
	md := m.modal
	s := m.s
	w := min(md.width, m.w-4)
	inner := w - 2
	maxRows := max(3, m.h-12)
	titleLines := strings.Split(md.title, "\n")
	var lines []string
	for _, extra := range titleLines[1:] {
		for _, l := range strings.Split(ansi.Wrap(extra, inner-1, " "), "\n") {
			lines = append(lines, s.Text.Render(l))
		}
	}
	if len(titleLines) > 1 {
		lines = append(lines, "")
	}
	prompt := s.Accent.Bold(true).Render(s.ic.Prompt) + " "
	sel := func(l string) string { return withBg(s.Sel.Render(ansi.Strip(l)), s.bgAccent, inner) }

	switch md.kind {
	case mText:
		end := min(len(md.text), md.scroll+maxRows)
		md.scroll = max(0, min(md.scroll, len(md.text)-maxRows))
		for _, l := range md.text[md.scroll:end] {
			lines = append(lines, trunc(l, inner))
		}
		hint := "↑↓ scroll · esc close"
		if md.raw != "" {
			hint = "↑↓ scroll · y copy · s save to Downloads · esc close"
		}
		lines = append(lines, "", s.Muted.Render(hint))
	case mPrompt:
		md.input.Width = inner - 3
		md.input.Prompt = prompt
		lines = append(lines, md.input.View(), "")
		lines = append(lines, zone.Mark("m:ok", s.ButtonHot.Render(" OK "))+"  "+zone.Mark("m:cancel", s.Button.Render("Cancel"))+"  "+s.Muted.Render("enter ok · esc cancel"))
	case mForm:
		for i, l := range md.labels {
			md.fields[i].Width = inner - 3
			md.fields[i].Prompt = prompt
			st := s.Muted
			if i == md.fieldSel {
				st = s.Accent.Bold(true)
			}
			lines = append(lines, st.Render(l))
			lines = append(lines, zone.Mark("mf:"+itoa(i), padRight(md.fields[i].View(), inner)))
		}
		lines = append(lines, "", zone.Mark("m:ok", s.ButtonHot.Render(" Schedule "))+"  "+zone.Mark("m:cancel", s.Button.Render("Cancel"))+"  "+s.Muted.Render("tab next · ctrl+s save"))
	default:
		if md.kind == mPeople && len(md.chosen) > 0 {
			var chips []string
			for i, u := range md.chosen {
				chips = append(chips, zone.Mark("mc:"+itoa(i), s.ChipMine.Render(" "+u.DisplayName+" ✕ ")))
			}
			lines = append(lines, ansi.Wrap(strings.Join(chips, " "), inner, " "))
		}
		if md.hasInput {
			md.input.Width = inner - 3
			md.input.Prompt = prompt
			lines = append(lines, md.input.View(), "")
		}
		if md.grid {
			cols := md.gridCols()
			var row []string
			for i, it := range md.items {
				cell := padRight(" "+it.label+" ", 4)
				if i == md.sel {
					cell = s.Sel.Render(cell)
				}
				row = append(row, zone.Mark("mi:"+itoa(i), cell))
				if len(row) == cols || i == len(md.items)-1 {
					lines = append(lines, strings.Join(row, " "))
					row = nil
				}
			}
			if md.sel >= 0 && md.sel < len(md.items) {
				lines = append(lines, "", s.Muted.Render(trunc(md.items[md.sel].hint, inner)))
			}
		} else {
			if md.sel < md.scroll {
				md.scroll = md.sel
			}
			if md.sel >= md.scroll+maxRows {
				md.scroll = md.sel - maxRows + 1
			}
			end := min(len(md.items), md.scroll+maxRows)
			section := ""
			for i := md.scroll; i < end; i++ {
				it := md.items[i]
				if it.section != "" && it.section != section {
					section = it.section
					if i > md.scroll {
						lines = append(lines, "")
					}
					lines = append(lines, s.Muted.Bold(true).Render(section))
				}
				l := it.label
				if it.hint != "" {
					l = spread(l, s.Muted.Render(trunc(it.hint, inner/3)), inner)
				}
				if i == md.sel {
					l = sel(l)
				}
				lines = append(lines, zone.Mark("mi:"+itoa(i), l))
			}
			if len(md.items) == 0 {
				if md.kind == mPeople {
					lines = append(lines, s.Muted.Render("Searching…"))
				} else {
					lines = append(lines, s.Muted.Render("No matches"))
				}
			}
			lines = append(lines, "")
			switch {
			case md.kind == mPeople && md.multi:
				lines = append(lines, zone.Mark("m:ok", s.ButtonHot.Render(" Done "))+"  "+s.Muted.Render("enter add · ctrl+s done · esc cancel"))
			case md.hasInput:
				lines = append(lines, s.Muted.Render("↑↓ move · enter run · esc close"))
			default:
				lines = append(lines, s.Muted.Render("↑↓ move · enter choose · esc close"))
			}
		}
	}
	out := []string{headerBar(s.Inverse, s.Inverse.Bold(true).Render(strings.ToUpper(trunc(titleLines[0], inner-6))), zone.Mark("m:cancel", "esc"), w)}
	for _, l := range lines {
		out = append(out, withBg(" "+l, s.bgCard, w))
	}
	return zone.Mark("modal", strings.Join(out, "\n"))
}

// overlay draws fg centered on top of bg.
func overlay(bg, fg string, w, h int) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < h {
		bgLines = append(bgLines, "")
	}
	fgLines := strings.Split(fg, "\n")
	fw := lipgloss.Width(fg)
	x := max(0, (w-fw)/2)
	y := max(0, (h-len(fgLines))/3)
	for i, fl := range fgLines {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		bl := padRight(bgLines[row], w)
		left := ansi.Truncate(bl, x, "")
		right := ansi.TruncateLeft(bl, x+ansi.StringWidth(fl), "")
		bgLines[row] = left + "\x1b[0m" + fl + "\x1b[0m" + right
	}
	return strings.Join(bgLines[:h], "\n")
}
