// Package legal owns Tappa's OWN published texts — the privacy policy, the terms,
// the company details and the cookie notice (M7-06, migration 00020).
//
// WHAT MAKES IT DIFFERENT FROM EVERY OTHER DOMAIN PACKAGE HERE: these documents
// belong to no tenant. internal/domain/tenant, ledger, review, billing and manual
// all take a tenant id and refuse without one; nothing in this package has one to
// take, because legal_documents has no tenant_id column. See migration 00020 for
// why, and docs/adr/0016 for the option that was measured and rejected.
//
// 🔴 THE PUBLIC PAGE READS A SNAPSHOT, NOT THE DATABASE, AND THAT IS THE WHOLE
// SHAPE OF THIS FILE. /legal/* is the product's second-most-public URL and
// internal/handler.Marketing has argued, in writing and with a reflection test,
// that it holds no pool: "A request here renders a fixed component tree and
// returns; it touches no pool, takes no lock and appends to no table", which is
// also why that surface carries no rate limiter. A per-request SELECT would have
// retired that argument and put an UNMETERED, UNAUTHENTICATED path onto the shared
// connection pool — the pool that check-in depends on. So the texts are read ONCE
// at boot and again after each publication, and served from memory.
//
// ⚠️ WHAT THE SNAPSHOT COSTS, stated rather than buried. It is a CACHE, and this
// repo has paid three times for second representations, so its bounds are written
// down:
//
//   - It is refreshed by the process that publishes. A SECOND process would serve
//     the previous text until it restarted. Tappa deploys to ONE VPS (CLAUDE.md §1)
//     so there is one process today; a second one makes this wrong, and
//     Store.Refresh is the one function that would have to be called on a timer.
//   - It is loaded at boot and a failure there is LOGGED, not fatal. A database
//     that is down at boot must not take the marketing site down with it, and the
//     pages degrade to exactly what they printed before this task existed: "this
//     text has not been published yet". That is honest in the wrong direction (it
//     under-claims), which is the correct direction to be wrong in.
package legal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// Slugs is the closed set of documents, in the order 00020's CHECK lists them.
//
// IT IS THE SAME CLOSED SET IN THREE PLACES and that is deliberate rather than
// duplicated: the column CHECK refuses a fifth value, this slice refuses one before
// the statement runs, and web/templates/pages.LegalPages gives each one a URL. A
// document with no page would be unreachable and a page with no slug would be
// unpublishable; internal/handler's TestLegalSlugs_AreExactlyTheDocumentsWithAPage
// holds all three together by deriving each from the others (it re-reads 00020's
// CHECK out of the migration file rather than retyping it).
var Slugs = []string{"privacy", "terms", "imprint", "cookies"}

// Valid reports whether slug names a document.
func Valid(slug string) bool {
	for _, s := range Slugs {
		if s == slug {
			return true
		}
	}
	return false
}

// Doc is one published text.
type Doc struct {
	Slug string
	// Body is what somebody typed, stored and returned VERBATIM. The form re-fills
	// itself from this, so a round trip through the screen must not silently
	// rewrite anybody's words.
	Body        string
	PublishedAt time.Time
	// Paragraphs is Body already split, computed ONCE when the snapshot is installed.
	//
	// 🔴 IT IS PRECOMPUTED BECAUSE THE READER IS AN UNMETERED PUBLIC PAGE. /legal/* is
	// deliberately rate-limit-free (handler.Marketing carries the argument), so any
	// per-request work there is work an anonymous caller can ask for as often as they
	// like. A security audit measured the splitting cost at 253 ms for a pathological
	// 9 MB body; the body is now bounded (handler.maxLegalBody) AND the split happens
	// at publication rather than at render, so the read is a map lookup.
	Paragraphs []string
}

// Paragraphs splits a body into the paragraphs a page renders, and it is the ONLY
// transformation applied to a legal text.
//
// 🔴 THE POINT IS THAT templ.Raw IS NOT REACHED. templ escapes every `{ expr }`
// through html.EscapeString, so a body containing <script> renders as text — but it
// does NOTHING to newlines, so a typed document would arrive as one run-on
// paragraph. The obvious fix (build <p> tags in Go and hand them to templ.Raw) is
// the one that must not be taken: measured on this repository, `templ.Raw` has ZERO
// call sites and NOT ONE of the twelve tests that scan .templ files would notice a
// thirteenth appearing — internal/handler/policies_test.go says so about its own
// blind spot in as many words. So the split returns STRINGS and the template loops
// over them, which keeps every character of a legal text inside templ's escaping.
//
// THE RULE, which a person typing into a textarea can predict: one or more blank
// lines start a new paragraph; a single newline inside a paragraph is a space,
// because a line wrapped by the textarea is not a new thought. Leading and trailing
// whitespace goes; an empty result means the body was blank, which 00020's CHECK
// already refuses at the column.
//
// ⚠️ IT IS NOT MARKDOWN AND MUST NOT BECOME MARKDOWN. A renderer would need a
// dependency (go.mod has five and no HTML sanitizer), and every markdown renderer
// worth having emits HTML — which is a templ.Raw with extra steps.
func Paragraphs(body string) []string {
	var out []string
	for _, block := range strings.Split(normalizeNewlines(body), "\n\n") {
		var lines []string
		for _, line := range strings.Split(block, "\n") {
			if t := strings.TrimSpace(line); t != "" {
				lines = append(lines, t)
			}
		}
		if len(lines) > 0 {
			out = append(out, strings.Join(lines, " "))
		}
	}
	return out
}

// normalizeNewlines makes CRLF and CR behave like LF, and collapses runs of three or
// more newlines to two.
//
// THE CRLF HALF IS NOT PEDANTRY: a browser submits a <textarea> with CRLF line
// endings (HTML's own spec says so), so a paragraph break typed on any platform
// arrives here as "\r\n\r\n" and a splitter that only knew "\n\n" would find no
// paragraphs at all in the ONE input this function exists to serve.
//
// 🔴 IT IS ONE PASS, AND THE FIRST VERSION WAS A `for strings.Contains(…)` LOOP THAT
// RE-SCANNED THE WHOLE STRING PER COLLAPSED NEWLINE. A security audit measured it:
// a body of 9 MB of blank lines cost 253 ms, against 9.5 ms for 9 MB of prose. The
// input is bounded now (handler.maxLegalBody) and the split is precomputed, so
// neither the size nor the frequency is what it was — but a quadratic loop over
// attacker-shaped input is the wrong thing to leave standing behind two other fixes.
func normalizeNewlines(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	runOfNewlines := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' {
			// CRLF counts once, not twice.
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			c = '\n'
		}
		if c != '\n' {
			runOfNewlines = 0
			b.WriteByte(c)
			continue
		}
		runOfNewlines++
		// Two is a paragraph break; anything beyond it is the same break.
		if runOfNewlines <= 2 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Database is the narrow slice of *db.DB this package needs, declared at the
// consumer (CLAUDE.md §7).
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Trail is the slice of audit.Recorder this package needs (§7).
//
// IT IS RecordTx AND NOT Record, the same choice internal/domain/signup makes and
// for the same reason. §4.3 requires the change to reach audit_log; RecordTx makes
// the row share the fate of the INSERT it describes, so there can be neither a
// published text with no trail entry nor a trail entry for a publication that
// rolled back. Record — its own transaction — is for the caller whose main
// transaction FAILED, and there is no such caller here.
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// ActionPublished is the audit action a publication writes.
//
// It follows the vocabulary the panel already uses — `<subject>.<past participle>`
// — so a reader scanning `action LIKE 'legal.%'` sees one shape.
const ActionPublished = "legal.published"

// PublishedDetail is the audit row's payload.
//
// 🔴 THE TEXT ITSELF IS NOT IN IT, AND THAT IS A SIZE DECISION RATHER THAN A SECRET
// ONE. A legal document is thousands of characters and audit_log.detail is jsonb on
// a table nothing may delete from; copying every version of every document into it
// would grow the trail without adding a fact, because legal_documents is
// APPEND-ONLY and already holds every version verbatim. What the row carries is
// enough to find that version: which document, how long the text was, and the
// row's own id.
//
// EXPLICIT EMPTIES, NO omitempty — a key that is absent cannot be told apart from a
// value that was empty, and this row exists so somebody can reconstruct what
// happened.
type PublishedDetail struct {
	Slug string `json:"slug"`
	// DocumentID is the legal_documents row. It is the join to the text.
	DocumentID string `json:"document_id"`
	// Bytes is the length of the body as stored. It is what lets a reader tell "the
	// policy was replaced" from "a typo was fixed" without reading both versions.
	Bytes int `json:"bytes"`
	// Replaced is true when this slug already had a text. The FIRST publication of a
	// document and a revision of one are different events to whoever is auditing.
	Replaced bool `json:"replaced"`
}

// Store reads and publishes the texts, and holds the snapshot the public pages
// serve from.
//
// ⚠️ IT STILL GOES THROUGH WithTenant EVEN THOUGH THE TABLE HAS NO TENANT. That is
// not a contradiction: WithTenant is this application's ONLY door to the pool
// (internal/db's package doc — handlers never see *pgxpool.Pool, so "did we set the
// tenant context?" is structural rather than remembered). The tenant it establishes
// is simply not consulted by legal_documents' policy, which is `USING (true)`. The
// alternative — a second, context-less pool accessor — is the "general bypass door"
// ADR 0002 forbids, and it would exist for a table that does not need it.
type Store struct {
	data  Database
	trail Trail
	snap  atomic.Pointer[map[string]Doc]
	// writing serialises publications.
	//
	// 🔴 WITHOUT IT THE SNAPSHOT CAN GO BACKWARDS, and a security audit named the
	// window: Publish reads every current text INSIDE its transaction and installs the
	// result AFTER commit, so two concurrent publications of the same document can
	// commit in one order and install in the other — leaving /legal/* serving the
	// SUPERSEDED text until the next publication or a restart. The database is right
	// either way (the table is append-only and the newest row wins on the next read);
	// it is the cache that would be wrong.
	//
	// A MUTEX IS AFFORDABLE HERE AND WOULD NOT BE ANYWHERE ELSE IN THIS PRODUCT. This
	// path runs a handful of times in the lifetime of a deployment, by one of two or
	// three people, and it holds the lock across one short transaction. Nothing on the
	// tap path, the panel's reads or the public pages touches it — Published() is a
	// lock-free atomic load.
	writing sync.Mutex
}

// NewStore builds a Store with an EMPTY snapshot, which is the state the pages
// rendered in before this package existed: every document unpublished.
func NewStore(data Database, trail Trail) (*Store, error) {
	if data == nil {
		return nil, errors.New("legal: nil database")
	}
	if trail == nil {
		// §4.3 is not optional and a Store that cannot write the trail must not be
		// constructible — otherwise the ONE path that publishes a legal text is the
		// one path with no record of who published it.
		return nil, errors.New("legal: nil audit trail")
	}
	s := &Store{data: data, trail: trail}
	empty := map[string]Doc{}
	s.snap.Store(&empty)
	return s, nil
}

// Published is the snapshot, and its signature is the guarantee.
//
// 🔴 NO context.Context, NO error, NO POINTER TO A POOL. A method that cannot be
// cancelled and cannot fail is a method that is not doing I/O, and
// internal/handler's TestLegalReader_CannotReachTheDatabase asserts exactly that by
// reflection over whatever the public surface holds — the same shape of proof
// TestMarketing_HandlerHoldsNoStatefulDependency already makes about the handler.
//
// The returned map MUST NOT be written to. It is shared by every reader; a
// publication replaces the pointer rather than mutating the map, so a reader that
// already holds one keeps a consistent view of the version it started with.
func (s *Store) Published() map[string]Doc {
	if m := s.snap.Load(); m != nil {
		return *m
	}
	return map[string]Doc{}
}

// readContext is the tenant context the BOOT read runs under.
//
// 🔴 IT IS A UUID THAT DELIBERATELY MATCHES NO TENANT, AND THAT IS A CONTAINMENT
// DECISION RATHER THAN A PLACEHOLDER. db.WithTenant is this application's only door
// to the pool (ADR 0002 forbids a general context-less accessor) and it refuses the
// nil uuid, so a read at start-up — where there is no request and therefore no
// natural tenant — has to name SOMETHING. The two candidates were a real tenant's
// id and this. A real one would run the callback with a live customer's rows
// visible, to read a table that does not belong to them; this one makes every
// RLS-scoped table in the database return zero rows for the duration, so the only
// thing reachable inside it is a table whose policy is `USING (true)` — which is
// exactly legal_documents and nothing else.
//
// ⚠️ IT IS NOT A "SYSTEM TENANT". Nothing is inserted under it, no FK points at it,
// and db/queries/audit.sql already records why this product refuses to invent one to
// paper over a missing owner. It exists for the length of one SELECT.
//
// The v4 shape is deliberate so it cannot collide with a generated id: bit 13 of the
// time_hi field is the version nibble and this value carries 4, while the remaining
// bits are a fixed, obviously-hand-written pattern.
var readContext = uuid.MustParse("00000000-0000-4000-8000-00000000f00d")

