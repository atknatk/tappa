package mail

import (
	"bufio"
	"crypto/rand"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

// fuzzSender is a sender built through New, with a display name in From and a
// Reply-To, so every header compose can write is written.
func fuzzSender(t testing.TB) *SMTP {
	t.Helper()
	s, err := New(Config{Host: "smtp.example.test", Port: 587,
		Username: NewCredential("smtp-user"), Password: NewCredential(rand.Text()),
		From: "Taptime Ħ <no-reply@taptime.test>", ReplyTo: "support@taptime.test"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The oracle below restates the rules from the design text (ADR 0022), written
// independently of compose's helpers, so the fuzzer checks compose against the
// rule rather than against itself.
var refRule = regexp.MustCompile(`^[A-Za-z0-9._:-]{0,128}$`)

func oracleAccepts(to, subject, text, html, ref string) (ok bool, class Class) {
	toOK := len(to) >= 1 && len(to) <= 254 && !strings.Contains(to, "=?")
	for _, b := range []byte(to) {
		if b < '!' || b > '~' {
			toOK = false
		}
	}
	if toOK {
		a, err := mail.ParseAddress(to)
		toOK = err == nil && a.Address == to && a.Name == ""
	}
	if !toOK {
		return false, ClassInvalidAddress
	}
	subjOK := subject != "" && utf8.ValidString(subject) && !strings.Contains(subject, "=?") &&
		len("Subject: "+mime.QEncoding.Encode("utf-8", subject)) <= 998
	for _, r := range subject {
		if unicode.In(r, unicode.Cc, unicode.Zl, unicode.Zp) {
			subjOK = false
		}
	}
	if !subjOK || !refRule.MatchString(ref) || text == "" || html == "" ||
		!utf8.ValidString(text) || !utf8.ValidString(html) {
		return false, ClassInvalidMessage
	}
	return true, ""
}

func lf(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n") }

// FuzzCompose: on the inputs of a run (the seeds under go test, generated inputs
// under -fuzz) compose does not panic, and
//   - it refuses exactly what the oracle refuses, with the oracle's class, as a
//     *SendError with no reply code;
//   - what it accepts is a message whose every line is CRLF-terminated, at most
//     998 octets and printable 7-bit ASCII (plus TAB in the body, which
//     quoted-printable keeps literal inside a line); whose header block is exactly the
//     expected headers, one line each (no folded or injected line); whose To is
//     the input verbatim, whose Subject decodes to the input;
//     and whose two quoted-printable parts decode to Text and HTML.
func FuzzCompose(f *testing.F) {
	seeds := [][5]string{
		{"maria@example.test", "Activate your account", "Hello\n", "<p>Hi</p>", "invite:1"},
		{"maria@example.test", "Ħello ċ ġ ħ ż", "a\r\n.\r\nb", ".\n.", ""},
		{"a@example.test\r\nBcc: b@example.test", "x", "t", "h", ""},
		{"a@example.test", "x\r\nBcc: b@example.test", "t", "h", ""},
		{"a@example.test", "x\nBcc: b", "t", "h", ""},
		{"a@example.test", "x\rBcc: b", "t", "h", ""},
		{"a@example.test", "x\u2028y", "t", "h", ""},
		{"a@example.test", "x\u0085y", "t", "h", ""},
		{"a@example.test", "x", "t", "h", "r\r\nBcc: b"},
		{"Victim <a@example.test>", "x", "t", "h", ""},
		{"a@example.test,b@example.test", "x", "t", "h", ""},
		{"ħ@example.test", "x", "t", "h", ""},
		{"a@example.test", strings.Repeat("ħ", 200), "t", "h", ""},
		{"a@example.test", strings.Repeat("a", 989), "t", "h", ""},
		{"a@example.test", "=?utf-8?q?a=0D=0ABcc:_b?=", "t", "h", ""},
		{"=?utf-8?q?a=0D=0ABcc=3A_victim?=@example.test", "x", "t", "h", ""}, // the third eye's B4
		{"a@example.test", "x", "line\x00nul\x7f", "\xff", ""},
		{"a@example.test", "x", strings.Repeat("y", 2000), strings.Repeat("<b>", 700), ""},
		{"0@0", "0", "0", "\t0", "0"},                // found by -fuzz: QP keeps an inner TAB
		{"0@0", "Ė", "\r\x1d\n", "0", "0"},           // found by -fuzz: CR, encoded byte, LF
		{"0@0", "x", "a\r=\nb\r\xc3\xa9\n", "0", ""}, // the same quirk with '=' and a UTF-8 rune
	}
	for _, s := range seeds {
		f.Add(s[0], s[1], s[2], s[3], s[4])
	}
	s := fuzzSender(f)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, to, subject, text, html, ref string) {
		raw, err := s.compose(Message{To: to, Subject: subject, Text: text, HTML: html, Ref: ref}, now)
		want, class := oracleAccepts(to, subject, text, html, ref)
		if err != nil {
			var se *SendError
			if !errors.As(err, &se) || se.SMTPCode != 0 {
				t.Fatalf("refusal %v (%T) is not a codeless *SendError", err, err)
			}
			if want || se.Class != class {
				t.Fatalf("compose refused with %q; the oracle says accept=%v class=%q", se.Class, want, class)
			}
			return
		}
		if !want {
			t.Fatalf("compose accepted what the oracle refuses (%q)", class)
		}
		msg := string(raw)
		head, _, found := strings.Cut(msg, "\r\n\r\n")
		if !found {
			t.Fatal("no header/body separator")
		}
		for i, line := range strings.SplitAfter(msg, "\n") {
			if line == "" {
				continue
			}
			content := strings.TrimSuffix(line, "\r\n")
			if content == line || len(content) > 998 {
				t.Fatalf("line %d is not a CRLF line of at most 998 octets: %q", i, line)
			}
			inHead := i < strings.Count(head, "\r\n")+1
			for _, b := range []byte(content) {
				// Quoted-printable keeps an inner TAB literal (RFC 2045 §6.7 rule 3),
				// so TAB is legal in the body; a header line has none.
				if (b < ' ' || b > '~') && (b != '\t' || inHead) {
					t.Fatalf("line %d (header=%v) carries byte %#x", i, inHead, b)
				}
			}
		}
		wantHeaders := []string{"Date", "From", "Reply-To", "To", "Subject", "Message-ID", "MIME-Version", "Content-Type"}
		lines := strings.Split(head, "\r\n")
		if len(lines) != len(wantHeaders) {
			t.Fatalf("%d header lines, want %d: %q", len(lines), len(wantHeaders), head)
		}
		seen := map[string]string{}
		for _, l := range lines {
			name, value, ok := strings.Cut(l, ": ")
			if !ok || name == "" || strings.ContainsAny(name, " \t") {
				t.Fatalf("header line %q is not name: value", l)
			}
			if _, dup := seen[name]; dup {
				t.Fatalf("header %s twice", name)
			}
			seen[name] = value
		}
		for _, n := range wantHeaders {
			if _, ok := seen[n]; !ok {
				t.Fatalf("header %s missing; got %v", n, seen)
			}
		}
		if seen["To"] != to {
			t.Fatalf("To = %q, want the input verbatim", seen["To"])
		}
		if dec, err := new(mime.WordDecoder).DecodeHeader(seen["Subject"]); err != nil || dec != subject {
			t.Fatalf("Subject decodes to %q (%v), want %q", dec, err, subject)
		}
		m, err := mail.ReadMessage(strings.NewReader(msg))
		if err != nil {
			t.Fatal(err)
		}
		_, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		mr := multipart.NewReader(m.Body, params["boundary"])
		for i, want := range []string{text, html} {
			p, err := mr.NextRawPart()
			if err != nil {
				t.Fatalf("part %d: %v", i, err)
			}
			b, err := io.ReadAll(quotedprintable.NewReader(p))
			if err != nil {
				t.Fatalf("part %d: %v", i, err)
			}
			if lf(string(b)) != lf(want) {
				t.Fatalf("part %d decodes to %q, want %q", i, b, want)
			}
		}
		if _, err := mr.NextRawPart(); err != io.EOF {
			t.Fatalf("after two parts: %v", err)
		}
	})
}

// TestMessageID_EnhancedStatusIsOnlyStripped: the RFC 3463 prefix is stripped
// only when it has that exact shape, so a reply whose first field merely looks
// numeric keeps it as a field.
func TestMessageID_EnhancedStatusIsOnlyStripped(t *testing.T) {
	for _, tt := range []struct {
		f    string
		want bool
	}{
		{"2.0.0", true}, {"4.7.1", true}, {"5.1.10", true}, {"2.123.456", true},
		{"3.0.0", false}, {"2.0", false}, {"2.0.0.0", false}, {"2..0", false},
		{"2.0000.0", false}, {"2.a.0", false}, {"Ok", false},
	} {
		if got := enhancedStatus(tt.f); got != tt.want {
			t.Errorf("enhancedStatus(%q) = %v, want %v", tt.f, got, tt.want)
		}
	}
	// "2.0 x" keeps "2.0" as a field, so "x" is an id after one other field.
	if got := messageID("2.0 x9", nil); got != "x9" {
		t.Fatalf("messageID = %q", got)
	}
	if got := messageID("2.0.0 x9", nil); got != "" {
		t.Fatalf("messageID = %q", got)
	}
}

// TestMessageID_KnownLimitIsACutOrReorderedEcho measures two limits doc.go's
// KNOWN LIMITS names: a sent value echoed with a separator (".", "_" or "-")
// at least every 7 characters, order kept, and the same value reversed, both
// pass the window rule. The control shows an 8-character contiguous run of it is
// caught. When a limit stops holding, this test turns red so the text is updated.
func TestMessageID_KnownLimitIsACutOrReorderedEcho(t *testing.T) {
	value := (rand.Text() + rand.Text())[:43]
	sent := []string{"https://taptime.test/activate?code=" + value}
	var cut strings.Builder
	for i := 0; i < len(value); i += 7 {
		if i > 0 {
			cut.WriteByte('.')
		}
		cut.WriteString(value[i:min(i+7, len(value))])
	}
	reversed := []byte(value)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	for name, echo := range map[string]string{"cut every 7": cut.String(), "reversed": string(reversed)} {
		if got := messageID("250 Ok "+echo, sent); got != echo {
			t.Errorf("%s: messageID = %q; the limit no longer holds, update doc.go's KNOWN LIMITS", name, got)
		}
	}
	if got := messageID("250 Ok x"+value[:8], sent); got != "" {
		t.Fatalf("control: an 8-character contiguous run passed as %q", got)
	}
}

// TestMail_ImportsOnlyTheStandardLibrary: the package's non-test files import
// nothing outside the standard library — in particular no internal/ package and
// no module (the design: internal/mail depends on stdlib only). A first path
// element without a dot is the standard library's form. Control: the scan saw
// the imports it must (net/smtp, crypto/tls), so an empty scan cannot pass.
func TestMail_ImportsOnlyTheStandardLibrary(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		af, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			seen[p] = true
			if first, _, _ := strings.Cut(p, "/"); strings.Contains(first, ".") {
				t.Errorf("%s imports %s, which is not the standard library", name, p)
			}
		}
	}
	if files < 4 || !seen["net/smtp"] || !seen["crypto/tls"] {
		t.Fatalf("control: %d files scanned, imports %v", files, seen)
	}
}

// TestHeaderBlock_RefusesWhatItCannotWriteVerbatim: the second gate, alone. A
// value with CR, LF, TAB, NUL, DEL or a non-ASCII byte, or a line past 998
// octets, marks the block broken instead of being written, stripped or folded.
func TestHeaderBlock_RefusesWhatItCannotWriteVerbatim(t *testing.T) {
	for _, v := range []string{"a\r\nBcc: b", "a\nb", "a\rb", "a\tb", "a\x00", "a\x7f", "ħ", strings.Repeat("a", 990)} {
		var h headerBlock
		h.add("Subject", v)
		if !h.broken || h.buf.Len() != 0 {
			t.Errorf("value %q: broken=%v, wrote %q", v, h.broken, h.buf.String())
		}
	}
	var h headerBlock
	h.add("Subject", strings.Repeat("a", 989))
	if h.broken || !strings.HasSuffix(h.buf.String(), "a\r\n") {
		t.Fatal("control: a printable value at the limit was not written")
	}
	// Read back as a header block it is one header.
	r := bufio.NewReader(strings.NewReader(h.buf.String() + "\r\n"))
	m, err := mail.ReadMessage(r)
	if err != nil || len(m.Header) != 1 {
		t.Fatalf("read back: %v, %v", m, err)
	}
}
