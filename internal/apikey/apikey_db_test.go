package apikey_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
)

// TestApiKeyAuth_RevokedIs401ClosedIs403 is plan §5 API-3's "401/403" (ADR 0028 §3): the seventh
// resolver CARRIES revoked_at and api_enabled_at and the application decides on every request,
// with no cache -- one Authenticator, one key, the state changed between requests:
//
//	the business's API closed (never opened) -> 403 ErrAPIClosed
//	the operator opens it                    -> the key's Principal (its tenant, its id)
//	the operator closes it                   -> 403 on the very next request
//	opened again                             -> through again
//	the owner revokes the key                -> 401 ErrRevoked
//	revoked AND closed                       -> 401 (a revoked key is no credential at all)
//	a well-shaped key nobody issued          -> 401 ErrUnknown
//	the shape wrong                          -> 401 ErrMalformed
//
// and StatusOf maps each to its status, a database failure to 500.
func TestApiKeyAuth_RevokedIs401ClosedIs403(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	s := newService(t, d, apikey.LivePrefix)
	b := newBusiness(t, d)

	setAPI(t, b.tenant, true)
	created, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "Payroll"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	header := "Bearer " + created.Key.RevealToTheOwnerOnce()

	step := func(what string, wantErr error, wantStatus int) {
		t.Helper()
		p, err := s.authenticate(ctx, header)
		if !errors.Is(err, wantErr) || (wantErr == nil && err != nil) {
			t.Fatalf("%s: %v, want %v", what, err, wantErr)
		}
		if wantErr == nil {
			if p.TenantID != b.tenant || p.KeyID != created.ID {
				t.Fatalf("%s: principal %v/%v, want the key's tenant and id", what, p.TenantID, p.KeyID)
			}
			return
		}
		if got := apikey.StatusOf(err); got != wantStatus {
			t.Fatalf("%s: status %d, want %d", what, got, wantStatus)
		}
	}
	step("open", nil, 0)
	setAPI(t, b.tenant, false)
	step("closed by the operator", apikey.ErrAPIClosed, http.StatusForbidden)
	setAPI(t, b.tenant, true)
	step("opened again", nil, 0)
	if _, err := s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: b.tenant, ActorID: b.owner, KeyID: created.ID}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	step("revoked", apikey.ErrRevoked, http.StatusUnauthorized)
	setAPI(t, b.tenant, false)
	step("revoked and closed", apikey.ErrRevoked, http.StatusUnauthorized)

	// A business whose API was never opened: its key (created while open elsewhere is impossible
	// -- Create refuses a closed API -- so the gate is closed AFTER creation) is 403.
	b2 := newBusiness(t, d)
	setAPI(t, b2.tenant, true)
	c2, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b2.tenant, ActorID: b2.owner, Name: "ERP"})
	if err != nil {
		t.Fatal(err)
	}
	setAPI(t, b2.tenant, false)
	if _, err := s.authenticate(ctx, "Bearer "+c2.Key.RevealToTheOwnerOnce()); !errors.Is(err, apikey.ErrAPIClosed) {
		t.Fatalf("a live key of a closed business: %v, want ErrAPIClosed", err)
	}

	unknown := apikey.LivePrefix + strings.Repeat("Q", 42) + "A"
	if _, err := s.authenticate(ctx, "Bearer "+unknown); !errors.Is(err, apikey.ErrUnknown) || apikey.StatusOf(err) != http.StatusUnauthorized {
		t.Fatalf("an unknown key: %v", err)
	}
	if _, err := s.authenticate(ctx, "Bearer nope"); !errors.Is(err, apikey.ErrMalformed) || apikey.StatusOf(err) != http.StatusUnauthorized {
		t.Fatalf("a malformed key: %v", err)
	}
	if apikey.StatusOf(errors.New("the database is down")) != http.StatusInternalServerError {
		t.Fatal("a database failure is not a 500")
	}
}

