package cards

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// adaptive renders an AdaptiveCard (body + actions). path keeps action keys
// stable for nested show-cards.
func (r *renderer) adaptive(card map[string]any, w int, path string) []string {
	if r.res.Title == "" {
		r.res.Title = firstText(card)
	}
	lines := r.items(arr(card, "body"), w, path+"b")
	if acts := arr(card, "actions"); len(acts) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, r.actions(acts, w, path+"A")...)
	}
	if len(lines) == 0 {
		if fb := str(card, "fallbackText"); fb != "" {
			lines = r.text(fb, r.s.Text, w, true, 0)
		}
	}
	return lines
}

func firstText(card map[string]any) string {
	var walk func([]map[string]any) string
	walk = func(items []map[string]any) string {
		for _, it := range items {
			switch str(it, "type") {
			case "TextBlock":
				if t := str(it, "text"); t != "" {
					return reTags.ReplaceAllString(t, "")
				}
			case "RichTextBlock":
				var b strings.Builder
				for _, in := range arr(it, "inlines") {
					b.WriteString(str(in, "text"))
				}
				if b.Len() > 0 {
					return b.String()
				}
			case "Container":
				if t := walk(arr(it, "items")); t != "" {
					return t
				}
			case "ColumnSet":
				for _, c := range arr(it, "columns") {
					if t := walk(arr(c, "items")); t != "" {
						return t
					}
				}
			}
		}
		return ""
	}
	return walk(arr(card, "body"))
}

// items renders a list of elements, honouring separators, spacing and visibility.
func (r *renderer) items(items []map[string]any, w int, path string) []string {
	var out []string
	for i, el := range items {
		if !r.opt.State.visible(str(el, "id"), boolv(el, "isVisible", true)) {
			continue
		}
		lines := r.element(el, w, path+"."+strconv.Itoa(i))
		if len(lines) == 0 {
			continue
		}
		if len(out) > 0 {
			if boolv(el, "separator", false) {
				out = append(out, r.s.Border.Render(strings.Repeat("─", w)))
			} else {
				switch strings.ToLower(str(el, "spacing")) {
				case "large", "extralarge", "padding":
					out = append(out, "")
				}
			}
		}
		out = append(out, lines...)
	}
	return out
}

func (r *renderer) element(el map[string]any, w int, path string) []string {
	if w < 4 {
		return nil
	}
	ha := strings.ToLower(str(el, "horizontalAlignment"))
	switch str(el, "type") {
	case "TextBlock":
		// terminals always wrap; maxLines still applies
		lines := r.text(str(el, "text"), r.textStyle(el), w, true, int(num(el, "maxLines")))
		return align(lines, ha, w)

	case "RichTextBlock":
		var b strings.Builder
		inlines, _ := el["inlines"].([]any)
		for _, in := range inlines {
			switch v := in.(type) {
			case string:
				b.WriteString(r.s.Text.Render(v))
			case map[string]any:
				st := r.textStyle(v)
				if boolv(v, "italic", false) {
					st = st.Italic(true)
				}
				if boolv(v, "strikethrough", false) {
					st = st.Strikethrough(true)
				}
				if boolv(v, "underline", false) {
					st = st.Underline(true)
				}
				txt := str(v, "text")
				if sa := obj(v, "selectAction"); sa != nil && str(sa, "url") != "" {
					r.res.Links = append(r.res.Links, str(sa, "url"))
					st = r.s.Link
				}
				b.WriteString(st.Render(txt))
			}
		}
		return align(strings.Split(ansi.Wrap(b.String(), w, " -/"), "\n"), ha, w)

	case "Image":
		return align([]string{r.image(el, w)}, ha, w)

	case "ImageSet":
		imgs := arr(el, "images")
		var parts []string
		for _, im := range imgs {
			parts = append(parts, r.image(im, w))
		}
		return strings.Split(ansi.Wrap(strings.Join(parts, "  "), w, " "), "\n")

	case "Media":
		url := ""
		if srcs := arr(el, "sources"); len(srcs) > 0 {
			url = str(srcs[0], "url")
		}
		if url != "" {
			r.res.Links = append(r.res.Links, url)
		}
		label := str(el, "altText")
		if label == "" {
			label = "media"
		}
		return []string{r.s.Link.Render(fit("▶ "+label, min(w, ansi.StringWidth(label)+2)))}

	case "Container":
		inner := r.items(arr(el, "items"), w-2, path)
		r.selectAction(el)
		return r.styled(inner, str(el, "style"), w)

	case "ColumnSet":
		r.selectAction(el)
		return r.styled(r.columns(arr(el, "columns"), w, path), str(el, "style"), w)

	case "FactSet":
		return r.facts(el, w)

	case "Table":
		return r.table(el, w, path)

	case "ActionSet":
		return r.actions(arr(el, "actions"), w, path+"A")

	case "Input.Text", "Input.Number", "Input.Date", "Input.Time":
		return r.input(el, w)
	case "Input.Toggle":
		box := "☐"
		if str(el, "value") == str(el, "valueOn") && str(el, "value") != "" || str(el, "value") == "true" {
			box = "☑"
		}
		return append(r.label(el, w), r.s.Input.Render(box+" "+fit(str(el, "title"), min(w-2, ansi.StringWidth(str(el, "title"))))))
	case "Input.ChoiceSet":
		return r.choices(el, w)

	case "Column":
		return r.items(arr(el, "items"), w, path)
	}
	// unknown element: use its fallback if present
	switch fb := el["fallback"].(type) {
	case map[string]any:
		return r.element(fb, w, path+"f")
	case string:
		if fb == "drop" {
			return nil
		}
	}
	return []string{r.s.Subtle.Render(fit("[unsupported "+str(el, "type")+"]", min(w, 30)))}
}

