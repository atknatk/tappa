// Package billing turns "who worked here this month" into "what this month cost",
// and freezes the answer.
//
// 🔴 IT IS THE FIRST PACKAGE IN THIS PRODUCT THAT WRITES A NUMBER SOMEBODY WILL BE
// ASKED TO PAY, and that changes what "correct" means. internal/domain/ledger
// recomputes everything on every view, deliberately (§4.3: a stored total is a
// second representation of a sum the immutable records already determine). This
// package does the OPPOSITE, and for the same reason read the other way round: the
// inputs to an invoice are MUTABLE. People are deactivated, moved and
// GDPR-anonymised; a price can change; a tenant's timezone can change, which moves
// the very boundary the month was counted over. A recomputed invoice is one that
// quietly disagrees with the one that was sent. So a closed month is stored, and
// billing_periods is APPEND-ONLY (migration 0016) because a frozen figure that can
// be edited is not frozen.
//
// 🔴 THE ARITHMETIC IS IN POSTGRES, NOT HERE, and that is the load-bearing decision
// of this package. The billable definition, the month's boundaries in the tenant's
// own zone, the founding-offer window and the multiplication all live in migration
// 0016's functions, called by db/queries/billing.sql. Go carries values; it does not
// derive them. Two consequences worth stating:
//
//   - There is ONE definition of each rule, shared by the live preview and the
//     statement that freezes -- so the number a manager saw before pressing the
//     button is the number that froze. A Go copy of the month boundary or the free
//     window would be a second definition that drifts, which is the class
//     internal/domain/ledger/report.go names at readLimit and fixed the same way.
//   - No caller can supply a figure. CloseBillingPeriod takes a tenant, a month and
//     an author; there is no parameter an amount could be posted into. On an
//     append-only table that is the difference between a bug and a permanent one.
//
// 🔴 §4.7 -- WHAT A BILLING ROW CANNOT CARRY. No EMPLOYEE's name, email,
// coordinate, address or tap reaches this package. The queries touch `tenants`,
// `employees` (as a COUNT, never a row) and `billing_periods`. An invoice draft
// needs a number of people, not a list of them, and Draft has no field for one.
// The only name here is the BUSINESS's, which is who the document is addressed to.
//
// TENANT SCOPE (§4.5, belt + braces): every statement runs inside
// db.(*DB).WithTenant so RLS applies, AND carries the same tenant id as an explicit
// predicate. The id is the caller's, taken from the signed panel session.
package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// ActionPeriodClosed is the audit action name for freezing a month.
const ActionPeriodClosed = "billing.period_closed"

// HistoryCap is the largest number of frozen periods one history read returns.
//
// IT IS A PAGE, NOT A REFUSAL, and the difference matters -- see
// internal/domain/ledger/report.go's ReportEventCap, which is the other kind. A
// half-read TOTAL is a wrong number wearing the clothes of a right one; a half-read
// LIST is an honest half, so this cap only decides whether the screen offers older
// periods. 60 is five years of monthly invoices, which is past the six-year document
// retention a Maltese business plans around, on a table that grows by exactly twelve
// rows a year per tenant.
const HistoryCap = 60

// readLimit and truncatedBy are the two halves of ONE invariant, and the invariant
// is BETWEEN them: the query must be asked for one row MORE than can be used, or
// "was there more?" can never be true and the answer is dead code. The identical
// pair is in internal/domain/ledger/report.go, where an audit found it written
// inline with no test; it is written as two named functions here for the same
// reason, and pinned by TestReadLimit_MakesTruncationDetectable.
//
// The comparison is STRICT: exactly HistoryCap rows is a complete read that happens
// to fill the budget; one more is the first row that did not fit.
func readLimit() int32 { return HistoryCap + 1 }

func truncatedBy(rowsRead int) bool { return rowsRead > HistoryCap }