// TestApiKeyAuth_WrongPrefixNeverReachesTheDatabase is K-10's "yanlış önek DB'ye gitmeden 401",
// measured with a counter on the resolver: in a production-like deployment (tt_live_) the
// sandbox's prefix, no prefix, one character short or long, a character outside base64url, a
// non-canonical last character and a missing or foreign scheme are all ErrMalformed (401) with 0
// resolver calls; the CONTROL, a well-shaped key nobody issued, reaches it exactly once. And the
// other way: a sandbox Authenticator never resolves a tt_live_ key.
//
// MUTANT (measured): Check's shape test removed (every value hashed and resolved) -> the
// malformed values reach the resolver and answer ErrUnknown instead of ErrMalformed; red.
func TestApiKeyAuth_WrongPrefixNeverReachesTheDatabase(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	live := newService(t, d, apikey.LivePrefix)
	body := strings.Repeat("Q", 42) + "A"
	for what, header := range map[string]string{
		"the sandbox prefix":     "Bearer " + apikey.SandboxPrefix + body,
		"no prefix":              "Bearer " + body,
		"one short":              "Bearer " + apikey.LivePrefix + body[1:],
		"one long":               "Bearer " + apikey.LivePrefix + body + "A",
		"a dot":                  "Bearer " + apikey.LivePrefix + "." + body[1:],
		"non-canonical last":     "Bearer " + apikey.LivePrefix + body[:42] + "B",
		"Basic scheme":           "Basic " + apikey.LivePrefix + body,
		"no scheme":              apikey.LivePrefix + body,
		"empty header":           "",
		"the upper-case prefix":  "Bearer TT_LIVE_" + body,
		"an operator's cookie":   "Bearer " + strings.Repeat("A", 43),
		"two keys, one header":   "Bearer " + apikey.LivePrefix + body + "," + apikey.LivePrefix + body,
		"a newline in the value": "Bearer " + apikey.LivePrefix + body[:20] + "\n" + body[20:],
	} {
		_, err := live.authenticate(ctx, header)
		if !errors.Is(err, apikey.ErrMalformed) || apikey.StatusOf(err) != http.StatusUnauthorized {
			t.Errorf("%s: %v, want ErrMalformed (401)", what, err)
		}
	}
	if n := live.resolver.calls.Load(); n != 0 {
		t.Fatalf("%d malformed credentials reached the resolver, want 0", n)
	}
	if _, err := live.authenticate(ctx, "Bearer "+apikey.LivePrefix+body); !errors.Is(err, apikey.ErrUnknown) {
		t.Fatalf("CONTROL: a well-shaped unknown key: %v, want ErrUnknown", err)
	}
	if n := live.resolver.calls.Load(); n != 1 {
		t.Fatalf("CONTROL: a well-shaped key reached the resolver %d times, want 1", n)
	}

	sandbox := newService(t, d, apikey.SandboxPrefix)
	if _, err := sandbox.authenticate(ctx, "Bearer "+apikey.LivePrefix+body); !errors.Is(err, apikey.ErrMalformed) || sandbox.resolver.calls.Load() != 0 {
		t.Fatalf("the sandbox resolved a live key: %v, %d calls", err, sandbox.resolver.calls.Load())
	}
}

