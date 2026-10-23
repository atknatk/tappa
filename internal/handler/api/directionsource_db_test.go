package api

// directionsource_db_test.go -- M15-13 (migration 00067; ADR 0032 §B.6, ADR 0028 §4): a punch on the
// wire says who chose its direction (directionSource), the same in both companies' feeds, and a v1
// client that does not know the field reads every other field as before.

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPunches_DirectionSourceIsTheRecordsInBothCompaniesFeeds is the card's "işveren ve saha
// satırında aynı değer; manuel satırlar akışa yine girmez (K-6)" at the API: every answer equals its
// own business's event row field by field (checkScope -- directionSource among the fields since
// M15-13), and the values are the records':
//   - KF's own tap with no stored source (every row before M15-11) answers "auto";
//   - KF's own tap the employee chose answers "employee", in the feed and in the history query;
//   - KM's employee on KF's plaque, chosen by the employee, answers "employee" in KM's employer scope
//     AND in KF's location scope (the site's row -- ADR 0027 §9 item 3's K note);
//   - an approved flag the employee chose answers "employee";
//   - a manual record carrying "employee" is in no scope.
func TestPunches_DirectionSourceIsTheRecordsInBothCompaniesFeeds(t *testing.T) {
	w := newPunchWorld(t)
	employee := sp("employee")
	auto := w.write(own(w.kf, "nfc", "in"))
	chosenRow := own(w.kf, "qr", "out")
	chosenRow.ago, chosenRow.source = 3*time.Minute, employee
	chosen := w.write(chosenRow)
	acrossRow := at(w.km, w.kf, "in")
	acrossRow.source = employee
	across := w.write(acrossRow)
	flagRow := own(w.kf, "nfc", "in")
	flagRow.verdict, flagRow.ago, flagRow.source = "flag", 4*time.Minute, employee
	flag := w.write(flagRow)
	w.review(w.kf, flag, "approved")
	manual := w.write(punchRow{employer: w.kf, verdict: "ok", channel: "manual", kind: "in", ago: 5 * time.Minute, source: employee})

	w.checkScope(w.kf, "employer", []uuid.UUID{auto, chosen, flag})
	w.checkScope(w.kf, "location", []uuid.UUID{auto, chosen, across, flag})
	w.checkScope(w.km, "employer", []uuid.UUID{across})
	w.checkScope(w.km, "location", nil)

	want := map[string]string{pch(auto): "auto", pch(chosen): "employee", pch(across): "employee", pch(flag): "employee"}
	check := func(where string, ps []punchBody) {
		t.Helper()
		for _, p := range ps {
			if p.ID == pch(manual) {
				t.Errorf("%s answers the manual record %s (K-6)", where, p.ID)
				continue
			}
			if p.DirectionSource == nil || *p.DirectionSource != want[p.ID] {
				t.Errorf("%s: %s answers directionSource %v, want %q", where, p.ID, p.DirectionSource, want[p.ID])
			}
		}
	}
	for _, b := range []punchBiz{w.kf, w.km} {
		for _, scope := range []string{"employer", "location"} {
			check(b.code+" feed scope="+scope, w.all(b, scope, 1000))
			from := time.Now().Add(-time.Hour)
			ps, _ := w.walk(b, scope, from, from.Add(2*time.Hour), 1000)
			check(b.code+" history scope="+scope, ps)
		}
	}
}

// v1Edition1Punch is a punch as a client written against the guide's edition 1 (2026-10-16) reads
// it: every field the contract had, and not directionSource.
type v1Edition1Punch struct {
	ID         string    `json:"id"`
	Version    int32     `json:"version"`
	Status     string    `json:"status"`
	PunchTime  time.Time `json:"punchTime"`
	RecordedAt time.Time `json:"recordedAt"`
	Channel    string    `json:"channel"`
	Direction  string    `json:"direction"`
	Employee   struct {
		ID           string  `json:"id"`
		ExternalRef  *string `json:"externalRef"`
		EmployeeCode *string `json:"employeeCode"`
		Company      *string `json:"company"`
	} `json:"employee"`
	Location struct {
		ID          *string `json:"id"`
		ExternalRef *string `json:"externalRef"`
		Name        *string `json:"name"`
		Company     *string `json:"company"`
	} `json:"location"`
	TagID *string `json:"tagId"`
}

// v1Edition1Keys and v1Edition1Required are edition 1's Punch: its keys in wire order and its
// required list (the OpenAPI document at 2026-10-16).
var (
	v1Edition1Keys     = []string{"id", "version", "status", "punchTime", "recordedAt", "channel", "direction", "employee", "location", "tagId"}
	v1Edition1Required = []string{"id", "version", "status", "punchTime", "recordedAt", "channel", "direction", "employee", "location", "tagId"}
)

