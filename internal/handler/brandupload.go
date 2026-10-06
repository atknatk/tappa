package handler

// brandupload.go -- POST /admin/account/brand/logo, the logo upload (M10 WL-7; ADR 0024
// §2 and §6). THE ONE PLACE IN THE PRODUCT THAT READS A MULTIPART BODY, and this file
// holds nothing else: the multipart reading, the gate in front of it, and the handler.
//
// THE ORDER, AND WHAT EACH STEP COSTS BEFORE THE NEXT:
//
//	ProtectWriting   flood -> Origin (BEFORE the resolver: a cross-origin POST costs
//	                 no database work) -> identity -> session budget (adminlogin.go)
//	brandUploadGate  1. owner?            no  -> 303 not-permitted + refusal row
//	                 2. business budget   no  -> 429 (+ refusal row on the crossing)
//	                 3. declared length   over 1 MiB -> 303 logo-too-large
//	                 4. admission         full -> 503, the body never read
//	                 5. read deadline on the connection, 1 MiB cap on the body
//	brandLogoSave    6. the body as a stream: one part, named logo, at most 512 KiB,
//	                    and then the end of the body -- before any decode
//	                 7. brand.LogoGate.Normalize: the decode slot (N = 1), the format,
//	                    size and scan gates, the re-encode (internal/brand, WL-3)
//	                 8. tenant.Brands.SaveLogo: the owner again, the UPDATE and its
//	                    trail row in one transaction (WL-4)
//	                 9. 303 logo-saved, or logo-saved-light when the logo is light on
//	                    the porcelain it sits on (brand.LogoLooksLight, a warning)
//
// Nothing is read from the body before step 6, so a manager, an exhausted budget, a
// declared body over the cap and a full admission are answered without reading it. The
// order is held step against step: 1 before 2 on all three routes, through one
// AdminAuth (TestBrandRoutes_AManagersRefusalsCostTheOwnersBudgetsNothing: a manager
// cannot spend the owner's budget), and 2 before 3, 3 before 4, 4 before 5
// (TestBrandUpload_TheGateRunsInItsOrder).
//
// 🔴 THE BODY IS READ ONLY THROUGH THE MULTIPART READER. The form-parsing helpers of
// net/http -- the five calls and three fields ADR 0024 §6 names -- accept any number of
// parts under any names, buffer the whole body, and at a small threshold write a part
// to a temporary file, which the pod's read-only root cannot hold (ADR 0024 S17, S20).
// A stream is what lets this handler insist on one part called logo and stop at 512
// KiB. Under the 1 MiB cap those helpers do NOT touch the disk (S20), so a test that
// runs with TMPDIR missing cannot tell them from the stream; the syntax pin can, and
// it reads this file and every function in this package this file reaches
// (TestBrandUpload_ReadsTheBodyOnlyAsAStream). Two things more are held, by two
// different pins. THE BODY: the same syntax pin finds it named in ONE place, the gate's
// http.MaxBytesReader bound, and nothing else in the reach reading, buffering, copying,
// closing, replacing or handing it on, or handing the request out of the package other
// than to httpx.AdminOf and the gate's next handler -- so nothing but the multipart
// reader is handed the body (bodyHits; fail closed: any other method on the request is
// a finding too). THE PART: the syntax pin does not see how a part is read once the
// reader has it. That the 512 KiB limit is applied AS the part is read -- at most the
// limit and one byte of it, into this file's two buffers, never the whole part first
// -- is held by counting the bytes read (TestBrandUpload_TheLimitIsAppliedAsThePartIsRead),
// and that the parts are read raw and one at a time (NextRawPart, not NextPart or
// ReadForm, which decode and buffer) by the quoted-printable shape of
// TestBrandUpload_AcceptsOnePartNamedLogoAndNothingElse.
//
// 🔴 NO FILE NAME, NO PART HEADER, NO BYTE IS LOGGED OR RECORDED. A refusal is logged
// by its class -- the outcome word -- with the tenant and the actor; the client's file
// name, its Content-Type and the part's bytes are never read into a log line or a trail
// row (ADR 0024 §6, Iddia G).
//
// THE CLAIM, IN THREE PARTS, FOR THE THREE BRAND ROUTES (this file and brandactions.go).
//
// THREAT MODEL: these pins are against an ACCIDENTAL drift in the three routes, the
// body they read, the order of the gate, the editor's view and the upload tripwire. Code
// written on purpose to get past a pin -- the request kept in another variable and read
// through it, reflect, a reader moved to another package -- is the subject of code review.
//
// PART I -- THE SHIPPED CODE, MEASURED (test · what it is fed · what it asserts · the
// mutation of ADR 0024's WL-7 note that turns it red):
//   - TestBrandUpload_AnOwnersLogoIsNormalizedAndHandedOnUnchanged · an owner's 64x32 PNG
//     through a real server and the real gate · 303 logo-saved, one decode, one save of
//     a value whose Normalized() is true, for the session's business and admin · U23.
//   - TestBrandUpload_SucceedsWithTMPDIRMissingOrReadOnly · a 700x700 JPEG over 64 KiB,
//     TMPDIR missing and read-only (a temporary file cannot be created) · logo-saved · U26.
//   - TestBrandUpload_ALightLogoIsSavedWithAWarning · a PNG near porcelain and a dark one
//     · both saved, logo-saved-light and logo-saved, the warning drawn in the editor · U19.
//   - TestBrandUpload_AcceptsOnePartNamedLogoAndNothingElse · sixteen body shapes (two
//     parts, a field, a path or a capital in the name, an attachment, no part, an empty
//     part, urlencoded, multipart/mixed, no or another boundary, a cut body, a
//     quoted-printable part, a preamble and an epilogue) · each one's word, the decoder
//     reached only by the two that pass the stream · U09, U10, U11, U13, U14.
//   - TestBrandUpload_ExactlyTheLimitIsReadAndOneMoreIsRefused · a part of 524 288 and
//     of 524 289 bytes · the first handed to the decoder whole, the second refused
//     logo-too-large with no decode · U12.
//   - TestBrandUpload_TheLimitIsAppliedAsThePartIsRead · readLogoPart over a counting
//     body, parts of 1 000 KiB, of the limit plus one byte and of the limit · the first
//     two logo-too-large, the third returned whole; each having read at most the limit +
//     16 KiB of the body · Q9 (the part read whole first, io.ReadAll), Q10 (ReadForm).
//   - TestBrandUpload_ABodyOverTheCapIsRefused · a declared length over 1 MiB with no
//     body sent; a chunked body whose preamble passes 1 MiB · logo-too-large both, no
//     decode, no write · U03, U08, U15.
//   - TestBrandUpload_TheBusinessBudgetRefusesPastTenBeforeReadingTheBody · ten attempts,
//     then two with no body sent, then another business · 429 twice without the body, one
//     refusal row with the budget reason, ten decodes, the other business read · U02,
//     U21, A03.
//   - TestBrandUpload_AdmissionRefusesBeforeTheBodyIsRead · half-sent bodies held over raw
//     TCP · 503 without the body for a second upload of one business and for one business
//     past the places; a place freed when its connection ends · U04, U05, U21.
//   - TestBrandUpload_TheGateRunsInItsOrder · ten attempts declaring 1 MiB + 1, then a
//     small one; a full admission with a declared body over the cap and a small one, on a
//     connection that cannot take a deadline · logo-too-large ten times then 429; then
//     logo-too-large, not 503; then 503, not 500 · X20, R01, R02.
//   - TestBrandRoutes_AManagersRefusalsCostTheOwnersBudgetsNothing · through ONE AdminAuth,
//     a manager's three POSTs, then the owner's ten uploads and thirty changes · the
//     manager's three not-permitted with three rows; the owner's forty answered as
//     themselves, none 429; the eleventh upload and thirty-first change 429 · X19, X19b,
//     X31, R03.
//   - TestBrandUpload_ASlowBodyIsCutAtTheReadDeadline,
//     TestBrandUpload_TheReadDeadlineReachesTheConnectionThroughTheRouter · ten bytes,
//     then nothing, with a 300 ms deadline, on chi and on httpx.NewRouter · logo-slow
//     between the deadline and 5 s, the place freed, no decode · U06, U16.
//   - TestBrandUpload_WithoutAConnectionDeadlineNothingIsRead · a writer that cannot take
//     a deadline · 500, nothing admitted, decoded or written · U06, U07.
//   - TestBrandUpload_EachRefusalOfTheDecoderHasItsWord · the gate's eleven errors and
//     six real files · each word, an ERROR line exactly for the internal three, no save
//     · U17, U18.
//   - TestBrandUpload_TheDomainsAnswersAreTheHandlersOwn,
//     TestBrandAccent_TheDomainsAnswersAreTheHandlersOwn · the writer's own refusal and
//     failure · not-permitted with the domain's refusal row; unavailable · U20, A15.
//   - TestBrandUpload_ARefusalLogsItsClassAndNeverTheFile · seven refusals of a file whose
//     name, part type and bytes carry markers, and a database error whose Detail does ·
//     the class logged; no marker, no Detail · U22.
//   - TestBrandUpload_ReadsTheBodyOnlyAsAStream, TestBrandUploadPin_RefusesEachShapeItExistsFor
//     · this file's functions and the package functions they reach (printed) · none
//     calls the five helpers or reads the three fields; one multipart reader; the body
//     named only in the gate's one bound; the request given no other method call and
//     handed to no other package's call but the two named · U24, U25, U26, U27, X24, R04.
//   - TestBrandUpload_TheGateIsBuiltOnceWithTheRulesSlots · the non-test Go of internal
//     and cmd · one brand.NewLogoGate(brand.LogoDecodeSlots), in NewAdminAuth, one
//     NewAdminAuth, in main · U28.
//   - TestBrandRoutes_CrossOriginIsRefusedBeforeTheResolver · three routes, two shapes ·
//     303 /admin, the resolver asked 0 times · D01.
//   - TestBrandRoutes_AManagerIsRefusedRecordedAndChangesNothing,
//     TestBrandRoutesDB_AnOwnersBrandIsStoredAndAManagersChangesNothing · a manager's
//     three POSTs (the upload without its body); on real Postgres also an owner's upload,
//     accent, budget and resets · not-permitted, one row each with exactly the five keys,
//     the row unchanged; the stored digest is the re-encoder's · U01, A01, A02, A10, U23.
//   - TestBrandAccent_TheBoundaryReadsWhatTheColourInputSends,
//     TestBrandAccent_AnIllegibleColourComesBackWithItsSuggestion · thirteen spellings;
//     four refused colours · the one colour saved, or the form back unsaved with
//     brand.Suggest's colour offered and saved when posted · A04, A05, A06, A07.
//   - TestBrandReset_TakesBackOneInputAtATime, TestBrandWrite_TheBusinessBudgetRefusesPastThirty
//     · one input at a time, each for the session's business whatever the body names; thirty
//     changes, then 429 with one refusal row · A03, A08, A09, A14.
//   - TestBrandOutcomes_EveryWordIsDrawnAndNoOtherText · A12.
//   - TestBrandPreview_IsTheTapScreensOwnComponentsAndCannotSubmit,
//     TestBrandPreview_TheLogoIsThePanelRoute, TestBrandPreview_ShowsOnlyTheSavedAccent,
//     TestBrandEditor_EveryControlIsATouchTarget, TestBrandEditor_AManagerSeesThePreviewAndNoForm
//     · two roles under five saved brands; a refused colour · the preview is the page's
//     one inert block and holds the tap screen's three components EACH ONCE and nothing
//     else; outside every form, sending nothing; the rest of the editor draws no header,
//     wordmark or image (withoutBrandEditor); its logo the panel route's with the stored
//     box; only the saved theme linked; every control a touch target; no form for a
//     manager · T01, T02, T03, T04, T05, A11, A13, A16, P01, P02, L05, X01, X02, X03, X21.
//   - TestBrandPreview_ARefusedColourLeavesThePreviewAsSaved · five saved brands × five
//     refused saves (illegible picked, typed and from the band's other side, no colour, a
//     legible colour the domain refuses) · 200 with the error; the preview block and the
//     page outside the editor byte-equal to the plain load · A11 (the one path left: the
//     preview has no input a refused save writes but the chrome itself).
//   - TestBrandEditor_TheFormsSendWhatTheRoutesRead, TestBrandWrite_AFormOverItsBoundIsNotRead,
//     TestBrandRoutes_TheWriterIsRequired · every form drawn, and none of the two "take it
//     back" ones with nothing saved; the two small forms at and one byte past 2 KiB;
//     NewAdminAuth with a nil writer · each form's sent fields are the routes' constants;
//     read at the bound, unreadable past it; refused · X22, X27, X28, X29, X30, X14.
//   - TestPanelBrandView_ThePreviewLogoIsDrawnOnlyWhereTheChromesIs · digests and boxes at
//     and past the table's bounds · the preview's <img> never without the chrome's · P03
//     is equivalent (both constructors refuse the same shapes) and stays green.
//   - TestBrandRoutesDB_ConcurrentWritesLeaveOneOfTheirStates · eighteen changes at once
//     on real Postgres · each its own word, one trail row each, a stored accent one of
//     theirs · no mutation run against it.
//
// PART II -- NAMED PINS: the tests above, and TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed
// with TestFactMechanisms_SayNoWhenTheFactIsAbsent (FactNoBulkImport, re-derived: no
// multipart reader in non-test Go outside this file, and here only the stream -- U29).
// What each catches is the mutation table of ADR 0024's WL-7 note.
//
// PART III -- No completeness claim: any change the table does not list is the subject
// of code review.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The upload's bounds (ADR 0024 §2.1, §6).
//
//	brandUploadMaxBody      1 MiB on the whole body, ADR 0024 §2.1: one 512 KiB part
//	                        and its multipart framing fit with room. The Ingress in
//	                        front of the pod has the same figure (proxy-body-size "1m",
//	                        deploy/k8s/40-ingress.yaml).
//	brandUploadsInFlight    uploads past step 4 at once, across all businesses. Each
//	                        holds at most two copies of a 512 KiB part (this file's and
//	                        Normalize's own read) while it waits for the ONE decode slot,
//	                        so four hold about 4 MiB besides the decode itself. A
//	                        business has at most ONE upload past step 4 (uploadAdmission),
//	                        so one business alone cannot fill the four.
//	brandUploadReadTimeout  how long the body may take to arrive once admitted. A slow
//	                        sender holds its admission place for at most this long. 1 MiB
//	                        in 15 s is about 70 KB/s.
const (
	brandUploadMaxBody     = 1 << 20
	brandUploadsInFlight   = 4
	brandUploadReadTimeout = 15 * time.Second
)

