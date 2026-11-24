package model

import (
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// userLocale returns the user's collation locale from the environment
// (LC_ALL, LC_COLLATE, LANG), e.g. "da_DK.UTF-8" -> da-DK.
func userLocale() language.Tag {
	for _, k := range []string{"LC_ALL", "LC_COLLATE", "LANG"} {
		v := os.Getenv(k)
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		if i := strings.IndexAny(v, ".@"); i >= 0 {
			v = v[:i]
		}
		if t, err := language.Parse(strings.ReplaceAll(v, "_", "-")); err == nil {
			return t
		}
	}
	return language.English
}

// SortChannels orders channels like the Teams client: the channel named
// "General" first, all others alphabetically.
func SortChannels(chs []Channel) {
	collator := collate.New(userLocale(), collate.IgnoreCase)
	sort.SliceStable(chs, func(i, j int) bool {
		gi, gj := chs[i].DisplayName == "General", chs[j].DisplayName == "General"
		if gi != gj {
			return gi
		}
		return collator.CompareString(chs[i].DisplayName, chs[j].DisplayName) < 0
	})
}

// LastActivity is the time Teams uses to order the "Recent" chat list.
func (c Chat) LastActivity() time.Time {
	if c.LastMessage != nil && !c.LastMessage.Created.IsZero() {
		return c.LastMessage.Created
	}
	return c.Updated
}

// SortChats orders chats by most recent activity first.
func SortChats(cs []Chat) {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].LastActivity().After(cs[j].LastActivity()) })
}

// ApplyOrder returns ids reordered so that those present in order come first,
// in that order, followed by the rest in their original order.
func ApplyOrder(ids, order []string) []string {
	present := make(map[string]bool, len(ids))
	for _, id := range ids {
		present[id] = true
	}
	out := make([]string, 0, len(ids))
	used := map[string]bool{}
	for _, id := range order {
		if present[id] && !used[id] {
			out = append(out, id)
			used[id] = true
		}
	}
	for _, id := range ids {
		if !used[id] {
			out = append(out, id)
		}
	}
	return out
}

// MoveInOrder moves id to position target (index into the current order).
func MoveInOrder(order []string, id string, target int) []string {
	from := -1
	for i, x := range order {
		if x == id {
			from = i
		}
	}
	if from < 0 || target < 0 || target >= len(order) || from == target {
		return order
	}
	out := append([]string{}, order[:from]...)
	out = append(out, order[from+1:]...)
	out = append(out[:target], append([]string{id}, out[target:]...)...)
	return out
}
