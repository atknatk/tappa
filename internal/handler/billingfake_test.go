package handler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/billing"
)

// fakeBooks is the billing register's test double.
//
// 🔴 IT MIRRORS THE PRODUCTION CONTRACT WHERE THE CONTRACT IS WHAT A TEST MEASURES,
// which is the lesson policies_test.go's fakeScribe records twice over: a double that
// answers "yes" to everything makes the handler's refusal arms dead code, and an audit
// deleted a real protection with the suite staying green. The three properties mirrored
// here are the ones handler code branches on:
//
//	Period returns a FROZEN draft when the month is in `frozen`, and a preview
//	    otherwise -- so billingCloseReview's "already closed" arm is reachable.
//	Close refuses a month that is already in `frozen` with billing.ErrAlreadyClosed,
//	    which is the UNIQUE constraint's answer, so a replayed confirmation is measured
//	    rather than assumed.
//	Close RECORDS that it was called, so a test can assert the read path never reaches
//	    it. That counter is the whole point of the double for
//	    TestBillingRead_WritesNothing's live half.
type fakeBooks struct {
	mu sync.Mutex

	// draft is what a preview of any not-frozen month answers. Its Month is
	// overwritten with the month asked for, so a caller cannot accidentally assert
	// against a month it did not request.
	draft billing.Draft
	// frozen maps an ISO month onto the stored answer for it.
	frozen map[string]billing.Draft
	// history and capped are what History answers.
	history []billing.Draft
	capped  bool

	// err, if set, is returned by every method -- the read-failure path §4.6 requires
	// a problem page for.
	err error

	// closes counts Close calls and closedMonths records them, in order.
	closes       int
	closedMonths []string
	// previews and periods count the read calls, so a test can prove the two-read
	// month resolution took one read when the month was named.
	previews  int
	periods   int
	histories int
}

func newFakeBooks() *fakeBooks {
	// A month that has ENDED and is AFTER SIGNUP, so the default fixture is the one
	// state in which the close button is offered. A double whose default state hid the
	// control would make every "the button is there" assertion vacuous.
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	return &fakeBooks{
		frozen: map[string]billing.Draft{},
		draft: billing.Draft{
			TenantID:             uuid.New(),
			TenantName:           "Kebab Factory Ltd",
			Month:                billing.Month{Year: 2026, Month: time.July},
			From:                 from,
			To:                   from.AddDate(0, 1, 0),
			Zone:                 "Europe/Malta",
			Plan:                 "founding",
			FirstChargeableMonth: billing.Month{Year: 2026, Month: time.September},
			Free:                 true,
			EmployeeCount:        24,
			UnitPrice:            billing.NewMoney(150, "EUR"),
			AmountDue:            billing.NewMoney(0, "EUR"),
			Frozen:               false,
			HasEnded:             true,
			AfterSignup:          true,
		},
	}
}

// frozenDraftFor builds a plausible stored row for one month.
func frozenDraftFor(m billing.Month, count int, minor int64) billing.Draft {
	from := time.Date(m.Year, m.Month, 1, 0, 0, 0, 0, time.UTC)
	return billing.Draft{
		ID:            uuid.New(),
		TenantName:    "Kebab Factory Ltd",
		Month:         m,
		From:          from,
		To:            from.AddDate(0, 1, 0),
		Zone:          "Europe/Malta",
		Plan:          "founding",
		EmployeeCount: count,
		UnitPrice:     billing.NewMoney(150, "EUR"),
		AmountDue:     billing.NewMoney(minor, "EUR"),
		Frozen:        true,
		ClosedAt:      from.AddDate(0, 1, 1),
		ClosedBy:      uuid.New(),
		HasEnded:      true,
		AfterSignup:   true,
	}
}

func (f *fakeBooks) Preview(_ context.Context, _ uuid.UUID, m billing.Month) (billing.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previews++
	if f.err != nil {
		return billing.Draft{}, f.err
	}
	return f.previewLocked(m), nil
}

func (f *fakeBooks) previewLocked(m billing.Month) billing.Draft {
	d := f.draft
	d.Month = m
	d.Frozen = false
	return d
}

func (f *fakeBooks) Period(_ context.Context, _ uuid.UUID, m billing.Month) (billing.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.periods++
	if f.err != nil {
		return billing.Draft{}, f.err
	}
	// THE FROZEN ROW WINS AND NOTHING RECOMPUTES IT -- the production Period's own
	// ordering, mirrored so a test that asserts on it is asserting on the shape the
	// handler will meet.
	if d, ok := f.frozen[m.String()]; ok {
		return d, nil
	}
	return f.previewLocked(m), nil
}

func (f *fakeBooks) History(_ context.Context, _ uuid.UUID) ([]billing.Draft, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.histories++
	if f.err != nil {
		return nil, false, f.err
	}
	return f.history, f.capped, nil
}

func (f *fakeBooks) Close(_ context.Context, _ uuid.UUID, m billing.Month, actor uuid.UUID) (billing.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	f.closedMonths = append(f.closedMonths, m.String())
	if f.err != nil {
		return billing.Draft{}, f.err
	}
	if actor == uuid.Nil {
		return billing.Draft{}, errors.New("billing: close: no actor")
	}
	// THE UNIQUE CONSTRAINT'S ANSWER. billing_periods refuses a second row for the same
	// month with 23505, which the domain reports as ErrAlreadyClosed; without this the
	// replay case would be untestable at this layer.
	if _, ok := f.frozen[m.String()]; ok {
		return billing.Draft{}, billing.ErrAlreadyClosed
	}
	d := frozenDraftFor(m, f.draft.EmployeeCount, f.draft.AmountDue.Minor())
	d.ClosedBy = actor
	f.frozen[m.String()] = d
	f.history = append([]billing.Draft{d}, f.history...)
	return d, nil
}

// closeCount and readCounts are the accessors a test asserts through, so no test
// touches the mutex-guarded fields directly.
func (f *fakeBooks) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

func (f *fakeBooks) readCounts() (previews, periods, histories int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.previews, f.periods, f.histories
}
