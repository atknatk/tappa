package signup

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// TestDraft_PrintsNoPassword (backlog T209): an Account in a Draft holds the form's password for one
// request, and a %+v of either, or of a struct holding one, used to print it (the audit: 566 of 712
// renderings).
//
// MUTANT (measured, 2026-11-06, reverted): config.Key over a plain string -- red here.
func TestDraft_PrintsNoPassword(t *testing.T) {
	const phrase = "a-passphrase-nobody-guesses"
	inHex := hex.EncodeToString([]byte(phrase))
	a := Account{FullName: "Maria", Email: NewAddress("maria@example.mt"), Password: testPassword(phrase)}
	d := Draft{Account: a}
	type holder struct{ d Draft }
	for name, v := range map[string]any{"account": a, "draft": d, "pointer": &d, "held": holder{d}, "slice": []Draft{d}} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
			text := fmt.Sprintf(verb, v)
			if strings.Contains(text, phrase) || strings.Contains(text, inHex) {
				t.Errorf("%s under %s prints the password", name, verb)
			}
		}
		if b, err := json.Marshal(v); err == nil && strings.Contains(string(b), phrase) {
			t.Errorf("%s through encoding/json carries the password", name)
		}
	}
}

// TestDraft_PrintsNoAddress (backlog T209): the account step's address is on CLAUDE.md §7's never-log
// list, and an Account or a Draft printed whole carried it. The same matrix over the field's former
// shape (a plain string, legacyAccount below) is the CONTROL: it has to leak, or the search looks at
// nothing; the counts are logged (measured 2026-11-08: the legacy shape in 174 of 260 renderings, the
// Address in 0 of 260).
//
// MUTANTS (measured, 2026-11-08, reverted): Address over a plain string field instead of a *string --
// red ("held", the value in an unexported field, prints by reflection); Format writing the address --
// red.
func TestDraft_PrintsNoAddress(t *testing.T) {
	const address = "maria.canary@example.mt"
	type legacyAccount struct {
		FullName string
		Email    string
	}
	type holder struct{ d any }
	render := func(top any) (texts []string) {
		for _, v := range []any{top, &top, holder{top}, []any{top}, map[string]any{"k": top}} {
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%p"} {
				texts = append(texts, fmt.Sprintf(verb, v))
			}
			texts = append(texts, fmt.Errorf("%w", fmt.Errorf("x: %v", v)).Error())
			for _, h := range []func(*strings.Builder) slog.Handler{
				func(b *strings.Builder) slog.Handler { return slog.NewTextHandler(b, nil) },
				func(b *strings.Builder) slog.Handler { return slog.NewJSONHandler(b, nil) },
			} {
				var b strings.Builder
				slog.New(h(&b)).Info("probe", "v", v)
				texts = append(texts, b.String())
			}
			if j, err := json.Marshal(v); err == nil {
				texts = append(texts, string(j))
			}
		}
		return texts
	}
	count := func(values map[string]any) (leaks, total int, where []string) {
		for name, v := range values {
			for i, text := range render(v) {
				total++
				if strings.Contains(text, address) || strings.Contains(text, hex.EncodeToString([]byte(address))) ||
					strings.Contains(strings.ToLower(text), hex.EncodeToString([]byte(address))) {
					leaks++
					where = append(where, fmt.Sprintf("%s #%d", name, i))
				}
			}
		}
		return leaks, total, where
	}

	legacy := legacyAccount{FullName: "Maria", Email: address}
	ctlLeaks, ctlTotal, _ := count(map[string]any{"account": legacy, "draft": struct{ Account legacyAccount }{legacy},
		"pointer": &legacy, "email": legacy.Email})
	if ctlLeaks == 0 {
		t.Fatal("CONTROL: the legacy shape leaks in no rendering; the matrix looks at nothing")
	}

	a := Account{FullName: "Maria", Email: NewAddress(address), Password: testPassword("a-passphrase-nobody-guesses")}
	d := Draft{Account: a}
	leaks, total, where := count(map[string]any{"account": a, "draft": d, "pointer": &d, "email": a.Email})
	t.Logf("legacy shape: %d of %d renderings carry the address; Address: %d of %d", ctlLeaks, ctlTotal, leaks, total)
	if leaks != 0 {
		t.Errorf("the address is in %d renderings: %v", leaks, where)
	}
	if a.Email.Reveal() != address || NewAddress("").Reveal() != "" || (Address{}).Reveal() != "" {
		t.Error("Reveal does not hand back what NewAddress wrapped")
	}
}