// problemBrandUploadBusy is the 503 for an upload refused at admission.
var problemBrandUploadBusy = pages.ProblemView{
	Title:   "Another logo is being uploaded",
	Message: "Logos are uploaded one at a time for each business, and only a few at once in all. This one was not read, and nothing was changed.",
	Hint:    "Go back and upload it again in a moment.",
}

// problemBrandUploadUnbounded is the 500 for a connection whose read cannot be given a
// deadline (step 5). Unreachable through the production router on HTTP/1.1
// (TestBrandUpload_TheReadDeadlineReachesTheConnectionThroughTheRouter); refused rather
// than read without one, because an unbounded read is what step 5 exists to stop.
var problemBrandUploadUnbounded = pages.ProblemView{
	Title:   "That upload could not be accepted",
	Message: "The server could not limit how long it would wait for the file, so it did not read it. Nothing was changed.",
	Hint:    "This one is ours rather than yours. Try again later.",
}

// uploadAdmission is step 4: at most limit uploads past it at once, and at most one
// per business. A refused request has read nothing of its body.
type uploadAdmission struct {
	mu       sync.Mutex
	limit    int
	inFlight map[uuid.UUID]struct{}
}

func newUploadAdmission(limit int) *uploadAdmission {
	return &uploadAdmission{limit: limit, inFlight: map[uuid.UUID]struct{}{}}
}

// enter admits tenantID's upload, or refuses it at once: no waiting, no queue (the same
// shape as the decode gate's, ADR 0024 §2.6).
func (u *uploadAdmission) enter(tenantID uuid.UUID) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, busy := u.inFlight[tenantID]; busy || len(u.inFlight) >= u.limit {
		return false
	}
	u.inFlight[tenantID] = struct{}{}
	return true
}

func (u *uploadAdmission) leave(tenantID uuid.UUID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.inFlight, tenantID)
}

// logoNormalizer is the slice of brand.LogoGate the upload needs, at the consumer
// (section 7). NewAdminAuth builds the one gate (brand.NewLogoGate(brand.LogoDecodeSlots)).
type logoNormalizer interface {
	Normalize(ctx context.Context, r io.Reader) (brand.Logo, error)
}

// brandUploadGate is steps 1 to 5. It runs after ProtectWriting (it needs the identity),
// and it is the only thing between the resolver and the body.
func (a *AdminAuth) brandUploadGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := httpx.AdminOf(r)
		if !id.Live() {
			// Unreachable behind requireAdmin; a chain that did not run reads nothing.
			a.log.Error("panel brand upload: no live panel identity on the request")
			closeUnread(w)
			a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
			return
		}
		// 1. The owner, before anything is charged or read.
		if !mayEditAccount(id) {
			a.refuseBrand(r, id, brandFieldLogo, brandRefusedRole)
			closeUnread(w)
			a.redirect(w, brandReturn("not-permitted"))
			return
		}
		// 2. The business's budget, per attempt (brandUploadLimit).
		if !a.chargeBrand(r, id, a.brandUploadLimiter, brandUploadLimit, brandFieldLogo) {
			closeUnread(w)
			a.renderProblem(w, r, http.StatusTooManyRequests, problemBrandTooMany)
			return
		}
		// 3. A body that says it is over the cap is not admitted to be read.
		if r.ContentLength > brandUploadMaxBody {
			a.refuseLogo(r, id, "logo-too-large")
			closeUnread(w)
			a.redirect(w, brandReturn("logo-too-large"))
			return
		}
		// 4. Admission.
		if !a.uploads.enter(id.TenantID()) {
			a.log.Warn("panel brand logo refused at admission", "tenant_id", id.TenantID())
			closeUnread(w)
			a.renderProblem(w, r, http.StatusServiceUnavailable, problemBrandUploadBusy)
			return
		}
		defer a.uploads.leave(id.TenantID())
		// 5. The read deadline is the CONNECTION's, set through the response controller,
		// so a sender that stops sending is cut off by the network read itself (the
		// decoder and the multipart reader read no context).
		if err := http.NewResponseController(w).SetReadDeadline(time.Now().Add(a.brandUploadTimeout)); err != nil {
			a.log.Error("panel brand logo refused: the connection's read cannot be bounded",
				"tenant_id", id.TenantID(), "err", err)
			closeUnread(w)
			a.renderProblem(w, r, http.StatusInternalServerError, problemBrandUploadUnbounded)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, brandUploadMaxBody)
		next.ServeHTTP(w, r)
	})
}

