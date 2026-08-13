package billing

// money_test.go -- the exactness claims in money.go, each with the value that would
// go wrong if the arithmetic were floating point.

import (
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// numeric builds a pgtype.Numeric the way pgx does when it decodes a value, so the
// tests below exercise the same shape production sees. Measured on pgx v5.10.0:
// Scan("1.50") yields Int=150, Exp=-2.
func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("scan %q: %v", s, err)
	}
	return n
}

func TestMoneyFromNumeric_IsExact(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		minor int64
		out   string
	}{
		{"the list price", "1.50", 150, "1.50"},
		{"one month of a real payroll", "2019.00", 201900, "2019.00"},
		{"a free period", "0.00", 0, "0.00"},
		{"trailing zero omitted by the wire format", "1.5", 150, "1.50"},
		{"no decimal point at all", "7", 700, "7.00"},
		{"one cent", "0.01", 1, "0.01"},
		{"a negative amount is representable even though nothing issues one", "-1.50", -150, "-1.50"},
		{"a large but ordinary invoice", "123456.78", 12345678, "123456.78"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := MoneyFromNumeric(numeric(t, c.in), "EUR")
			if err != nil {
				t.Fatalf("MoneyFromNumeric(%q): %v", c.in, err)
			}
			if m.Minor() != c.minor {
				t.Errorf("minor units = %d, want %d", m.Minor(), c.minor)
			}
			if got := m.String(); got != c.out {
				t.Errorf("String() = %q, want %q", got, c.out)
			}
			if m.Currency() != "EUR" || !m.Valid() {
				t.Errorf("currency = %q valid = %v, want EUR / true", m.Currency(), m.Valid())
			}
		})
	}
}

// TestMoneyFromNumeric_RefusesWhatIsNotAnAmount -- all four refusals in money.go,
// because each one is a place a wrong invoice could otherwise be produced silently.
func TestMoneyFromNumeric_RefusesWhatIsNotAnAmount(t *testing.T) {
	huge := new(big.Int).Mul(big.NewInt(math.MaxInt64), big.NewInt(1000))

	cases := []struct {
		name string
		in   pgtype.Numeric
		cur  string
	}{
		{"NULL is not zero", pgtype.Numeric{Valid: false}, "EUR"},
		{"NaN", pgtype.Numeric{NaN: true, Valid: true}, "EUR"},
		{"infinity", pgtype.Numeric{InfinityModifier: pgtype.Infinity, Valid: true}, "EUR"},
		{"no mantissa", pgtype.Numeric{Valid: true}, "EUR"},
		{"a third of a cent has no rounding rule here", numeric(t, "1.005"), "EUR"},
		{"beyond int64 minor units", pgtype.Numeric{Int: huge, Exp: 0, Valid: true}, "EUR"},
		{"an amount with no currency is not an amount", numeric(t, "1.50"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if m, err := MoneyFromNumeric(c.in, c.cur); err == nil {
				t.Fatalf("accepted %v and produced %q; a refusal is the point", c.in, m.String())
			}
		})
	}
}

