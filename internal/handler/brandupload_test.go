package handler

// brandupload_test.go -- POST /admin/account/brand/logo (M10 WL-7; ADR 0024 §2, §6),
// against the doubles of brandfake_test.go and the REAL decode gate (brand.LogoGate)
// wrapped in a counter. The upload route sets a read deadline on its connection, so
// every test here that expects a body to be read goes through a real HTTP server; the
// ones that need to hold a body half-sent speak HTTP over a raw TCP connection.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"image/color"
	"io"
	"io/fs"
	"log/slog"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
)

// --- raw HTTP, for the bodies a client holds back ------------------------------------

// rawUpload opens a TCP connection to s and writes a POST to the logo route: the
// signed-in, same-origin headers, the given Content-Type and Content-Length, and then
// only the first part of the body -- the rest is the test's to send, or not. It
// returns the connection; the test closes it.
func rawUpload(t *testing.T, s *httptest.Server, contentType string, contentLength int, first []byte) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", s.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	head := "POST " + brandLogoHref + " HTTP/1.1\r\n" +
		"Host: " + s.Listener.Addr().String() + "\r\n" +
		"Origin: " + testBaseURL + "\r\n" +
		"Cookie: " + adminauth.CookieName + "=" + panelCookie().Value + "\r\n" +
		"Content-Type: " + contentType + "\r\n" +
		"Content-Length: " + strconv.Itoa(contentLength) + "\r\n\r\n"
	if _, err := conn.Write(append([]byte(head), first...)); err != nil {
		t.Fatalf("writing the request: %v", err)
	}
	return conn
}

// rawAnswer reads one response from conn, waiting at most d for it.
func rawAnswer(t *testing.T, conn net.Conn, d time.Duration) (*http.Response, error) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(d)); err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return nil, err
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res, nil
}

// inFlight is how many uploads are past admission now.
func inFlight(p *brandPanel) int {
	p.h.uploads.mu.Lock()
	defer p.h.uploads.mu.Unlock()
	return len(p.h.uploads.inFlight)
}

// waitUntil polls cond for up to d.
func waitUntil(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited %v for %s", d, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// tenantSwitch makes p's sessions resolve to whichever business the test sets, each
// with a session id of its own (so the per-session budget is not shared).
func tenantSwitch(p *brandPanel, role string) func(uuid.UUID) {
	var mu sync.Mutex
	current := panelTestTenant
	p.admins.verify = func() (adminauth.Resolved, error) {
		mu.Lock()
		id := current
		mu.Unlock()
		return adminauth.Resolved{SessionID: uuid.NewSHA1(uuid.Nil, id[:]), TenantID: id,
			AdminUserID: panelTestAdmin, Role: role, FullName: "Maria Borg"}, nil
	}
	return func(id uuid.UUID) {
		mu.Lock()
		current = id
		mu.Unlock()
	}
}

// --- the happy path ------------------------------------------------------------------

// TestBrandUpload_AnOwnersLogoIsNormalizedAndHandedOnUnchanged: an owner's PNG goes
// through the real decode gate once and reaches the writer as Normalize's own value
// (Normalized() true -- the domain refuses anything else), for the session's business
// and the session's admin; the answer is 303 to the editor with logo-saved.
func TestBrandUpload_AnOwnersLogoIsNormalizedAndHandedOnUnchanged(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	s := p.server(t)
	body, ct := oneLogo(t, inkLogo(t))
	res, _ := upload(t, s, body, ct)
	if got := outcomeOf(res); got != "logo-saved" {
		t.Fatalf("answer %d %q, want 303 logo-saved", res.StatusCode, res.Header.Get("Location"))
	}
	logos := p.writer.savedLogos()
	if len(logos) != 1 || p.gate.count() != 1 {
		t.Fatalf("%d saves, %d decodes; want 1 and 1", len(logos), p.gate.count())
	}
	c := logos[0]
	if !c.Logo.Normalized() || c.Logo.MIME != brand.LogoMIMEPNG || c.Logo.Width != 64 || c.Logo.Height != 32 {
		t.Errorf("the writer was handed %s %dx%d, Normalized %v", c.Logo.MIME, c.Logo.Width, c.Logo.Height, c.Logo.Normalized())
	}
	if c.TenantID != panelTestTenant || c.ActorID != panelTestAdmin {
		t.Errorf("the save names business %s and actor %s, not the session's", c.TenantID, c.ActorID)
	}
	if inFlight(p) != 0 {
		t.Errorf("%d uploads still admitted after the answer", inFlight(p))
	}
}

// TestBrandUpload_SucceedsWithTMPDIRMissingOrReadOnly: a JPEG of more than 64 KiB --
// larger than the threshold at which a form parser would spill a part to a temporary
// file -- uploads with TMPDIR pointing at a directory that does not exist, and at one
// that cannot be written (ADR 0024 §6; the pod's root is read-only).
func TestBrandUpload_SucceedsWithTMPDIRMissingOrReadOnly(t *testing.T) {
	data := noisyJPEG(t, 700, 700, 92, 7)
	if len(data) <= 64<<10 || len(data) > brand.LogoMaxInputBytes {
		t.Fatalf("PREMISE: the JPEG is %d bytes, want over 64 KiB and at most 512 KiB", len(data))
	}
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	for name, dir := range map[string]string{
		"missing":   filepath.Join(t.TempDir(), "no", "such", "dir"),
		"read-only": readOnly,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TMPDIR", dir)
			if _, err := os.CreateTemp("", "probe"); err == nil {
				t.Fatalf("PREMISE: a temporary file can be created under TMPDIR=%s", dir)
			}
			p := newBrandPanel(t, "owner", panelTestTenant)
			body, ct := oneLogo(t, data)
			res, _ := upload(t, p.server(t), body, ct)
			if got := outcomeOf(res); got != "logo-saved" {
				t.Fatalf("answer %d %q, want 303 logo-saved", res.StatusCode, res.Header.Get("Location"))
			}
			if n := len(p.writer.savedLogos()); n != 1 {
				t.Errorf("%d saves, want 1", n)
			}
		})
	}
}

// TestBrandUpload_ALightLogoIsSavedWithAWarning: a logo almost the colour of porcelain
// is saved and answered logo-saved-light (a warning, not a refusal); a dark one is
// answered logo-saved. The section draws each word's notice.
func TestBrandUpload_ALightLogoIsSavedWithAWarning(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	s := p.server(t)
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{{"pale", paleLogo(t), "logo-saved-light"}, {"ink", inkLogo(t), "logo-saved"}} {
		body, ct := oneLogo(t, tc.data)
		res, _ := upload(t, s, body, ct)
		if got := outcomeOf(res); got != tc.want {
			t.Errorf("%s: answer %q, want %s", tc.name, res.Header.Get("Location"), tc.want)
		}
	}
	if n := len(p.writer.savedLogos()); n != 2 {
		t.Errorf("%d saves, want 2 -- the light logo is saved too", n)
	}
	page := htmlOf(t, p.browser(t).do(http.MethodGet, brandReturnPath("logo-saved-light"), nil))
	if !strings.Contains(page, htmlText(brandOutcomes["logo-saved-light"].Sentence)) {
		t.Error("the editor does not draw the light-logo warning")
	}
}

// brandReturnPath is brandReturn without the fragment, as a browser requests it.
func brandReturnPath(word string) string {
	return strings.TrimSuffix(brandReturn(word), "#brand")
}

