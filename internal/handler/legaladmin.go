package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The OPERATOR's legal-text screen (M7-06): GET /admin/legal and POST /admin/legal.
//
// WHY IT EXISTS. M7-01 shipped /legal/privacy, /legal/terms, /legal/imprint and
// /legal/cookies as skeletons that print "Nothing on this page is in force" and a
// list of the facts each is waiting for, because none of those facts was in this
// repository. Three sessions later they still were not, and the reason was
// structural rather than anybody's forgetfulness: the only way to supply them was
// to edit Go source. This is the form.
//
// 🔴 THERE IS NO SUPER-ADMIN IN THIS PRODUCT AND THIS TASK DID NOT INVENT ONE.
// admin_users.role is a closed dictionary of ('owner','manager') (00006) and BOTH
// are tenant-scoped; the policy engine reads that column as actor:role, so a third
// value would be a value the engine has no rule for, on the one code path that
// decides how every tap in every business is judged. A separate operator login
// would be a second authentication surface — a second cookie, a second resolver, a
// second lockout story. Four documents justify neither. What ships is ONE EXTRA
// SCREEN for an identity that already exists, gated on an id allow-list this
// deployment configures (config.OperatorAdminIDs).
//
// 🔴 THE SCREEN NAMES NO TENANT BUT ITS OWN, AND THE FIRST VERSION OF THIS
// PARAGRAPH OVERSTATED THAT INTO A FALSEHOOD. It said the screen "touches no
// tenant's data, structurally", which a third-eye review disproved in one step:
// legalView calls a.chrome, and a.chrome calls a.queue.Pending(ctx, id.TenantID())
// — a real query, whose answer is the number on the Review tab of this very page.
//
// THE TRUE STATEMENT IS THE NARROW ONE, and it is the one the whole design rests
// on: the DOCUMENTS this screen reads and writes have no tenant_id at all
// (migration 00020), so the generated store types carry no TenantID field and there
// is no tenant for that path to name. The only tenant named anywhere on this screen
// — by the audit_log row it writes and by the panel chrome it renders — is the
// CALLER'S OWN, which is the same tenant every other panel request uses. A
// cross-tenant read is not something this code refuses to do; it is something it has
// no parameter to express.
//
// 🔴 THE GATE IS ASKED TWICE AND NEITHER ASKING SUBSTITUTES FOR THE OTHER. The tab
// is hidden from everybody who is not an operator (PanelSection.OperatorOnly) and
// the two handlers refuse on their own. That is the shape M6-12 measured for the
// billing section and locationactions.go for the removals: hiding a link is the
// courtesy, the server refusing the route is the guarantee, and a stale page, a
// shared link or a deliberate POST reaches the second one.

// panelTexts is the slice of internal/domain/legal.Store this package needs,
// declared HERE at the consumer (§7).
//
// 🔴 Published TAKES NO CONTEXT AND RETURNS NO ERROR, WHICH IS THE PUBLIC SURFACE'S
// WHOLE GUARANTEE. handler.Marketing has argued in writing — and asserted by
// reflection — that it holds nothing it could reach a database through, and that
// argument is what excuses /legal/* from the rate limiter every other surface
// carries. A method that cannot be cancelled and cannot fail is not doing I/O, so
// the public pages keep serving from memory and the shared connection pool stays
// out of reach of an unauthenticated URL. See legal.Store.Published.
type panelTexts interface {
	Published() map[string]legal.Doc
	Publish(ctx context.Context, tenantID, actorID uuid.UUID, slug, body string) (legal.Doc, error)
}

// maxLegalBody is the ceiling on the publish POST's body.
//
// 🔴 A BARE ParseForm INHERITS net/http's 10 MB, AND THIS ROUTE WRITES INTO A TABLE
// NOTHING CAN CLEAN UP. A security audit measured the unbounded shape: 1 MiB, 4 MiB
// and 9 MiB bodies were all accepted and STORED, and legal_documents takes no UPDATE
// and no DELETE from either role — so the only remedy for a filled table is a
// migration. With adminSessionLimit at 300 requests per ten minutes that is ~2.7 GB
// per session per window, from somebody who has merely signed in.
//
// ⚠️ AND THE SECOND HALF IS THE PUBLIC PAGE. A published body is rendered on
// /legal/*, which is deliberately unmetered (see handler.Marketing), so an oversized
// document turns into CPU on an anonymous GET. The audit measured 253 ms per request
// for a 9 MB body. Bounding the write is what bounds the read.
//
// 256 KiB IS GENEROUS FOR THE JOB. A long privacy policy is a few tens of kilobytes;
// this is an order of magnitude above the real thing and two below the default. The
// pattern is maxReviewBody's and checkinMaxBody's — the same shape every other
// mutating route in this product already uses, and NOT copying it here was the
// defect.
const maxLegalBody = 256 << 10