// TestApiKeyAuth_NothingLogged is ADR 0028 §3's "asla loglanmaz" for this package, with canaries:
// a real key (created through the panel act), its keyed hash and the Authorization header carrying
// it never appear in -- every error the chain returns, for each outcome (ok, unknown, revoked,
// closed, malformed), with %v, %+v and %#v; the Token, the Checked hash and the Principal through
// fmt, slog (text and JSON) and encoding/json, alone and inside a caller's struct with an
// unexported field; or any audit_log row of the business (action, target, detail). The CONTROL: the
// same renderings of a plain string do carry the value, and the audit rows do carry the key's id.
func TestApiKeyAuth_NothingLogged(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	s := newService(t, d, apikey.LivePrefix)
	b := newBusiness(t, d)
	setAPI(t, b.tenant, true)
	created, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "Canary"})
	if err != nil {
		t.Fatal(err)
	}
	raw := created.Key.RevealToTheOwnerOnce()
	header := "Bearer " + raw
	hash := hashOf(t, s.macKey, raw)
	canaries := map[string]string{"the key": raw, "the key's body": raw[len(apikey.LivePrefix):], "the hash": hash, "the header": header}

	var texts []string
	collect := func(what string, v any) {
		texts = append(texts, what+": "+fmt.Sprintf("%v | %+v | %#v", v, v, v))
	}
	tok, _ := apikey.ParseBearer(header)
	checked, err := s.auth.Check(tok)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.auth.Resolve(ctx, checked)
	if err != nil {
		t.Fatal(err)
	}
	type callerState struct {
		tok     apikey.Token
		checked apikey.Checked
		who     apikey.Principal
		label   string
	}
	st := callerState{tok: tok, checked: checked, who: p, label: "control-label"}
	collect("token", tok)
	collect("checked", checked)
	collect("principal", p)
	collect("caller state", st)
	collect("created", created)
	var sl bytes.Buffer
	slog.New(slog.NewTextHandler(&sl, nil)).Info("x", "tok", tok, "checked", checked, "state", st)
	slog.New(slog.NewJSONHandler(&sl, nil)).Info("x", "tok", tok, "checked", checked, "created", created)
	texts = append(texts, "slog: "+sl.String())
	js, err := json.Marshal(map[string]any{"tok": tok, "checked": checked, "created": created})
	if err != nil {
		t.Fatal(err)
	}
	texts = append(texts, "json: "+string(js))

	// Every outcome's error.
	if _, err := s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: b.tenant, ActorID: b.owner, KeyID: created.ID}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{header, "Bearer " + apikey.LivePrefix + strings.Repeat("Q", 42) + "A", header[:len(header)-1], header + " "} {
		_, err := s.authenticate(ctx, h)
		collect("chain error", err)
	}
	setAPI(t, b.tenant, false)
	_, err = s.authenticate(ctx, header)
	collect("closed error", err)
	_, err = s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "Closed"})
	collect("create on a closed API", err)

	rows := auditRows(t, d, b.tenant)
	if countAction(rows, apikey.ActionCreated, created.ID.String()) != 1 || countAction(rows, apikey.ActionRevoked, created.ID.String()) != 1 {
		t.Fatalf("CONTROL: the trail does not hold the key's created and revoked rows: %+v", rows)
	}
	for _, r := range rows {
		texts = append(texts, "audit: "+r.action+" "+r.target+" "+r.detail)
	}

	all := strings.Join(texts, "\n")
	for what, canary := range canaries {
		if strings.Contains(all, canary) {
			t.Errorf("%s appears in a rendering, an error or the trail", what)
		}
	}
	if !strings.Contains(all, "control-label") || !strings.Contains(all, created.ID.String()) {
		t.Fatal("CONTROL: the renderings lost their non-secret fields; the negative assertions prove nothing")
	}
	if !strings.Contains(fmt.Sprintf("%+v", struct{ s string }{raw}), raw) {
		t.Fatal("CONTROL: a plain string field does not render the value")
	}
}