// --- the stream: one part, named logo ------------------------------------------------

// rawMultipart is a multipart body written by hand, for the shapes the stdlib writer
// will not produce.
func rawMultipart(boundary string, parts ...string) []byte {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString("--" + boundary + "\r\n" + p + "\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// TestBrandUpload_AcceptsOnePartNamedLogoAndNothingElse drives the body shapes ADR 0024
// §2.2 refuses, each through the real server: the outcome word, no save, and -- except
// for the one that reaches the format gate -- no decode. A preamble and an epilogue
// around one logo part are accepted.
func TestBrandUpload_AcceptsOnePartNamedLogoAndNothingElse(t *testing.T) {
	logo := inkLogo(t)
	const b = "XyZb0undary"
	formCT := "multipart/form-data; boundary=" + b
	part := func(disposition string, data []byte) string {
		return "Content-Disposition: " + disposition + "\r\nContent-Type: image/png\r\n\r\n" + string(data)
	}
	good := part(`form-data; name="logo"; filename="logo.png"`, logo)
	// The whole logo, quoted-printable: a decoding reader would hand the decoder a
	// valid PNG; the raw part is text, and the format gate refuses it. Binary mode, so
	// the PNG signature's CR and LF are encoded (=0D, =0A) rather than written as line
	// breaks a decoder would hand back as CRLF; the control below decodes it back to the
	// logo byte for byte (the first version used text mode, did not round-trip, and the
	// NextPart mutation stayed green against it).
	var qpBody strings.Builder
	qw := quotedprintable.NewWriter(&qpBody)
	qw.Binary = true
	if _, err := qw.Write(logo); err != nil || qw.Close() != nil {
		t.Fatal("encoding the quoted-printable part")
	}
	if back, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(qpBody.String()))); err != nil || !bytes.Equal(back, logo) {
		t.Fatal("PREMISE: the quoted-printable part does not decode back to the logo")
	}
	qp := "Content-Disposition: form-data; name=\"logo\"; filename=\"logo.png\"\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\n" + qpBody.String()
	for _, tc := range []struct {
		name, ct string
		body     []byte
		want     string
		decodes  int
	}{
		{"two logo parts", formCT, rawMultipart(b, good, good), "logo-parts", 0},
		{"a logo and a field", formCT, rawMultipart(b, good, "Content-Disposition: form-data; name=\"note\"\r\n\r\nhi"), "logo-parts", 0},
		{"a field before the logo", formCT, rawMultipart(b, "Content-Disposition: form-data; name=\"note\"\r\n\r\nhi", good), "logo-parts", 0},
		{"a name with a path", formCT, rawMultipart(b, part(`form-data; name="../logo"; filename="logo.png"`, logo)), "logo-parts", 0},
		{"a name with a slash after it", formCT, rawMultipart(b, part(`form-data; name="logo/x"`, logo)), "logo-parts", 0},
		{"the name in capitals", formCT, rawMultipart(b, part(`form-data; name="LOGO"`, logo)), "logo-parts", 0},
		{"an attachment, not form-data", formCT, rawMultipart(b, part(`attachment; name="logo"`, logo)), "logo-parts", 0},
		{"no part at all", formCT, []byte("--" + b + "--\r\n"), "unreadable", 0},
		{"an empty logo part", formCT, rawMultipart(b, part(`form-data; name="logo"; filename=""`, nil)), "logo-missing", 0},
		{"a urlencoded form", "application/x-www-form-urlencoded", []byte("logo=" + string(logo[:8])), "unreadable", 0},
		{"multipart/mixed", "multipart/mixed; boundary=" + b, rawMultipart(b, good), "unreadable", 0},
		{"no boundary", "multipart/form-data", rawMultipart(b, good), "unreadable", 0},
		{"another boundary in the body", formCT, rawMultipart("SomethingElse", good), "unreadable", 0},
		{"a body cut before its end", formCT, rawMultipart(b, good)[:200], "unreadable", 0},
		{"the part sent quoted-printable", formCT, rawMultipart(b, qp), "logo-format", 1},
		{"a preamble and an epilogue around one logo", formCT,
			append(append([]byte("preamble the reader skips\r\n"), rawMultipart(b, good)...), []byte("epilogue nobody reads\r\n")...), "logo-saved", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newBrandPanel(t, "owner", panelTestTenant)
			res, _ := upload(t, p.server(t), tc.body, tc.ct)
			if got := outcomeOf(res); got != tc.want {
				t.Fatalf("answer %d %q, want %s", res.StatusCode, res.Header.Get("Location"), tc.want)
			}
			if got := p.gate.count(); got != tc.decodes {
				t.Errorf("the decode gate was asked %d times, want %d", got, tc.decodes)
			}
			wantSaves := map[bool]int{true: 1, false: 0}[tc.want == "logo-saved"]
			if n := len(p.writer.savedLogos()); n != wantSaves {
				t.Errorf("%d saves, want %d", n, wantSaves)
			}
		})
	}
}

// --- the bounds ----------------------------------------------------------------------

// paddedPNG is a valid PNG followed by zero bytes, exactly n bytes long: the decoder
// stops at IEND and the re-encode drops the rest (WL-3), so it is a logo of any size.
func paddedPNG(t *testing.T, n int) []byte {
	t.Helper()
	p := inkLogo(t)
	if len(p) > n {
		t.Fatalf("PREMISE: the PNG is %d bytes, more than %d", len(p), n)
	}
	return append(p, make([]byte, n-len(p))...)
}

