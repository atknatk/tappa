package billing

// money.go -- an exact amount of money, and the only bridge between Postgres
// numeric and anything Go does with a price.
//
// 🔴 NO float64 APPEARS IN THIS FILE, AND THAT IS CLAUDE.md §6 RATHER THAN TASTE.
// The obvious conversion -- pgtype.Numeric.Float64Value() -- is exactly the one
// internal/domain/tenant/venue.go uses for a GPS coordinate, and it is right there
// and wrong here: a coordinate is a measurement with a tolerance, and an invoice
// line is not.
//
// ⚠️ AND THE USUAL EXAMPLE DOES NOT WORK HERE, WHICH IS THE INTERESTING PART. This
// comment first claimed that "a hundred employees at 1.50 is 149.99999999999997";
// that is FALSE and was caught by a test of it going green in the wrong direction.
// 1.5 is 3/2, exactly representable in binary, so at TODAY's list price float64
// gives the exact answer -- measured, 1.50 x 1346 = 2019 exactly, by repeated
// addition and by multiplication.
//
// That is the argument FOR this file rather than against it: the safety would be a
// property of the PRICE, and the price is a per-tenant column that exists precisely
// so it can differ (docs/handoff.md §11 promises founding customers a lifetime rate).
// One list-price change away from 1.10, measured on the same figures:
//
//	1.10 summed 1346 times   1480.5999999999772
//	1.10 x 1346              1480.6000000000001
//	the exact answer         1480.60
//
// Two routes to one invoice line, disagreeing with each other and neither correct.
// The conversion below is integer-only instead: big.Int mantissa, decimal exponent,
// and a refusal when the value does not fit exactly.
//
// 🔴 IT ALSO NEVER COMPUTES A TOTAL. unit_price x employee_count happens in
// Postgres (migration 0016's tappa_billing_amount_due, which the frozen column is
// generated from and the live preview calls), so there is one multiplication in the
// system rather than two that must agree. Money's job is to CARRY that answer
// exactly and render it; Add exists for summing invoices that already exist, not
// for building one.

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// moneyScale is how many decimal places a money value has. Migration 0016 declares
// numeric(10,2) for the unit price and numeric(12,2) for the amount, so Postgres
// hands back exactly two -- this constant is the Go side of that same decision, and
// TestMoney_ScaleMatchesTheSchema pins the two together against a live column rather
// than trusting this comment.
const moneyScale = 2

// DefaultCurrency is the currency a period is billed in when nothing says otherwise.
//
// It mirrors billing_periods.currency's column DEFAULT (migration 0016), which is
// the authority; this constant exists because the LIVE preview of an open month has
// no frozen row to read one from. The two are asserted equal against the live schema
// by TestBillingDB_DefaultCurrencyMatchesTheColumnDefault -- a hand-copied constant
// that nothing checks is the class of stale fact this repository keeps finding.
const DefaultCurrency = "EUR"

// Money is an exact amount, held in minor units (cents) rather than as a decimal.
//
// THE ZERO VALUE IS NOT A VALID AMOUNT. An amount with no currency is not an
// amount, so Valid reports whether this came from somewhere real. That distinction
// is the same one Report.Queried draws: "zero" and "we never asked" must not render
// identically.
type Money struct {
	minor    int64
	currency string
}

// NewMoney builds an amount from minor units. It is here for tests and for callers
// that already hold cents; the production path is MoneyFromNumeric.
func NewMoney(minor int64, currency string) Money {
	return Money{minor: minor, currency: currency}
}

// Valid reports whether this Money carries a real amount rather than the zero value.
func (m Money) Valid() bool { return m.currency != "" }

// Minor returns the amount in minor units. Exact by construction.
func (m Money) Minor() int64 { return m.minor }

// Currency returns the ISO code.
func (m Money) Currency() string { return m.currency }

