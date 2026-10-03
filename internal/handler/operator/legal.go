package operator

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// THE LEGAL TEXTS (M10 OP-10, phase B; ADR 0020 §7). Taptime's own four documents --
// privacy policy, terms, company details, cookie notice -- are published here, by the
// platform operator, through op_publish_legal; the M7-06 panel screen that published
// them for an env allow-list of customer admins is gone. Both routes sit in the
// console's group (routes.go, mount): host gate, security headers, flood gate,
// same-origin gate, requireOperator, sessionGate.
//
// WHAT THE DATABASE DECIDES AND WHAT THIS FILE DECIDES. op_publish_legal resolves the
// session (op_touch_session), takes the publisher from it (there is no parameter for
// one), refuses a slug outside the closed set, a body over 256 KiB and a body of
// `[:space:]` alone, and writes the version and its legal_publish audit row in one
// statement (ADR 0021, OP-10 note). This file refuses, before any store call, what the
// operator can be TOLD about: a document that does not exist, a body with no visible
// text (visibleText -- a wider set than the database's, measured below), a request body
// over maxLegalBody. Then it refreshes the public snapshot (legal.Store.Refresh), which
// is what makes /legal/* show the new text on this replica.

// LegalStore is the operator database's slice this screen needs, declared at the
// consumer (CLAUDE.md §7). *db.OperatorDB implements it by delegating to internal/db's
// LegalVersions and PublishLegal (TestOperatorDB_EveryMethodDelegatesVerbatim), and
// that type's method set is derived from this interface and operatorauth.Store
// (TestOperatorDB_IsTheStoreAndNothingMore). sessionHash is operatorauth's
// SessionHash of the request's token -- on ADR 0020 §5's never-log list.
type LegalStore interface {
	// LegalVersions is ADR 0021 §2 v's two-phase read: op_begin_read writes and commits
	// the read's audit row, op_read_legal_versions consumes the ticket and returns the
	// page. Two transactions; db.LegalVersions hands the ticket from the first phase to
	// the second, and this signature carries none.
	LegalVersions(ctx context.Context, sessionHash string, page db.LegalVersionsPage) ([]db.LegalVersion, error)
	// PublishLegal is op_publish_legal: one new version and its legal_publish audit row,
	// in one statement.
	PublishLegal(ctx context.Context, sessionHash, slug, body string) error
}

// LegalTexts is the slice of internal/domain/legal.Store this screen needs: the
// snapshot the public pages serve (the editors re-open on it) and the read that
// refreshes it after a publication (ADR 0020 §7: one replica).
type LegalTexts interface {
	Published() map[string]legal.Doc
	Refresh(ctx context.Context) error
}

// maxLegalBody bounds the publication's request body: 256 KiB, ADR 0016 §6's ceiling,
// which op_publish_legal enforces on the stored text as octet_length > 262144. The
// bound is applied to the URL-encoded REQUEST body (readForm's MaxBytesReader), and a
// form value decodes to at most as many bytes as its encoding, so a body that passed it
// is within the database's ceiling too. (It moved here from the M7-06 panel's
// legaladmin.go with the screen.)
const maxLegalBody = 256 << 10

// legalVersionsLimit is the version list's one page: the newest 100 (op_read_legal_versions
// caps a page at 200). The page says when the list is that long.
const legalVersionsLimit = 100

// legalWriteTimeout bounds a publication and the refresh after it, which run detached from
// the request's cancellation (publishLegal): one INSERT through op_publish_legal and one
// read of the current texts -- ten seconds is far above either on a reachable database.
const legalWriteTimeout = 10 * time.Second