// Errors a caller is expected to handle rather than log.
var (
	// ErrUnknownTenant means no tenant row was visible -- which under RLS is the
	// same answer for "does not exist" and "belongs to somebody else", deliberately.
	ErrUnknownTenant = errors.New("billing: unknown tenant")
	// ErrPeriodNotEnded refuses to freeze a month that is still running. The
	// headcount is still moving and the row could never be corrected.
	ErrPeriodNotEnded = errors.New("billing: the period has not ended yet")
	// ErrBeforeSignup refuses a month from before the business existed.
	ErrBeforeSignup = errors.New("billing: the period is before the tenant signed up")
	// ErrAlreadyClosed means the month already has its one frozen answer.
	ErrAlreadyClosed = errors.New("billing: the period is already closed")
)

// Database is the slice of the store this package needs, declared HERE because §7
// puts an interface on the consumer's side. One method: the tenant-scoped
// transaction. Nothing in this package can reach an unscoped pool.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Trail is the audit sink, again declared at the consumer.
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Month is a billing period's label: a calendar month with no clock and no zone.
//
// IT IS NOT A time.Time, for the reason internal/domain/ledger.Date gives about a
// day: "August 2026" is not an instant. It becomes two instants only once a zone is
// known, the zone is the tenant's, and the tenant's is in the database -- which is
// why the instants on a Draft come FROM the database rather than from a conversion
// performed here.
type Month struct {
	Year  int
	Month time.Month
}

// Zero reports whether no month was asked for.
func (m Month) Zero() bool { return m.Year == 0 && m.Month == 0 }

// String renders the ISO 8601 year-month form, which is what the query string and
// the export filename use: "2026-08".
func (m Month) String() string {
	if m.Zero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", m.Year, int(m.Month))
}

// Add returns the month n months later (n may be negative). It goes through
// time.Date so the year rolls over correctly.
func (m Month) Add(n int) Month {
	t := time.Date(m.Year, m.Month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, n, 0)
	return Month{Year: t.Year(), Month: t.Month()}
}

// Before reports whether m precedes o.
func (m Month) Before(o Month) bool {
	if m.Year != o.Year {
		return m.Year < o.Year
	}
	return m.Month < o.Month
}

// ParseMonth reads "2026-08". An empty string is the zero Month rather than an
// error: "no month given" is a legitimate request and means "the last finished one",
// which cannot be computed until the tenant's zone has been read.
func ParseMonth(s string) (Month, error) {
	if s == "" {
		return Month{}, nil
	}
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return Month{}, fmt.Errorf("billing: parse month %q: %w", s, err)
	}
	return Month{Year: t.Year(), Month: t.Month()}, nil
}

// MonthOf is the month an instant falls in, IN THE ZONE GIVEN. The zone is not
// optional and there is no default: "which month is it?" has a different answer in
// Malta and in UTC for two hours of every month, which is the same seam §6 names for
// the overnight shift.
func MonthOf(t time.Time, zone *time.Location) Month {
	if zone == nil {
		zone = time.UTC
	}
	l := t.In(zone)
	return Month{Year: l.Year(), Month: l.Month()}
}

// date renders the month as the first-of-month DATE the queries take.
func (m Month) date() pgtype.Date {
	return pgtype.Date{
		Time:  time.Date(m.Year, m.Month, 1, 0, 0, 0, 0, time.UTC),
		Valid: true,
	}
}

// monthFrom reads a first-of-month DATE back. An invalid date yields the zero Month,
// which String renders as "" rather than as a plausible-looking wrong month.
func monthFrom(d pgtype.Date) Month {
	if !d.Valid {
		return Month{}
	}
	return Month{Year: d.Time.Year(), Month: d.Time.Month()}
}