// legalHref is the operator screen. It is the SAME STRING pages.PanelSections gives
// the tab, and the route is mounted by ranging over that slice, so the link and the
// route cannot drift.
const legalHref = "/admin/legal"

// problemLegalNotOperator is what somebody who is not on the allow-list sees.
//
// 🔴 IT SAYS WHY WITHOUT SAYING WHO. §4.6 requires a refusal to state its reason,
// and this one does: the screen belongs to whoever runs this deployment. What it
// does NOT do is print the allow-list, or say whether it is empty — an address is
// personal data and "there is nobody configured" is a fact about the deployment
// that a signed-in customer has no business learning.
//
// IT DOES NOT SAY "ask an owner", WHICH THE BILLING REFUSAL DOES. That advice is
// right there because every business has an owner; here it would send somebody to a
// person who cannot help, because no role in their business grants this.
var problemLegalNotOperator = pages.ProblemView{
	Title:   "This screen is not part of your dashboard",
	Message: "It publishes Tappa's own privacy policy, terms and company details — the documents on tappa's public pages, not anything belonging to your business. It is open to the people who run this deployment. Nothing is wrong with your account.",
	Hint:    "Everything that belongs to your business is on the tabs above.",
}

// legalRefusalFor is problemLegalNotOperator with the caller's OWN admin id appended
// to the hint.
//
// 🔴 WHY SHOWING IT IS SAFE, AND WHY IT IS THE POINT. The allow-list is a list of
// admin_users.id values, and the person who runs this deployment has to get theirs
// into an env file somehow. The alternative is a database session — which is exactly
// the "go and dig it out yourself" this whole task exists to end. An admin id is not
// a credential: it is the PK of the caller's own row, it is already in every
// audit_log entry that admin caused, and holding it gets nobody past the password.
//
// ⚠️ IT IS THE CALLER'S OWN AND NOBODY ELSE'S. The page never prints the allow-list,
// never says whether it is empty, and never names another admin — those are facts
// about the deployment and about other people, and a refused caller is owed neither.
func legalRefusalFor(id httpx.AdminIdentity) pages.ProblemView {
	v := problemLegalNotOperator
	if id.Live() && id.Admin.AdminUserID != uuid.Nil {
		v.Hint += " If you run this deployment, this account's id is " +
			id.Admin.AdminUserID.String() + " — that is what TAPPA_OPERATOR_ADMIN_IDS takes."
	}
	return v
}

// problemLegalUnavailable is what a failed publication renders.
var problemLegalUnavailable = pages.ProblemView{
	Title:   "That text was not published",
	Message: "The database refused the change, so nothing was written and the page still shows what it showed before.",
	Hint:    "Try again in a moment. If it keeps failing, the server log has the reason.",
}

// mayPublishLegal reports whether this session may reach the operator screen.
//
// 🔴 IT KEYS ON admin_users.id, AND THE FIRST VERSION KEYED ON THE EMAIL ADDRESS.
// A security audit broke that version end to end and the break is the reason this
// function is worth reading twice: admin_users.email is unique only WITHIN a tenant
// (admin_users_tenant_email_key is UNIQUE (tenant_id, email)), /signup is public,
// and nothing verifies an address. So anybody who knew an allow-listed address could
// register their own business with that same address and sign in as its owner —
// measured: a stranger tenant holding the operator's address got 200 on GET and 303
// on POST, and rewrote the published privacy policy. AN ALLOW-LIST IS WORTH THE
// UNIQUENESS OF ITS KEY, and in this schema an address has none.
//
// admin_users.id is the PRIMARY KEY, globally unique, and assigned by the DATABASE
// (gen_random_uuid()); internal/domain/signup reads it back off the INSERT rather
// than choosing it. There is no form, header or registration that lets a caller
// declare it, which is exactly the property the old key lacked.
//
// 🔴 IT IS FAIL-CLOSED AND THE EMPTY LIST IS THE PROOF, NOT AN EDGE CASE. There is
// no branch here that reads "if nobody is configured, let everybody in" — the loop
// over an empty slice simply never matches, so an unset TAPPA_OPERATOR_ADMIN_IDS
// admits nobody. That is the direction a misconfiguration has to fail in: the cost
// of being too closed is that an operator adds one line to an env file, and the cost
// of being too open is that every admin of every customer can rewrite Tappa's
// privacy policy.
//
// 🔴 THE id COMES FROM THE RESOLVED SESSION. adminauth.Resolved.AdminUserID is
// returned by TouchAdminSession during session resolution, so this costs no extra
// query and cannot be supplied by the caller.
//
// ⚠️ THE NIL uuid NEVER MATCHES. id.Live() is false without a resolved session, and
// config refuses a nil entry at startup — two independent halves, because either one
// alone would let an unresolved identity through if the list ever held uuid.Nil.
func mayPublishLegal(id httpx.AdminIdentity, allow []uuid.UUID) bool {
	if !id.Live() || id.Admin.AdminUserID == uuid.Nil {
		return false
	}
	for _, want := range allow {
		if want != uuid.Nil && want == id.Admin.AdminUserID {
			return true
		}
	}
	return false
}

