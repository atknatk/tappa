package handler

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/manual"
)

// manualvenue_test.go -- K-14 (M11 API-1) on the manual-entry screen: the venue picker is
// rendered for a person with no home venue and only for them; the review refuses without a
// venue of this business, the confirmation is bound to the chosen venue, and the write forwards
// it -- while for a person WITH a home venue a posted venue never reaches the domain.

// TestManualEntry_AVenuelessPersonChoosesTheVenueAndOnlyThem.
func TestManualEntry_AVenuelessPersonChoosesTheVenueAndOnlyThem(t *testing.T) {
	stj, sliema := uuid.New(), uuid.New()
	venueless := manual.Subject{NeedsVenue: true, Venues: []manual.Venue{{ID: stj, Name: "KF St Julians"}, {ID: sliema, Name: "KF Sliema"}}}

	// The form: a picker with this business's venues and a "choose" option selected.
	rec := &fakeRecorder{subject: venueless}
	b := panelBrowserWithRecorder(t, rec)
	body := htmlOf(t, b.do(http.MethodGet, manualFormHref(), nil))
	for _, want := range []string{`name="location"`, stj.String(), sliema.String(), "Choose the venue", "no home venue"} {
		if !strings.Contains(body, want) {
			t.Errorf("the venue-less person's form lacks %q", want)
		}
	}
	// A person WITH a home venue: no picker at all.
	homeBody := htmlOf(t, panelBrowserWithRecorder(t, &fakeRecorder{subject: manual.Subject{LocationName: "KF St Julians"}}).do(http.MethodGet, manualFormHref(), nil))
	if strings.Contains(homeBody, `name="location"`) {
		t.Error("a person WITH a home venue is offered a venue picker; the statement files them at home")
	}

	// The review without a venue, and with another business's: refused, nothing minted.
	form := url.Values{"id": {entryID.String()}, "direction": {"in"}, "date": {"2026-08-05"}, "time": {"09:00"}}
	for _, venue := range []string{"", uuid.NewString(), "not-a-uuid"} {
		f := url.Values{}
		for k, v := range form {
			f[k] = v
		}
		if venue != "" {
			f.Set("location", venue)
		}
		rec := &fakeRecorder{subject: venueless}
		b := panelBrowserWithRecorder(t, rec)
		page := htmlOf(t, b.do(http.MethodPost, manualEntryHref, f))
		if !strings.Contains(page, htmlText("Nothing was recorded — choose the venue")) || strings.Contains(page, "Record it") {
			t.Errorf("a review with venue %q was not refused with the venue sentence", venue)
		}
		if _, ok := b.cookies[adminConfirmCookieName]; ok {
			t.Errorf("a review with venue %q minted a confirmation", venue)
		}
	}

	// The chosen venue: shown on the summary, carried by the write form, bound into the
	// confirmation, and forwarded to the domain.
	rec = &fakeRecorder{subject: venueless}
	b = panelBrowserWithRecorder(t, rec)
	form.Set("location", sliema.String())
	page := htmlOf(t, b.do(http.MethodPost, manualEntryHref, form))
	if !strings.Contains(page, "At KF Sliema") || !strings.Contains(page, `name="location" value="`+sliema.String()+`"`) {
		t.Fatal("the summary does not name the chosen venue or the write form does not carry it")
	}
	form.Set(confirmField, confirmTokenIn(t, page))
	switched := url.Values{}
	for k, v := range form {
		switched[k] = v
	}
	switched.Set("location", stj.String())
	if res := b.do(http.MethodPost, manualRecordHref, switched); len(rec.entries()) != 0 {
		t.Errorf("a confirmation served for KF Sliema was spent on KF St Julians (status %d)", res.Code)
	}
	if res := b.do(http.MethodPost, manualRecordHref, form); res.Code != http.StatusSeeOther {
		t.Fatalf("the confirmed write answered %d, want 303", res.Code)
	}
	if got := rec.entries(); len(got) != 1 || got[0].LocationID != sliema {
		t.Errorf("the domain saw %+v, want one entry at the chosen venue %s", got, sliema)
	}

	// The domain's own refusal re-renders with the venue sentence.
	rec = &fakeRecorder{subject: venueless, recordErr: manual.ErrVenue}
	b = panelBrowserWithRecorder(t, rec)
	form.Del(confirmField)
	form.Set("location", stj.String())
	page = htmlOf(t, b.do(http.MethodPost, manualEntryHref, form))
	form.Set(confirmField, confirmTokenIn(t, page))
	if page := htmlOf(t, b.do(http.MethodPost, manualRecordHref, form)); !strings.Contains(page, htmlText("Nothing was recorded — choose the venue")) {
		t.Error("manual.ErrVenue from the domain is not the venue sentence")
	}

	// A person WITH a home venue: a posted venue never reaches the domain.
	rec = &fakeRecorder{subject: manual.Subject{LocationName: "KF St Julians"}}
	b = panelBrowserWithRecorder(t, rec)
	form.Del(confirmField)
	form.Set("location", sliema.String())
	page = htmlOf(t, b.do(http.MethodPost, manualEntryHref, form))
	form.Set(confirmField, confirmTokenIn(t, page))
	if res := b.do(http.MethodPost, manualRecordHref, form); res.Code != http.StatusSeeOther {
		t.Fatalf("a home-venue person's write answered %d", res.Code)
	}
	if got := rec.entries(); len(got) != 1 || got[0].LocationID != uuid.Nil {
		t.Errorf("a home-venue person's entry carried the posted venue: %+v", got)
	}
}