// TestBrandUpload_ExactlyTheLimitIsReadAndOneMoreIsRefused: a part of exactly
// brand.LogoMaxInputBytes (512 KiB) is read in full and handed to the decoder; one byte
// more is refused logo-too-large before the decoder is asked.
func TestBrandUpload_ExactlyTheLimitIsReadAndOneMoreIsRefused(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	s := p.server(t)
	body, ct := oneLogo(t, paddedPNG(t, brand.LogoMaxInputBytes))
	res, _ := upload(t, s, body, ct)
	if got := outcomeOf(res); got != "logo-saved" {
		t.Fatalf("exactly 512 KiB: answer %q, want logo-saved", res.Header.Get("Location"))
	}
	p.gate.mu.Lock()
	seen := append([]int(nil), p.gate.seen...)
	p.gate.mu.Unlock()
	if len(seen) != 1 || seen[0] != brand.LogoMaxInputBytes {
		t.Fatalf("the decoder was handed %v bytes, want [%d]", seen, brand.LogoMaxInputBytes)
	}
	body, ct = oneLogo(t, paddedPNG(t, brand.LogoMaxInputBytes+1))
	res, _ = upload(t, s, body, ct)
	if got := outcomeOf(res); got != "logo-too-large" {
		t.Fatalf("512 KiB + 1: answer %q, want logo-too-large", res.Header.Get("Location"))
	}
	if p.gate.count() != 1 {
		t.Errorf("the decoder was asked about the oversized part (%d calls)", p.gate.count())
	}
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// TestBrandUpload_TheLimitIsAppliedAsThePartIsRead counts what readLogoPart reads of the
// body. Handed one logo part of 1 000 KiB, and one of the limit plus one byte, it answers
// logo-too-large having read at most brand.LogoMaxInputBytes + 16 KiB of the body (the
// part's limit and one byte, the part's headers, the multipart reader's buffer); a part
// of exactly the limit is returned whole, read to the body's end within the same bound.
// A reader that took the whole part first and judged its size after (io.ReadAll) gives
// the same word -- only the count tells them apart: it reads the 1 MiB body to its end.
// WL-3 holds brand.logoRead, the same shape for the same reason, against the same
// change (its M35).
func TestBrandUpload_TheLimitIsAppliedAsThePartIsRead(t *testing.T) {
	const bound = brand.LogoMaxInputBytes + 16<<10
	for _, tc := range []struct {
		size int
		word string
	}{
		{1000 << 10, "logo-too-large"},
		{brand.LogoMaxInputBytes + 1, "logo-too-large"},
		{brand.LogoMaxInputBytes, ""},
	} {
		body, ct := oneLogo(t, bytes.Repeat([]byte{0x89}, tc.size))
		if tc.size == 1000<<10 && len(body) <= bound {
			t.Fatalf("PREMISE: the %d-byte body is within the bound %d; reading it whole would not be seen", len(body), bound)
		}
		cr := &countingReader{r: bytes.NewReader(body)}
		req := httptest.NewRequest(http.MethodPost, brandLogoHref, cr)
		req.Header.Set("Content-Type", ct)
		data, word := readLogoPart(req)
		if word != tc.word || (word == "" && len(data) != tc.size) {
			t.Errorf("a %d-byte part: %d bytes and %q, want %q", tc.size, len(data), word, tc.word)
		}
		if cr.n > bound {
			t.Errorf("a %d-byte part: %d bytes of the %d-byte body were read, want at most %d -- the limit is applied as the part is read",
				tc.size, cr.n, len(body), bound)
		}
		t.Logf("a %d-byte part: %d of the %d-byte body read, %q", tc.size, cr.n, len(body), word)
	}
}

// TestBrandUpload_ABodyOverTheCapIsRefused: a Content-Length over 1 MiB is refused
// before admission, unread; a chunked body (no length) whose preamble passes 1 MiB is
// cut by the body cap. Neither reaches the decoder.
func TestBrandUpload_ABodyOverTheCapIsRefused(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	s := p.server(t)
	// The declared length, with NO body sent: the answer arrives anyway.
	conn := rawUpload(t, s, "multipart/form-data; boundary=x", brandUploadMaxBody+1, nil)
	res, err := rawAnswer(t, conn, 5*time.Second)
	conn.Close()
	if err != nil {
		t.Fatalf("a declared length over the cap got no answer without its body: %v", err)
	}
	if got := outcomeOf(res); got != "logo-too-large" {
		t.Fatalf("declared over the cap: %d %q, want logo-too-large", res.StatusCode, res.Header.Get("Location"))
	}
	// Chunked: the length is unknown until it is read. The preamble is short lines, so
	// the multipart reader keeps reading them (one line longer than its buffer is a
	// different refusal, unreadable) until the body cap stops the read.
	body, ct := oneLogo(t, inkLogo(t))
	big := append(bytes.Repeat([]byte("a preamble line\r\n"), brandUploadMaxBody/16+10), body...)
	req, _ := http.NewRequest(http.MethodPost, s.URL+brandLogoHref, io.MultiReader(bytes.NewReader(big)))
	req.ContentLength = -1
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Origin", testBaseURL)
	req.AddCookie(panelCookie())
	res2, err := noRedirects().Do(req)
	if err != nil {
		t.Fatalf("chunked upload: %v", err)
	}
	res2.Body.Close()
	if got := outcomeOf(res2); got != "logo-too-large" {
		t.Fatalf("chunked over the cap: %d %q, want logo-too-large", res2.StatusCode, res2.Header.Get("Location"))
	}
	if p.gate.count() != 0 || p.writer.writes() != 0 {
		t.Errorf("%d decodes, %d writes; want 0 and 0", p.gate.count(), p.writer.writes())
	}
}

// TestBrandUpload_TheBusinessBudgetRefusesPastTenBeforeReadingTheBody: ten attempts
// from one business are each read and decided (here: a part that is not an image,
// logo-format); the eleventh is answered 429 WITHOUT ITS BODY BEING SENT, and the trail
// gets one tenant.brand_update_refused row, on that crossing request only; the twelfth
// is 429 with no second row. Another business's attempt in the same window is read.
func TestBrandUpload_TheBusinessBudgetRefusesPastTenBeforeReadingTheBody(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	s := p.server(t)
	setTenant := tenantSwitch(p, "owner")
	notAnImage, ct := oneLogo(t, []byte("this is not an image, it is a sentence"))
	for i := 1; i <= brandUploadLimit; i++ {
		res, _ := upload(t, s, notAnImage, ct)
		if got := outcomeOf(res); got != "logo-format" {
			t.Fatalf("attempt %d: %d %q, want logo-format", i, res.StatusCode, res.Header.Get("Location"))
		}
	}
	for i := 1; i <= 2; i++ {
		conn := rawUpload(t, s, ct, len(notAnImage), nil)
		res, err := rawAnswer(t, conn, 5*time.Second)
		conn.Close()
		if err != nil {
			t.Fatalf("attempt %d past the budget got no answer without its body: %v", brandUploadLimit+i, err)
		}
		if res.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("attempt %d past the budget: %d, want 429", brandUploadLimit+i, res.StatusCode)
		}
	}
	if n := p.trail.count(ActionBrandUpdateRefused); n != 1 {
		t.Errorf("%d refusal rows for two refused attempts, want 1 (on the crossing)", n)
	}
	ev := p.trail.eventsSnapshot()
	if len(ev) == 0 || ev[len(ev)-1].Detail.(refusedBrandDetail).Reason != brandRefusedBudget ||
		ev[len(ev)-1].Detail.(refusedBrandDetail).Field != brandFieldLogo {
		t.Errorf("the refusal row is %+v, want the budget reason for the logo", ev)
	}
	if p.gate.count() != brandUploadLimit || p.writer.writes() != 0 {
		t.Errorf("%d decodes, %d writes; want %d and 0", p.gate.count(), p.writer.writes(), brandUploadLimit)
	}
	other := uuid.MustParse("0b0b0b0b-0000-4000-8000-0000000000b7")
	setTenant(other)
	res, _ := upload(t, s, notAnImage, ct)
	if got := outcomeOf(res); got != "logo-format" {
		t.Errorf("another business in the same window: %d %q, want its body read (logo-format)", res.StatusCode, res.Header.Get("Location"))
	}
}

