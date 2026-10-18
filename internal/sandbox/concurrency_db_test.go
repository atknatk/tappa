package sandbox

// concurrency_db_test.go -- API-11 fix 1: the simulation under concurrency (one connection per
// simulation; one plaque's taps in turn) and the CMAC it is judged by. Same rig as sandbox_db_test.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/sun"
)

// doWithin is rig.do with a deadline: a request that waits on the pool past it is answered 503
// rather than holding the test for the router's 30 s.
func (r *rig) doWithin(d time.Duration, method, path, key, body string) *httptest.ResponseRecorder {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, method, "http://"+apiHost+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return w
}

// TestSandbox_SimulationsHoldOneConnectionEach (API-11 fix 1, the security review's ORTA-1): on a
// pool of 2 and of 4 connections, TWICE as many simulations as the pool has connections at once --
// at two venues, each by its own employee -- are each 201 within 5 s, record one row each, and a
// /v1 read sent among them is 200. Each simulation runs its whole tap on the runner's one
// transaction (runner.go), so none of them holds a connection while it waits for another.
//
// MUTANT (measured): onRunner falling back to the pool (WithTenant -> the pool's WithTenant,
// GetTagByUID -> the pool's resolver) -- the nested shape before the fix: every request holds the
// runner's connection and waits for a second; red, every simulation and the read 503 at the
// deadline.
func TestSandbox_SimulationsHoldOneConnectionEach(t *testing.T) {
	for _, pool := range []int{2, 4} {
		t.Run(fmt.Sprintf("pool=%d", pool), func(t *testing.T) {
			r := newRigOn(t, pool)
			kf, key := r.business(true, "KF")
			r.venue(key, "KF-A")
			r.venue(key, "KF-B")
			n := 2 * pool
			for i := range n {
				r.employee(kf, fmt.Sprintf("E-%d", i), "active")
			}
			const budget = 5 * time.Second
			codes := make([]int, n)
			var read int
			var wg sync.WaitGroup
			start := time.Now()
			for i := range n {
				wg.Add(1)
				go func() {
					defer wg.Done()
					venue := "KF-A"
					if i%2 == 1 {
						venue = "KF-B"
					}
					codes[i] = r.doWithin(2*budget, http.MethodPost, "/v1/sandbox/taps", key,
						fmt.Sprintf(`{"employeeRef":"E-%d","locationRef":%q,"channel":"nfc"}`, i, venue)).Code
				}()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(5 * time.Millisecond)
				read = r.doWithin(2*budget, http.MethodGet, "/v1/employees", key, "").Code
			}()
			wg.Wait()
			elapsed := time.Since(start)
			t.Logf("pool %d, %d simulations at once: %v in %v; the read: %d", pool, n, codes, elapsed.Round(time.Millisecond), read)
			for i, c := range codes {
				if c != http.StatusCreated {
					t.Errorf("simulation %d: %d, want 201", i, c)
				}
			}
			if read != http.StatusOK {
				t.Errorf("the /v1 read among them: %d, want 200", read)
			}
			if elapsed > budget {
				t.Errorf("%d simulations on %d connections took %v, over %v", n, pool, elapsed, budget)
			}
			if got := r.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1`, kf); got != n {
				t.Errorf("%d simulations recorded %d rows", n, got)
			}
		})
	}
}

// TestSandbox_OwnAndGroupTapsOfOnePlaqueTakeTurns (API-11 fix 1, the third eye's finding 6): KF's
// plaque, provisioned by one KF tap, is then tapped by eight KF employees (own taps) and eight KM
// employees (group taps) all at once. Each simulation reads the counter only once it holds the
// plaque (tags.sql's LockSandboxPlaque), so none of the 16 is a replayed counter: no reject, the
// counter is 1 + 16, and the records carry 2..17 once each.
//
// MUTANT (measured): the lockPlaque call removed -- two simulations mint the same read and the
// second advance refuses it: red (rejects, counters missing).
func TestSandbox_OwnAndGroupTapsOfOnePlaqueTakeTurns(t *testing.T) {
	r := newRig(t)
	kf, kfKey := r.business(true, "KF")
	km, kmKey := r.business(true, "KM")
	r.holding(kf, km)
	venue := r.venue(kfKey, "KF-VLT")
	r.employee(kf, "KF-0", "active")
	decodeTap(t, r.tap(kfKey, "KF-0", "KF-VLT", "nfc"))
	const n = 8
	for i := range n {
		r.employee(kf, fmt.Sprintf("KF-%d", i+1), "active")
		r.employee(km, fmt.Sprintf("KM-%d", i+1), "active")
	}
	type answer struct {
		code    int
		verdict string
	}
	answers := make([]answer, 2*n)
	var wg sync.WaitGroup
	for i := range 2 * n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, who := kfKey, fmt.Sprintf("KF-%d", i/2+1)
			if i%2 == 1 {
				key, who = kmKey, fmt.Sprintf("KM-%d", i/2+1)
			}
			w := r.doWithin(20*time.Second, http.MethodPost, "/v1/sandbox/taps", key,
				fmt.Sprintf(`{"employeeRef":%q,"locationRef":"KF-VLT","channel":"nfc"}`, who))
			a := answer{code: w.Code}
			if w.Code == http.StatusCreated {
				var b struct{ Verdict string }
				if err := json.Unmarshal(w.Body.Bytes(), &b); err == nil {
					a.verdict = b.Verdict
				}
			}
			answers[i] = a
		}()
	}
	wg.Wait()
	rejects := 0
	for i, a := range answers {
		if a.code != http.StatusCreated {
			t.Errorf("tap %d: %d", i, a.code)
		}
		if a.verdict == "reject" {
			rejects++
		}
	}
	if rejects != 0 {
		t.Errorf("%d of %d concurrent taps of one plaque were rejected (a replayed counter no chip produced)", rejects, 2*n)
	}
	var last int32
	if err := r.owner.QueryRow(r.ctx, `SELECT last_ctr FROM tags WHERE uid = $1`, VirtualUID(venue)).Scan(&last); err != nil || last != 1+2*n {
		t.Errorf("the plaque's counter: %d (%v), want %d", last, err, 1+2*n)
	}
	rows, err := r.owner.Query(r.ctx, `SELECT ctr FROM transactions WHERE (tenant_id = $1 OR tenant_id = $2) AND coalesce(site_tag_uid, tag_uid) = $3 ORDER BY ctr`,
		kf, km, VirtualUID(venue))
	if err != nil {
		t.Fatal(err)
	}
	var got []int32
	for rows.Next() {
		var c *int32
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		if c != nil {
			got = append(got, *c)
		}
	}
	rows.Close()
	if len(got) != 1+2*n {
		t.Fatalf("the plaque's records carry counters %v, want 1..%d", got, 1+2*n)
	}
	for i, c := range got {
		if c != int32(i+1) {
			t.Fatalf("the plaque's records carry counters %v, want 1..%d once each", got, 1+2*n)
		}
	}
}

// TestSandbox_TheSimulationIsJudgedByTheRealCMAC (API-11 fix 1, the third eye's finding 2): the
// simulated chip's URL goes through the real CMAC check. A URL whose MAC is off by one hex digit is
// recorded `reject` by sys:sun-invalid with sun_valid false, and the plaque's counter does not move
// (the advance is not reached); the untampered URL of the next tap verifies, and an INDEPENDENT
// sun.Verifier over the pool says the same of both URLs: the good one's MAC is valid, the tampered
// one's is not.
//
// MUTANTS (measured): checkin.Request's CMACVerified set to true; the pre-check skipped
// (CMACVerified true, the tenant from the plaque) -- each red: the tampered tap is recorded and its
// counter advances.
func TestSandbox_TheSimulationIsJudgedByTheRealCMAC(t *testing.T) {
	r := newRig(t)
	kf, key := r.business(true, "KF")
	venue := r.venue(key, "KF-VLT")
	r.evidence(venue)
	r.employee(kf, "E-0", "active")
	r.employee(kf, "E-1", "active")
	r.employee(kf, "E-2", "active")
	decodeTap(t, r.tap(key, "E-0", "KF-VLT", "nfc")) // provisions the plaque; counter 1

	var minted []string
	r.sim.mint = func(k, uid []byte, ctr uint32) (string, error) {
		p, err := sun.MintTapPath(k, uid, ctr)
		minted = append(minted, p)
		return p, err
	}
	tampered := func(path string) string {
		u, err := url.Parse(path)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		mac := []byte(q.Get(sunMACParam))
		if len(mac) == 0 {
			t.Fatalf("the minted URL carries no MAC: %s", path)
		}
		if mac[0] == '0' {
			mac[0] = '1'
		} else {
			mac[0] = '0'
		}
		q.Set(sunMACParam, string(mac))
		u.RawQuery = q.Encode()
		return u.String()
	}
	good := r.sim.mint
	r.sim.mint = func(k, uid []byte, ctr uint32) (string, error) {
		p, err := good(k, uid, ctr)
		if err != nil {
			return "", err
		}
		bad := tampered(p)
		minted[len(minted)-1] = bad
		return bad, nil
	}
	bad := decodeTap(t, r.tap(key, "E-1", "KF-VLT", "nfc"))
	rec := r.record(bad.PunchID)
	if bad.Verdict != "reject" || bad.Rule == nil || *bad.Rule != "sys:sun-invalid" || rec.sunValid == nil || *rec.sunValid {
		t.Fatalf("a tap whose MAC is not the key's: %+v (sun_valid %v), want reject by sys:sun-invalid", bad, rec.sunValid)
	}
	var last int32
	if err := r.owner.QueryRow(r.ctx, `SELECT last_ctr FROM tags WHERE uid = $1`, VirtualUID(venue)).Scan(&last); err != nil || last != 1 {
		t.Fatalf("the tampered tap moved the counter to %d (%v), want 1", last, err)
	}

	r.sim.mint = good
	ok := decodeTap(t, r.tap(key, "E-2", "KF-VLT", "nfc"))
	if ok.Verdict != "ok" {
		t.Fatalf("CONTROL: the untampered tap: %+v", ok)
	}
	if len(minted) != 2 {
		t.Fatalf("minted %d URLs, want 2", len(minted))
	}
	independent := sun.NewVerifier(r.d, r.kek)
	for i, want := range []bool{false, true} {
		u, err := url.Parse(minted[i])
		if err != nil {
			t.Fatal(err)
		}
		params, err := sun.Parse(u.Query())
		if err != nil {
			t.Fatalf("URL %d does not parse: %v", i, err)
		}
		pv, err := independent.PreviewWithoutReplayProtection(r.ctx, params)
		if err != nil || pv.CMACValid != want {
			t.Errorf("an independent check of URL %d: CMAC valid %v (%v), want %v", i, pv.CMACValid, err, want)
		}
	}
}