// Add returns the sum of two amounts of the SAME currency.
//
// It refuses a mismatch rather than picking one, and refuses overflow rather than
// wrapping -- an invoice total that silently became negative is worse than an error
// somebody has to handle.
func (m Money) Add(o Money) (Money, error) {
	if !m.Valid() || !o.Valid() {
		return Money{}, fmt.Errorf("billing: add: an amount with no currency is not an amount")
	}
	if m.currency != o.currency {
		return Money{}, fmt.Errorf("billing: add: currency mismatch %s + %s", m.currency, o.currency)
	}
	sum := m.minor + o.minor
	// Overflow test that does not depend on the sign of either operand.
	if (m.minor > 0 && o.minor > 0 && sum < 0) || (m.minor < 0 && o.minor < 0 && sum > 0) {
		return Money{}, fmt.Errorf("billing: add: %d + %d overflows", m.minor, o.minor)
	}
	return Money{minor: sum, currency: m.currency}, nil
}

// String renders the amount with its full scale and NO currency symbol or grouping:
// "2019.00", "0.00", "-1.50".
//
// The symbol is the render layer's business (a template knows the reader's locale;
// this package does not), and grouping separators are a formatting choice that would
// have to be undone before the value could be parsed back. What this guarantees is
// that the digits are the exact ones Postgres sent.
func (m Money) String() string {
	if !m.Valid() {
		return ""
	}
	neg := m.minor < 0
	// Negate through int64 min safely by working in the unsigned domain.
	var abs uint64
	if neg {
		abs = uint64(-(m.minor + 1)) + 1
	} else {
		abs = uint64(m.minor)
	}
	var div uint64 = 1
	for i := 0; i < moneyScale; i++ {
		div *= 10
	}
	whole, frac := abs/div, abs%div
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	fmt.Fprintf(&b, "%d.%0*d", whole, moneyScale, frac)
	return b.String()
}

// MoneyFromNumeric converts a Postgres numeric into an exact Money.
//
// 🔴 THE WHOLE FUNCTION IS INTEGER ARITHMETIC. pgtype.Numeric carries a *big.Int
// mantissa and a decimal exponent -- measured on pgx v5.10.0: "1.50" arrives as
// Int=150, Exp=-2 -- so rescaling to cents is a multiplication or an exact division
// by a power of ten, never a conversion to binary floating point.
//
// IT REFUSES RATHER THAN ROUNDS, in all four ways a value can fail to be money:
//
//	NULL                a missing amount is not zero (§4.6's habit: absence is not a value)
//	NaN / +-Infinity    numeric can hold them; an invoice cannot
//	more than two decimals   a fraction of a cent has no rounding rule this product
//	                         has decided, so it is an error rather than a guess
//	beyond int64        ~92 million billion cents; refusing is honest, wrapping is not
//
// The third is not theoretical: it is what a numeric column with a wider scale, or
// an unrounded division, would produce, and silently truncating it is how a total
// stops matching its own line items.
func MoneyFromNumeric(n pgtype.Numeric, currency string) (Money, error) {
	if currency == "" {
		return Money{}, fmt.Errorf("billing: numeric to money: no currency given")
	}
	if !n.Valid {
		return Money{}, fmt.Errorf("billing: numeric to money: value is NULL")
	}
	if n.NaN {
		return Money{}, fmt.Errorf("billing: numeric to money: value is NaN")
	}
	if n.InfinityModifier != pgtype.Finite {
		return Money{}, fmt.Errorf("billing: numeric to money: value is infinite")
	}
	if n.Int == nil {
		return Money{}, fmt.Errorf("billing: numeric to money: value has no mantissa")
	}

	// The value is Int x 10^Exp; the target is Int x 10^(Exp+moneyScale) minor units.
	shift := int(n.Exp) + moneyScale
	minor := new(big.Int).Set(n.Int)
	switch {
	case shift > 0:
		minor.Mul(minor, pow10(shift))
	case shift < 0:
		// Exact division only: a non-zero remainder means the value carries more
		// precision than money has, and this function does not invent a rounding rule.
		var rem big.Int
		minor.QuoRem(minor, pow10(-shift), &rem)
		if rem.Sign() != 0 {
			return Money{}, fmt.Errorf(
				"billing: numeric to money: value has more than %d decimal places", moneyScale)
		}
	}
	if !minor.IsInt64() {
		return Money{}, fmt.Errorf("billing: numeric to money: value does not fit in int64 minor units")
	}
	return Money{minor: minor.Int64(), currency: currency}, nil
}

// pow10 returns 10^n as a big.Int. n is small and non-negative at every call site.
func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}