// brandLogoSave is steps 6 to 9.
func (a *AdminAuth) brandLogoSave(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	data, word := readLogoPart(r)
	if word != "" {
		a.refuseLogo(r, id, word)
		closeUnread(w)
		a.redirect(w, brandReturn(word))
		return
	}
	logo, err := a.logoGate.Normalize(r.Context(), bytes.NewReader(data))
	if err != nil {
		word, internal := logoRefusalWord(err)
		if internal {
			// The re-encoder's own failure, its output failing its own check, or the
			// request's context ending: not a verdict on the file. Normalize's refusals
			// carry their class's text only (brand.logoError), and its other two errors
			// wrap an encoder error or the context's -- neither carries the upload.
			a.log.Error("panel brand logo: normalizing failed", "tenant_id", id.TenantID(),
				"actor_id", id.Admin.AdminUserID, "err", err)
		} else {
			a.refuseLogo(r, id, word)
		}
		a.redirect(w, brandReturn(word))
		return
	}
	// Normalize's value goes to the domain UNCHANGED: a Logo with any field changed or
	// rebuilt is refused there (tenant.ErrBrandLogoNotNormalized, WL-4).
	err = a.brandWriter.SaveLogo(r.Context(), tenant.BrandLogoCommand{
		TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID, Logo: logo,
	})
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrBrandNotPermitted):
		a.refuseBrand(r, id, brandFieldLogo, brandRefusedDomain)
		a.redirect(w, brandReturn("not-permitted"))
		return
	default:
		// A database error's text is its message and SQLSTATE; pgconn's Detail (which can
		// quote a row) is not in it (WL-4's note, measured again in
		// TestBrandUpload_ARefusalLogsItsClassAndNeverTheFile).
		a.log.Error("panel brand logo: the save failed", "tenant_id", id.TenantID(),
			"actor_id", id.Admin.AdminUserID, "err", err)
		a.redirect(w, brandReturn("unavailable"))
		return
	}
	done := "logo-saved"
	switch light, err := brand.LogoLooksLight(logo); {
	case err != nil:
		// The logo is saved; only the advice is missing.
		a.log.Warn("panel brand logo: could not measure how light the saved logo is",
			"tenant_id", id.TenantID(), "err", err)
	case light:
		done = "logo-saved-light"
	}
	a.redirect(w, brandReturn(done))
}

