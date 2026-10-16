package api

// examples_db_test.go -- the OpenAPI document's and the guide's request and answer examples are the
// SHIPPED handlers' answers (M11 API-12: "elle uydurma JSON yok"). One story -- the guide's -- runs
// through the real chain, the real domain, the real triggers and the real webhook sender against
// Postgres, and every answer is compared with its example in openapi.yaml: the same keys at every
// level, the same values except where a value is this run's own (an id, an instant, a cursor -- there
// the same kind of value: the same prefix, the same JSON type).
//
// THE STORY (the guide's, sections 4 to 9): two companies of one group, KF and KM. KF puts its
// location KF-VLT ("Valletta") and KM its department DEP-1 ("Production" -- a KM department is a
// location, plan §1's mapping note); KF puts two employees -- E-1042 with an address (its activation
// e-mail is queued) and E-1043 without one, then deactivates E-1043; KM puts M-2001. Each company has
// a webhook endpoint. Jane (KF) taps in at Valletta; Joe (KM) taps in at Valletta and out at
// Production -- plan §2 item 30's shift. KF reads its location scope (Jane's in, Joe's in), KM its
// employer scope as a history range (Joe's in at KF's Valletta, his out at KM's Production), KF its
// directory (its two and KM's one). KF's sender posts Jane's punch; KF sends a test message.
//
// With -v the test prints every answer: that is how the examples were made (a fresh container,
// 2026-10-16) and how they are remade after a contract change.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/webhook/delivery"
	"github.com/atknatk/tappa/internal/webhook/endpoints"
)

// regexpSignature is the documented X-Taptime-Signature (components.parameters.Signature).
var regexpSignature = regexp.MustCompile(`^sha256=[0-9a-f]{64}$`)

// capturingSender records each post and answers 204 -- the receiver of the story.
type capturingSender struct {
	mu    sync.Mutex
	posts []capturedPost
}

type capturedPost struct {
	body   []byte
	header http.Header
}

func (c *capturingSender) Post(_ context.Context, _ egress.Endpoint, body []byte, header http.Header) egress.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.posts = append(c.posts, capturedPost{body: append([]byte(nil), body...), header: header.Clone()})
	return egress.Result{Status: http.StatusNoContent}
}

func (c *capturingSender) all() []capturedPost {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedPost(nil), c.posts...)
}

// exampleAnswer is one exchange of the story: what was sent and what came back.
type exampleAnswer struct {
	status int
	header http.Header
	body   []byte
	// request is the request's body (a write), or nil.
	request []byte
	// query is the request's query string (a read), or "".
	query string
}

// exampleStory is the story's answers by example name ("<operationId> <status>", "webhook punch",
// "webhook ping").
type exampleStory map[string]exampleAnswer