// TestApiKeyAuth_OutsiderKeyCannotSeeAnotherTenant is plan §5 API-3's "C'nin anahtarı A'yı
// göremez": the Principal of business C's key names C and only C -- the one authority every later
// query is scoped to -- and in C's context A's keys and A's trail are 0 rows (RLS, the filterless
// probe); C's key cannot be touched as A's and A's not as C's. In the panel, A's owner cannot
// revoke C's key by naming its id (ErrUnknownKey, and C's key still resolves), nor can C's owner
// list A's keys.
func TestApiKeyAuth_OutsiderKeyCannotSeeAnotherTenant(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	s := newService(t, d, apikey.LivePrefix)
	a, c := newBusiness(t, d), newBusiness(t, d)
	setAPI(t, a.tenant, true)
	setAPI(t, c.tenant, true)
	ka, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: a.tenant, ActorID: a.owner, Name: "A's ERP"})
	if err != nil {
		t.Fatal(err)
	}
	kc, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: c.tenant, ActorID: c.owner, Name: "C's ERP"})
	if err != nil {
		t.Fatal(err)
	}
	pc, err := s.authenticate(ctx, "Bearer "+kc.Key.RevealToTheOwnerOnce())
	if err != nil {
		t.Fatal(err)
	}
	if pc.TenantID != c.tenant || pc.KeyID != kc.ID {
		t.Fatalf("C's key resolved to %v/%v", pc.TenantID, pc.KeyID)
	}
	var keysSeen, trailSeen, ownSeen int
	if err := d.WithTenant(ctx, pc.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		// The FILTERLESS probe (CLAUDE.md §6): RLS alone, no tenant_id predicate.
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE id = $1`, ka.ID).Scan(&keysSeen); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE target = $1`, ka.ID.String()).Scan(&trailSeen); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE id = $1`, kc.ID).Scan(&ownSeen)
	}); err != nil {
		t.Fatal(err)
	}
	if keysSeen != 0 || trailSeen != 0 {
		t.Fatalf("in C's context A's key is %d rows and its trail %d, want 0 and 0", keysSeen, trailSeen)
	}
	if ownSeen != 1 {
		t.Fatalf("POSITIVE CONTROL: in C's context C's own key is %d rows, want 1", ownSeen)
	}
	if wrote, err := s.auth.Touch(ctx, apikey.Principal{TenantID: c.tenant, KeyID: ka.ID}); err != nil || wrote {
		t.Fatalf("A's key touched in C's context: %v, %v", wrote, err)
	}

	if _, err := s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: a.tenant, ActorID: a.owner, KeyID: kc.ID}); !errors.Is(err, apikey.ErrUnknownKey) {
		t.Fatalf("A's owner revoking C's key by its id: %v, want ErrUnknownKey", err)
	}
	if _, err := s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: c.tenant, ActorID: a.owner, KeyID: kc.ID}); !errors.Is(err, apikey.ErrNotPermitted) {
		t.Fatalf("A's owner naming C's tenant: %v, want ErrNotPermitted", err)
	}
	if _, err := s.authenticate(ctx, "Bearer "+kc.Key.RevealToTheOwnerOnce()); err != nil {
		t.Fatalf("C's key after A's attempts: %v, want it live", err)
	}
	if _, err := s.keys.List(ctx, a.tenant, c.owner); !errors.Is(err, apikey.ErrNotPermitted) {
		t.Fatalf("C's owner listing A's keys: %v, want ErrNotPermitted", err)
	}
}

// TestApiKeyRevocation_ConcurrentAndBulkAreSafe is plan §5 API-3's "eşzamanlı çift/toplu iptal"
// (00011's BOUNDARY 2): 16 goroutines revoke one key at once -> exactly one succeeds, the rest
// answer ErrAlreadyRevoked (none fails), and the trail holds exactly ONE api_key.revoked row; then
// five keys, each revoked singly by one goroutine while "revoke all" runs in another, 20 rounds
// -> no error anywhere, every key revoked, exactly one revoked row per key, the bulk rows marked
// bulk. The guard is what keeps the loser off the one-way trigger: the bare, unguarded statement a
// careless revocation would be is shown to FAIL on a revoked key (55 -- restrict_violation), which
// is the failure the guard avoids.
//
// MUTANTS (measured): RevokeAPIKey's `revoked_at IS NULL` removed -> the losers' COALESCE matches
// the committed row, so all 16 "succeed" and each writes a trail row; RevokeAllAPIKeys' guard
// removed -> a key revoked singly first gets a second, bulk row (round 0: 2 rows).
func TestApiKeyRevocation_ConcurrentAndBulkAreSafe(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	s := newService(t, d, apikey.LivePrefix)
	b := newBusiness(t, d)
	setAPI(t, b.tenant, true)

	one, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "Race"})
	if err != nil {
		t.Fatal(err)
	}
	const racers = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: b.tenant, ActorID: b.owner, KeyID: one.ID})
		}()
	}
	close(start)
	wg.Wait()
	won := 0
	for _, e := range errs {
		switch {
		case e == nil:
			won++
		case errors.Is(e, apikey.ErrAlreadyRevoked):
		default:
			t.Errorf("a racing revocation failed: %v", e)
		}
	}
	if won != 1 {
		t.Fatalf("%d of %d racing revocations succeeded, want exactly 1", won, racers)
	}
	if n := countAction(auditRows(t, d, b.tenant), apikey.ActionRevoked, one.ID.String()); n != 1 {
		t.Fatalf("%d api_key.revoked rows for one key revoked by %d racers, want 1", n, racers)
	}

	for round := range 20 {
		var ids []uuid.UUID
		for i := range 5 {
			k, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: fmt.Sprintf("Bulk %d.%d", round, i)})
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, k.ID)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		var failures []error
		fail := func(e error) { mu.Lock(); failures = append(failures, e); mu.Unlock() }
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := s.keys.RevokeAll(ctx, b.tenant, b.owner); err != nil {
				fail(err)
			}
		}()
		for _, id := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: b.tenant, ActorID: b.owner, KeyID: id}); err != nil && !errors.Is(err, apikey.ErrAlreadyRevoked) {
					fail(err)
				}
			}()
		}
		close(start)
		wg.Wait()
		if len(failures) > 0 {
			t.Fatalf("round %d: %v", round, failures)
		}
		rows := auditRows(t, d, b.tenant)
		for _, id := range ids {
			if n := countAction(rows, apikey.ActionRevoked, id.String()); n != 1 {
				t.Fatalf("round %d: key %s has %d revoked rows, want 1", round, id, n)
			}
		}
	}
	l, err := s.keys.List(ctx, b.tenant, b.owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range l.Keys {
		if k.RevokedAt == nil {
			t.Fatalf("key %q is still live after the rounds", k.Name)
		}
	}

	// What the guard avoids: the bare statement on a revoked key reaches the one-way trigger.
	err = d.WithTenant(ctx, b.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE api_keys SET revoked_at = clock_timestamp() WHERE tenant_id = $1 AND id = $2`, b.tenant, one.ID)
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "23001") {
		t.Fatalf("an unguarded re-revocation: %v, want restrict_violation (23001) from the one-way trigger", err)
	}
}