// legalPage is GET /operator/legal: the four editors, re-opened on the snapshot, and the
// version list.
//
// THE READ IS TWO DATABASE TRANSACTIONS (op_begin_read, then op_read_legal_versions --
// ADR 0021 §2 v), and the session budget counts it as two: sessionGate charged this
// request once, and the handler charges it once more before the read
// (TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget).
func (s *Surface) legalPage(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.legalSession(w, r)
	if !ok {
		return
	}
	if !s.spendSession(w, r, id) {
		return
	}
	// THE SNAPSHOT IS READ BEFORE THE LIST. A snapshot read first cannot be newer than a
	// list read after it, so a difference between the two means the snapshot is behind.
	// Read the other way round, a publication (and its refresh) landing between the two
	// reads would make the snapshot NEWER than the list: the view would refresh for
	// nothing and open the editor on a version the list does not show yet. Measured:
	// TestLegalPage_AVersionPublishedBetweenTheTwoReadsIsNotCalledBehind.
	published := s.texts.Published()
	versions, err := s.legalStore.LegalVersions(r.Context(), hash, db.LegalVersionsPage{Number: 1, Size: legalVersionsLimit})
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		// The session predicate passed in sessionGate and refused now: the session ended
		// in between. The sign-in, as the gate would answer.
		s.redirect(w, pathSignIn)
		return
	case err != nil:
		// internal/db's operatorErr reduces a PostgreSQL error to the call and its
		// SQLSTATE (TestOperatorErr_NeverCarriesAPgError); the hash is not an argument of
		// this line.
		s.log.ErrorContext(r.Context(), "operator: the legal version list could not be read", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemLegalUnreadable)
		return
	}
	// THE SCREEN HEALS A SNAPSHOT IT CAN SEE IS BEHIND. When a document's live version in
	// the list is not the text the snapshot serves (a refresh after a publication failed,
	// or another process published), this view refreshes the snapshot once -- a read of
	// legal_documents on the customer pool, under this request's context -- and renders
	// what that left. A refresh that fails leaves the editor's warning in place
	// (legalView, Behind); the next view tries again. Measured:
	// TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind. The refresh
	// can install a version NEWER than the list (a publication between the list's read and
	// it); the warning is for a snapshot published at an EARLIER time only, so that view
	// shows none (legalView; TestLegalPage_ASnapshotNewerThanTheListAfterTheHealIsNotCalledBehind)
	// -- unless the two versions share a published_at to the microsecond, where compare
	// cannot tell their order (compare's comment; the card's LB11).
	if snapshotDiffers(published, versions) {
		if err := s.texts.Refresh(r.Context()); err != nil {
			s.log.ErrorContext(r.Context(), "operator: the legal screen found the public snapshot behind and could not refresh it", "err", err)
		}
		published = s.texts.Published()
	}
	s.render(w, r, http.StatusOK, operatorpages.Legal(legalView(published, versions)))
}

// publishLegal is POST /operator/legal: one new version of one document.
//
// The refusals, each before any store call and each a fixed page with a way back:
// a request body over maxLegalBody (413), an unreadable form (400), a slug outside
// legal.Slugs (400), a body with no visible text (400). Then op_publish_legal; then the
// snapshot's refresh. POST -> 303 -> GET, so a reload does not publish twice -- a failed
// refresh included: it is logged and answered with the same 303, and the screen the
// redirect lands on says the public page is behind (legalPage, legalView).
//
// THE PUBLICATION AND THE REFRESH DO NOT DIE WITH THE CLIENT. They run under
// context.WithoutCancel(r.Context()) with legalWriteTimeout: a browser that closes the
// connection after pressing Publish (the ingress then cancels the request) would
// otherwise cancel the refresh after the version had committed, and the public page would
// stay on the previous text with nobody shown the failure. Measured:
// TestLegalPublish_AClientThatLeavesStillGetsThePublicationAndTheRefresh.
//
// THE TEXT IS TRIMMED AT ITS ENDS AND STORED AS TYPED IN BETWEEN (the M7-06 panel's
// rule): leading and trailing white space is a typing artefact; what is inside the text
// is never touched.
//
// WHAT THE LOG LINES HERE CARRY: the document, the operator's id, the length, and
// internal/db's error (the call and a SQLSTATE: operatorErr). The text and the session
// hash are not arguments of them, and the leak test's arms A31-A43 search both on the
// process log (TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor, G16 and G8).
func (s *Surface) publishLegal(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.legalSession(w, r)
	if !ok {
		return
	}
	if !s.readForm(w, r, maxLegalBody, problemLegalTooLarge) {
		return
	}
	slug := r.PostForm.Get("slug")
	if !legal.Valid(slug) {
		s.problem(w, r, http.StatusBadRequest, problemLegalUnknownDocument)
		return
	}
	body := strings.TrimSpace(r.PostForm.Get("body"))
	if !visibleText(body) {
		s.problem(w, r, http.StatusBadRequest, problemLegalEmpty)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), legalWriteTimeout)
	defer cancel()
	err := s.legalStore.PublishLegal(ctx, hash, slug, body)
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
		return
	case err != nil:
		s.log.ErrorContext(r.Context(), "operator: a legal text could not be published", "slug", slug, "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemLegalNotPublished)
		return
	}
	s.log.InfoContext(r.Context(), "operator published a legal text",
		"slug", slug, "admin_id", id.AdminID.String(), "bytes", len(body))
	// THE VERSION IS IN THE DATABASE WHATEVER HAPPENS NEXT. A failed refresh leaves this
	// replica's public page on the previous text; the screen the redirect lands on says so
	// and refreshes again (legalPage).
	if err := s.texts.Refresh(ctx); err != nil {
		s.log.ErrorContext(r.Context(), "operator: a legal text was published and the public snapshot could not be refreshed",
			"slug", slug, "err", err)
	}
	s.redirect(w, pathLegal)
}

