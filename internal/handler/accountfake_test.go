package handler

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// fakeAccounts is the double for internal/domain/tenant.Accounts.
//
// 🔴 IT STARTS AS A REAL BUSINESS RATHER THAN AS A ZERO VALUE, and that is the same
// argument fakeTexts makes in reverse. A double whose Settings returned an empty struct
// would make every "the screen prints what is on file" assertion pass over blanks, and
// the one field this screen exists to expose — what the VAT register said — has FOUR
// states whose zero value is one of them. So the default row is a checked, valid,
// Maltese restaurant, and a test that wants another state says so.
type fakeAccounts struct {
	mu sync.Mutex
	// settings is the stored row.
	settings tenant.Settings
	// saves records every Save that reached this double, in order, so a test can
	// assert what the handler PASSED rather than only what came back. The TENANT AND
	// ACTOR are recorded because the §4.5 and §4.7 story of this feature is that both
	// are the session's and neither is the form's.
	saves []tenant.AccountCommand
	// readErr / saveErr, when set, are returned instead of answering.
	readErr error
	saveErr error
	// reads counts Settings calls, so a test can assert that a refused POST cost no
	// database read at all.
	reads int
}

// fakeVATChecked is the instant the double's default row was checked. It is a FIXED
// date rather than time.Now so a rendered page is byte-stable across runs.
var fakeVATChecked = time.Date(2026, 5, 3, 11, 15, 0, 0, time.UTC)

func newFakeAccount() *fakeAccounts {
	verified := true
	checked := fakeVATChecked
	return &fakeAccounts{
		settings: tenant.Settings{
			Name:         "Kebab Factory Ltd.",
			VATNumber:    "MT20250001",
			BusinessType: "restaurant",
			Timezone:     "Europe/Malta",
			VAT:          tenant.VATCheck{Verified: &verified, CheckedAt: &checked},
			CreatedAt:    time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
		},
	}
}

func (f *fakeAccounts) Settings(_ context.Context, _ uuid.UUID) (tenant.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.readErr != nil {
		return tenant.Settings{}, f.readErr
	}
	return f.settings, nil
}

// Save applies what the handler passed, so a re-read after a save sees the new values —
// which is what the section's "you land on what was STORED" claim needs in order to be
// testable at all.
func (f *fakeAccounts) Save(_ context.Context, c tenant.AccountCommand) (tenant.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves = append(f.saves, c)
	if f.saveErr != nil {
		return tenant.Settings{}, f.saveErr
	}
	f.settings.Name = c.Name
	f.settings.BusinessType = c.BusinessType
	f.settings.Timezone = c.Timezone
	return f.settings, nil
}

// The accessors below take the lock, so a test never reads a field the handler's
// goroutine may still be writing (-race).
func (f *fakeAccounts) savedCommands() []tenant.AccountCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tenant.AccountCommand(nil), f.saves...)
}

func (f *fakeAccounts) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

func (f *fakeAccounts) setVAT(verified *bool, checkedAt *time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings.VAT = tenant.VATCheck{Verified: verified, CheckedAt: checkedAt}
}