func num(m map[string]any, k string) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
		return f
	}
	return 0
}

func (r *renderer) selectAction(el map[string]any) {
	if sa := obj(el, "selectAction"); sa != nil && str(sa, "url") != "" {
		r.res.Links = append(r.res.Links, str(sa, "url"))
	}
}

func (r *renderer) image(el map[string]any, w int) string {
	alt := str(el, "altText")
	if alt == "" {
		alt = "image"
	}
	url := str(el, "url")
	if sa := obj(el, "selectAction"); sa != nil && str(sa, "url") != "" {
		url = str(sa, "url")
	}
	if url != "" && !strings.HasPrefix(url, "data:") {
		r.res.Links = append(r.res.Links, url)
	}
	label := "📷 " + alt
	return r.s.Link.Render(ansi.Truncate(label, w, "…"))
}

// styled marks a Container/ColumnSet with a coloured bar for emphasis styles.
func (r *renderer) styled(lines []string, style string, w int) []string {
	var bar lipgloss.Style
	switch strings.ToLower(style) {
	case "emphasis":
		bar = r.s.Subtle
	case "good":
		bar = r.s.Good
	case "attention":
		bar = r.s.Attention
	case "warning":
		bar = r.s.Warning
	case "accent":
		bar = r.s.Accent
	default:
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = bar.Render("▎") + " " + ansi.Truncate(l, w-2, "…")
	}
	return out
}

func (r *renderer) label(el map[string]any, w int) []string {
	if l := str(el, "label"); l != "" {
		suffix := ""
		if boolv(el, "isRequired", false) {
			suffix = r.s.Attention.Render(" *")
		}
		return []string{r.s.Bold.Render(ansi.Truncate(l, w-2, "…")) + suffix}
	}
	return nil
}

func (r *renderer) input(el map[string]any, w int) []string {
	v := str(el, "value")
	st := r.s.Input
	if v == "" {
		v = str(el, "placeholder")
		st = r.s.Subtle
	}
	if v == "" {
		v = strings.TrimPrefix(str(el, "type"), "Input.")
	}
	bw := min(w, 40)
	box := r.s.Border.Render("[") + st.Render(fit(" "+v, bw-2)) + r.s.Border.Render("]")
	return append(r.label(el, w), box)
}