// legalSession is the session sessionGate resolved and its hash for the store. Through
// mount both are in place; a route mounted outside the chain by mistake answers the
// sign-in.
func (s *Surface) legalSession(w http.ResponseWriter, r *http.Request) (operatorauth.Identity, string, bool) {
	id, ok := operatorOf(r)
	tok, hasToken := sessionTokenOf(r)
	if !ok || !hasToken {
		s.redirect(w, pathSignIn)
		return operatorauth.Identity{}, "", false
	}
	hash, err := s.auth.SessionHash(tok)
	if err != nil {
		s.redirect(w, pathSignIn)
		return operatorauth.Identity{}, "", false
	}
	return id, hash, true
}

// blankSymbol reports the symbols (category So) that render as nothing or as empty
// space, named one by one: U+2800 BRAILLE PATTERN BLANK, U+303F IDEOGRAPHIC HALF FILL
// SPACE and U+1D159 MUSICAL SYMBOL NULL NOTEHEAD. Each alone passes the dev database's [:space:]
// check (measured, en_US.utf8: '^[[:space:]]*$' is false for all three). U+FFFC OBJECT
// REPLACEMENT CHARACTER is NOT here: it stands for an embedded object and renders as a
// visible replacement glyph -- a decision, read from the Unicode names, not a rendering
// measured.
func blankSymbol(r rune) bool {
	switch r {
	case '\u2800', '\u303f', '\U0001d159':
		return true
	}
	return false
}

// visibleText reports whether body has a rune a reader can see: at least one letter,
// number, punctuation mark or symbol (Unicode categories L, N, P, S) that is not
// Other_Default_Ignorable_Code_Point (the Hangul fillers U+115F, U+1160, U+3164, U+FFA0
// are letters by category and render as nothing) and not one blankSymbol names.
//
// WHY THE RULE IS STATED POSITIVELY. The database refuses a body of `[:space:]` alone,
// and that class depends on the server's locale; Go's strings.TrimSpace removes
// Unicode white space. The OP-10A card measured (dev, en_US.utf8; Go 1.26.7) eight code
// points that pass both -- U+180E, U+200B, U+200C, U+200D, U+2060, U+2800, U+3164,
// U+FEFF -- so a body made of them published a blank-looking legal page. A rule that
// lists what is invisible would be a list to keep extending; this one names what
// counts. TestVisibleText_TheListedInvisibleBodiesAreRefused holds its cases: the
// eight, white space, controls, format characters, marks alone, the fillers, the three
// blank symbols -- refused; a letter, a digit, a punctuation mark, a symbol and U+FFFC,
// alone and around invisible ones -- accepted. A symbol outside blankSymbol's three that
// a font draws as nothing passes (the card's LB3).
func visibleText(body string) bool {
	for _, r := range body {
		if blankSymbol(r) || unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) {
			continue
		}
		if unicode.In(r, unicode.L, unicode.N, unicode.P, unicode.S) {
			return true
		}
	}
	return false
}

// liveVersion is the list's current version of slug, if the list holds it (the list is
// the newest legalVersionsLimit versions; an older current version is not in it).
func liveVersion(versions []db.LegalVersion, slug string) (db.LegalVersion, bool) {
	for _, x := range versions {
		if x.Slug == slug && x.Current {
			return x, true
		}
	}
	return db.LegalVersion{}, false
}