// legalSection serves GET /admin/legal.
//
// 🔴 THE GATE RUNS BEFORE ANY READ, so a refused request costs nothing — the same
// ordering billingSection uses for its role check.
//
// ⚠️ "COSTS NOTHING" IS ABOUT THE REFUSED REQUEST, NOT ABOUT THE PAGE. An earlier
// version of this comment said this screen's read "is a map lookup rather than a
// query" and that was wrong: the DOCUMENTS come from a map (the snapshot), but the
// page also renders the panel chrome, which reads the caller's own review-queue
// count. Refusing before either is what makes the refusal free.
//
// 🔴 A REFUSED GET WRITES NO audit_log ROW, and that is the rule M6-12 recorded
// rather than an omission: a read path does not write, and the process log is enough
// for a page nobody could have changed anything from. The POST below is different
// and writes one.
func (a *AdminAuth) legalSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayPublishLegal(id, a.operators) {
		// The address is NOT logged. The admin_user_id is what an operator needs to
		// find the row, and it is not personal data on its own.
		a.log.Warn("panel: the legal-text screen was reached by somebody who is not an operator",
			"actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, legalRefusalFor(id))
		return
	}
	v := a.legalView(r)
	v.Saved = legalSavedTitle(r.URL.Query().Get("saved"))
	v.Problem = legalProblemSentence(r.URL.Query().Get("problem"))
	a.render(w, r, http.StatusOK, pages.AdminLegal(v))
}

// legalPublish serves POST /admin/legal.
func (a *AdminAuth) legalPublish(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayPublishLegal(id, a.operators) {
		a.refuseLegalByAllowList(w, r, id)
		return
	}
	// The bound is applied to the BODY, before ParseForm reads it into memory — the
	// shape every other mutating panel route uses. MaxBytesReader makes ParseForm
	// return an error rather than allocating, so an oversized document costs a
	// sentence instead of a permanent row.
	r.Body = http.MaxBytesReader(w, r.Body, maxLegalBody)
	if err := r.ParseForm(); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			a.log.Warn("panel: a legal text was too long to publish",
				"actor_id", id.Admin.AdminUserID, "limit_bytes", maxLegalBody)
			a.redirect(w, legalReturn("", "too-long"))
			return
		}
		a.log.Warn("panel: the legal-text form could not be read", "err", err)
		a.redirect(w, legalReturn("", "unreadable"))
		return
	}
	slug := strings.TrimSpace(r.PostFormValue("slug"))
	// 🔴 THE SLUG IS CHECKED AGAINST THE CLOSED SET, NOT AGAINST THE FORM. The hidden
	// field is a convenience for the browser; legal.Valid is the authority, and
	// 00020's CHECK is the authority under that. A caller who posts a fifth slug gets
	// a sentence, not a row.
	if !legal.Valid(slug) {
		a.log.Warn("panel: the legal-text form named a document that does not exist",
			"actor_id", id.Admin.AdminUserID)
		a.redirect(w, legalReturn("", "unknown-document"))
		return
	}
	// 🔴 TRIMMED ON THE WAY IN, STORED VERBATIM AFTER THAT. Leading and trailing
	// whitespace is a typing artefact and not part of anybody's legal text; what is
	// INSIDE the text is never touched, so a document that uses indentation keeps it.
	body := strings.TrimSpace(r.PostFormValue("body"))
	if body == "" {
		// This is the most likely mistake on this screen — press Publish on the
		// document you have not written yet — so it gets its own sentence rather than
		// a generic failure.
		a.redirect(w, legalReturn(slug, "empty"))
		return
	}

	// The tenant and the actor are the PUBLISHER'S OWN, from the session. There is no
	// field on this form for either and there could not be: the id below was resolved
	// from a signed cookie against admin_users, which is the only thing
	// audit_log.actor_id may hold.
	_, err := a.texts.Publish(r.Context(), id.TenantID(), id.Admin.AdminUserID, slug, body)
	switch {
	case err == nil:
	case errors.Is(err, legal.ErrEmptyBody), errors.Is(err, legal.ErrUnknownSlug):
		// The domain refused what the boundary let through, which means the two
		// disagree. It is reported as a form failure — the operator can still fix it —
		// and logged as the inconsistency it is.
		a.log.Error("panel: the legal-text writer refused input this handler accepted",
			"slug", slug, "err", err)
		a.redirect(w, legalReturn(slug, "empty"))
		return
	default:
		// §4.6: say the write failed. Never fall through to a page that looks saved.
		a.log.Error("panel: a legal text could not be published", "slug", slug, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemLegalUnavailable)
		return
	}
	// POST -> 303 -> GET, so a refresh does not re-publish. Every mutating route in
	// this panel ends this way.
	a.redirect(w, legalReturn(slug, ""))
}