// TestApiKeyTouch_AtMostOncePerMinute: last_used_at is written on the first use and not again
// within the minute (ADR 0028 §3); a revoked key is not touched.
func TestApiKeyTouch_AtMostOncePerMinute(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	s := newService(t, d, apikey.LivePrefix)
	b := newBusiness(t, d)
	setAPI(t, b.tenant, true)
	k, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "Touch"})
	if err != nil {
		t.Fatal(err)
	}
	p := apikey.Principal{TenantID: b.tenant, KeyID: k.ID}
	for i, want := range []bool{true, false, false} {
		if wrote, err := s.auth.Touch(ctx, p); err != nil || wrote != want {
			t.Fatalf("touch %d: %v, %v; want %v", i, wrote, err, want)
		}
	}
	// A minute later (written back by the owner, as time would): written again.
	ownerExec(t, `UPDATE api_keys SET last_used_at = last_used_at - interval '61 seconds', created_at = created_at - interval '61 seconds' WHERE id = $1`, k.ID)
	if wrote, err := s.auth.Touch(ctx, p); err != nil || !wrote {
		t.Fatalf("a minute later: %v, %v; want a write", wrote, err)
	}
	if _, err := s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: b.tenant, ActorID: b.owner, KeyID: k.ID}); err != nil {
		t.Fatal(err)
	}
	ownerExec(t, `UPDATE api_keys SET last_used_at = last_used_at - interval '61 seconds' WHERE id = $1`, k.ID)
	if wrote, err := s.auth.Touch(ctx, p); err != nil || wrote {
		t.Fatalf("a revoked key: %v, %v; want no write", wrote, err)
	}
}