// closeUnread marks the response to end its connection. It is called on the refusals of
// steps 1 to 5, which leave the body unread, and of step 6, which can leave it read in
// part: without it net/http reads up to 256 KiB of an unread body before it answers, to
// keep the connection for another request (net/http's chunkWriter.writeHeader) -- a
// read of a body this route has just refused, and before step 5 a read with no
// deadline: a sender that stopped would hold the answer, and the goroutine, for as long
// as it liked (measured: with this taken out, the budget and admission refusals of a
// small, unsent body were not answered within 5 s). The answers of steps 7 to 9 (a
// refused or failed decode, a failed save, the saved logo) do not call it: by then the
// stream has read up to the body's closing boundary, and what may be left -- an
// epilogue -- net/http reads at most 256 KiB of (past that it closes the connection
// itself), under step 5's deadline, so the connection is kept.
func closeUnread(w http.ResponseWriter) { w.Header().Set("Connection", "close") }

// refuseLogo logs one refused upload by its class. It writes no trail row: a file that
// is not a usable logo is the owner's own attempt, not a breach of the gate, and the
// budget already bounds how often it can happen.
func (a *AdminAuth) refuseLogo(r *http.Request, id httpx.AdminIdentity, word string) {
	a.log.WarnContext(r.Context(), "panel brand logo refused", "tenant_id", id.TenantID(),
		"actor_id", id.Admin.AdminUserID, "class", word)
}

