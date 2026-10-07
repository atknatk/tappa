package mail_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/mail"
)

// TestValidRecipient_AgreesWithSend holds the exported rule (M10 EM-6) to Send's
// own answer: for every value below, ValidRecipient(v) is false exactly when Send
// refuses v as Message.To with invalid_address — and a refused send never dials.
//
// 🔴 THE ORACLE IS Send, NOT A COPY OF THE RULE. A caller that stores an address for
// a later send (the panel's change-the-address action) must refuse what Send would
// refuse; a second implementation of the rule that drifts would store an address the
// later send turns into invalid_address. Replacing ValidRecipient's body with
// anything that answers differently on a row below turns this red.
//
// The rows are TestSend_RefusesBadInputBeforeAnyDial's To rows and controls, plus the
// shapes EM-6's own review tried: look-alike letters (Cyrillic, full width), invisible
// runes, a doubled @, an empty side, the 254/255 byte edge. The look-alikes are
// spelled as UTF-8 byte escapes so the source carries no invisible or reordering rune.
func TestValidRecipient_AgreesWithSend(t *testing.T) {
	long := strings.Repeat("a", 254-len("@example.test")+1) + "@example.test" // 255 bytes
	values := []string{
		// TestSend_RefusesBadInputBeforeAnyDial's To rows.
		"a@example.test\r\nBcc: b@example.test",
		"a@example.test\nBcc: b@example.test",
		"a@example.test\rBcc: b@example.test",
		"a@example.test\x00",
		"a\x7f@example.test",
		"Victim <a@example.test>",
		"Victim<a@example.test>",
		"<a@example.test>",
		"a@example.test,b@example.test",
		`"a"@example.test`,
		`"Victim" <a@example.test>`,
		"a(x)@example.test",
		"a@example.test(x)",
		" a@example.test",
		"a@example.test ",
		"a b@example.test",
		"\xc4\xa7@example.test",    // U+0127 h with stroke
		"\xc4\x87ali@example.test", // U+0107 c with acute
		"ali@ex\xc3\xa4mple.test",  // U+00E4 a with diaeresis
		long,
		"=?utf-8?q?a=0D=0ABcc=3A_victim?=@example.test",
		"",
		// Its controls.
		long[1:],
		"ali+payroll@example.test",
		"ALI@EXAMPLE.TEST",
		"ali@[192.0.2.1]",
		// EM-6's review shapes.
		"\xd0\xb0li@example.test",            // U+0430 CYRILLIC SMALL LETTER A
		"\xef\xbd\x81li@example.test",        // U+FF41 FULLWIDTH LATIN SMALL LETTER A
		"ali@exa\xe2\x80\x8bmple.test",       // U+200B ZERO WIDTH SPACE
		"ali@example.test\xc2\xa0",           // U+00A0 NO-BREAK SPACE
		"\xe2\x80\xaeali@example.test",       // U+202E RIGHT-TO-LEFT OVERRIDE
		"ali@@example.test",                  // doubled @
		"@example.test",                      // empty local part
		"ali@",                               // empty domain
		"ali@example.test\t",                 // trailing tab
		"maria.borg-2_x@kebab.example.test",  // ordinary punctuation
		"Maria.Borg@Example.Test",            // mixed case
		"ali@example.test\r",                 // trailing CR alone
		"ali@example.test%0d%0aBcc:b@x.test", // percent-encoded CRLF
	}

	f := newFake(t, always(fakeConfig{}))
	s, _ := newSender(t, f, nil)
	accepted, refused := 0, 0
	for i, v := range values {
		m := validMessage()
		m.To = v
		before := f.accepted()
		_, _, err := sendWithin(context.Background(), t, 10*time.Second, s, m)
		var se *mail.SendError
		sendRefusedIt := errors.As(err, &se) && se.Class == mail.ClassInvalidAddress
		if err != nil && !sendRefusedIt {
			t.Fatalf("row %d (%q): Send failed with %v, which is neither success nor invalid_address — the fake is not answering", i, v, err)
		}
		if got := mail.ValidRecipient(v); got == sendRefusedIt {
			t.Errorf("row %d (%q): ValidRecipient = %v but Send refused-as-invalid_address = %v", i, v, got, sendRefusedIt)
		}
		if sendRefusedIt {
			refused++
			if f.accepted() != before {
				t.Errorf("row %d (%q): refused as invalid_address AFTER a dial", i, v)
			}
		} else {
			accepted++
		}
	}
	// ANTI-VACUITY: both answers occur, so neither a constant-true nor a constant-false
	// ValidRecipient can pass.
	if accepted == 0 || refused == 0 {
		t.Fatalf("accepted %d, refused %d — the table no longer exercises both answers", accepted, refused)
	}
	t.Logf("%d values: %d accepted by both, %d refused by both", len(values), accepted, refused)
}