func (r *renderer) choices(el map[string]any, w int) []string {
	out := r.label(el, w)
	sel := map[string]bool{}
	for _, v := range strings.Split(str(el, "value"), ",") {
		sel[strings.TrimSpace(v)] = true
	}
	chs := arr(el, "choices")
	if strings.ToLower(str(el, "style")) == "expanded" {
		mark := "○"
		on := "●"
		if boolv(el, "isMultiSelect", false) {
			mark, on = "☐", "☑"
		}
		for _, c := range chs {
			m := mark
			if sel[str(c, "value")] {
				m = on
			}
			out = append(out, r.s.Input.Render(ansi.Truncate(m+" "+str(c, "title"), w, "…")))
		}
		return out
	}
	cur := str(el, "placeholder")
	for _, c := range chs {
		if sel[str(c, "value")] {
			cur = str(c, "title")
		}
	}
	if cur == "" && len(chs) > 0 {
		cur = str(chs[0], "title")
	}
	bw := min(w, 40)
	return append(out, r.s.Border.Render("[")+r.s.Input.Render(fit(" "+cur, bw-4))+r.s.Border.Render(" ▾]"))
}

// facts renders a FactSet as aligned title/value pairs.
func (r *renderer) facts(el map[string]any, w int) []string {
	facts := arr(el, "facts")
	tw := 0
	for _, f := range facts {
		tw = max(tw, ansi.StringWidth(str(f, "title")))
	}
	tw = min(tw, w*2/5)
	vw := max(6, w-tw-2)
	var out []string
	for _, f := range facts {
		vals := r.text(str(f, "value"), r.s.Text, vw, true, 0)
		for i, v := range vals {
			t := ""
			if i == 0 {
				t = r.s.Bold.Render(ansi.Truncate(str(f, "title"), tw, "…"))
			}
			out = append(out, fit(t, tw)+"  "+v)
		}
	}
	return out
}