// TestApiKeyPanel_OnlyAnActiveOwnerOfThisBusiness: the domain re-reads the actor -- a manager, a
// disabled owner and another business's owner are ErrNotPermitted on all four acts, and nothing
// is written; the active owner's acts land. A key cannot be created while the operator has not
// opened the API (K-22), two live keys cannot share a name (case aside), a revoked key's name is
// free again, and a revocation needs no open API (K-26).
func TestApiKeyPanel_OnlyAnActiveOwnerOfThisBusiness(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	s := newService(t, d, apikey.LivePrefix)
	b, other := newBusiness(t, d), newBusiness(t, d)

	if _, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "Early"}); !errors.Is(err, apikey.ErrAPIClosed) {
		t.Fatalf("create on a closed API: %v, want ErrAPIClosed", err)
	}
	setAPI(t, b.tenant, true)
	k, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "Payroll"})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []uuid.UUID{b.manager, b.disabledOwner, other.owner} {
		if _, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: actor, Name: "Sneaky"}); !errors.Is(err, apikey.ErrNotPermitted) {
			t.Errorf("create by %s: %v", actor, err)
		}
		if _, err := s.keys.List(ctx, b.tenant, actor); !errors.Is(err, apikey.ErrNotPermitted) {
			t.Errorf("list by %s: %v", actor, err)
		}
		if _, err := s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: b.tenant, ActorID: actor, KeyID: k.ID}); !errors.Is(err, apikey.ErrNotPermitted) {
			t.Errorf("revoke by %s: %v", actor, err)
		}
		if _, err := s.keys.RevokeAll(ctx, b.tenant, actor); !errors.Is(err, apikey.ErrNotPermitted) {
			t.Errorf("revoke all by %s: %v", actor, err)
		}
	}
	if _, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "  PAYROLL "}); !errors.Is(err, apikey.ErrNameTaken) {
		t.Fatalf("a second live key named PAYROLL: %v, want ErrNameTaken", err)
	}
	if _, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "\t"}); !errors.Is(err, apikey.ErrName) {
		t.Fatalf("a blank name: %v, want ErrName", err)
	}
	setAPI(t, b.tenant, false)
	if _, err := s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: b.tenant, ActorID: b.owner, KeyID: k.ID}); err != nil {
		t.Fatalf("revoking on a closed API (K-26): %v", err)
	}
	setAPI(t, b.tenant, true)
	if _, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "payroll"}); err != nil {
		t.Fatalf("the revoked key's name, again: %v", err)
	}
	l, err := s.keys.List(ctx, b.tenant, b.owner)
	if err != nil || len(l.Keys) != 2 || !l.APIEnabled {
		t.Fatalf("list: %d keys, enabled %v, %v; want 2, true", len(l.Keys), l.APIEnabled, err)
	}
	rows := auditRows(t, d, b.tenant)
	if n := countAction(rows, apikey.ActionCreated, ""); n != 2 {
		t.Fatalf("%d created rows, want 2 (the refused acts write none here)", n)
	}
	for _, r := range rows {
		if r.action == apikey.ActionCreated && (r.actor == nil || *r.actor != b.owner || !strings.Contains(r.detail, `"via": "panel"`)) {
			t.Errorf("a created row's actor %v / detail %s: want the owner and via panel", r.actor, r.detail)
		}
	}
}

// hashOf is the stored value of a key -- what token_hash holds -- for the canary.
func hashOf(t *testing.T, macKey []byte, raw string) string {
	t.Helper()
	return apikey.KeyedHashForTest(macKey, raw)
}