// refuseLegalByAllowList records a refused publication and answers it.
//
// 🔴 UNLIKE THE GET, THIS ONE WRITES TO audit_log (§4.3, §4.6), and the difference
// is the act rather than the outcome. A refused GET is somebody looking at a page;
// a refused POST is somebody ATTEMPTING TO REWRITE THE PRIVACY POLICY OF EVERY
// BUSINESS ON THIS DEPLOYMENT, and that attempt must survive a log rotation. The
// shape follows locationactions.go's refuseRemovalByRole, which is the panel's
// existing refusal-with-a-trail.
//
// 🔴 THE ROW GOES TO THE CALLER'S OWN TENANT, WHICH IS THE ONLY TENANT THIS PATH MAY
// NAME. audit_log.tenant_id is NOT NULL (00005) and there is no Tappa tenant to
// attribute it to; the tenant whose admin made the attempt is both available and
// correct, and it is what that tenant's own owner would want to see.
//
// 🔴 IT IS audit.Record AND NOT RecordTx, WHICH IS THE OPPOSITE OF THE PUBLISH PATH.
// Nothing is being written — that is the point — so there is no surrounding
// transaction for the row to share a fate with. This is the case audit.Record's own
// comment describes: the caller who most needs a trail row is the one whose act did
// NOT happen. The successful path uses RecordTx, inside legal.Store.Publish, because
// there the row is only true if the INSERT is.
//
// AND A FAILED AUDIT WRITE DOES NOT CHANGE THE ANSWER: the refusal already happened
// on the line above, and turning a trail outage into a 500 would report OUR failure
// as a verdict on THEIR request. a.record logs loudly and returns.
func (a *AdminAuth) refuseLegalByAllowList(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity) {
	// The address is deliberately absent from both the log line and the audit row.
	a.log.Warn("panel: a legal text publication was refused: this admin is not an operator",
		"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
	a.record(r.Context(), auditLegalRefused(id))
	a.renderProblem(w, r, http.StatusForbidden, legalRefusalFor(id))
}

// ActionLegalPublishRefused marks an attempt to publish by somebody who is not on
// the allow-list. It takes the `<subject>.<verb>_<refusal>` shape the panel's other
// refusals use, so `action LIKE 'legal.%'` distinguishes it from
// legal.ActionPublished at a glance.
const ActionLegalPublishRefused = "legal.publish_refused"

// refusedLegalDetail is the audit row's payload for a refused publication.
//
// 🔴 §4.7: THERE IS NO FIELD HERE A SECRET OR AN ADDRESS COULD TRAVEL IN. Not the
// email that failed the check — that is personal data and putting it in an
// append-only table nobody may delete from would be a disclosure with no expiry —
// not the allow-list, not its size (which is a fact about the deployment), and not
// the body that was posted. What it carries is the act, the role the actor held and
// the document that was aimed at.
//
// 🔴 EXPLICIT EMPTIES, NO omitempty — a key that is absent cannot be told apart from
// a value that was empty, and this row exists so somebody can reconstruct what was
// attempted.
type refusedLegalDetail struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
	// Role is the role the actor actually held. It is their own attribute and it is
	// recorded to make the row readable, NOT because any role would have passed:
	// this gate does not read the role at all.
	Role string `json:"role"`
}

