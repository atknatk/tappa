package config_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
)

// TestLoad_BusinessDayFromIsOptionalAnInstantAndAtMostThirtyDaysAhead is ADR 0032 §A.8's
// configuration contract for TAPPA_BUSINESS_DAY_FROM, through Load itself: unset is the switch
// off (nil, and Load succeeds); an RFC 3339 instant is read in UTC; a value in the past is always
// accepted; a malformed one and one more than 30 days after start-up refuse to start, naming the
// variable (security lens F-3: one replica, so a typo is better as a refusal than as a switch that
// silently never turns on).
func TestLoad_BusinessDayFromIsOptionalAnInstantAndAtMostThirtyDaysAhead(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		val     string
		wantErr string // substring; "" means Load succeeds
		want    *time.Time
	}{
		{name: "unset is off", val: ""},
		{name: "far past is accepted", val: "2000-01-01T00:00:00Z", want: ptrTime(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))},
		{name: "an offset is read as its instant", val: "2026-10-01T05:00:00+02:00", want: ptrTime(time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC))},
		{name: "29 days ahead is accepted", val: now.Add(29 * 24 * time.Hour).UTC().Format(time.RFC3339)},
		{name: "31 days ahead is refused", val: now.Add(31 * 24 * time.Hour).UTC().Format(time.RFC3339), wantErr: "more than 30 days"},
		{name: "the 2062 typo is refused", val: "2062-11-02T03:00:00Z", wantErr: "more than 30 days"},
		{name: "a date without a time is refused", val: "2026-11-02", wantErr: "RFC 3339"},
		{name: "a leading blank is refused", val: " 2026-11-02T03:00:00Z", wantErr: "RFC 3339"},
		{name: "garbage is refused", val: "tomorrow", wantErr: "RFC 3339"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv(config.EnvBusinessDayFrom, tc.val)
			c, err := config.Load()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Load accepted %q", tc.val)
				}
				if !strings.Contains(err.Error(), config.EnvBusinessDayFrom) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to name %s and say %q", err, config.EnvBusinessDayFrom, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			switch {
			case tc.val == "":
				if c.BusinessDayFrom != nil {
					t.Fatalf("unset gave %v, want nil (the switch off)", *c.BusinessDayFrom)
				}
			case c.BusinessDayFrom == nil:
				t.Fatalf("%q gave nil", tc.val)
			case c.BusinessDayFrom.Location() != time.UTC:
				t.Fatalf("%q is not held in UTC: %v", tc.val, *c.BusinessDayFrom)
			case tc.want != nil && !c.BusinessDayFrom.Equal(*tc.want):
				t.Fatalf("%q = %v, want %v", tc.val, *c.BusinessDayFrom, *tc.want)
			}
		})
	}
}

// TestParseBusinessDayFrom_TheBoundIsMeasuredFromTheStartUpInstant pins the bound's edge on a
// fixed clock: exactly now + 30 days is accepted, one second more is refused.
func TestParseBusinessDayFrom_TheBoundIsMeasuredFromTheStartUpInstant(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	edge := now.Add(config.BusinessDayFromMaxAhead)
	if config.BusinessDayFromMaxAhead != 30*24*time.Hour {
		t.Fatalf("the bound is %v, ADR 0032 §A.8 says 30 days", config.BusinessDayFromMaxAhead)
	}
	if v, err := config.ParseBusinessDayFrom(edge.Format(time.RFC3339), now); err != nil || v == nil || !v.Equal(edge) {
		t.Fatalf("exactly 30 days ahead: %v, %v", v, err)
	}
	if _, err := config.ParseBusinessDayFrom(edge.Add(time.Second).Format(time.RFC3339), now); err == nil {
		t.Fatal("30 days and a second ahead was accepted")
	}
}

// TestAnnounceBusinessDay_TheValueIsOnTheStartUpLine pins §A.8's "the value is logged at
// start-up" — in each of the three states (unset, set and on, set and not yet on), one line that
// names the variable, carries the value (it is not a secret) and says whether it is on.
func TestAnnounceBusinessDay_TheValueIsOnTheStartUpLine(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	tests := []struct {
		name   string
		from   *time.Time
		value  string
		active bool
		says   string
	}{
		{"unset", nil, "", false, "OFF"},
		{"set and on", &past, "2026-10-21T11:00:00Z", true, "ON"},
		{"set and not yet on", &future, "2026-10-21T13:00:00Z", false, "not yet on"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			config.AnnounceBusinessDay(slog.New(slog.NewJSONHandler(&buf, nil)), tc.from, now)
			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			if len(lines) != 1 {
				t.Fatalf("%d lines, want exactly one: %q", len(lines), buf.String())
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
				t.Fatalf("not JSON: %v", err)
			}
			if rec["level"] != "INFO" || rec["variable"] != config.EnvBusinessDayFrom ||
				rec["business_day_from"] != tc.value || rec["active"] != tc.active ||
				!strings.Contains(rec["msg"].(string), tc.says) {
				t.Fatalf("line = %v, want INFO, variable %s, business_day_from %q, active %v, msg saying %q",
					rec, config.EnvBusinessDayFrom, tc.value, tc.active, tc.says)
			}
		})
	}
}

func ptrTime(v time.Time) *time.Time { return &v }
