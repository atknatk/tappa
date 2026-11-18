package checkin

import (
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
)

// TestInsertParams_WritesTheClientOnEveryTapAndNoneOnAManualRecord is the mapping of
// transactions.client (M16-04, migration 00109, ADR 0045 §2.4): the handler's server-derived
// client on every nfc and qr row WHATEVER the decision -- a refused or ignored tap still came
// through a client -- and NULL, forced in code, on the manual channel, for the zero value and for
// a value 00109's CHECKs do not admit (which would refuse the INSERT with 23514 and lose the
// record, §4.6). The CHECKs admit NULL on purpose, so these lines are the first and only proof
// that a new tap carries its client (the DB half: internal/handler's client_db_test.go).
func TestInsertParams_WritesTheClientOnEveryTapAndNoneOnAManualRecord(t *testing.T) {
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	in := tap.TypeIn
	tests := []struct {
		name    string
		channel tap.Channel
		verdict tap.Verdict
		dir     *tap.Type
		client  tap.Client
		want    string // "" = NULL
	}{
		{"a browser nfc ok", tap.ChannelNFC, tap.VerdictOK, &in, tap.ClientBrowser, "browser"},
		{"a browser nfc reject", tap.ChannelNFC, tap.VerdictReject, nil, tap.ClientBrowser, "browser"},
		{"a browser nfc ignored", tap.ChannelNFC, tap.VerdictIgnored, nil, tap.ClientBrowser, "browser"},
		{"a browser qr flag", tap.ChannelQR, tap.VerdictFlag, &in, tap.ClientBrowser, "browser"},
		{"an app nfc ok", tap.ChannelNFC, tap.VerdictOK, &in, tap.ClientApp, "app"},
		{"an app qr reject", tap.ChannelQR, tap.VerdictReject, nil, tap.ClientApp, "app"},
		{"a manual record naming the browser is NULL", tap.ChannelManual, tap.VerdictOK, &in, tap.ClientBrowser, ""},
		{"a manual record naming the app is NULL", tap.ChannelManual, tap.VerdictOK, &in, tap.ClientApp, ""},
		{"a manual record with no client is NULL", tap.ChannelManual, tap.VerdictOK, &in, "", ""},
		{"an nfc tap with the zero client is NULL (the sandbox)", tap.ChannelNFC, tap.VerdictOK, &in, "", ""},
		{"a client the column does not admit is NULL, never written", tap.ChannelNFC, tap.VerdictOK, &in, "ios", ""},
		{"a capital is no client", tap.ChannelQR, tap.VerdictOK, &in, "Browser", ""},
		{"a channel outside the three is NULL", tap.Channel("qr_live"), tap.VerdictOK, &in, tap.ClientApp, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := tap.Decision{Verdict: tc.verdict, Type: tc.dir, Trust: 20}
			p, err := (&Service{}).insertParams(Request{Channel: tc.channel, Client: tc.client}, db.ResolvedTag{UID: "04AABBCCDDEEFF"}, tapFacts{}, dec, at)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if p.Client != nil {
				got = *p.Client
				if got == "" {
					t.Fatal("client is an empty string, not NULL")
				}
			}
			if got != tc.want {
				t.Fatalf("client = %q, want %q (\"\" = NULL)", got, tc.want)
			}
		})
	}
}
