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
//   - It is refreshed by the process that publishes: since M10 OP-10 (phase B) the
//     platform operator's /operator/legal calls Store.Refresh after op_publish_legal
//     (ADR 0020 §7, one replica). A SECOND process would serve the previous text
//     until it restarted. Tappa deploys to ONE VPS (CLAUDE.md §1) so there is one
//     process today; a second one makes this wrong, and Store.Refresh is the one
//     function that would have to be called on a timer.
//   - THIS PACKAGE NO LONGER WRITES. The M7-06 panel's Store.Publish (an INSERT on
//     tappa_app's pool plus an audit_log row) went with the panel's legal screen:
//     00027 revoked tappa_app's INSERT on legal_documents, and the one writer is the
//     operator's op_publish_legal (internal/db.PublishLegal), whose audit row is in
//     operator_audit_log. What is left here is the snapshot and the read that fills it.
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
	// Body is what somebody typed, stored and returned VERBATIM. The operator's
	// legal screen re-opens its editor on this, so a round trip through the screen
	// must not silently rewrite anybody's words.
	Body        string
	PublishedAt time.Time
	// Paragraphs is Body already split, computed ONCE when the snapshot is installed.
	//
	// 🔴 IT IS PRECOMPUTED BECAUSE THE READER IS AN UNMETERED PUBLIC PAGE. /legal/* is
	// deliberately rate-limit-free (handler.Marketing carries the argument), so any
	// per-request work there is work an anonymous caller can ask for as often as they
	// like. A security audit measured the splitting cost at 253 ms for a pathological
	// 9 MB body; the body is now bounded (256 KiB: op_publish_legal's octet_length
	// check, and internal/handler/operator's maxLegalBody) AND the split happens
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
// input is bounded now (256 KiB, op_publish_legal) and the split is precomputed, so
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

// Store holds the snapshot the public pages serve from, and the read that fills it.
//
// ⚠️ IT STILL GOES THROUGH WithTenant EVEN THOUGH THE TABLE HAS NO TENANT. That is
// not a contradiction: WithTenant is this application's ONLY door to the pool
// (internal/db's package doc — handlers never see *pgxpool.Pool, so "did we set the
// tenant context?" is structural rather than remembered). The tenant it establishes
// is simply not consulted by legal_documents' policy, which is `USING (true)`. The
// alternative — a second, context-less pool accessor — is the "general bypass door"
// ADR 0002 forbids, and it would exist for a table that does not need it.
type Store struct {
	data Database
	snap atomic.Pointer[map[string]Doc]
	// refreshing serialises Refresh's read-and-install.
	//
	// 🔴 WITHOUT IT THE SNAPSHOT CAN GO BACKWARDS. Two refreshes that overlap -- two
	// operators publishing at once, each refreshing after its own publication -- can
	// READ in one order and INSTALL in the other, leaving /legal/* serving the
	// superseded text until the next refresh or a restart (the M7-06 security audit
	// named this window on the old publish path; the read-then-install shape is the
	// same). Held across the read AND the install, a refresh that starts later reads a
	// later committed state and installs it later. The database is right either way (the
	// table is append-only and the newest row wins on the next read); it is the cache
	// that would be wrong.
	//
	// WHAT IS MEASURED IS HALF OF THAT: two refreshes never READ at once
	// (TestRefresh_TwoRefreshesNeverReadAtOnce). That the INSTALL is inside the lock too
	// -- the half that orders the installs -- is not measured: a lock released between
	// the read and the install leaves that test green (OP-10 phase B, round 2), and
	// pausing a refresh between the two needs a hook this type does not have. It is held
	// by reading Refresh, whose first two statements are the Lock and its deferred
	// Unlock.
	//
	// A MUTEX IS AFFORDABLE HERE. Refresh runs at boot, after a publication and when
	// the operator's legal screen finds the snapshot behind the version list -- a
	// handful of times in the lifetime of a deployment -- and holds the lock across one
	// short read. Nothing on the tap path, the panel's reads or the public pages
	// touches it: Published() is a lock-free atomic load.
	refreshing sync.Mutex
}

// NewStore builds a Store with an EMPTY snapshot, which is the state the pages
// rendered in before this package existed: every document unpublished.
func NewStore(data Database) (*Store, error) {
	if data == nil {
		return nil, errors.New("legal: nil database")
	}
	s := &Store{data: data}
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
// refresh replaces the pointer rather than mutating the map, so a reader that
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
	s.refreshing.Lock()
	defer s.refreshing.Unlock()
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