// columns lays out a ColumnSet side by side (stacked when too narrow).
func (r *renderer) columns(cols []map[string]any, w int, path string) []string {
	var visible []map[string]any
	for _, c := range cols {
		if r.opt.State.visible(str(c, "id"), boolv(c, "isVisible", true)) {
			visible = append(visible, c)
		}
	}
	cols = visible
	n := len(cols)
	if n == 0 {
		return nil
	}
	const gap = 2
	avail := w - gap*(n-1)
	if avail < n*8 {
		var out []string
		for i, c := range cols {
			out = append(out, r.items(arr(c, "items"), w, path+".c"+strconv.Itoa(i))...)
		}
		return out
	}
	widths := make([]int, n)
	weights := make([]float64, n)
	fixed := 0
	for i, c := range cols {
		switch v := c["width"].(type) {
		case string:
			switch {
			case v == "auto":
				nat := maxWidth(r.dry(func(rr *renderer) []string { return rr.items(arr(c, "items"), avail, "") }))
				widths[i] = max(4, min(nat, avail/2))
				fixed += widths[i]
			case strings.HasSuffix(v, "px"):
				px, _ := strconv.Atoi(strings.TrimSuffix(v, "px"))
				widths[i] = max(4, min(px/8, avail/2))
				fixed += widths[i]
			case v == "stretch" || v == "":
				weights[i] = 1
			default:
				f, err := strconv.ParseFloat(v, 64)
				if err != nil || f <= 0 {
					f = 1
				}
				weights[i] = f
			}
		case float64:
			weights[i] = max(v, 0.1)
		default:
			weights[i] = 1
		}
	}
	rest := avail - fixed
	var total float64
	for _, wt := range weights {
		total += wt
	}
	used := fixed
	for i := range widths {
		if weights[i] > 0 {
			widths[i] = max(4, int(float64(rest)*weights[i]/total))
			used += widths[i]
		}
	}
	// give rounding leftovers to the last stretch column / shrink if too wide
	for i := n - 1; i >= 0 && used != avail; i-- {
		if weights[i] > 0 || used > avail {
			d := avail - used
			if widths[i]+d >= 4 {
				widths[i] += d
				used = avail
			}
		}
	}
	rendered := make([][]string, n)
	height := 0
	for i, c := range cols {
		rendered[i] = r.items(arr(c, "items"), widths[i], path+".c"+strconv.Itoa(i))
		r.selectAction(c)
		height = max(height, len(rendered[i]))
	}
	out := make([]string, height)
	for row := 0; row < height; row++ {
		var b strings.Builder
		for i := range cols {
			cell := ""
			if row < len(rendered[i]) {
				cell = rendered[i][row]
			}
			if i < n-1 {
				b.WriteString(fit(cell, widths[i]) + strings.Repeat(" ", gap))
			} else {
				b.WriteString(cell)
			}
		}
		out[row] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// dry renders without recording links/actions (for measuring).
func (r *renderer) dry(fn func(*renderer) []string) []string {
	rr := &renderer{opt: r.opt, s: r.s}
	rr.opt.Mark = func(_, l string) string { return l }
	return fn(rr)
}

// table renders an Adaptive Card Table.
func (r *renderer) table(el map[string]any, w int, path string) []string {
	cols := arr(el, "columns")
	rows := arr(el, "rows")
	if len(rows) == 0 {
		return nil
	}
	n := len(cols)
	for _, row := range rows {
		n = max(n, len(arr(row, "cells")))
	}
	grid := boolv(el, "showGridLines", true)
	header := boolv(el, "firstRowAsHeader", true)
	sep := 3 // " │ "
	avail := w - sep*(n-1)
	if avail < n*4 {
		avail = w
		sep = 0
	}
	weights := make([]float64, n)
	var total float64
	for i := range weights {
		weights[i] = 1
		if i < len(cols) {
			if v := num(cols[i], "width"); v > 0 {
				weights[i] = v
			}
		}
		total += weights[i]
	}
	widths := make([]int, n)
	used := 0
	for i := range widths {
		widths[i] = max(3, int(float64(avail)*weights[i]/total))
		used += widths[i]
	}
	widths[n-1] += avail - used
	var out []string
	vbar := " "
	if grid {
		vbar = r.s.Border.Render("│")
	}
	for ri, row := range rows {
		cells := arr(row, "cells")
		rendered := make([][]string, n)
		h := 1
		for ci := 0; ci < n; ci++ {
			if ci < len(cells) {
				lines := r.items(arr(cells[ci], "items"), widths[ci], path+".r"+strconv.Itoa(ri)+"c"+strconv.Itoa(ci))
				if header && ri == 0 {
					for k := range lines {
						lines[k] = r.s.Bold.Render(ansi.Strip(lines[k]))
					}
				}
				rendered[ci] = lines
				h = max(h, len(lines))
			}
		}
		for line := 0; line < h; line++ {
			var b strings.Builder
			for ci := 0; ci < n; ci++ {
				cell := ""
				if line < len(rendered[ci]) {
					cell = rendered[ci][line]
				}
				b.WriteString(fit(cell, widths[ci]))
				if ci < n-1 && sep > 0 {
					b.WriteString(" " + vbar + " ")
				}
			}
			out = append(out, strings.TrimRight(b.String(), " "))
		}
		if grid && (ri == 0 && header || ri < len(rows)-1 && grid && !header) {
			var b strings.Builder
			for ci := 0; ci < n; ci++ {
				b.WriteString(strings.Repeat("─", widths[ci]))
				if ci < n-1 && sep > 0 {
					b.WriteString("─┼─")
				}
			}
			out = append(out, r.s.Border.Render(b.String()))
		}
	}
	return out
}

// actions renders buttons, and expanded show-cards below them.
func (r *renderer) actions(acts []map[string]any, w int, path string) []string {
	var buttons []string
	var expanded []string
	for i, a := range acts {
		if !boolv(a, "isEnabled", true) {
			continue
		}
		key := path + strconv.Itoa(i)
		title := str(a, "title")
		act := Action{Key: key, Title: title}
		st := r.s.Button
		switch strings.ToLower(str(a, "style")) {
		case "positive":
			st = r.s.ButtonPositive
		case "destructive":
			st = r.s.ButtonDanger
		}
		label := title
		switch str(a, "type") {
		case "Action.OpenUrl":
			act.Type, act.URL = OpenURL, str(a, "url")
			label = "↗ " + title
			r.res.Links = append(r.res.Links, act.URL)
		case "Action.ShowCard":
			act.Type = ShowCard
			if r.opt.State.Expanded[key] {
				label = "▾ " + title
				if c := obj(a, "card"); c != nil {
					sub := r.adaptive(c, w-2, key+">")
					for _, l := range sub {
						expanded = append(expanded, r.s.Border.Render("┃")+" "+l)
					}
				}
			} else {
				label = "▸ " + title
			}
		case "Action.ToggleVisibility":
			act.Type = Toggle
			label = "⇅ " + title
			targets, _ := a["targetElements"].([]any)
			for _, t := range targets {
				switch v := t.(type) {
				case string:
					act.Targets = append(act.Targets, ToggleTarget{ID: v})
				case map[string]any:
					tt := ToggleTarget{ID: str(v, "elementId")}
					if b, ok := v["isVisible"].(bool); ok {
						tt.Visible = &b
					}
					act.Targets = append(act.Targets, tt)
				}
			}
		default: // Action.Submit, Action.Execute
			act.Type = BotAction
			st = r.s.ButtonDead
		}
		if title == "" && act.Type == OpenURL {
			label = "↗ Open"
		}
		r.res.Actions = append(r.res.Actions, act)
		buttons = append(buttons, r.opt.Mark(key, st.Render("[ "+label+" ]")))
	}
	var out []string
	cur := ""
	for _, b := range buttons {
		if cur != "" && ansi.StringWidth(cur)+1+ansi.StringWidth(b) > w {
			out = append(out, cur)
			cur = ""
		}
		if cur != "" {
			cur += " "
		}
		cur += b
	}
	if cur != "" {
		out = append(out, cur)
	}
	return append(out, expanded...)
}

// ---- legacy card types -------------------------------------------------------------

func (r *renderer) buttons(bs []map[string]any, w int) []string {
	var acts []map[string]any
	for _, b := range bs {
		t := strings.ToLower(str(b, "type"))
		a := map[string]any{"title": str(b, "title")}
		if t == "openurl" || t == "signin" || t == "playaudio" || t == "playvideo" || t == "showimage" || t == "downloadfile" {
			a["type"] = "Action.OpenUrl"
			a["url"] = str(b, "value")
		} else {
			a["type"] = "Action.Submit"
		}
		acts = append(acts, a)
	}
	return r.actions(acts, w, "B")
}

func (r *renderer) hero(c map[string]any, w int) []string {
	var out []string
	if t := str(c, "title"); t != "" {
		r.res.Title = t
		out = append(out, r.text(t, r.s.Heading, w, true, 0)...)
	}
	if t := str(c, "subtitle"); t != "" {
		out = append(out, r.text(t, r.s.Subtle, w, true, 0)...)
	}
	if t := str(c, "text"); t != "" {
		out = append(out, r.text(reTags.ReplaceAllString(strings.ReplaceAll(t, "<br>", "\n"), ""), r.s.Text, w, true, 0)...)
	}
	for _, im := range arr(c, "images") {
		out = append(out, r.image(map[string]any{"url": str(im, "url"), "altText": str(im, "alt")}, w))
	}
	if bs := arr(c, "buttons"); len(bs) > 0 {
		out = append(out, "")
		out = append(out, r.buttons(bs, w)...)
	}
	return out
}

func (r *renderer) connector(c map[string]any, w int) []string {
	var out []string
	title := str(c, "title")
	if title == "" {
		title = str(c, "summary")
	}
	if title != "" {
		r.res.Title = title
		out = append(out, r.text(title, r.s.Heading, w, true, 0)...)
	}
	if t := str(c, "text"); t != "" {
		out = append(out, r.text(t, r.s.Text, w, true, 0)...)
	}
	for _, sec := range arr(c, "sections") {
		if len(out) > 0 {
			out = append(out, "")
		}
		if t := str(sec, "activityTitle"); t != "" {
			out = append(out, r.text(t, r.s.Bold, w, true, 0)...)
		}
		if t := str(sec, "activitySubtitle"); t != "" {
			out = append(out, r.text(t, r.s.Subtle, w, true, 0)...)
		}
		if t := str(sec, "activityText"); t != "" {
			out = append(out, r.text(t, r.s.Text, w, true, 0)...)
		}
		if t := str(sec, "title"); t != "" {
			out = append(out, r.text(t, r.s.Bold, w, true, 0)...)
		}
		if facts := arr(sec, "facts"); len(facts) > 0 {
			var fs []any
			for _, f := range facts {
				fs = append(fs, map[string]any{"title": str(f, "name"), "value": str(f, "value")})
			}
			out = append(out, r.facts(map[string]any{"facts": fs}, w)...)
		}
		if t := str(sec, "text"); t != "" {
			out = append(out, r.text(t, r.s.Text, w, true, 0)...)
		}
	}
	var acts []map[string]any
	for _, pa := range arr(c, "potentialAction") {
		a := map[string]any{"title": str(pa, "name")}
		if str(pa, "@type") == "OpenUri" {
			a["type"] = "Action.OpenUrl"
			for _, t := range arr(pa, "targets") {
				if u := str(t, "uri"); u != "" {
					a["url"] = u
					break
				}
			}
		} else {
			a["type"] = "Action.Submit"
		}
		acts = append(acts, a)
	}
	if len(acts) > 0 {
		out = append(out, "")
		out = append(out, r.actions(acts, w, "P")...)
	}
	return out
}

func (r *renderer) list(c map[string]any, w int) []string {
	var out []string
	if t := str(c, "title"); t != "" {
		r.res.Title = t
		out = append(out, r.text(t, r.s.Heading, w, true, 0)...)
	}
	for _, it := range arr(c, "items") {
		if str(it, "type") == "separator" {
			out = append(out, r.s.Border.Render(strings.Repeat("─", w)))
			continue
		}
		title := str(it, "title")
		if tap := obj(it, "tap"); tap != nil && strings.EqualFold(str(tap, "type"), "openUrl") {
			r.res.Links = append(r.res.Links, str(tap, "value"))
			title = "[" + title + "](" + str(tap, "value") + ")"
		}
		out = append(out, r.text("• "+title, r.s.Text, w, true, 0)...)
		if s := str(it, "subtitle"); s != "" {
			out = append(out, r.text("  "+s, r.s.Subtle, w, true, 0)...)
		}
	}
	if bs := arr(c, "buttons"); len(bs) > 0 {
		out = append(out, "")
		out = append(out, r.buttons(bs, w)...)
	}
	return out
}

func (r *renderer) receipt(c map[string]any, w int) []string {
	var out []string
	if t := str(c, "title"); t != "" {
		r.res.Title = t
		out = append(out, r.text(t, r.s.Heading, w, true, 0)...)
	}
	if facts := arr(c, "facts"); len(facts) > 0 {
		var fs []any
		for _, f := range facts {
			fs = append(fs, map[string]any{"title": str(f, "key"), "value": str(f, "value")})
		}
		out = append(out, r.facts(map[string]any{"facts": fs}, w)...)
	}
	for _, it := range arr(c, "items") {
		price := str(it, "price")
		left := str(it, "title")
		if q := str(it, "quantity"); q != "" {
			left = q + " × " + left
		}
		out = append(out, fit(left, w-ansi.StringWidth(price)-1)+" "+price)
	}
	for _, k := range []string{"tax", "vat", "total"} {
		if v := str(c, k); v != "" {
			st := r.s.Text
			if k == "total" {
				st = r.s.Bold
			}
			label := strings.ToUpper(k[:1]) + k[1:]
			out = append(out, st.Render(fit(label, w-ansi.StringWidth(v)-1)+" "+v))
		}
	}
	if bs := arr(c, "buttons"); len(bs) > 0 {
		out = append(out, "")
		out = append(out, r.buttons(bs, w)...)
	}
	return out
}