// TestPunches_AV1ClientReadsAPunchWithoutKnowingDirectionSource is the card's geriye uyum testi
// ("v1 istemcisi alanı bilmeden çalışır"; ADR 0028's v1, the field additive and optional):
//   - the answer's keys are edition 1's, in edition 1's order, with directionSource the one addition
//     -- right after direction; nothing renamed, moved or dropped;
//   - an edition-1 client's struct decodes the answer, and every field it knows equals what the
//     current wire struct reads;
//   - the published Punch and PunchDelivery keep edition 1's required list (directionSource is a
//     property, not required), so an edition-1 answer -- the same punch without the field -- is
//     still a valid answer, and so is the answer with it.
//
// Not covered, and said so in the report: a client that validates answers against edition 1's
// document itself, whose Punch is additionalProperties: false, refuses any added field.
func TestPunches_AV1ClientReadsAPunchWithoutKnowingDirectionSource(t *testing.T) {
	w := newPunchWorld(t)
	row := own(w.kf, "nfc", "out")
	row.source = sp("employee")
	id := w.write(row)
	rec := w.get(w.kf.raw, "scope=employer")
	page := pageOf(t, rec)
	if len(page.Data) != 1 || page.Data[0].ID != pch(id) {
		t.Fatalf("the feed answered %v, want the one punch", ids(page.Data))
	}
	full := page.Data[0]

	val, err := jsonValue(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := yat(val, "data")
	item := mustMap(t, data.([]any)[0], "the punch")
	var raw struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	wantKeys := slices.Insert(slices.Clone(v1Edition1Keys), slices.Index(v1Edition1Keys, "direction")+1, "directionSource")
	if got := wireKeys(t, raw.Data[0]); !slices.Equal(got, wantKeys) {
		t.Errorf("the punch's keys in wire order are %v, want edition 1's with directionSource after direction: %v", got, wantKeys)
	}

	var page1 struct {
		Data []v1Edition1Punch `json:"data"`
	}
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&page1); err != nil {
		t.Fatalf("an edition-1 client cannot decode the answer: %v", err)
	}
	old := page1.Data[0]
	switch {
	case old.ID != full.ID || old.Version != full.Version || old.Status != full.Status || old.Channel != full.Channel ||
		old.Direction != full.Direction || !old.PunchTime.Equal(full.PunchTime) || !old.RecordedAt.Equal(full.RecordedAt):
		t.Errorf("the edition-1 client reads %+v, the wire says %+v", old, full)
	case old.Employee.ID != full.Employee.ID || !eqStr(old.Employee.ExternalRef, full.Employee.ExternalRef) ||
		!eqStr(old.Employee.EmployeeCode, full.Employee.EmployeeCode) || !eqStr(old.Employee.Company, full.Employee.Company):
		t.Errorf("the edition-1 client reads the employee %+v, the wire %+v", old.Employee, full.Employee)
	case !eqStr(old.Location.ID, full.Location.ID) || !eqStr(old.Location.ExternalRef, full.Location.ExternalRef) ||
		!eqStr(old.Location.Name, full.Location.Name) || !eqStr(old.Location.Company, full.Location.Company) || !eqStr(old.TagID, full.TagID):
		t.Errorf("the edition-1 client reads the location %+v / tag %v, the wire %+v / %v", old.Location, old.TagID, full.Location, full.TagID)
	case full.DirectionSource == nil || *full.DirectionSource != "employee":
		t.Errorf("the wire's directionSource is %v, want employee", full.DirectionSource)
	}

	doc := openAPIDoc(t)
	for _, schema := range []string{"Punch", "PunchDelivery"} {
		req, _ := yat(doc, "components", "schemas", schema, "required")
		var got []string
		for _, k := range req.([]any) {
			got = append(got, k.(string))
		}
		want := v1Edition1Required
		if schema == "PunchDelivery" {
			want = append([]string{"event", "deliveryId"}, v1Edition1Required...)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s's required is %v, want edition 1's %v (an added field is optional)", schema, got, want)
		}
		if _, ok := yat(doc, "components", "schemas", schema, "properties", "directionSource"); !ok {
			t.Errorf("%s does not document directionSource", schema)
		}
	}
	punchSchema, _ := yat(doc, "components", "schemas", "Punch")
	checker := schemaChecker{doc: doc}
	if errs := checker.check(punchSchema, item, "the answer"); len(errs) > 0 {
		t.Errorf("the answer is not a Punch: %v", errs)
	}
	without := &ymap{m: map[string]any{}}
	for _, k := range item.keys {
		if k != "directionSource" {
			without.keys = append(without.keys, k)
			without.m[k] = item.m[k]
		}
	}
	if errs := checker.check(punchSchema, without, "the edition-1 answer"); len(errs) > 0 {
		t.Errorf("an edition-1 answer (no directionSource) is no longer a Punch: %v", errs)
	}
}

// wireKeys is an object's top-level keys in the order the bytes carry them.
func wireKeys(t *testing.T, obj []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(obj))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %s", obj)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}
