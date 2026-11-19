package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/liveqr"
)

// The fake values every test here uses. Both are the platforms' own documentation samples (Apple's
// "ABCDE12345" Team ID, Android's example certificate digest), never a real Team ID or certificate: the
// repository holds none (Q35).
const (
	fakeTeamID = "ABCDE12345"
	fakeCertA  = "14:6D:E9:83:C5:73:06:50:D8:EE:B9:95:2F:34:FC:64:16:A0:83:42:E6:1D:BE:A8:8A:04:96:B2:3F:CF:44:E5"
	fakeCertB  = "00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF"
)

// The two bodies, byte for byte, for fakeTeamID and [fakeCertA, fakeCertB].
const (
	goldenAppleAssociation = `{"applinks":{"details":[{"appIDs":["ABCDE12345.mt.taptime.app"],"components":[{"/":"/t"},{"/":"/q"}]}]}}`
	goldenAssetLinks       = `[{"relation":["delegate_permission/common.handle_all_urls"],` +
		`"target":{"namespace":"android_app","package_name":"mt.taptime.app","sha256_cert_fingerprints":[` +
		`"14:6D:E9:83:C5:73:06:50:D8:EE:B9:95:2F:34:FC:64:16:A0:83:42:E6:1D:BE:A8:8A:04:96:B2:3F:CF:44:E5",` +
		`"00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF"]},` +
		`"relation_extensions":{"delegate_permission/common.handle_all_urls":{"dynamic_app_link_components":[{"/":"/t"},{"/":"/q"}]}}}]`
)

func appLinksRouter(t *testing.T, baseURL, teamID string, prints []string) http.Handler {
	t.Helper()
	a, err := NewAppLinks(baseURL, teamID, prints)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	a.Mount(r)
	return r
}