// logoPartName is the one part the upload carries (pages: accountBrandLogo's input).
const logoPartName = "logo"

// readLogoPart reads the body as a multipart stream and returns the logo part's bytes,
// or the outcome word that refuses the upload. It accepts EXACTLY one part, named logo,
// of at most brand.LogoMaxInputBytes, followed by the end of the body; anything else is
// refused before a byte is decoded.
//
// The part is read raw (NextRawPart): no Content-Transfer-Encoding is applied, so what
// reaches the decoder is what was sent. The part's file name and Content-Type header
// are never read -- the type is decided from the bytes (ADR 0024 §1).
func readLogoPart(r *http.Request) ([]byte, string) {
	// The request's own type must be a form's. net/http's multipart reader also takes
	// multipart/mixed (measured: a mixed body carrying one form-data part named logo was
	// read and saved); a browser's form never sends one, so it is refused here.
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "multipart/form-data" {
		return nil, "unreadable"
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, "unreadable"
	}
	part, err := mr.NextRawPart()
	if err != nil {
		return nil, logoReadFailure(err)
	}
	if part.FormName() != logoPartName {
		return nil, "logo-parts"
	}
	data, err := readLogoBytes(part)
	if err != nil {
		return nil, logoReadFailure(err)
	}
	switch {
	case len(data) > brand.LogoMaxInputBytes:
		return nil, "logo-too-large"
	case len(data) == 0:
		return nil, "logo-missing"
	}
	switch _, err := mr.NextRawPart(); {
	case err == io.EOF:
		return data, ""
	case err == nil:
		return nil, "logo-parts"
	default:
		return nil, logoReadFailure(err)
	}
}