func runExampleStory(t *testing.T) exampleStory {
	t.Helper()
	w := newPunchWorld(t, unmetered)
	r := w.r
	story := exampleStory{}
	ctx := context.Background()
	record := func(name string, rec interface {
		Result() *http.Response
	}, body []byte, request []byte, query string) {
		res := rec.Result()
		story[name] = exampleAnswer{status: res.StatusCode, header: res.Header, body: body, request: request, query: query}
		t.Logf("example %s: %d\nrequest: %s%s\nanswer: %s", name, res.StatusCode, request, query, body)
	}
	must := func(name string, got, want int, body string) {
		if got != want {
			t.Fatalf("%s: %d %s, want %d", name, got, body, want)
		}
	}

	// The two companies: open APIs, company codes, keys, one group.
	newCompany := func(code string) punchBiz {
		var b punchBiz
		b.code = code
		b.id, b.admin = r.business(true)
		w.exec(`UPDATE tenants SET api_code = $2 WHERE id = $1`, b.id, code)
		b.raw, b.keyID = r.key(b.id, b.admin)
		return b
	}
	kf, km := newCompany("KF"), newCompany("KM")
	h := w.holding()
	t.Cleanup(func() {
		bg := context.Background()
		if _, err := r.owner.Exec(bg, `DELETE FROM holding_members WHERE holding_id = $1`, h); err != nil {
			t.Errorf("cleanup memberships: %v", err)
		}
		if _, err := r.owner.Exec(bg, `DELETE FROM holdings WHERE id = $1`, h); err != nil {
			t.Errorf("cleanup holding: %v", err)
		}
	})
	w.join(h, kf.id)
	w.join(h, km.id)

	// Locations (section 4).
	putLocation := func(b punchBiz, ref, body, example string) uuid.UUID {
		rec := r.putLoc(b.raw, ref, body)
		must("putLocation", rec.Code, http.StatusCreated, rec.Body.String())
		if example != "" {
			record(example, rec, rec.Body.Bytes(), []byte(body), "")
		}
		var got locationBody
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		id, err := uuid.Parse(strings.TrimPrefix(got.ID, locationIDPrefix))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	kf.venue = putLocation(kf, "KF-VLT", `{"name":"Valletta","active":true,"timezone":"Europe/Malta"}`, "putLocation 201")
	km.venue = putLocation(km, "DEP-1", `{"name":"Production","active":true}`, "")

	// Plaques at the two locations (mounted in the panel: no API writes a plaque).
	for _, b := range []*punchBiz{&kf, &km} {
		b.tag = strings.ToUpper(hex.EncodeToString(randomBytes(t, 7)))
		w.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 1000000, 'active')`,
			b.tag, b.id, b.venue, randomBytes(t, 44))
	}

	// Employees (section 5).
	putEmployee := func(b punchBiz, ref, body string, want int) employeeBody {
		rec := r.putEmp(b.raw, ref, body)
		must("putEmployee "+ref, rec.Code, want, rec.Body.String())
		if ref == "E-1042" {
			record("putEmployee 201", rec, rec.Body.Bytes(), []byte(body), "")
		}
		return empOf(t, rec)
	}
	jane := putEmployee(kf, "E-1042", `{"firstName":"Jane","lastName":"Example","email":"jane.example@example.com","active":true,"employeeCode":"1042"}`, http.StatusCreated)
	putEmployee(kf, "E-1043", `{"firstName":"Sam","lastName":"Placeholder","email":null,"active":true}`, http.StatusCreated)
	joe := putEmployee(km, "M-2001", `{"firstName":"Joe","lastName":"Sample","email":"joe.sample@example.com","active":true,"employeeCode":"2001"}`, http.StatusCreated)
	del := r.do(http.MethodDelete, "/v1/employees/E-1043", kf.raw, "")
	must("deleteEmployee", del.Code, http.StatusOK, del.Body.String())
	record("deleteEmployee 200", del, del.Body.Bytes(), nil, "")
	resend := r.do(http.MethodPost, "/v1/employees/E-1042/resend-activation", kf.raw, "")
	must("resendActivation", resend.Code, http.StatusAccepted, resend.Body.String())
	record("resendActivation 202", resend, resend.Body.Bytes(), nil, "")
	one := r.do(http.MethodGet, "/v1/employees/E-1042", kf.raw, "")
	must("getEmployee", one.Code, http.StatusOK, one.Body.String())
	record("getEmployee 200", one, one.Body.Bytes(), nil, "")
	list := r.do(http.MethodGet, "/v1/employees?limit=2", kf.raw, "")
	must("listEmployees", list.Code, http.StatusOK, list.Body.String())
	record("listEmployees 200", list, list.Body.Bytes(), nil, "limit=2")
	kf.emp, km.emp = idOfEmp(t, jane), idOfEmp(t, joe)

	// Webhook endpoints (section 9), before the taps: an endpoint receives the events written after it.
	sender := &capturingSender{}
	kek := randomBytes(t, 32)
	svc, err := endpoints.New(kek, r.d, r.trail, sender)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		b    punchBiz
		addr string
	}{{kf, "https://hooks.example.com/taptime"}, {km, "https://erp.example.com/taptime/webhook"}} {
		if _, err := svc.Create(ctx, endpoints.CreateCommand{TenantID: c.b.id, ActorID: c.b.admin, Address: c.addr}); err != nil {
			t.Fatalf("create the endpoint: %v", err)
		}
	}

	// The taps: Joe in at Valletta (a punch between two companies), Jane in at Valletta, Joe out at
	// Production. Every row is recorded now, so each declared instant is what a phone would send:
	// within the 120 seconds a late tap is still judged live (guide §6.1) -- Jane's and Joe's out a
	// few seconds, Joe's in 65 -- and Joe's two taps more than the 60-second repeat window apart
	// (closer, the second would be an ignored repeat, not a punch). The rows are written in their
	// punchTime order, so the feed's order and punchTime's agree in the examples.
	w.write(punchRow{employer: km, site: &kf, verdict: "ok", channel: "nfc", kind: "in", ago: 65 * time.Second})
	w.write(punchRow{employer: kf, verdict: "ok", channel: "nfc", kind: "in", ago: 3 * time.Second})
	w.write(punchRow{employer: km, verdict: "ok", channel: "nfc", kind: "out", ago: 2 * time.Second})

	// The feed (section 6): KF's location scope.
	feed := w.get(kf.raw, "scope=location&limit=100")
	must("listPunches", feed.Code, http.StatusOK, feed.Body.String())
	record("listPunches 200 feed", feed, feed.Body.Bytes(), nil, "scope=location&limit=100")

	// The history (section 7): KM's employer scope over the last two hours.
	to := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	from := to.Add(-3 * time.Hour)
	hq := "scope=employer&from=" + url.QueryEscape(from.Format(time.RFC3339)) + "&to=" + url.QueryEscape(to.Format(time.RFC3339))
	hist := w.get(km.raw, hq)
	must("listPunches history", hist.Code, http.StatusOK, hist.Body.String())
	record("listPunches 200 history", hist, hist.Body.Bytes(), nil, hq)

	// The directory (section 8): KF's whole directory.
	dir := r.do(http.MethodGet, "/v1/directory", kf.raw, "")
	must("listDirectory", dir.Code, http.StatusOK, dir.Body.String())
	record("listDirectory 200", dir, dir.Body.Bytes(), nil, "")

	// The webhook (section 9): KF's sender posts its deliveries; KF sends a test message.
	job, err := delivery.New(kek, r.d, sender, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := job.RunFor(ctx, kf.id); err != nil {
		var later *delivery.Later
		if !errors.As(err, &later) {
			t.Fatalf("run KF's deliveries: %v", err)
		}
	}
	var janeDelivery *capturedPost
	for _, p := range sender.all() {
		if bytes.Contains(p.body, []byte(`"externalRef":"E-1042"`)) {
			janeDelivery = &p
		}
	}
	if janeDelivery == nil {
		t.Fatalf("KF's sender did not post Jane's punch (%d posts)", len(sender.all()))
	}
	story["webhook punch"] = exampleAnswer{header: janeDelivery.header, body: janeDelivery.body}
	t.Logf("example webhook punch:\n%s\n%v", janeDelivery.body, janeDelivery.header)
	before := len(sender.all())
	if _, err := svc.Ping(ctx, kf.id, kf.admin); err != nil {
		t.Fatalf("ping: %v", err)
	}
	posts := sender.all()
	if len(posts) != before+1 {
		t.Fatalf("the ping posted %d requests", len(posts)-before)
	}
	ping := posts[len(posts)-1]
	story["webhook ping"] = exampleAnswer{header: ping.header, body: ping.body}
	t.Logf("example webhook ping:\n%s", ping.body)
	return story
}

// The keys whose values are a run's own: an id, an instant, a cursor. Their example and their answer
// are compared by kind (prefix and JSON type), every other value exactly.
var volatileExampleKeys = map[string]bool{
	"id": true, "createdAt": true, "updatedAt": true, "activatedAt": true, "at": true, "punchTime": true,
	"recordedAt": true, "asOf": true, "nextCursor": true, "tagId": true, "deliveryId": true, "sentAt": true,
}

// sameExample compares an example with an answer (both in the subset's types), path naming where.
func sameExample(path string, example, answer any, volatile bool) error {
	switch e := example.(type) {
	case *ymap:
		a, ok := answer.(*ymap)
		if !ok {
			return fmt.Errorf("%s: the example is an object, the answer %T", path, answer)
		}
		if strings.Join(sortedKeys(e), ",") != strings.Join(sortedKeys(a), ",") {
			return fmt.Errorf("%s: the example's keys %v, the answer's %v", path, sortedKeys(e), sortedKeys(a))
		}
		for _, k := range e.keys {
			if err := sameExample(path+"."+k, e.m[k], a.m[k], volatileExampleKeys[k]); err != nil {
				return err
			}
		}
		return nil
	case []any:
		a, ok := answer.([]any)
		if !ok || len(a) != len(e) {
			return fmt.Errorf("%s: the example is a list of %d, the answer %v", path, len(e), answer)
		}
		for i := range e {
			if err := sameExample(fmt.Sprintf("%s[%d]", path, i), e[i], a[i], false); err != nil {
				return err
			}
		}
		return nil
	}
	if volatile {
		if (example == nil) != (answer == nil) {
			return fmt.Errorf("%s: the example %v, the answer %v (null on one side only)", path, example, answer)
		}
		es, eok := example.(string)
		as, aok := answer.(string)
		if eok != aok {
			return fmt.Errorf("%s: the example %v and the answer %v are not the same type", path, example, answer)
		}
		if eok && valueKind(es) != valueKind(as) {
			return fmt.Errorf("%s: the example %q and the answer %q are not the same kind", path, es, as)
		}
		return nil
	}
	if canonical(example) != canonical(answer) {
		return fmt.Errorf("%s: the example %v, the answer %v", path, example, answer)
	}
	return nil
}

// valueKind is a volatile string's kind: its prefix up to the first '_' (loc_, cur_, hst_ ...), or
// "instant" for an RFC 3339 instant.
func valueKind(s string) string {
	if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return "instant"
	}
	if i := strings.IndexByte(s, '_'); i > 0 {
		return s[:i+1]
	}
	return "other"
}

// byRef orders a page's data by externalRef: GET /v1/employees pages in id order, and the story's
// ids are random, so the example (E-1042, then E-1043) is compared with the answer in that order.
func byRef(page any) {
	data, _ := yat(page, "data")
	list, _ := data.([]any)
	ref := func(v any) string { r, _ := yat(v, "externalRef"); return fmt.Sprint(r) }
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && ref(list[j]) < ref(list[j-1]); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func sortedKeys(m *ymap) []string {
	k := append([]string(nil), m.keys...)
	sortStrings(k)
	return k
}

// storyExamples names where each answer of the story is in openapi.yaml: the request body's example
// (a write) and the answer's.
var storyExamples = map[string]struct{ request, answer []string }{
	"putLocation 201": {
		request: []string{"paths", "/v1/locations/{ref}", "put", "requestBody", "content", "application/json", "examples", "valletta", "value"},
		answer:  []string{"paths", "/v1/locations/{ref}", "put", "responses", "201", "content", "application/json", "examples", "valletta", "value"}},
	"putEmployee 201": {
		request: []string{"paths", "/v1/employees/{ref}", "put", "requestBody", "content", "application/json", "examples", "jane", "value"},
		answer:  []string{"paths", "/v1/employees/{ref}", "put", "responses", "201", "content", "application/json", "examples", "jane", "value"}},
	"getEmployee 200":         {answer: []string{"paths", "/v1/employees/{ref}", "get", "responses", "200", "content", "application/json", "examples", "jane", "value"}},
	"listEmployees 200":       {answer: []string{"paths", "/v1/employees", "get", "responses", "200", "content", "application/json", "examples", "kf", "value"}},
	"deleteEmployee 200":      {answer: []string{"paths", "/v1/employees/{ref}", "delete", "responses", "200", "content", "application/json", "examples", "sam", "value"}},
	"resendActivation 202":    {answer: []string{"paths", "/v1/employees/{ref}/resend-activation", "post", "responses", "202", "content", "application/json", "examples", "jane", "value"}},
	"listPunches 200 feed":    {answer: []string{"paths", "/v1/punches", "get", "responses", "200", "content", "application/json", "examples", "feed", "value"}},
	"listPunches 200 history": {answer: []string{"paths", "/v1/punches", "get", "responses", "200", "content", "application/json", "examples", "history", "value"}},
	"listDirectory 200":       {answer: []string{"paths", "/v1/directory", "get", "responses", "200", "content", "application/json", "examples", "kf", "value"}},
	"webhook punch":           {answer: []string{"webhooks", "punch", "post", "requestBody", "content", "application/json", "examples", "jane", "value"}},
	"webhook ping":            {answer: []string{"webhooks", "ping", "post", "requestBody", "content", "application/json", "examples", "ping", "value"}},
}

// TestOpenAPI_TheExamplesAreTheHandlersAnswers (M11 API-12): every request and answer example of
// openapi.yaml's operations and webhooks is the story's -- the shipped handlers' -- exchange: the
// request body exactly as sent, the answer with the same keys everywhere and the same values except
// a run's own (volatileExampleKeys: the same kind there). Both ways: an example the story does not
// produce is red, and so is a story answer without its example. The webhook's headers are the
// documented ones.
//
// MUTANTS (measured, API-12 report): an example's name edited; a key added to an example; a field
// dropped from the punch item (punchwire) -- each red.
func TestOpenAPI_TheExamplesAreTheHandlersAnswers(t *testing.T) {
	story := runExampleStory(t)
	doc := openAPIDoc(t)
	pinned := map[string]bool{}
	for name, where := range storyExamples {
		got, ok := story[name]
		if !ok {
			t.Errorf("the story has no answer %q", name)
			continue
		}
		for _, side := range []struct {
			path []string
			body []byte
		}{{where.request, got.request}, {where.answer, got.body}} {
			if side.path == nil {
				continue
			}
			pinned[strings.Join(side.path, "/")] = true
			example, ok := yat(doc, side.path...)
			if !ok {
				t.Errorf("%s: no example at %v", name, side.path)
				continue
			}
			answer, err := jsonValue(side.body)
			if err != nil {
				t.Fatalf("%s: %v (%q)", name, err, side.body)
			}
			if name == "listEmployees 200" {
				byRef(answer)
			}
			if err := sameExample(name, example, answer, false); err != nil {
				t.Errorf("%v\nthe handler's: %s", err, side.body)
			}
		}
	}
	for name := range story {
		if _, ok := storyExamples[name]; !ok {
			t.Errorf("the story's %q has no example in the document", name)
		}
	}
	// Every operation example of the document is pinned here (the error responses' are pinned by
	// TestOpenAPI_EveryRouteAndErrorDocumented).
	walked := 0
	for _, section := range []string{"paths", "webhooks"} {
		sec, _ := doc.get(section)
		walkValues(sec, section, func(path string, v any) {
			if !strings.HasSuffix(path, "/value") || !strings.Contains(path, "/examples/") {
				return
			}
			walked++
			if !pinned[path] {
				t.Errorf("the example %s is pinned to no answer of the story", path)
			}
		})
	}
	if walked != len(pinned) {
		t.Errorf("anti-vacuity: %d examples walked, %d pinned", walked, len(pinned))
	}
	// The webhook's headers: the documented content type, user agent and signature shape.
	for _, name := range []string{"webhook punch", "webhook ping"} {
		h := story[name].header
		if h.Get("Content-Type") != "application/json" || h.Get("User-Agent") != "Taptime-Webhooks/1" ||
			!regexpSignature.MatchString(h.Get("X-Taptime-Signature")) || len(h) != 3 {
			t.Errorf("%s: the headers %v are not the documented three", name, h)
		}
	}
}