// Draft is one month of billing: the figures, where they came from, and whether they
// are final.
//
// 🔴 Frozen IS THE FIELD THAT MATTERS. A frozen Draft was READ from billing_periods
// and nothing in it was recomputed; an unfrozen one was computed from today's roster
// and today's price and WILL change if either does. Rendering them identically is
// the failure this whole task exists to prevent, so phase B must say which it is
// looking at, in words, on the screen and in the export.
type Draft struct {
	// ID is the frozen row's identity. It is uuid.Nil on a preview, because a
	// preview is not a row -- which is a second way to read Frozen, not a
	// substitute for it.
	ID         uuid.UUID
	TenantID   uuid.UUID
	TenantName string
	Month      Month
	// From and To are the UTC instants of the local month, To EXCLUSIVE. They come
	// from the database on both paths -- computed by tappa_local_month_start for a
	// preview, read from the frozen row for a closed period -- so that a closed
	// month keeps the boundary it was COUNTED over even if the tenant later changes
	// zone.
	From, To time.Time
	// Zone is the IANA name those instants were resolved in.
	Zone string
	// Plan is 'founding' or 'standard', frozen with the row so a CLOSED month keeps
	// the terms it was closed under. It does not protect a month that is still open
	// -- migration 0016 bounds that claim where free_period is declared.
	Plan string
	// FirstChargeableMonth is the month the founding offer's free window ends. It is
	// only set on a PREVIEW: a frozen row records the decision (Free) rather than the
	// rule, because the rule is allowed to change and the decision is not.
	FirstChargeableMonth Month
	// Free means this month falls inside the founding offer's three free periods.
	Free bool
	// EmployeeCount is the billable headcount: people whose employment interval
	// overlapped [From, To) and whose status agrees with their lifecycle stamps.
	//
	// 🔴 THE NUMBER IS FROZEN; WHO WAS COUNTED IS NOT. A closed period holds no list
	// of employees and nothing else recovers one, so "why 24?" can only be answered
	// by re-running the definition against today's roster -- which no longer resolves
	// for anybody anonymised under GDPR since. Freezing defends the INVOICE against a
	// changing roster; it does not defend an ITEMISATION of it. Migration 0016 states
	// why storing the ids was rejected rather than forgotten, and any screen or export
	// that offers to "show the people behind this figure" is making a promise this
	// data cannot keep for an old period.
	EmployeeCount int
	// UnstampedEmployees is how many of the TENANT's employee rows had a status that
	// disagreed with their lifecycle stamps at the moment this was computed. Such
	// rows are never billable -- see migration 0016's tappa_employee_is_billable for
	// why, and what that costs -- and the count is carried so that "excluded" never
	// means "invisible" (§4.6).
	//
	// 🔴 A NON-ZERO VALUE MAKES EmployeeCount A FLOOR, and phase B owes the screen a
	// sentence saying so. It is the same obligation Report.Truncated carries.
	//
	// ⚠️ IT IS NOT "HOW MANY THIS MONTH LOST". The count is tenant-wide and
	// point-in-time: it includes people who left long before this period and were
	// never candidates for it, so it is an UPPER BOUND on the effect. It cannot be
	// narrowed to the period, because narrowing means intersecting an interval that
	// a disagreeing row does not have -- 0016 states the measurement.
	UnstampedEmployees int
	UnitPrice          Money
	AmountDue          Money

	// Frozen, and the two fields that only mean anything when it is true.
	Frozen   bool
	ClosedAt time.Time
	ClosedBy uuid.UUID

	// HasEnded and AfterSignup only mean anything on a PREVIEW: they are the
	// screen's copy of two of the three conditions CloseBillingPeriod enforces
	// itself, so a button can be disabled with a reason. They are never the
	// authority -- the statement is.
	HasEnded    bool
	AfterSignup bool
}

// Book is the billing period register: read one month, freeze one month, list the
// frozen history.
type Book struct {
	data  Database
	trail Trail
	log   *slog.Logger
}

// NewBook wires it. A nil database or a nil trail is refused rather than tolerated:
// a Book that cannot write its audit row must not be constructible, because closing
// a period without a trail is precisely the untraceable act §4.3 forbids.
func NewBook(data Database, trail Trail, log *slog.Logger) (*Book, error) {
	if data == nil {
		return nil, errors.New("billing: nil database")
	}
	if trail == nil {
		return nil, errors.New("billing: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Book{data: data, trail: trail, log: log}, nil
}

// Preview computes one month LIVE, without writing anything.
//
// It is the right answer for a month that is still running and for one that has
// ended but not been closed. For a month that HAS been closed it is the wrong
// answer, and Period is what callers should use -- see there.
func (b *Book) Preview(ctx context.Context, tenantID uuid.UUID, m Month) (Draft, error) {
	var out Draft
	err := b.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		d, err := preview(ctx, store.New(tx), tenantID, m)
		out = d
		return err
	})
	if err != nil {
		return Draft{}, wrap("preview", err)
	}
	return out, nil
}