// TestMoney_TotalsAreExactWhereAFloatAccumulatorIsNot.
//
// 🔴 THE PRICES ARE CHOSEN, AND THE FIRST DRAFT OF THIS TEST CHOSE WRONG. It used
// only the list price, 1.50, and asserted that a float64 accumulator would disagree
// -- which SKIPPED, because 1.5 is 3/2 and therefore exact in binary. The skip is
// what made the mistake visible, in money.go's comment as well as here.
//
// So the test now runs BOTH prices and asserts the honest thing about each: Money is
// exact for both, float64 happens to be exact for 1.50 and is wrong for 1.10 in two
// different directions. The point is not that floating point always loses -- it is
// that whether it loses depends on the PRICE, and the price is a per-tenant column.
func TestMoney_TotalsAreExactWhereAFloatAccumulatorIsNot(t *testing.T) {
	const n = 1346

	cases := []struct {
		name string
		// price is the per-employee price, exactly as Postgres would send it.
		price string
		// wantMinor is n x price in cents, computed by hand.
		wantMinor int64
		wantText  string
		// floatIsExact records the measured behaviour of `sum += price` n times.
		floatIsExact bool
	}{
		{
			name: "the list price, which float64 gets RIGHT",
			// 1.5 = 3/2 is exactly representable, so this row is a control: it shows
			// the test is not rigged to make floating point look bad.
			price: "1.50", wantMinor: 201900, wantText: "2019.00", floatIsExact: true,
		},
		{
			name: "one plausible price change away, which float64 gets WRONG",
			// 1.10 is not representable. Measured: the sum is 1480.5999999999772 and
			// the product is 1480.6000000000001 -- two routes, two answers, and the
			// right one is neither.
			price: "1.10", wantMinor: 148060, wantText: "1480.60", floatIsExact: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			unit, err := MoneyFromNumeric(numeric(t, c.price), "EUR")
			if err != nil {
				t.Fatalf("unit: %v", err)
			}
			total := NewMoney(0, "EUR")
			for i := 0; i < n; i++ {
				if total, err = total.Add(unit); err != nil {
					t.Fatalf("add at %d: %v", i, err)
				}
			}
			if total.Minor() != c.wantMinor || total.String() != c.wantText {
				t.Fatalf("%s added %d times = %s (%d cents), want %s (%d)",
					c.price, n, total.String(), total.Minor(), c.wantText, c.wantMinor)
			}

			// The float64 route, measured rather than assumed, so this test tells the
			// truth about which prices are safe by accident.
			var sum float64
			var one float64
			if _, err := fmt.Sscan(c.price, &one); err != nil {
				t.Fatalf("parse %s as float: %v", c.price, err)
			}
			for i := 0; i < n; i++ {
				sum += one
			}
			exactWant := float64(c.wantMinor) / 100
			if got := sum == exactWant; got != c.floatIsExact {
				t.Fatalf("float64 accumulation of %s x %d = %.17g; exact = %v, but this case "+
					"records exact = %v. The measurement moved and the comments that cite it "+
					"are now wrong.", c.price, n, sum, got, c.floatIsExact)
			}
		})
	}
}

func TestMoney_AddRefusesMismatchAndOverflow(t *testing.T) {
	eur := NewMoney(100, "EUR")
	try := NewMoney(100, "TRY")
	if _, err := eur.Add(try); err == nil {
		t.Error("adding EUR to TRY was accepted; picking one silently is how a total lies")
	}
	if _, err := eur.Add(Money{}); err == nil {
		t.Error("adding the zero Money was accepted; it carries no currency")
	}
	if _, err := NewMoney(math.MaxInt64, "EUR").Add(NewMoney(1, "EUR")); err == nil {
		t.Error("overflow was accepted; wrapping turns a huge invoice into a negative one")
	}
	if _, err := NewMoney(math.MinInt64, "EUR").Add(NewMoney(-1, "EUR")); err == nil {
		t.Error("negative overflow was accepted")
	}
}

// TestMoney_ZeroValueRendersAsNothing -- "we have no amount" and "the amount is
// zero" must not render identically, the same distinction Report.Queried draws.
func TestMoney_ZeroValueRendersAsNothing(t *testing.T) {
	var m Money
	if m.Valid() {
		t.Error("the zero Money claims to be valid")
	}
	if m.String() != "" {
		t.Errorf("the zero Money renders as %q; a real 0.00 renders as 0.00 and the two "+
			"must be distinguishable", m.String())
	}
	real0 := NewMoney(0, "EUR")
	if !real0.Valid() || real0.String() != "0.00" {
		t.Errorf("a real zero renders as %q valid=%v, want 0.00 / true", real0.String(), real0.Valid())
	}
}
