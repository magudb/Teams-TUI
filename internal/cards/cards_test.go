package cards

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func load(t *testing.T, name string) string {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func plain(res Result) string {
	var out []string
	for _, l := range res.Lines {
		out = append(out, ansi.Strip(l))
	}
	return strings.Join(out, "\n")
}

func checkWidth(t *testing.T, res Result, w int) {
	t.Helper()
	for _, l := range res.Lines {
		if ansi.StringWidth(l) > w {
			t.Errorf("line wider than %d: %q", w, ansi.Strip(l))
		}
	}
}

func TestApprovalCard(t *testing.T) {
	st := NewState()
	opt := Options{Width: 70, State: st, Frame: true}
	res, ok := Render(TypeAdaptive, load(t, "approval.json"), opt)
	if !ok {
		t.Fatal("not rendered")
	}
	out := plain(res)
	t.Log("\n" + out)
	checkWidth(t, res, 70)
	for _, want := range []string{"╭", "Approval requested: Contract renewal", "Sofie Jensen requested approval",
		"Submitted Fri, 20 Nov 2026", "Customer", "Contoso A/S", "DKK 125,000", "[ Add a comment", "Comment",
		"[ Approve ]", "[ Reject ]", "[ ⇅ Details ]", "[ ▸ Delegate ]", "[ ↗ View in Approvals ]", "📷 Sofie"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "5% uplift") {
		t.Error("hidden element rendered")
	}
	// columns: image and text share a line
	if !strings.Contains(out, "📷 Sofie  Sofie Jensen requested approval") {
		t.Error("column set not laid out side by side")
	}
	if len(res.Actions) != 5 || res.Actions[0].Type != BotAction || res.Actions[4].URL != "https://approvals.example.com/123" {
		t.Fatalf("actions: %+v", res.Actions)
	}

	// toggle visibility + show card
	st.Apply(res.Actions[2])
	st.Apply(res.Actions[3])
	res, _ = Render(TypeAdaptive, load(t, "approval.json"), opt)
	out = plain(res)
	t.Log("\n" + out)
	checkWidth(t, res, 70)
	for _, want := range []string{"5% uplift", "[ ▾ Delegate ]", "Delegate to", "Lars Nielsen", "▾]"} {
		if !strings.Contains(out, want) {
			t.Errorf("after actions missing %q", want)
		}
	}
	st.Apply(res.Actions[2])
	res, _ = Render(TypeAdaptive, load(t, "approval.json"), opt)
	if strings.Contains(plain(res), "5% uplift") {
		t.Error("toggle back failed")
	}
}

func TestTableCard(t *testing.T) {
	res, ok := Render(TypeAdaptive, load(t, "table.json"), Options{Width: 50, IsMe: func(n string) bool { return n == "Alex Morgan" }})
	if !ok {
		t.Fatal("not rendered")
	}
	out := plain(res)
	t.Log("\n" + out)
	checkWidth(t, res, 50)
	for _, want := range []string{"Weekly incidents", "Service", "│", "┼", "billing-service", "Degraded", "▎ On-call: @Alex Morgan", "fallback text", "[unsupported Other]"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestConnectorCard(t *testing.T) {
	res, ok := Render(TypeConnector, load(t, "connector.json"), Options{Width: 60, Frame: true})
	if !ok {
		t.Fatal("not rendered")
	}
	out := plain(res)
	t.Log("\n" + out)
	checkWidth(t, res, 60)
	for _, want := range []string{"Pull request opened", "Alex Morgan opened a pull request", "Branch", "fix/billing-retry", "[ ↗ View pull request ]", "[ Approve ]"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if res.Title == "" || len(res.Links) == 0 {
		t.Errorf("title=%q links=%v", res.Title, res.Links)
	}
}

func TestHeroAndBadInput(t *testing.T) {
	res, ok := Render(TypeHero, `{"title":"Lunch menu","subtitle":"Friday","text":"Pasta<br>Salad","buttons":[{"type":"openUrl","title":"Order","value":"https://x.example"}]}`, Options{Width: 40})
	if !ok || !strings.Contains(plain(res), "Pasta") || !strings.Contains(plain(res), "[ ↗ Order ]") {
		t.Fatalf("hero: %v\n%s", ok, plain(res))
	}
	if _, ok := Render(TypeAdaptive, `not json`, Options{Width: 40}); ok {
		t.Fatal("invalid JSON should not render")
	}
	if _, ok := Render(TypeAdaptive, `"{\"type\":\"AdaptiveCard\",\"body\":[{\"type\":\"TextBlock\",\"text\":\"double\"}]}"`, Options{Width: 40}); !ok {
		t.Fatal("double-encoded content should render")
	}
}