// Period is the answer the screen wants: the FROZEN figures when the month has been
// closed, and a live preview when it has not.
//
// 🔴 THE FROZEN ROW WINS AND NOTHING RECOMPUTES IT. That is the M6-12 card's fourth
// criterion, and the ORDER of the two reads is where it is enforced: the frozen row
// is asked for first, and the preview only runs when there is none. Doing it the
// other way -- preview always, then overwrite with the frozen row -- would look
// identical and would cost every viewer of a two-year-old invoice a full count over
// today's roster.
func (b *Book) Period(ctx context.Context, tenantID uuid.UUID, m Month) (Draft, error) {
	var out Draft
	err := b.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.GetBillingPeriod(ctx, store.GetBillingPeriodParams{
			TenantID:    tenantID,
			PeriodMonth: m.date(),
		})
		if err == nil {
			d, e := frozenDraft(row.ID, row.TenantID, row.TenantName, row.PeriodMonth, row.PeriodFrom, row.PeriodTo,
				row.Timezone, row.Plan, row.FreePeriod, row.EmployeeCount, row.UnstampedEmployees,
				row.UnitPrice, row.Currency, row.AmountDue, row.ClosedBy, row.ClosedAt)
			out = d
			return e
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("load frozen period: %w", err)
		}
		d, err := preview(ctx, q, tenantID, m)
		out = d
		return err
	})
	if err != nil {
		return Draft{}, wrap("period", err)
	}
	return out, nil
}

// Close FREEZES one month and records who did it, in ONE transaction.
//
// 🔴 THE TRAIL SHARES THE TRANSACTION, which is the opposite of this codebase's
// usual advice and right for the same reason internal/domain/tenant.Staff gives: if
// the audit row cannot be written, the frozen period rolls back with it. Everywhere
// else a lost audit row would hide a FAILURE (§4.6 wants the failure recorded); here
// it would hide a permanent, irreversible SUCCESS, and an invoice nobody can attach
// to a person is worse than no invoice.
//
// The three refusals -- not ended, before signup, already closed -- come back as
// sentinel errors so a caller can say WHICH, rather than as a bare "no rows".
func (b *Book) Close(ctx context.Context, tenantID uuid.UUID, m Month, actor uuid.UUID) (Draft, error) {
	if actor == uuid.Nil {
		return Draft{}, errors.New("billing: close: no actor")
	}
	if m.Zero() {
		return Draft{}, errors.New("billing: close: no month")
	}
	var out Draft
	err := b.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.CloseBillingPeriod(ctx, store.CloseBillingPeriodParams{
			TenantID:    tenantID,
			PeriodMonth: m.date(),
			ClosedBy:    actor,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return ErrAlreadyClosed
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("close period: %w", err)
			}
			// THE STATEMENT REFUSED AND ONLY IT KNOWS WHY. Reading the same facts
			// back INSIDE the same transaction turns one "no rows" into the two
			// different sentences a manager needs. This read is diagnosis, never
			// authority: the statement above already declined.
			return b.explainRefusal(ctx, q, tenantID, m)
		}
		// TenantName is "" here: a RETURNING clause cannot join, and the caller of
		// Close already knows whose period it just closed. Period and History, which
		// are the read paths a document is rendered from, do carry it.
		d, err := frozenDraft(row.ID, row.TenantID, "", row.PeriodMonth, row.PeriodFrom, row.PeriodTo,
			row.Timezone, row.Plan, row.FreePeriod, row.EmployeeCount, row.UnstampedEmployees,
			row.UnitPrice, row.Currency, row.AmountDue, row.ClosedBy, row.ClosedAt)
		if err != nil {
			return err
		}
		out = d

		// 🔴 THE DETAIL CARRIES THE FIGURES, NOT A NAME. Amounts are rendered as
		// exact decimal STRINGS: audit_log.detail is jsonb, and a JSON number is a
		// float64 in every reader that will ever parse it -- storing 2019.00 as a
		// JSON number would be the one place this package let a float touch money.
		if _, err := b.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID,
			ActorID:  &actor,
			Action:   ActionPeriodClosed,
			Target:   m.String(),
			Detail: closeDetail{
				Month:              m.String(),
				EmployeeCount:      d.EmployeeCount,
				UnstampedEmployees: d.UnstampedEmployees,
				UnitPrice:          d.UnitPrice.String(),
				AmountDue:          d.AmountDue.String(),
				Currency:           d.AmountDue.Currency(),
				Free:               d.Free,
				Plan:               d.Plan,
			},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Draft{}, wrap("close", err)
	}
	b.log.Info("billing period closed",
		"tenant_id", tenantID, "period", m.String(), "actor_id", actor,
		"employee_count", out.EmployeeCount, "amount_due", out.AmountDue.String())
	return out, nil
}

// closeDetail is the audit row's payload -- a purpose-built struct with known keys,
// as internal/audit's package doc requires. Money is text, never a JSON number.
type closeDetail struct {
	Month              string `json:"month"`
	EmployeeCount      int    `json:"employee_count"`
	UnstampedEmployees int    `json:"unstamped_employees"`
	UnitPrice          string `json:"unit_price"`
	AmountDue          string `json:"amount_due"`
	Currency           string `json:"currency"`
	Free               bool   `json:"free_period"`
	Plan               string `json:"plan"`
}

// explainRefusal turns CloseBillingPeriod's empty result into the reason for it.
func (b *Book) explainRefusal(ctx context.Context, q *store.Queries, tenantID uuid.UUID, m Month) error {
	d, err := preview(ctx, q, tenantID, m)
	if err != nil {
		return err
	}
	switch {
	case !d.AfterSignup:
		return ErrBeforeSignup
	case !d.HasEnded:
		return ErrPeriodNotEnded
	default:
		// The statement declined and the two named conditions both hold. Saying so
		// is better than inventing a third reason: something changed between the
		// two reads, or a guard exists that this function does not know about.
		return errors.New("billing: the period could not be closed and no stated reason applies")
	}
}

// History returns the frozen periods, newest first, and whether there are more than
// this read could carry.
func (b *Book) History(ctx context.Context, tenantID uuid.UUID) ([]Draft, bool, error) {
	var out []Draft
	var truncated bool
	err := b.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := store.New(tx).ListBillingPeriods(ctx, store.ListBillingPeriodsParams{
			TenantID: tenantID,
			// ONE ROW MORE THAN CAN BE USED -- see readLimit.
			RowLimit: readLimit(),
		})
		if err != nil {
			return fmt.Errorf("list periods: %w", err)
		}
		truncated = truncatedBy(len(rows))
		if len(rows) > HistoryCap {
			rows = rows[:HistoryCap]
		}
		out = make([]Draft, 0, len(rows))
		for _, r := range rows {
			d, e := frozenDraft(r.ID, r.TenantID, r.TenantName, r.PeriodMonth, r.PeriodFrom, r.PeriodTo,
				r.Timezone, r.Plan, r.FreePeriod, r.EmployeeCount, r.UnstampedEmployees,
				r.UnitPrice, r.Currency, r.AmountDue, r.ClosedBy, r.ClosedAt)
			if e != nil {
				return e
			}
			out = append(out, d)
		}
		return nil
	})
	if err != nil {
		return nil, false, wrap("history", err)
	}
	return out, truncated, nil
}

// preview runs the live computation on an already-scoped transaction. Both Preview
// and Period call it, so there is one place the row becomes a Draft.
func preview(ctx context.Context, q *store.Queries, tenantID uuid.UUID, m Month) (Draft, error) {
	row, err := q.PreviewBillingPeriod(ctx, store.PreviewBillingPeriodParams{
		TenantID:    tenantID,
		PeriodMonth: m.date(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Draft{}, ErrUnknownTenant
		}
		return Draft{}, fmt.Errorf("preview period: %w", err)
	}
	// A PREVIEW HAS NO FROZEN CURRENCY TO READ, so it uses the package default,
	// which is asserted equal to the column default against the live schema.
	unit, err := MoneyFromNumeric(row.UnitPrice, DefaultCurrency)
	if err != nil {
		return Draft{}, fmt.Errorf("unit price: %w", err)
	}
	amount, err := MoneyFromNumeric(row.AmountDue, DefaultCurrency)
	if err != nil {
		return Draft{}, fmt.Errorf("amount due: %w", err)
	}
	return Draft{
		TenantID:             row.TenantID,
		TenantName:           row.TenantName,
		Month:                monthFrom(row.PeriodMonth),
		From:                 row.PeriodFrom,
		To:                   row.PeriodTo,
		Zone:                 row.Timezone,
		Plan:                 row.Plan,
		FirstChargeableMonth: monthFrom(row.FirstChargeableMonth),
		Free:                 row.FreePeriod,
		EmployeeCount:        int(row.EmployeeCount),
		UnstampedEmployees:   int(row.UnstampedEmployees),
		UnitPrice:            unit,
		AmountDue:            amount,
		Frozen:               false,
		HasEnded:             row.PeriodHasEnded,
		AfterSignup:          row.PeriodIsAfterSignup,
	}, nil
}

// frozenDraft builds a Draft from a stored row. The argument list is long and
// positional because the three generated row types (Close/Get/List) are structurally
// identical but distinct Go types, and one conversion is better than three copies.
func frozenDraft(
	id, tenantID uuid.UUID, name string, month pgtype.Date, from, to time.Time,
	zone, plan string, free bool, count, unstamped int32,
	unitPrice pgtype.Numeric, currency string, amountDue pgtype.Numeric,
	closedBy uuid.UUID, closedAt time.Time,
) (Draft, error) {
	unit, err := MoneyFromNumeric(unitPrice, currency)
	if err != nil {
		return Draft{}, fmt.Errorf("unit price: %w", err)
	}
	amount, err := MoneyFromNumeric(amountDue, currency)
	if err != nil {
		return Draft{}, fmt.Errorf("amount due: %w", err)
	}
	return Draft{
		ID:                 id,
		TenantID:           tenantID,
		TenantName:         name,
		Month:              monthFrom(month),
		From:               from,
		To:                 to,
		Zone:               zone,
		Plan:               plan,
		Free:               free,
		EmployeeCount:      int(count),
		UnstampedEmployees: int(unstamped),
		UnitPrice:          unit,
		AmountDue:          amount,
		Frozen:             true,
		ClosedAt:           closedAt,
		ClosedBy:           closedBy,
		// A frozen row records the DECISION (Free), not the rule that produced it,
		// so there is no FirstChargeableMonth to report and the zero Month renders
		// as "" rather than as a plausible-looking wrong answer.
		//
		// HasEnded / AfterSignup are true by construction: the statement that wrote
		// the row refused to run otherwise.
		HasEnded:    true,
		AfterSignup: true,
	}, nil
}

// wrap keeps the sentinel errors unwrapped-through and prefixes everything else.
func wrap(op string, err error) error {
	switch {
	case errors.Is(err, ErrUnknownTenant), errors.Is(err, ErrPeriodNotEnded),
		errors.Is(err, ErrBeforeSignup), errors.Is(err, ErrAlreadyClosed):
		return err
	default:
		return fmt.Errorf("billing: %s: %w", op, err)
	}
}

// isUniqueViolation reports whether Postgres refused the statement because a unique
// index already held the row (SQLSTATE 23505) -- here, a second close of the same
// month. It matches the CODE, not the message, for the reason
// internal/domain/tenant/rulewriter.go gives: an error STRING is locale- and
// version-dependent.
func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