// TestBrandUpload_AdmissionRefusesBeforeTheBodyIsRead holds uploads half-sent and asks
// for more: a second upload of the SAME business is answered 503 without its body;
// with brandUploadsInFlight businesses holding a place, one more business is answered
// 503 without its body; once a held connection ends, its place is free again.
func TestBrandUpload_AdmissionRefusesBeforeTheBodyIsRead(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	p.h.brandUploadTimeout = 10 * time.Second
	s := p.server(t)
	setTenant := tenantSwitch(p, "owner")
	body, ct := oneLogo(t, inkLogo(t))
	tenants := make([]uuid.UUID, brandUploadsInFlight+1)
	for i := range tenants {
		tenants[i] = uuid.NewSHA1(uuid.Nil, []byte("admission-"+strconv.Itoa(i)))
	}
	var held []net.Conn
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()
	hold := func(id uuid.UUID) {
		setTenant(id)
		before := inFlight(p)
		held = append(held, rawUpload(t, s, ct, len(body), body[:10]))
		waitUntil(t, 5*time.Second, "the held upload to be admitted", func() bool { return inFlight(p) == before+1 })
	}
	refusedUnread := func(id uuid.UUID, why string) {
		t.Helper()
		setTenant(id)
		conn := rawUpload(t, s, ct, len(body), nil)
		res, err := rawAnswer(t, conn, 5*time.Second)
		conn.Close()
		if err != nil {
			t.Fatalf("%s: no answer without the body: %v", why, err)
		}
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s: %d, want 503", why, res.StatusCode)
		}
	}
	hold(tenants[0])
	refusedUnread(tenants[0], "a second upload of the same business")
	for _, id := range tenants[1:brandUploadsInFlight] {
		hold(id)
	}
	if inFlight(p) != brandUploadsInFlight {
		t.Fatalf("PREMISE: %d admitted, want %d", inFlight(p), brandUploadsInFlight)
	}
	refusedUnread(tenants[brandUploadsInFlight], "one business more than the places")
	// A held connection ends: the server's read fails, the handler returns, the place
	// is given back.
	held[0].Close()
	waitUntil(t, 5*time.Second, "the ended upload's place to be freed", func() bool { return inFlight(p) == brandUploadsInFlight-1 })
	setTenant(tenants[brandUploadsInFlight])
	res, _ := upload(t, s, body, ct)
	if got := outcomeOf(res); got != "logo-saved" {
		t.Errorf("after a place was freed: %d %q, want logo-saved", res.StatusCode, res.Header.Get("Location"))
	}
}

// slowUploadCut sends the first ten bytes of a body and then nothing, to h's server,
// and returns the answer and how long it took.
func slowUploadCut(t *testing.T, s *httptest.Server, body []byte, ct string) (*http.Response, time.Duration) {
	t.Helper()
	start := time.Now()
	conn := rawUpload(t, s, ct, len(body), body[:10])
	defer conn.Close()
	res, err := rawAnswer(t, conn, 10*time.Second)
	if err != nil {
		t.Fatalf("a stalled body got no answer: %v", err)
	}
	return res, time.Since(start)
}

// TestBrandUpload_ASlowBodyIsCutAtTheReadDeadline: with the read deadline shortened to
// 300 ms, a body that stops after ten bytes is answered logo-slow after the deadline and
// well before the test's own patience, its admission place is free afterwards, and
// nothing reached the decoder.
func TestBrandUpload_ASlowBodyIsCutAtTheReadDeadline(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	p.h.brandUploadTimeout = 300 * time.Millisecond
	s := p.server(t)
	body, ct := oneLogo(t, inkLogo(t))
	res, took := slowUploadCut(t, s, body, ct)
	if got := outcomeOf(res); got != "logo-slow" {
		t.Fatalf("a stalled body: %d %q, want logo-slow", res.StatusCode, res.Header.Get("Location"))
	}
	if took < 300*time.Millisecond || took > 5*time.Second {
		t.Errorf("the stalled body was answered after %v, want between the deadline and 5 s", took)
	}
	waitUntil(t, 2*time.Second, "the place to be freed", func() bool { return inFlight(p) == 0 })
	if p.gate.count() != 0 || p.writer.writes() != 0 {
		t.Errorf("%d decodes, %d writes; want 0 and 0", p.gate.count(), p.writer.writes())
	}
}

// TestBrandUpload_TheReadDeadlineReachesTheConnectionThroughTheRouter: through
// httpx.NewRouter -- whose access log wraps the response writer, with the request
// timeout and the recoverer -- an upload is saved, and a stalled body is still cut at
// the deadline: the response controller reaches the connection through the wrappers.
func TestBrandUpload_TheReadDeadlineReachesTheConnectionThroughTheRouter(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	p.h.brandUploadTimeout = 300 * time.Millisecond
	s := httptest.NewServer(httpx.NewRouter(nil, slog.New(slog.DiscardHandler), p.h))
	t.Cleanup(s.Close)
	body, ct := oneLogo(t, inkLogo(t))
	res, _ := upload(t, s, body, ct)
	if got := outcomeOf(res); got != "logo-saved" {
		t.Fatalf("through the router: %d %q, want logo-saved", res.StatusCode, res.Header.Get("Location"))
	}
	res, took := slowUploadCut(t, s, body, ct)
	if got := outcomeOf(res); got != "logo-slow" || took > 5*time.Second {
		t.Fatalf("a stalled body through the router: %d %q after %v, want logo-slow", res.StatusCode, res.Header.Get("Location"), took)
	}
}

// TestBrandUpload_WithoutAConnectionDeadlineNothingIsRead: a response writer whose read
// cannot be given a deadline (an httptest recorder) is answered 500, and neither the
// decoder nor the writer is reached -- the route refuses an unbounded read rather than
// performing one.
func TestBrandUpload_WithoutAConnectionDeadlineNothingIsRead(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	body, ct := oneLogo(t, inkLogo(t))
	req := httptest.NewRequest(http.MethodPost, brandLogoHref, bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Origin", testBaseURL)
	req.AddCookie(panelCookie())
	rec := httptest.NewRecorder()
	p.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("%d, want 500", rec.Code)
	}
	if p.gate.count() != 0 || p.writer.writes() != 0 || inFlight(p) != 0 {
		t.Errorf("%d decodes, %d writes, %d admitted; want 0, 0, 0", p.gate.count(), p.writer.writes(), inFlight(p))
	}
}

// --- the decoder's answers -----------------------------------------------------------

