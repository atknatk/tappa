package signup

import (
	"regexp"
	"strings"
)

// VAT NUMBER HANDLING — the "real business filter" (docs/handoff.md §11).
//
// 🔴 THE VAT NUMBER IS NOT A FORMALITY ON THIS FORM, IT IS THE PRODUCT'S ONLY
// FILTER. handoff §11 gives it two jobs: it keeps casual sign-ups out of a system
// that records legally significant attendance, and it pre-fills an invoice. Both
// need the value to be a VAT number rather than a string somebody typed, and
// migration 00001 makes it NOT NULL and globally UNIQUE precisely so one business
// is one tenant.
//
// TWO CHECKS, AND THEY ANSWER DIFFERENT QUESTIONS. Q09 (orchestrator decision,
// 2026-08-13) settled which is which:
//
//	FORMAT  mandatory, server-side, and decided HERE. It answers "could this
//	        possibly be a VAT number in that country" and it is a pure function —
//	        no network, no timeout, no outage.
//	VIES    best effort, decided in vies.go. It answers "does the European
//	        Commission's register know this number today", and it may fail. A
//	        failure NEVER stops a registration (the M7-02 card's own criterion):
//	        a VIES outage is the EU's fault, and turning it into a gate would lose
//	        a paying customer to a service we do not run.
//
// ⚠️ WHAT THE FORMAT CHECK IS NOT. It is not a checksum and it is not a claim that
// the number belongs to the person typing it. Several member states publish check
// digit algorithms and several do not; implementing a subset would make the filter
// stricter in some countries than others for no stated reason, and a wrong
// implementation would refuse a genuine customer. The strictness that matters comes
// from VIES, which is authoritative, and from the fact that an invoice with a wrong
// VAT number is a problem the customer discovers immediately.

// MaxVATRunes bounds the field before any pattern is applied.
//
// The longest EU VAT number is 12 characters after the country prefix (Lithuania's
// legal-entity form), so 16 is generous. The bound exists so an unauthenticated
// caller cannot hand a megabyte to a regular expression: every pattern below is
// linear, but a bound that does not depend on that is cheaper to be sure of.
const MaxVATRunes = 16