// auditLegalRefused builds the event. It is a function rather than an inline
// literal so the refusal path and its test name the same thing.
func auditLegalRefused(id httpx.AdminIdentity) audit.Event {
	return audit.Event{
		// §4.5: the tenant is the SESSION's, never the request's.
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionLegalPublishRefused,
		Target:   "legal",
		Detail: refusedLegalDetail{
			Outcome: "refused",
			Reason:  "publishing Tappa's own legal texts is reserved for this deployment's operators",
			Role:    id.Admin.Role,
		},
	}
}

// legalView builds the screen from the snapshot and pages.LegalPages.
//
// 🔴 THE ROWS COME FROM LegalPages RATHER THAN FROM THE SNAPSHOT'S KEYS, so the
// screen shows every document that HAS a page — including the ones with no text,
// which are precisely the ones somebody has to write. Driving it off the snapshot
// would have produced an empty screen on the day it was needed most.
func (a *AdminAuth) legalView(r *http.Request) pages.LegalAdminView {
	published := a.texts.Published()
	v := pages.LegalAdminView{
		PanelChrome: a.chrome(r, pages.TabLegal),
		PostHref:    legalHref,
	}
	for _, p := range pages.LegalPages {
		slug := legalSlugOf(p.Path)
		e := pages.LegalEditor{
			Slug:  slug,
			Title: p.Title,
			Lede:  p.Lede,
			Needs: p.Needs,
		}
		if d, ok := published[slug]; ok {
			e.Published = true
			e.Body = d.Body
			e.PublishedAt = d.PublishedAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		v.Docs = append(v.Docs, e)
	}
	return v
}

// legalSlugOf turns "/legal/privacy" into "privacy".
//
// 🔴 THE SLUG IS DERIVED FROM THE PATH RATHER THAN STORED BESIDE IT, which is what
// keeps the database's closed set and the router's closed set from becoming two
// lists that drift. pages.LegalPages owns the URL; 00020's CHECK owns the value; this
// is the one line that says they are the same thing, and
// TestLegalSlugs_AreExactlyTheDocumentsWithAPage fails if any pair stops matching.
func legalSlugOf(path string) string {
	return strings.TrimPrefix(path, "/legal/")
}

// legalReturn is the address a POST redirects to.
//
// The words are OURS and come from closed lists below; nothing a caller types
// reaches the query string, let alone the page.
func legalReturn(saved, problem string) string {
	switch {
	case problem != "":
		return legalHref + "?problem=" + problem
	case saved != "":
		return legalHref + "?saved=" + saved
	default:
		return legalHref
	}
}

// legalSavedTitle turns the `saved` word into the document's own title, or "".
//
// 🔴 IT IS MATCHED AGAINST THE CLOSED SET AND THE STRING THE PAGE PRINTS IS OURS, so
// a confirmation banner is not something somebody can make an operator see with
// arbitrary text in it. An unknown word produces NO banner rather than a generic
// one — a banner that fires for any junk in the query string is a banner an attacker
// can aim.
func legalSavedTitle(raw string) string {
	slug := strings.TrimSpace(raw)
	if !legal.Valid(slug) {
		return ""
	}
	for _, p := range pages.LegalPages {
		if legalSlugOf(p.Path) == slug {
			return p.Title
		}
	}
	return ""
}

// legalProblemWords is the closed list of refusal words this screen understands.
var legalProblemWords = []string{"empty", "unknown-document", "unreadable", "too-long"}

// legalProblemSentence turns a query word into the sentence the page prints, or ""
// when there is nothing to say. Same closed-list discipline as policyProblem.
func legalProblemSentence(raw string) string {
	switch oneOfWords(strings.TrimSpace(raw), legalProblemWords...) {
	case "empty":
		return "There was no text in the box, so nothing was published and the page still shows its placeholder. A document with nothing in it would say less than the placeholder does."
	case "unknown-document":
		return "That form named a document this product does not have. Nothing was written."
	case "too-long":
		return "That text is longer than this screen accepts, so nothing was written. A legal document runs to tens of kilobytes; the limit is 256 KiB. If the text really is that long, it needs to be split across the documents it belongs to."
	case "unreadable":
		return "The form could not be read, so nothing was written."
	default:
		return ""
	}
}