// TestBrandUpload_EachRefusalOfTheDecoderHasItsWord maps each of the decode gate's
// answers to the editor's word: the nine refusal classes of internal/brand to theirs
// (ErrLogoVerify, the context's end and an encoder error are internal: unavailable,
// logged at ERROR), and nothing is saved. Three real files reach their class through
// the real gate: an SVG, a GIF and a PNG of 8 x 8.
func TestBrandUpload_EachRefusalOfTheDecoderHasItsWord(t *testing.T) {
	for _, tc := range []struct {
		err      error
		want     string
		internal bool
	}{
		{brand.ErrLogoBusy, "logo-busy", false},
		{brand.ErrLogoInputTooLarge, "logo-too-large", false},
		{brand.ErrLogoRead, "unreadable", false},
		{brand.ErrLogoFormat, "logo-format", false},
		{brand.ErrLogoDimensions, "logo-dimensions", false},
		{brand.ErrLogoScans, "logo-scans", false},
		{brand.ErrLogoCorrupt, "logo-corrupt", false},
		{brand.ErrLogoOutputTooLarge, "logo-simplify", false},
		{brand.ErrLogoVerify, "unavailable", true},
		{fmt.Errorf("brand: logo: %w", context.Canceled), "unavailable", true},
		{errors.New("brand: logo: encode: disk full"), "unavailable", true},
	} {
		t.Run(tc.want+"/"+tc.err.Error(), func(t *testing.T) {
			p := newBrandPanel(t, "owner", panelTestTenant)
			p.gate.fail = tc.err
			body, ct := oneLogo(t, inkLogo(t))
			res, _ := upload(t, p.server(t), body, ct)
			if got := outcomeOf(res); got != tc.want {
				t.Fatalf("%v: %q, want %s", tc.err, res.Header.Get("Location"), tc.want)
			}
			if p.writer.writes() != 0 {
				t.Errorf("a refused logo was saved")
			}
			if logged := strings.Contains(p.logs.String(), "level=ERROR"); logged != tc.internal {
				t.Errorf("an ERROR line: %v, want %v\n%s", logged, tc.internal, p.logs.String())
			}
		})
	}
	gif := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="64" height="32"><script>alert(1)</script></svg>`)
	for name, tc := range map[string]struct {
		data []byte
		want string
	}{
		"an SVG with a script":    {svg, "logo-format"},
		"a GIF":                   {gif, "logo-format"},
		"a WebP header":           {[]byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00"), "logo-format"},
		"HTML behind a PNG magic": {append([]byte("\x89PNG\r\n\x1a\n"), []byte("<html><script>alert(1)</script></html>")...), "logo-format"},
		"a PNG of 8 x 8":          {solidPNG(t, 8, 8, color.NRGBA{0, 0, 0, 255}), "logo-dimensions"},
		"a PNG of 3000 x 16 wide": {solidPNG(t, 3000, 16, color.NRGBA{0, 0, 0, 255}), "logo-dimensions"},
	} {
		p := newBrandPanel(t, "owner", panelTestTenant)
		body, ct := oneLogo(t, tc.data)
		res, _ := upload(t, p.server(t), body, ct)
		if got := outcomeOf(res); got != tc.want {
			t.Errorf("%s: %q, want %s", name, res.Header.Get("Location"), tc.want)
		}
		if p.writer.writes() != 0 {
			t.Errorf("%s was saved", name)
		}
	}
}

// TestBrandUpload_TheDomainsAnswersAreTheHandlersOwn: the writer's own refusal of the
// actor (tenant.ErrBrandNotPermitted -- the role changed after the session resolved)
// gets the handler's answer, 303 not-permitted, AND the refusal row with the domain's
// reason; a database failure is unavailable, logged at ERROR, with no row.
func TestBrandUpload_TheDomainsAnswersAreTheHandlersOwn(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	s := p.server(t)
	p.writer.logoErr = tenant.ErrBrandNotPermitted
	body, ct := oneLogo(t, inkLogo(t))
	res, _ := upload(t, s, body, ct)
	if got := outcomeOf(res); got != "not-permitted" {
		t.Fatalf("domain refusal: %q, want not-permitted", res.Header.Get("Location"))
	}
	ev := p.trail.eventsSnapshot()
	if len(ev) != 1 || ev[0].Action != ActionBrandUpdateRefused || ev[0].Detail.(refusedBrandDetail).Reason != brandRefusedDomain {
		t.Fatalf("the trail holds %+v, want one refusal with the domain's reason", ev)
	}
	p.writer.logoErr = fmt.Errorf("tenant: brand logo: %w", errors.New("connection reset"))
	res, _ = upload(t, s, body, ct)
	if got := outcomeOf(res); got != "unavailable" || !strings.Contains(p.logs.String(), "level=ERROR") {
		t.Errorf("a failed save: %q (ERROR logged %v), want unavailable and an ERROR line",
			res.Header.Get("Location"), strings.Contains(p.logs.String(), "level=ERROR"))
	}
	if n := p.trail.total(); n != 1 {
		t.Errorf("a failed save wrote %d trail rows here, want none beyond the refusal", n-1)
	}
}

// --- the log -------------------------------------------------------------------------

// TestBrandUpload_ARefusalLogsItsClassAndNeverTheFile uploads a file whose name, part
// type and bytes each carry a marker, and drives seven refusals (format, parts,
// missing, too large, dimensions, the domain's refusal) and a database failure whose
// pgconn error carries the marker in its Detail. Each refusal logs its class; no line
// carries the file name, the part's type, the marker in the bytes (as text, hex or
// quoted) or the Detail.
func TestBrandUpload_ARefusalLogsItsClassAndNeverTheFile(t *testing.T) {
	const fileName = "Ħaġar-PLAIN-FILENAME-91c4.png"
	const partType = "image/x-PLAIN-PARTTYPE-77ab"
	const marker = "PLAIN-BYTES-3f0e"
	withMarker := append(inkLogo(t), []byte(marker)...)
	part := func(name string, data []byte) ([]byte, string) {
		return logoMultipart(t, logoPart{name: name, fileName: fileName, contentType: partType, data: data})
	}
	type shot struct {
		name, class string
		body        func() ([]byte, string)
		setup       func(*brandPanel)
	}
	shots := []shot{
		{"format", "logo-format", func() ([]byte, string) { return part("logo", []byte(marker+" not an image")) }, nil},
		{"parts", "logo-parts", func() ([]byte, string) { return part("not-logo-"+marker, withMarker) }, nil},
		{"missing", "logo-missing", func() ([]byte, string) { return part("logo", nil) }, nil},
		{"too large", "logo-too-large", func() ([]byte, string) {
			return part("logo", append(withMarker, bytes.Repeat([]byte(marker), brand.LogoMaxInputBytes/len(marker))...))
		}, nil},
		{"dimensions", "logo-dimensions", func() ([]byte, string) {
			return part("logo", append(solidPNG(t, 8, 8, color.NRGBA{0, 0, 0, 255}), []byte(marker)...))
		}, nil},
		{"the domain refuses", "", func() ([]byte, string) { return part("logo", withMarker) },
			func(p *brandPanel) { p.writer.logoErr = tenant.ErrBrandNotPermitted }},
		{"the database fails", "", func() ([]byte, string) { return part("logo", withMarker) },
			func(p *brandPanel) {
				p.writer.logoErr = fmt.Errorf("tenant: brand logo: write the logo: %w", &pgconn.PgError{
					Severity: "ERROR", Code: "23514", Message: "new row violates check constraint",
					Detail: "Failing row contains (\\x" + fmt.Sprintf("%x", marker) + ", " + marker + ")",
				})
			}},
	}
	for _, sh := range shots {
		t.Run(sh.name, func(t *testing.T) {
			p := newBrandPanel(t, "owner", panelTestTenant)
			if sh.setup != nil {
				sh.setup(p)
			}
			body, ct := sh.body()
			upload(t, p.server(t), body, ct)
			out := p.logs.String()
			if sh.class != "" && !strings.Contains(out, "class="+sh.class) {
				t.Errorf("no line names the class %s:\n%s", sh.class, out)
			}
			if out == "" {
				t.Fatal("PREMISE: nothing was logged")
			}
			for _, never := range []string{fileName, "PLAIN-FILENAME", partType, "PLAIN-PARTTYPE", marker,
				fmt.Sprintf("%x", marker), strconv.Quote(marker), "Failing row"} {
				if strings.Contains(out, never) {
					t.Errorf("the log carries %q:\n%s", never, out)
				}
			}
		})
	}
}

// gateTry posts to the logo route through b (a recorder: no connection, so step 5
// cannot set its deadline and answers 500) a body that DECLARES contentLength and is
// never read past its first byte.
func gateTry(b *browser, contentLength int64) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(http.MethodPost, brandLogoHref, strings.NewReader("x"))
	req.ContentLength = contentLength
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.RemoteAddr = b.ip
	req.Header.Set("Origin", b.origin)
	for name, value := range b.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	return rec
}

// TestBrandUpload_TheGateRunsInItsOrder holds steps 2 to 5 of brandUploadGate in their
// order, each against the next, by what each step's answer is when both would refuse
// (step 1 against step 2 is TestBrandRoutes_AManagersRefusalsCostTheOwnersBudgetsNothing):
// (2 before 3) ten attempts that declare a body over 1 MiB are each refused
// logo-too-large AND charged, so the eleventh, declaring a small body, is 429; (3 before
// 4) with every admission place taken, a declared body over the cap is still
// logo-too-large, not 503; (4 before 5) with every place taken, on a recorder that
// cannot take a deadline, a small body is 503, not 500. Nothing is decoded or written.
func TestBrandUpload_TheGateRunsInItsOrder(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	b := p.browser(t)
	for i := 1; i <= brandUploadLimit; i++ {
		rec := gateTry(b, brandUploadMaxBody+1)
		if got := outcomeOf(rec.Result()); got != "logo-too-large" {
			t.Fatalf("attempt %d declaring 1 MiB + 1: %d %q, want logo-too-large", i, rec.Code, rec.Header().Get("Location"))
		}
	}
	if rec := gateTry(b, 1); rec.Code != http.StatusTooManyRequests {
		t.Errorf("attempt %d, after ten over the cap: %d, want 429 (the budget is charged before the length is read)", brandUploadLimit+1, rec.Code)
	}

	p = newBrandPanel(t, "owner", panelTestTenant)
	p.h.uploads = newUploadAdmission(0)
	b = p.browser(t)
	if rec := gateTry(b, brandUploadMaxBody+1); outcomeOf(rec.Result()) != "logo-too-large" {
		t.Errorf("admission full, declaring 1 MiB + 1: %d %q, want logo-too-large (the length before admission)",
			rec.Code, rec.Header().Get("Location"))
	}
	if rec := gateTry(b, 1); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("admission full, a small body, no deadline possible: %d, want 503 (admission before the deadline)", rec.Code)
	}
	if p.gate.count() != 0 || p.writer.writes() != 0 {
		t.Errorf("%d decodes, %d writes; want none", p.gate.count(), p.writer.writes())
	}
}

// --- the syntax pin ------------------------------------------------------------------

// The five form-parsing calls and three fields ADR 0024 §6 forbids to the logo handler.
var (
	formReaderCalls  = map[string]bool{"FormValue": true, "PostFormValue": true, "FormFile": true, "ParseMultipartForm": true, "ParseForm": true}
	formReaderFields = map[string]bool{"Form": true, "PostForm": true, "MultipartForm": true}
)

// handlerFuncs is every function and method declared in this package's non-test files,
// by the key funcKey gives.
type handlerFuncs struct {
	decls map[string]*ast.FuncDecl
	file  map[string]string // key -> file name
	// methods is method name -> the keys of every method with that name.
	methods map[string][]string
}

func funcKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return "?." + fn.Name.Name
}

func parseHandlerFuncs(t *testing.T, dir string) handlerFuncs {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", dir, err)
	}
	h := handlerFuncs{decls: map[string]*ast.FuncDecl{}, file: map[string]string{}, methods: map[string][]string{}}
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				k := funcKey(fn)
				h.decls[k] = fn
				h.file[k] = filepath.Base(name)
				if fn.Recv != nil {
					h.methods[fn.Name.Name] = append(h.methods[fn.Name.Name], k)
				}
			}
		}
	}
	return h
}

// requestParams is the names fn declares as *http.Request (its parameters, and its
// function literals' parameters).
func requestParams(fn ast.Node) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		var ft *ast.FuncType
		switch x := n.(type) {
		case *ast.FuncDecl:
			ft = x.Type
		case *ast.FuncLit:
			ft = x.Type
		default:
			return true
		}
		for _, f := range ft.Params.List {
			s, ok := f.Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			sel, ok := s.X.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Request" {
				continue
			}
			if pk, ok := sel.X.(*ast.Ident); ok && pk.Name == "http" {
				for _, nm := range f.Names {
					out[nm.Name] = true
				}
			}
		}
		return true
	})
	return out
}

// formReaderHits lists the forbidden uses in fn: a call to one of the five by any
// receiver, and -- when anyField -- a read of one of the three fields of any value,
// otherwise only of a value fn declares as *http.Request.
func formReaderHits(fn *ast.FuncDecl, anyField bool) []string {
	reqs := requestParams(fn)
	called := map[*ast.SelectorExpr]bool{}
	var hits []string
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			called[sel] = true
			if formReaderCalls[sel.Sel.Name] {
				hits = append(hits, sel.Sel.Name+"()")
			}
		}
		return true
	})
	ast.Inspect(fn, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || called[sel] || !formReaderFields[sel.Sel.Name] {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); anyField || (ok && reqs[id.Name]) {
			hits = append(hits, "."+sel.Sel.Name)
		}
		return true
	})
	return hits
}

// requestMethodsAllowed are the methods the logo handler's reach may call on a value it
// declares as *http.Request: its context, and the multipart reader (which the file-level
// count holds to one call). Any other method is a finding whatever it does -- fail
// closed: Write, Clone and WithContext hand the body on, the five form helpers read it.
var requestMethodsAllowed = map[string]bool{"Context": true, "MultipartReader": true}

// requestCalleesAllowed are the calls outside this package the request itself may be
// handed to: the panel's identity, and the gate's own next handler (brandLogoSave, a
// root of the reach).
var requestCalleesAllowed = map[string]bool{"httpx.AdminOf": true, "next.ServeHTTP": true}

// exprName is a call's callee as written: a name, or names joined by dots; "?" for
// anything else.
func exprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprName(x.X) + "." + x.Sel.Name
	}
	return "?"
}

// inPackage reports whether a callee resolves into this package: a bare name it
// declares, or a selector naming one of its methods (reachableFrom's over-approximation;
// such a callee is in the reach and checked on its own).
func (h handlerFuncs) inPackage(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.Ident:
		_, ok := h.decls[f.Name]
		return ok
	case *ast.SelectorExpr:
		return len(h.methods[f.Sel.Name]) != 0
	}
	return false
}

// bodyHits lists fn's touches of a request's body other than THE one bound: the
// assignment X.Body = http.MaxBytesReader(W, X.Body, N), whose two .Body selectors are
// the only ones allowed. Every other .Body selector -- read, copied, closed, handed to a
// function, replaced, kept in a variable -- is a hit: of any value when anyValue (the
// file level), else of a value fn declares as *http.Request. On such a request it also
// lists a method call outside requestMethodsAllowed (the five form helpers are
// formReaderHits' and not counted twice) and the request handed as an argument to a
// call that is neither this package's (inPackage) nor in requestCalleesAllowed. wraps
// is how many bounds fn makes.
func bodyHits(fn *ast.FuncDecl, anyValue bool, inPackage func(ast.Expr) bool) (hits []string, wraps int) {
	reqs := requestParams(fn)
	isReq := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && reqs[id.Name]
	}
	sanctioned := map[*ast.SelectorExpr]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		lhs, ok := as.Lhs[0].(*ast.SelectorExpr)
		call, isCall := as.Rhs[0].(*ast.CallExpr)
		if !ok || !isCall || lhs.Sel.Name != "Body" || exprName(call.Fun) != "http.MaxBytesReader" || len(call.Args) != 3 {
			return true
		}
		arg, ok := call.Args[1].(*ast.SelectorExpr)
		target, isIdent := lhs.X.(*ast.Ident)
		if !ok || !isIdent || arg.Sel.Name != "Body" {
			return true
		}
		if src, ok := arg.X.(*ast.Ident); ok && src.Name == target.Name {
			sanctioned[lhs], sanctioned[arg] = true, true
			wraps++
		}
		return true
	})
	ast.Inspect(fn, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if x.Sel.Name == "Body" && !sanctioned[x] && (anyValue || isReq(x.X)) {
				hits = append(hits, ".Body")
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && isReq(sel.X) &&
				!requestMethodsAllowed[sel.Sel.Name] && !formReaderCalls[sel.Sel.Name] {
				hits = append(hits, sel.Sel.Name+"() on the request")
			}
			for _, a := range x.Args {
				if isReq(a) && !inPackage(x.Fun) && !requestCalleesAllowed[exprName(x.Fun)] {
					hits = append(hits, "the request handed to "+exprName(x.Fun))
				}
			}
		}
		return true
	})
	return hits, wraps
}

// reachableFrom is the closure of roots over this package's calls: a bare call to a
// package function, a call on a method's own receiver resolved to that type's method,
// and any other selector call to EVERY method of the package with that name (an
// over-approximation: it can only add functions). Calls into other packages are not
// followed.
func (h handlerFuncs) reachableFrom(roots []string) []string {
	seen := map[string]bool{}
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		if seen[k] {
			continue
		}
		seen[k] = true
		fn := h.decls[k]
		recvName, recvType := "", ""
		if fn.Recv != nil && len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
			recvName = fn.Recv.List[0].Names[0].Name
			recvType = strings.SplitN(k, ".", 2)[0]
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch f := call.Fun.(type) {
			case *ast.Ident:
				if _, ok := h.decls[f.Name]; ok {
					queue = append(queue, f.Name)
				}
			case *ast.SelectorExpr:
				if id, ok := f.X.(*ast.Ident); ok && id.Name == recvName && recvName != "" {
					if _, ok := h.decls[recvType+"."+f.Sel.Name]; ok {
						queue = append(queue, recvType+"."+f.Sel.Name)
						return true
					}
				}
				queue = append(queue, h.methods[f.Sel.Name]...)
			}
			return true
		})
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestBrandUpload_ReadsTheBodyOnlyAsAStream is ADR 0024 §6's syntax pin, in two
// reaches. (1) Every function in brandupload.go: none calls FormValue, PostFormValue,
// FormFile, ParseMultipartForm or ParseForm, and none reads a Form, PostForm or
// MultipartForm field of anything; the file calls the multipart reader exactly once;
// and the file names a Body of anything in exactly one place -- the gate's
// X.Body = http.MaxBytesReader(W, X.Body, N) -- so nothing but the multipart reader is
// handed the body: it is not buffered, copied or handed on BEFORE that reader
// (bodyHits). What the reader then does with it is not this pin's: the parts read raw,
// one at a time (NextRawPart, not NextPart or ReadForm, which decode and buffer), is
// held by the quoted-printable shape of TestBrandUpload_AcceptsOnePartNamedLogoAndNothingElse,
// and the part's limit applied as the part is read by
// TestBrandUpload_TheLimitIsAppliedAsThePartIsRead.
// (2) Every function of this package those reach (reachableFrom, printed): none calls
// the five, none reads the three fields or the Body of a value it declares as
// *http.Request, calls a method on it other than Context and MultipartReader, or hands
// it out of the package other than to httpx.AdminOf and the gate's next handler. A
// TMPDIR test cannot tell the helpers from the stream under the 1 MiB cap (ADR 0024
// S20); this can.
func TestBrandUpload_ReadsTheBodyOnlyAsAStream(t *testing.T) {
	h := parseHandlerFuncs(t, ".")
	var roots []string
	for k, f := range h.file {
		if f == "brandupload.go" {
			roots = append(roots, k)
		}
	}
	sort.Strings(roots)
	if len(roots) < 6 {
		t.Fatalf("PREMISE: brandupload.go declares %d functions; it declares the gate, the handler and the readers", len(roots))
	}
	streams, wraps := 0, 0
	for _, k := range roots {
		if hits := formReaderHits(h.decls[k], true); len(hits) != 0 {
			t.Errorf("brandupload.go: %s uses %v", k, hits)
		}
		hits, n := bodyHits(h.decls[k], true, h.inPackage)
		if len(hits) != 0 {
			t.Errorf("brandupload.go: %s touches the body or the request as %v", k, hits)
		}
		if n != 0 && k != "AdminAuth.brandUploadGate" {
			t.Errorf("brandupload.go: %s bounds a body; the gate is the one place", k)
		}
		wraps += n
		ast.Inspect(h.decls[k], func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "MultipartReader" {
					streams++
				}
			}
			return true
		})
	}
	if streams != 1 {
		t.Errorf("brandupload.go calls the multipart reader %d times, want 1", streams)
	}
	if wraps != 1 {
		t.Errorf("brandupload.go bounds the body %d times, want 1", wraps)
	}
	reach := h.reachableFrom(roots)
	for _, must := range []string{"AdminAuth.redirect", "AdminAuth.refuseBrand", "AdminAuth.record", "AdminAuth.renderProblem", "brandReturn"} {
		if !slices.Contains(reach, must) {
			t.Fatalf("PREMISE: the closure misses %s; it is reading the wrong calls", must)
		}
	}
	for _, k := range reach {
		if h.file[k] == "brandupload.go" {
			continue
		}
		if hits := formReaderHits(h.decls[k], false); len(hits) != 0 {
			t.Errorf("%s (%s), reached from the logo handler, uses %v", k, h.file[k], hits)
		}
		if hits, _ := bodyHits(h.decls[k], false, h.inPackage); len(hits) != 0 {
			t.Errorf("%s (%s), reached from the logo handler, touches the body or the request as %v", k, h.file[k], hits)
		}
	}
	t.Logf("the logo handler's file declares %d functions; they reach %d in this package: %s",
		len(roots), len(reach), strings.Join(reach, ", "))
}

// TestBrandUploadPin_RefusesEachShapeItExistsFor is the pin's negative control: on
// sources written here, formReaderHits sees each of the five calls on a request and on
// another value, each of the three fields read off a request (and off any value in the
// file-level reach), a field read through a function literal's request parameter; and it
// does not see a multipart stream, a method named like a field on another value in the
// closure reach, or the words in a comment. bodyHits sees the body buffered, copied,
// read, closed, handed to a function, kept in a variable, replaced, or bounded from
// another value's body; a method on the request that hands it on; the request handed to
// another package -- and not the one bound, the context, the multipart reader, the
// identity, the next handler or a call into this package. reachableFrom follows a
// receiver call, a bare call and a selector call on another value.
func TestBrandUploadPin_RefusesEachShapeItExistsFor(t *testing.T) {
	parse := func(src string) *ast.FuncDecl {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x\n"+src, 0)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		return f.Decls[len(f.Decls)-1].(*ast.FuncDecl)
	}
	for _, tc := range []struct {
		src      string
		anyField bool
		want     int
	}{
		{`func f(w http.ResponseWriter, r *http.Request) { _ = r.FormValue("x") }`, false, 1},
		{`func f(w http.ResponseWriter, r *http.Request) { _ = r.PostFormValue("x") }`, false, 1},
		{`func f(w http.ResponseWriter, r *http.Request) { _, _, _ = r.FormFile("x") }`, false, 1},
		{`func f(w http.ResponseWriter, r *http.Request) { _ = r.ParseMultipartForm(1) }`, false, 1},
		{`func f(w http.ResponseWriter, r *http.Request) { _ = r.ParseForm() }`, false, 1},
		{`func f(w http.ResponseWriter, q *http.Request) { req := q; _ = req.ParseForm() }`, false, 1},
		{`func f(w http.ResponseWriter, r *http.Request) { _ = r.Form }`, false, 1},
		{`func f(w http.ResponseWriter, r *http.Request) { _ = r.PostForm }`, false, 1},
		{`func f(w http.ResponseWriter, r *http.Request) { _ = r.MultipartForm }`, false, 1},
		{`func f() http.Handler { return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = r.Form }) }`, false, 1},
		{`func f(w http.ResponseWriter, r *http.Request) { x := r; _ = x.Form }`, true, 1},
		{`func f(v view) { _ = v.Form }`, false, 0},
		{`func f(w http.ResponseWriter, r *http.Request) { mr, _ := r.MultipartReader(); _ = mr }`, true, 0},
		{`func f(w http.ResponseWriter, r *http.Request) { // r.FormValue("x") and r.Form
}`, true, 0},
	} {
		if got := len(formReaderHits(parse(tc.src), tc.anyField)); got != tc.want {
			t.Errorf("%q (any field %v): %d hits, want %d", tc.src, tc.anyField, got, tc.want)
		}
	}
	helperOnly := func(e ast.Expr) bool { id, ok := e.(*ast.Ident); return ok && id.Name == "helper" }
	const req = `func f(w http.ResponseWriter, r *http.Request) { `
	for _, tc := range []struct {
		body     string
		anyValue bool
		hits     int
		wraps    int
	}{
		{`r.Body = http.MaxBytesReader(w, r.Body, 1) }`, true, 0, 1},
		{`b, _ := io.ReadAll(r.Body); _ = b }`, false, 1, 0},
		{`_, _ = io.Copy(io.Discard, r.Body) }`, false, 1, 0},
		{`buf := make([]byte, 1); _, _ = r.Body.Read(buf) }`, false, 1, 0},
		{`_ = r.Body.Close() }`, false, 1, 0},
		{`helper(r.Body) }`, false, 1, 0},
		{`body := r.Body; _ = body }`, false, 1, 0},
		{`r.Body = io.NopCloser(nil) }`, false, 1, 0},
		{`all, _ := io.ReadAll(r.Body); r.Body = io.NopCloser(bytes.NewReader(all)); mr, _ := r.MultipartReader(); _ = mr }`, false, 2, 0},
		{`r.Body = http.MaxBytesReader(w, other.Body, 1) }`, true, 2, 0},
		{`_ = r.Write(w) }`, false, 1, 0},
		{`_ = r.Clone(r.Context()) }`, false, 1, 0},
		{`_, _ = httputil.DumpRequest(r, true) }`, false, 1, 0},
		{`_ = httpx.AdminOf(r) }`, false, 0, 0},
		{`helper(r) }`, false, 0, 0},
		{`next.ServeHTTP(w, r) }`, false, 0, 0},
		{`_ = r.Context(); mr, _ := r.MultipartReader(); _ = mr; _ = r.Header.Get("Content-Type"); _ = r.ContentLength }`, false, 0, 0},
		{`// io.ReadAll(r.Body) and r.Write(w)
}`, true, 0, 0},
	} {
		got, wraps := bodyHits(parse(req+tc.body), tc.anyValue, helperOnly)
		if len(got) != tc.hits || wraps != tc.wraps {
			t.Errorf("%q (any value %v): hits %v and %d bounds, want %d and %d", tc.body, tc.anyValue, got, wraps, tc.hits, tc.wraps)
		}
	}
	for _, tc := range []struct {
		src      string
		anyValue bool
		hits     int
	}{
		{`func f(v view) { _ = v.Body }`, true, 1},
		{`func f(v view) { _ = v.Body }`, false, 0},
		{`func f() http.Handler { return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.ReadAll(r.Body) }) }`, false, 1},
	} {
		if got, _ := bodyHits(parse(tc.src), tc.anyValue, helperOnly); len(got) != tc.hits {
			t.Errorf("%q (any value %v): hits %v, want %d", tc.src, tc.anyValue, got, tc.hits)
		}
	}
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", `package x
type A struct{}
type B struct{}
func (a *A) root() { a.next(); helper(); var b B; b.far() }
func (a *A) next() {}
func helper() {}
func (b B) far() {}
func (a *A) unreached() {}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	h := handlerFuncs{decls: map[string]*ast.FuncDecl{}, file: map[string]string{}, methods: map[string][]string{}}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			k := funcKey(fn)
			h.decls[k] = fn
			if fn.Recv != nil {
				h.methods[fn.Name.Name] = append(h.methods[fn.Name.Name], k)
			}
		}
	}
	got := strings.Join(h.reachableFrom([]string{"A.root"}), ",")
	if got != "A.next,A.root,B.far,helper" {
		t.Errorf("reachableFrom = %s, want A.next,A.root,B.far,helper", got)
	}
}