func appLinksDo(h http.Handler, method, host, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestAppLinks_ConfiguredServesTheExactFiles: both platforms configured, each file is 200 with
// exactly the golden body, application/json, a short public max-age, nosniff and a Content-Length;
// no redirect (no Location, a 2xx), no cookie. HEAD is GET's status and headers with no body. The
// host is accepted in the spellings httpx.OnHost reduces (a port, one trailing dot, case).
//
// MUTANTS (measured 2026-11-14, reverted): Content-Type "text/plain" -> red here and in cmd/tappa;
// "/activate" added to appLinkPaths -> red here and in TestAppLinks_LinksOnlyThePlaqueTapPath; HEAD
// writing the body -> red here; the host check disabled -> red in TestAppLinks_OtherHostsAreNotServed
// and cmd/tappa.
func TestAppLinks_ConfiguredServesTheExactFiles(t *testing.T) {
	h := appLinksRouter(t, "https://taptime.mt", fakeTeamID, []string{fakeCertA, fakeCertB})
	for _, tc := range []struct{ path, body string }{
		{AppleAppSiteAssociationPath, goldenAppleAssociation},
		{AssetLinksPath, goldenAssetLinks},
	} {
		for _, host := range []string{"taptime.mt", "taptime.mt:443", "taptime.mt.", "TAPTIME.MT"} {
			rec := appLinksDo(h, http.MethodGet, host, tc.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s on %s = %d, want 200", tc.path, host, rec.Code)
			}
			if got := rec.Body.String(); got != tc.body {
				t.Errorf("GET %s body:\n got %s\nwant %s", tc.path, got, tc.body)
			}
			for k, want := range map[string]string{
				"Content-Type":           "application/json",
				"Cache-Control":          "public, max-age=300",
				"X-Content-Type-Options": "nosniff",
				"Content-Length":         strconv.Itoa(len(tc.body)),
			} {
				if got := rec.Header().Get(k); got != want {
					t.Errorf("GET %s on %s: %s = %q, want %q", tc.path, host, k, got, want)
				}
			}
			for _, k := range []string{"Location", "Set-Cookie"} {
				if v := rec.Header().Values(k); len(v) > 0 {
					t.Errorf("GET %s on %s carries %s: %v", tc.path, host, k, v)
				}
			}
			if !json.Valid(rec.Body.Bytes()) {
				t.Errorf("GET %s: the body is not JSON", tc.path)
			}

			head := appLinksDo(h, http.MethodHead, host, tc.path)
			if head.Code != rec.Code || head.Body.Len() != 0 {
				t.Errorf("HEAD %s on %s = %d with %d body bytes, want %d and none", tc.path, host, head.Code, head.Body.Len(), rec.Code)
			}
			if !slices.Equal(head.Header().Values("Content-Type"), rec.Header().Values("Content-Type")) ||
				head.Header().Get("Content-Length") != rec.Header().Get("Content-Length") {
				t.Errorf("HEAD %s headers %v differ from GET's %v", tc.path, head.Header(), rec.Header())
			}
		}
	}
}

// TestAppLinks_LinksOnlyThePlaqueTapPath reads both files as JSON (not as golden text): one app ID,
// TeamID.mt.taptime.app; one package, mt.taptime.app; and the path list is exactly [/t, /q] on both
// platforms -- the plaque tap and, since M16-20 (M16-08's Phase D item), the live QR's -- and
// /activate is not linked (Q40). The name predates /q and is kept (docs name it).
func TestAppLinks_LinksOnlyThePlaqueTapPath(t *testing.T) {
	h := appLinksRouter(t, "https://taptime.mt", fakeTeamID, []string{fakeCertA})

	var apple appleAssociation
	if err := json.Unmarshal(appLinksDo(h, http.MethodGet, "taptime.mt", AppleAppSiteAssociationPath).Body.Bytes(), &apple); err != nil {
		t.Fatal(err)
	}
	if len(apple.AppLinks.Details) != 1 {
		t.Fatalf("apple: %d details, want 1", len(apple.AppLinks.Details))
	}
	d := apple.AppLinks.Details[0]
	if !slices.Equal(d.AppIDs, []string{fakeTeamID + ".mt.taptime.app"}) {
		t.Errorf("apple appIDs %v", d.AppIDs)
	}
	var applePaths []string
	for _, c := range d.Components {
		applePaths = append(applePaths, c.Path)
	}

	var android []assetStatement
	if err := json.Unmarshal(appLinksDo(h, http.MethodGet, "taptime.mt", AssetLinksPath).Body.Bytes(), &android); err != nil {
		t.Fatal(err)
	}
	if len(android) != 1 || android[0].Target.PackageName != "mt.taptime.app" || android[0].Target.Namespace != "android_app" ||
		!slices.Equal(android[0].Target.CertSHA256, []string{fakeCertA}) ||
		!slices.Equal(android[0].Relation, []string{"delegate_permission/common.handle_all_urls"}) {
		t.Fatalf("android statement %+v", android)
	}
	var androidPaths []string
	for _, c := range android[0].RelationExtensions["delegate_permission/common.handle_all_urls"].Components {
		androidPaths = append(androidPaths, c.Path)
	}

	for name, paths := range map[string][]string{"apple": applePaths, "android": androidPaths} {
		if !slices.Equal(paths, []string{TapPath, liveqr.LiveQRTapPath}) || TapPath != "/t" || liveqr.LiveQRTapPath != "/q" {
			t.Errorf("%s links %v, want exactly [/t /q]", name, paths)
		}
		if slices.Contains(paths, LiveScreenPath) {
			t.Errorf("%s links the live QR SCREEN page; only the code it shows (/q) is the app's", name)
		}
		if slices.Contains(paths, ActivationStatusPath) || slices.Contains(paths, "/activate") {
			t.Errorf("%s links the activation path (Q40)", name)
		}
	}
}

// TestAppLinks_UnconfiguredIsNotMounted: a platform without its value has no route -- the path is
// the router's 404, for GET and HEAD -- and the other platform's file is unaffected. With neither,
// nothing is mounted at all.
func TestAppLinks_UnconfiguredIsNotMounted(t *testing.T) {
	for _, tc := range []struct {
		name          string
		teamID        string
		prints        []string
		apple, assets bool
	}{
		{"neither", "", nil, false, false},
		{"apple only", fakeTeamID, nil, true, false},
		{"android only", "", []string{fakeCertA}, false, true},
		{"both", fakeTeamID, []string{fakeCertA}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewAppLinks("https://taptime.mt", tc.teamID, tc.prints)
			if err != nil {
				t.Fatal(err)
			}
			r := chi.NewRouter()
			a.Mount(r)
			routes := 0
			if err := chi.Walk(r, func(string, string, http.Handler, ...func(http.Handler) http.Handler) error {
				routes++
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			want := 0
			for path, on := range map[string]bool{AppleAppSiteAssociationPath: tc.apple, AssetLinksPath: tc.assets} {
				if on {
					want += 2
				}
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					rec := appLinksDo(r, method, "taptime.mt", path)
					if on && rec.Code != http.StatusOK {
						t.Errorf("%s %s = %d, want 200", method, path, rec.Code)
					}
					if !on && rec.Code != http.StatusNotFound {
						t.Errorf("%s %s = %d, want 404 (not configured)", method, path, rec.Code)
					}
				}
			}
			if routes != want {
				t.Errorf("%d routes mounted, want %d", routes, want)
			}
		})
	}
}

// TestAppLinks_OtherHostsAreNotServed: on any host but TAPPA_BASE_URL's the file is http.NotFound's
// answer, byte for byte -- the operator and API hosts (their gates answer first in production; this
// is the handler's own half), a redirect host, an unknown one, a host that only CONTAINS the
// canonical one. The development base URL's host (localhost, with its port) is the canonical one.
func TestAppLinks_OtherHostsAreNotServed(t *testing.T) {
	plain := httptest.NewRecorder()
	http.NotFound(plain, httptest.NewRequest(http.MethodGet, "/", nil))

	h := appLinksRouter(t, "https://taptime.mt", fakeTeamID, []string{fakeCertA})
	for _, path := range []string{AppleAppSiteAssociationPath, AssetLinksPath} {
		for _, host := range []string{"ops.taptime.mt", "api.taptime.mt", "www.taptime.mt", "tappa.everva.com.tr",
			"evil.example", "taptime.mt.evil.example", "xtaptime.mt", "10.42.0.7:8080", ""} {
			rec := appLinksDo(h, http.MethodGet, host, path)
			if rec.Code != http.StatusNotFound || rec.Body.String() != plain.Body.String() ||
				rec.Header().Get("Content-Type") != plain.Header().Get("Content-Type") {
				t.Errorf("GET %s on %q = %d %q, want the plain 404", path, host, rec.Code, rec.Body.String())
			}
		}
	}

	dev := appLinksRouter(t, "http://localhost:8080", fakeTeamID, nil)
	if rec := appLinksDo(dev, http.MethodGet, "localhost:8080", AppleAppSiteAssociationPath); rec.Code != http.StatusOK {
		t.Errorf("development: GET on localhost:8080 = %d, want 200", rec.Code)
	}
	if rec := appLinksDo(dev, http.MethodGet, "taptime.mt", AppleAppSiteAssociationPath); rec.Code != http.StatusNotFound {
		t.Errorf("development: GET on taptime.mt = %d, want 404 (not the base URL's host)", rec.Code)
	}
}

// TestNewAppLinks_RefusesABaseURLWithoutAHost: the host gate cannot be built from nothing.
func TestNewAppLinks_RefusesABaseURLWithoutAHost(t *testing.T) {
	for _, base := range []string{"", "/relative", "https://", "::"} {
		if _, err := NewAppLinks(base, fakeTeamID, nil); err == nil {
			t.Errorf("NewAppLinks(%q) accepted a base URL without a host", base)
		}
	}
}

// TestAppLinks_TheCertificateListIsCopied: the caller's slice changing after construction does not
// change the published file.
func TestAppLinks_TheCertificateListIsCopied(t *testing.T) {
	prints := []string{fakeCertA}
	h := appLinksRouter(t, "https://taptime.mt", "", prints)
	before := appLinksDo(h, http.MethodGet, "taptime.mt", AssetLinksPath).Body.String()
	prints[0] = fakeCertB
	if after := appLinksDo(h, http.MethodGet, "taptime.mt", AssetLinksPath).Body.String(); after != before {
		t.Errorf("the file changed with the caller's slice: %s", after)
	}
}

// TestAppLinks_ANilValueMountsNothing: a nil *AppLinks inside the router's Mounter interface is not a
// nil interface, so NewRouter calls Mount on it; Mount must mount nothing rather than panic.
func TestAppLinks_ANilValueMountsNothing(t *testing.T) {
	var a *AppLinks
	r := chi.NewRouter()
	a.Mount(r)
	routes := 0
	if err := chi.Walk(r, func(string, string, http.Handler, ...func(http.Handler) http.Handler) error {
		routes++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if routes != 0 {
		t.Errorf("a nil *AppLinks mounted %d routes", routes)
	}
	h := httpx.NewRouter(nil, nil, a)
	if rec := appLinksDo(h, http.MethodGet, "taptime.mt", AssetLinksPath); rec.Code != http.StatusNotFound {
		t.Errorf("through NewRouter: GET %s = %d, want 404", AssetLinksPath, rec.Code)
	}
}