// Refresh re-reads every current text and replaces the snapshot.
//
// IT TAKES NO TENANT, AND THE ABSENCE IS THE POINT: there is no tenant this read
// could be scoped to, so there is no parameter for a caller to get wrong. See
// readContext for what it runs under instead.
func (s *Store) Refresh(ctx context.Context) error {
	var rows []store.ListPublishedLegalDocumentsRow
	err := s.data.WithTenant(ctx, readContext, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		rows, e = store.New(tx).ListPublishedLegalDocuments(ctx)
		return e
	})
	if err != nil {
		return fmt.Errorf("legal: refresh: %w", err)
	}
	s.set(rows)
	return nil
}

// set installs rows as the new snapshot.
//
// A row whose slug is not in Slugs is DROPPED rather than served. 00020's CHECK
// makes that unreachable through this application; it stays because the snapshot
// feeds a page that has to have a URL for what it renders, and a slug with no page
// has none.
func (s *Store) set(rows []store.ListPublishedLegalDocumentsRow) {
	next := make(map[string]Doc, len(rows))
	for _, r := range rows {
		if !Valid(r.Slug) {
			continue
		}
		next[r.Slug] = Doc{
			Slug:        r.Slug,
			Body:        r.Body,
			PublishedAt: r.PublishedAt,
			Paragraphs:  Paragraphs(r.Body),
		}
	}
	s.snap.Store(&next)
}

// ErrUnknownSlug is returned for a document this product does not have.
var ErrUnknownSlug = errors.New("legal: unknown document")

// ErrEmptyBody is returned for a body with no text in it.
//
// IT IS REFUSED IN GO AS WELL AS IN THE COLUMN, and the duplication is on purpose:
// the CHECK is what makes it impossible, this is what makes it EXPLAINABLE. A
// 23514 surfacing as "an internal error" would tell an operator who pasted
// whitespace nothing about what to do next.
var ErrEmptyBody = errors.New("legal: a document with no text is not a publication")

// Publish appends one version, records it, and installs the new snapshot.
//
// 🔴 FOUR THINGS IN ONE TRANSACTION, AND THE FOURTH IS WHY THERE IS NO SEPARATE
// REFRESH. The INSERT, the audit_log row (RecordTx — see Trail), and the read-back
// of every current text all run inside one db.WithTenant; the snapshot pointer is
// replaced only AFTER that transaction commits. So the memory the public pages
// serve from can never show a version the database rolled back, and a publication
// costs no second round trip to become visible.
//
// tenantID and actorID are the PUBLISHER'S OWN, taken from their resolved session.
// That is the whole of this path's §4.5 story: legal_documents needs no tenant
// (00020), audit_log's is NOT NULL and gets the publisher's, and no code here can
// name anybody else's — there is no parameter for one.
//
// The body is stored VERBATIM. Trimming happens on the way in (the handler) and
// splitting happens on the way out (Paragraphs); the column holds what was typed.
func (s *Store) Publish(ctx context.Context, tenantID, actorID uuid.UUID, slug, body string) (Doc, error) {
	s.writing.Lock()
	defer s.writing.Unlock()
	if !Valid(slug) {
		return Doc{}, fmt.Errorf("%w: %q", ErrUnknownSlug, slug)
	}
	if strings.TrimSpace(body) == "" {
		return Doc{}, ErrEmptyBody
	}
	// Read BEFORE the write so "was there already a text?" is a fact about the
	// world the publisher acted on, not about the row they just inserted.
	_, replaced := s.Published()[slug]

	var doc Doc
	var rows []store.ListPublishedLegalDocumentsRow
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		var actor *uuid.UUID
		if actorID != uuid.Nil {
			a := actorID
			actor = &a
		}
		row, e := q.PublishLegalDocument(ctx, store.PublishLegalDocumentParams{
			Slug: slug, Body: body, PublishedBy: actor,
		})
		if e != nil {
			return e
		}
		doc = Doc{
			Slug:        row.Slug,
			Body:        row.Body,
			PublishedAt: row.PublishedAt,
			Paragraphs:  Paragraphs(row.Body),
		}
		var actorPtr *uuid.UUID
		if actorID != uuid.Nil {
			a := actorID
			actorPtr = &a
		}
		if _, e = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID,
			ActorID:  actorPtr,
			Action:   ActionPublished,
			Target:   slug,
			Detail: PublishedDetail{
				Slug:       slug,
				DocumentID: row.ID.String(),
				Bytes:      len(row.Body),
				Replaced:   replaced,
			},
		}); e != nil {
			return e
		}
		rows, e = q.ListPublishedLegalDocuments(ctx)
		return e
	})
	if err != nil {
		return Doc{}, fmt.Errorf("legal: publish %q: %w", slug, err)
	}
	s.set(rows)
	return doc, nil
}