// NormaliseVAT puts a typed VAT number into the ONE form this product stores.
//
// 🔴 THERE IS EXACTLY ONE REPRESENTATION AND THIS FUNCTION IS IT, which is the
// M5-01/02/03 lesson stated in CLAUDE.md's own history: "a check and its consumer
// must see the same representation; the fix is to delete the second representation,
// not to teach it twice". The stored value, the value the format check reads, the
// value sent to VIES and the value the UNIQUE constraint compares are all this
// function's output.
//
// WHAT IT REMOVES: surrounding and interior whitespace, dots and hyphens — the
// three things a person types when reading a number off a letterhead. WHAT IT DOES
// NOT REMOVE: anything else, so a value carrying other punctuation fails the format
// check rather than being quietly repaired into a different number.
//
// CASE IS FOLDED UP because the country prefix is defined upper-case and `mt12345678`
// and `MT12345678` are the same number. Without this, two tenants could hold what
// tax authorities consider one VAT number and the global UNIQUE constraint —
// which is byte-wise — would not notice.
func NormaliseVAT(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		// '\u00a0' is the NON-BREAKING SPACE a browser produces when a value is
		// pasted out of a PDF letterhead. It is written as an escape rather than
		// typed, because a literal one in source is invisible to review.
		case ' ', '\t', '\n', '\r', '.', '-', '\u00a0':
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

// vatPatterns is the per-country shape of a VAT number, keyed by the two-letter
// prefix the number itself carries.
//
// THE TABLE IS THE EU MEMBER STATES PLUS THE TWO PREFIXES THAT ARE NOT COUNTRY
// CODES (EL for Greece, whose ISO code is GR; XI for Northern Ireland, which stayed
// inside the EU VAT area after Brexit). Both are the prefixes VIES itself expects,
// which is the only definition that matters — a number this table accepts and VIES
// cannot address would be a number we could never check.
//
// MALTA IS FIRST IN THIS COMMENT BECAUSE IT IS THE MARKET (handoff §3): MT plus
// exactly eight digits. Every design-partner tenant is Maltese, so the seed
// fixture's numbers are the shape this entry describes.
//
// ⚠️ THE PATTERNS ARE ANCHORED AT BOTH ENDS. An unanchored pattern would accept a
// VAT number with anything appended, which is precisely how a "validated" field
// ends up holding a sentence.
var vatPatterns = map[string]*regexp.Regexp{
	"MT": regexp.MustCompile(`^[0-9]{8}$`),
	"AT": regexp.MustCompile(`^U[0-9]{8}$`),
	"BE": regexp.MustCompile(`^[01][0-9]{9}$`),
	"BG": regexp.MustCompile(`^[0-9]{9,10}$`),
	"CY": regexp.MustCompile(`^[0-9]{8}[A-Z]$`),
	"CZ": regexp.MustCompile(`^[0-9]{8,10}$`),
	"DE": regexp.MustCompile(`^[0-9]{9}$`),
	"DK": regexp.MustCompile(`^[0-9]{8}$`),
	"EE": regexp.MustCompile(`^[0-9]{9}$`),
	"EL": regexp.MustCompile(`^[0-9]{9}$`),
	"ES": regexp.MustCompile(`^[A-Z0-9][0-9]{7}[A-Z0-9]$`),
	"FI": regexp.MustCompile(`^[0-9]{8}$`),
	"FR": regexp.MustCompile(`^[A-Z0-9]{2}[0-9]{9}$`),
	"HR": regexp.MustCompile(`^[0-9]{11}$`),
	"HU": regexp.MustCompile(`^[0-9]{8}$`),
	"IE": regexp.MustCompile(`^([0-9]{7}[A-Z]{1,2}|[0-9][A-Z0-9+*][0-9]{5}[A-Z])$`),
	"IT": regexp.MustCompile(`^[0-9]{11}$`),
	"LT": regexp.MustCompile(`^([0-9]{9}|[0-9]{12})$`),
	"LU": regexp.MustCompile(`^[0-9]{8}$`),
	"LV": regexp.MustCompile(`^[0-9]{11}$`),
	"NL": regexp.MustCompile(`^[0-9]{9}B[0-9]{2}$`),
	"PL": regexp.MustCompile(`^[0-9]{10}$`),
	"PT": regexp.MustCompile(`^[0-9]{9}$`),
	"RO": regexp.MustCompile(`^[0-9]{2,10}$`),
	"SE": regexp.MustCompile(`^[0-9]{12}$`),
	"SI": regexp.MustCompile(`^[0-9]{8}$`),
	"SK": regexp.MustCompile(`^[0-9]{10}$`),
	"XI": regexp.MustCompile(`^([0-9]{9}|[0-9]{12}|(GD|HA)[0-9]{3})$`),
}

// SplitVAT separates a NORMALISED number into its country prefix and the rest.
// The second result is false when the value does not begin with a prefix this
// product knows how to address.
func SplitVAT(normalised string) (country, number string, ok bool) {
	if len(normalised) < 3 {
		return "", "", false
	}
	country = normalised[:2]
	if _, known := vatPatterns[country]; !known {
		return "", "", false
	}
	return country, normalised[2:], true
}

// ValidVATFormat reports whether a NORMALISED VAT number could be a VAT number.
//
// IT TAKES THE NORMALISED FORM AND DOES NOT NORMALISE ITSELF, deliberately: a
// function that quietly repaired its input would let a caller store one value and
// check another, which is the whole failure this file's normalisation note is
// about. The boundary normalises once, and everything downstream sees that.
func ValidVATFormat(normalised string) bool {
	if normalised == "" || len([]rune(normalised)) > MaxVATRunes {
		return false
	}
	country, number, ok := SplitVAT(normalised)
	if !ok {
		return false
	}
	return vatPatterns[country].MatchString(number)
}

// KnownVATCountries reports how many prefixes the table carries. It exists so a
// test can assert the table is populated rather than silently empty — an empty map
// would make ValidVATFormat refuse everything, which no test asserting "this is
// invalid" would ever notice.
func KnownVATCountries() int { return len(vatPatterns) }
