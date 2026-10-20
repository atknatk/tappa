package pages

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func renderAPIKeys(t *testing.T, v APIKeysView) string {
	t.Helper()
	var sb strings.Builder
	if err := AdminAPIKeys(v).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// developersFixture is an owner's Developers page with two keys (one live, one revoked), an
// endpoint that is on, and one delivery in each of webhookDeliveryWord's five states.
func developersFixture() APIKeysView {
	return APIKeysView{
		PanelChrome: PanelChrome{FullName: "Maria Borg", Role: "owner", Tab: TabAPIKeys},
		Available:   true, APIEnabled: true,
		PostHref: "/admin/api-keys", RevokeHref: "/admin/api-keys/revoke", RevokeAllHref: "/admin/api-keys/revoke-all",
		NameMaxLength: 64,
		Keys: []APIKeyRow{
			{ID: "k1", Name: "Payroll", PrefixHint: "tt_live_3f9a", CreatedOn: "2 Oct 2026, 09:12", LastUsed: "Never"},
			{ID: "k2", Name: "Old", PrefixHint: "tt_live_0d4e", CreatedOn: "12 Sep 2026, 11:05", LastUsed: "Never", RevokedOn: "3 Oct 2026, 10:00"},
		},
		Webhook: WebhookView{
			Configured: true, APIEnabled: true, Exists: true, Active: true,
			Address: "https://hooks.example.com/taptime", CreatedOn: "2 Oct 2026, 09:12",
			Spent: 1, Limit: 10, LogKeepsDays: 90, AddressMaxLength: 2048,
			CreateHref: "/admin/api-keys/webhook", AddressHref: "/admin/api-keys/webhook/address",
			RotateHref: "/admin/api-keys/webhook/rotate", DisableHref: "/admin/api-keys/webhook/disable",
			EnableHref: "/admin/api-keys/webhook/enable", PingHref: "/admin/api-keys/webhook/ping",
			ResendHref: "/admin/api-keys/webhook/resend",
			Deliveries: []WebhookDeliveryRow{
				{ID: "d1", PunchID: "pch_1", Queued: "20 Oct 2026, 14:31:00", State: "Delivered", Attempts: 1, LastAnswer: "HTTP 200", Delivered: "20 Oct 2026, 14:31:02"},
				{ID: "d2", PunchID: "pch_2", Queued: "20 Oct 2026, 14:30:00", State: "Sending", Attempts: 1},
				{ID: "d3", PunchID: "pch_3", Queued: "20 Oct 2026, 14:28:00", State: "Retrying", Attempts: 2, LastAnswer: "no answer in 10 s", NextTry: "20 Oct 2026, 14:35", Resendable: true},
				{ID: "d4", PunchID: "pch_4", Queued: "20 Oct 2026, 13:58:00", State: "Queued", Resendable: true},
				{ID: "d5", PunchID: "pch_5", Queued: "19 Oct 2026, 09:02:00", State: "Failed", Attempts: 6, LastAnswer: "could not be reached", Resendable: true},
			},
		},
	}
}

// TestDevelopers_EveryDeliveryStateIsAWordInItsOwnFrame (M12-42; README §5 "Delivery status pill"):
// the five words -- Sending included -- each in a pill whose frame is chosen for that word, no two
// alike; each carries what the state means for that delivery; and the page prints no latency.
func TestDevelopers_EveryDeliveryStateIsAWordInItsOwnFrame(t *testing.T) {
	page := renderAPIKeys(t, developersFixture())
	// Each word's own frame, pinned: a word whose case is deleted falls to the plain line frame
	// of a word nobody named, and must not stay green.
	want := map[string]string{
		"Delivered": "mono-pill border-tappa-green bg-tappa-green/10",
		"Sending":   "mono-pill border-dashed border-ink/50",
		"Retrying":  "mono-pill border-dashed border-saffron bg-saffron/10",
		"Queued":    "mono-pill border-ink/50",
		"Failed":    "mono-pill border-tomato bg-tomato/10",
	}
	frames := map[string]string{}
	for _, word := range []string{"Delivered", "Sending", "Retrying", "Queued", "Failed"} {
		m := regexp.MustCompile(`<span class="(mono-pill[^"]*)">`+word+`</span>`).FindAllStringSubmatch(page, -1)
		if len(m) != 2 {
			t.Fatalf("%s: %d pills, want 2 (the desk table and the list below it)", word, len(m))
		}
		if m[0][1] != want[word] || m[1][1] != want[word] {
			t.Errorf("%s is drawn in %q / %q, want %q", word, m[0][1], m[1][1], want[word])
		}
		for other, frame := range frames {
			if frame == m[0][1] {
				t.Errorf("%s and %s share the frame %q: the tone does not follow the word", word, other, frame)
			}
		}
		frames[word] = m[0][1]
	}
	for _, note := range []string{"arrived 20 Oct 2026, 14:31:02", "being sent now", "next try 20 Oct 2026, 14:35", "waiting for its first try", "gave up after 6 tries"} {
		if strings.Count(page, note) != 2 {
			t.Errorf("the note %q is not on both the table row and the list row", note)
		}
	}
	if regexp.MustCompile(`\d\s?ms\b`).MatchString(page) {
		t.Error("the page prints a latency")
	}
}

// TestDevelopers_OneTableAndOneListFromTheSameRows: the keys and the deliveries are each a table
// that shows from the desk up and a list that shows below it, drawn from the same rows; a key's
// hint is its first characters followed by an ellipsis; nothing on the page is a link with a
// scheme (the webhook's address is a field's value, never an href), and no masked signing key.
func TestDevelopers_OneTableAndOneListFromTheSameRows(t *testing.T) {
	page := renderAPIKeys(t, developersFixture())
	if n := strings.Count(page, `<table class="hidden w-full border-collapse desk:table">`); n != 2 {
		t.Errorf("%d desk tables, want 2 (keys, deliveries)", n)
	}
	if n := strings.Count(page, `<ul class="flex flex-col desk:hidden">`); n != 2 {
		t.Errorf("%d lists below the desk, want 2 (keys, deliveries)", n)
	}
	for _, hint := range []string{"tt_live_3f9a…", "tt_live_0d4e…"} {
		if strings.Count(page, hint) != 2 {
			t.Errorf("the hint %q is not on both the table and the list", hint)
		}
	}
	if n := strings.Count(page, `name="key_id" value="k1"`); n != 2 {
		t.Errorf("the live key has %d revoke forms, want 2 (one per rendering)", n)
	}
	// The revoke button's accessible name is "Revoke <name>": the space before the name must be
	// in the bytes (templ drops a literal leading space inside an element).
	if n := strings.Count(page, `Revoke<span class="sr-only"> Payroll</span>`); n != 2 {
		t.Errorf("the revoke button reads as \"Revoke Payroll\" %d times, want 2", n)
	}
	if strings.Contains(page, `value="k2"`) {
		t.Error("the revoked key is offered a revocation")
	}
	if regexp.MustCompile(`href="[^"]*://`).MatchString(page) {
		t.Error("a link with a scheme on a panel page")
	}
	if strings.Contains(page, "tt_whsec_") || strings.Contains(page, "••") {
		t.Error("the page prints a masked signing key")
	}
	if !strings.Contains(page, `value="https://hooks.example.com/taptime"`) {
		t.Error("the change-address field is not pre-filled with the saved address")
	}
	if !strings.Contains(page, "9 of 10 left this hour") {
		t.Error("the test-message bucket does not read Limit less Spent")
	}
}