// readLogoBytes reads r to its end or to one byte past brand.LogoMaxInputBytes,
// whichever comes first, into at most two buffers: 32 KiB, and once that is full one
// of the limit plus one byte (brand's logoRead has the same shape, for the same
// reason: what a read allocates is set by the two buffers, not by how r splits its
// data). What it reads of the body is counted by
// TestBrandUpload_TheLimitIsAppliedAsThePartIsRead (WL-3 holds logoRead against the
// same change: its M35).
func readLogoBytes(r io.Reader) ([]byte, error) {
	buf := make([]byte, 32<<10)
	n := 0
	for {
		if n == len(buf) {
			if len(buf) > brand.LogoMaxInputBytes {
				return buf, nil
			}
			full := make([]byte, brand.LogoMaxInputBytes+1)
			copy(full, buf)
			buf = full
		}
		m, err := r.Read(buf[n:])
		n += m
		if err == io.EOF {
			return buf[:n], nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// logoReadFailure classifies an error met while reading the body: the 1 MiB cap, the
// read deadline, or anything else (a malformed multipart body, a connection that
// ended early).
func logoReadFailure(err error) string {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		return "logo-too-large"
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "logo-slow"
	default:
		return "unreadable"
	}
}

// logoRefusalWord maps Normalize's error to the outcome word, and says whether it is an
// internal failure rather than a refusal of the file (internal/brand's WL-3 hand-off:
// ErrLogoBusy -> try again, ErrLogoScans -> a standard JPEG, ErrLogoOutputTooLarge ->
// simplify or JPEG, ErrLogoVerify -> internal).
func logoRefusalWord(err error) (word string, internal bool) {
	switch {
	case errors.Is(err, brand.ErrLogoBusy):
		return "logo-busy", false
	case errors.Is(err, brand.ErrLogoInputTooLarge):
		return "logo-too-large", false
	case errors.Is(err, brand.ErrLogoFormat):
		return "logo-format", false
	case errors.Is(err, brand.ErrLogoDimensions):
		return "logo-dimensions", false
	case errors.Is(err, brand.ErrLogoScans):
		return "logo-scans", false
	case errors.Is(err, brand.ErrLogoCorrupt):
		return "logo-corrupt", false
	case errors.Is(err, brand.ErrLogoOutputTooLarge):
		return "logo-simplify", false
	case errors.Is(err, brand.ErrLogoRead):
		return "unreadable", false
	default:
		return "unavailable", true
	}
}