// --- the wiring ----------------------------------------------------------------------

// TestBrandUpload_TheGateIsBuiltOnceWithTheRulesSlots: outside internal/brand and its
// tests, the decode gate is built by exactly one call -- in NewAdminAuth, with
// brand.LogoDecodeSlots -- and NewAdminAuth is called once, by cmd/tappa's main; a fresh
// AdminAuth's gate is a *brand.LogoGate (WL-3's hand-off: wire it once, pin it).
func TestBrandUpload_TheGateIsBuiltOnceWithTheRulesSlots(t *testing.T) {
	root := filepath.Join("..", "..")
	type site struct{ file, fn, arg string }
	var gates []site
	newAuth := 0
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
				strings.HasPrefix(path, filepath.Join(root, "internal", "brand")+string(filepath.Separator)) {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				ast.Inspect(fn, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					switch sel.Sel.Name {
					case "NewLogoGate":
						arg := ""
						if len(call.Args) == 1 {
							if a, ok := call.Args[0].(*ast.SelectorExpr); ok {
								if x, ok := a.X.(*ast.Ident); ok {
									arg = x.Name + "." + a.Sel.Name
								}
							}
						}
						gates = append(gates, site{filepath.Base(path), fn.Name.Name, arg})
					case "NewAdminAuth":
						if strings.HasSuffix(path, filepath.Join("cmd", "tappa", "main.go")) {
							newAuth++
						}
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(gates) != 1 || gates[0] != (site{"adminlogin.go", "NewAdminAuth", "brand.LogoDecodeSlots"}) {
		t.Errorf("the decode gate is built at %+v, want once, in NewAdminAuth, with brand.LogoDecodeSlots", gates)
	}
	if newAuth != 1 {
		t.Errorf("cmd/tappa/main.go calls NewAdminAuth %d times, want 1", newAuth)
	}
	p := newBrandPanel(t, "owner", panelTestTenant)
	if _, ok := p.gate.inner.(*brand.LogoGate); !ok {
		t.Errorf("NewAdminAuth's gate is a %T, want *brand.LogoGate", p.gate.inner)
	}
	if p.h.brandUploadTimeout != brandUploadReadTimeout {
		t.Errorf("NewAdminAuth sets the read deadline to %v, want brandUploadReadTimeout", p.h.brandUploadTimeout)
	}
	if p.h.uploads.limit != brandUploadsInFlight {
		t.Errorf("NewAdminAuth admits %d uploads, want brandUploadsInFlight", p.h.uploads.limit)
	}
}