// compare places the snapshot's text of slug against the list's live version of it
// (live). differs: the snapshot has no text, or one published at another time, or of
// another length -- the view's reason to refresh. older: the snapshot has no text, or one
// published BEFORE the live version, or at the same time with another length -- the
// editor's reason to warn. A snapshot published later than the list's live version
// differs and is not older. A slug whose live version the list does not hold is neither.
//
// THE ORDER IS ONLY HALF THE DATABASE'S. ListPublishedLegalDocuments and
// op_read_legal_versions pick the current version by (published_at DESC, id DESC); a
// legal.Doc carries no id, so this compares published_at alone and, at an EQUAL
// published_at, uses the length as a stand-in for the id -- which is wrong both ways
// when two publications of one slug land on the same clock_timestamp() microsecond
// (op_publish_legal takes no per-slug lock): (i) a snapshot newer than the list by id,
// of another length, is called older and warns for one view; (ii) another text of the
// same length is neither differs nor older -- no refresh and no warning. The exact
// comparison needs the id in legal.Doc and (PublishedAt, ID) here; not written (the
// card's LB11).
func compare(published map[string]legal.Doc, versions []db.LegalVersion, slug string) (live db.LegalVersion, differs, older bool) {
	live, ok := liveVersion(versions, slug)
	if !ok {
		return db.LegalVersion{}, false, false
	}
	doc, has := published[slug]
	if !has {
		return live, true, true
	}
	sameLength := len(doc.Body) == int(live.BodyBytes)
	switch {
	case doc.PublishedAt.Before(live.PublishedAt):
		return live, true, true
	case doc.PublishedAt.Equal(live.PublishedAt):
		return live, !sameLength, !sameLength
	default:
		return live, true, false
	}
}

// snapshotDiffers reports whether any document's snapshot differs from the list.
func snapshotDiffers(published map[string]legal.Doc, versions []db.LegalVersion) bool {
	for _, slug := range legal.Slugs {
		if _, d, _ := compare(published, versions, slug); d {
			return true
		}
	}
	return false
}

// legalView builds the screen: an editor per document in legal.Slugs' order (so a
// document with no text yet has one), and the version list. An editor whose snapshot is
// older than the list's live version by compare's rule (an earlier published_at; at an
// equal one, a different length -- LB11) says so (Behind, and LiveAt: the live version's
// time): it opened on the public page's text, and publishing from it replaces the newer
// version.
func legalView(published map[string]legal.Doc, versions []db.LegalVersion) operatorpages.LegalView {
	v := operatorpages.LegalView{Limit: legalVersionsLimit}
	for _, slug := range legal.Slugs {
		d := operatorpages.LegalDoc{Slug: slug, Path: "/legal/" + slug}
		if doc, ok := published[slug]; ok {
			d.Published, d.PublishedAt, d.Body = true, utcStamp(doc.PublishedAt), doc.Body
		}
		if live, _, older := compare(published, versions, slug); older {
			d.Behind, d.LiveAt = true, utcStamp(live.PublishedAt)
		}
		v.Docs = append(v.Docs, d)
	}
	for _, x := range versions {
		row := operatorpages.LegalVersionRow{
			Path:        "/legal/" + x.Slug,
			PublishedAt: utcStamp(x.PublishedAt),
			Bytes:       strconv.Itoa(int(x.BodyBytes)) + " bytes",
			Current:     x.Current,
		}
		switch {
		case x.PublishedBy == db.LegalPublishedByOperator && x.PublisherName != nil:
			row.Publisher = *x.PublisherName
		case x.PublishedBy == db.LegalPublishedByOperator:
			row.Publisher = "operator"
		default:
			// ADR 0020 §7: a version the M7-06 panel wrote (or one the owner's SQL did).
			// Its publisher id is not returned (ADR 0021, OP-10 note md. 8).
			row.Publisher = "tenant admin (legacy)"
		}
		v.Versions = append(v.Versions, row)
	}
	return v
}

// utcStamp is a database time as the operator screens print it: UTC, to the minute.
func utcStamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }
