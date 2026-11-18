package main

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/handler"
)

// applinks_test.go -- the native app's association files (M16-08) on the router the binary serves,
// and in the manifests that deliver requests to it. internal/handler's TestAppLinks_* hold the
// files themselves; these hold where they are and are not answered.

// Fake values of the right shapes -- the platforms' documentation samples (Q35: the repository
// holds no real Team ID or certificate).
const (
	appLinksFakeTeamID = "ABCDE12345"
	appLinksFakePrint  = "14:6D:E9:83:C5:73:06:50:D8:EE:B9:95:2F:34:FC:64:16:A0:83:42:E6:1D:BE:A8:8A:04:96:B2:3F:CF:44:E5"
)

var appLinksPaths = []string{handler.AppleAppSiteAssociationPath, handler.AssetLinksPath}

// TestAppRouter_TheAppFilesAnswerOnTheCanonicalHostOnly composes the production router with the
// customer base URL https://taptime.mt, an operator host, an API host and the two redirect hosts --
// every gate NewRouter mounts -- and asks for both files on each host:
//
//   - taptime.mt: 200, application/json, no Location, no cookie;
//   - ops.taptime.mt and api.taptime.mt: their gates' plain 404;
//   - www.taptime.mt and tappa.everva.com.tr: the redirect hosts' 301 to taptime.mt. A redirect is no
//     association (Apple and Android follow none), so Universal Links and App Links work for
//     taptime.mt alone -- the one host a plaque's URL names;
//   - an unknown host: the plain 404.
//
// And with neither app value configured, taptime.mt answers both paths with the plain 404 -- not
// the public page, and the composition still builds (the app is optional).
func TestAppRouter_TheAppFilesAnswerOnTheCanonicalHostOnly(t *testing.T) {
	for _, configured := range []bool{true, false} {
		cfg := walkConfig(t, false)
		if cfg.BaseURL != "https://taptime.mt" {
			t.Fatalf("PREMISE: walkConfig's base URL is %q", cfg.BaseURL)
		}
		cfg.OperatorHost, cfg.APIHost = "ops.taptime.mt", "api.taptime.mt"
		cfg.RedirectHosts = []string{"tappa.everva.com.tr", "www.taptime.mt"}
		if configured {
			cfg.AppAppleTeamID, cfg.AppAndroidSignerSHA256 = appLinksFakeTeamID, []string{appLinksFakePrint}
		}
		h := buildAppFeatures(t, cfg, false).router(cfg)

		for _, path := range appLinksPaths {
			got := answerOf(h, http.MethodGet, customerHost, path)
			switch {
			case configured && got.status != http.StatusOK:
				t.Errorf("configured: GET %s on %s = %d, want 200", path, customerHost, got.status)
			case configured && (got.header.Get("Content-Type") != "application/json" || got.header.Get("Location") != "" ||
				len(got.header.Values("Set-Cookie")) > 0):
				t.Errorf("configured: GET %s headers %v", path, got.header)
			case configured && !strings.Contains(got.body, "mt.taptime.app"):
				t.Errorf("configured: GET %s body %q", path, got.body)
			case !configured && (got.status != http.StatusNotFound || got.isThePage()):
				t.Errorf("unconfigured: GET %s on %s = %d (page %v), want the plain 404", path, customerHost, got.status, got.isThePage())
			}

			for _, host := range []string{"ops.taptime.mt", "api.taptime.mt", "evil.example"} {
				if got := answerOf(h, http.MethodGet, host, path); got.status != http.StatusNotFound || got.isThePage() {
					t.Errorf("configured=%v: GET %s on %s = %d, want the plain 404", configured, path, host, got.status)
				}
			}
			for _, host := range cfg.RedirectHosts {
				got := answerOf(h, http.MethodGet, host, path)
				if got.status != http.StatusMovedPermanently || got.header.Get("Location") != "https://taptime.mt"+path {
					t.Errorf("GET %s on %s = %d Location %q, want the redirect to taptime.mt", path, host, got.status, got.header.Get("Location"))
				}
			}
		}
	}
}

// TestPackaging_TheAppValuesAreTheHandedOverOnes: tappa-config carries both app variables, each with
// exactly the value the user handed over and nothing invented in a public repository (Q35): the iOS
// Team ID since the device tour (2026-11-18), the Android signer still EMPTY until its key is fixed.
// Neither is an env entry of the serving container (which would override the ConfigMap). The
// shipped ConfigMap still loading with them is TestPackaging_TheConfigMapsMailSettingsLoadInProduction's.
// It used to pin both empty and stayed green while a second, empty key under the same name shadowed
// the value set above it (kubectl keeps the last) -- configMapValues now refuses a repeated key.
func TestPackaging_TheAppValuesAreTheHandedOverOnes(t *testing.T) {
	cm := configMapValues(t, readK8s(t, "05-config.yaml"))
	if cm["TAPPA_ENV"] != config.EnvProd {
		t.Fatalf("CONTROL FAILED: the ConfigMap parse reads TAPPA_ENV as %q", cm["TAPPA_ENV"])
	}
	env := containerEnvEntries(t, readK8s(t, "20-app.yaml"), servingContainer)
	want := map[string]string{
		config.EnvAppAppleTeamID:         "P6PM935BK2", // Everva Bilisim A.S., handed over 2026-11-18
		config.EnvAppAndroidSignerSHA256: "",           // until the Android signer is fixed (M16-14/M16-28)
	}
	for _, name := range []string{config.EnvAppAppleTeamID, config.EnvAppAndroidSignerSHA256} {
		v, ok := cm[name]
		switch {
		case !ok:
			t.Errorf("%s is not a tappa-config key", name)
		case v != want[name]:
			t.Errorf("tappa-config ships %s = %q, want %q (only a value the user handed over, Q35)", name, v, want[name])
		}
		if _, overridden := env[name]; overridden {
			t.Errorf("%s is an env entry of the serving container: it would override the ConfigMap", name)
		}
	}
}

// TestPackaging_TheAppFilesAreRoutedToTheAppOnTheCanonicalHost reads every Ingress in deploy/k8s and
// resolves each file's path the way ingress-nginx does (the longest matching path on the host wins)
// on the customer origin's host (05-config's TAPPA_BASE_URL):
//
//   - the winning path routes to the tappa Service's http port, in an object with TLS for that host
//     (Apple and Android fetch over HTTPS) and no redirect, rewrite or auth annotation beyond the two
//     HTTPS redirects (which never touch an https request);
//   - NO Ingress in the tree declares a path at or under /.well-known on that host. The one path that
//     belongs there is cert-manager's HTTP-01 solver's /.well-known/acme-challenge/<token>, which the
//     solver adds for itself, more specific than "/", only while a certificate is being solved
//     (42-api-ingress.yaml's and 43-redirect-ingress.yaml's headers). A static /.well-known path in a
//     manifest would either steal the files from the app or sit beside the solver's -- neither is
//     needed, since "/" already carries both files.
func TestPackaging_TheAppFilesAreRoutedToTheAppOnTheCanonicalHost(t *testing.T) {
	base, err := url.Parse(configMapValues(t, readK8s(t, "05-config.yaml"))["TAPPA_BASE_URL"])
	if err != nil || base.Host == "" {
		t.Fatalf("05-config.yaml's TAPPA_BASE_URL does not parse: %v", err)
	}
	manifests, err := filepath.Glob(filepath.Join(repoRoot, "deploy", "k8s", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var all []ingressObject
	for _, path := range manifests {
		all = append(all, ingressesIn(t, filepath.Base(path))...)
	}
	if len(all) < 4 {
		t.Fatalf("CONTROL FAILED: %d Ingresses parsed", len(all))
	}

	for _, o := range all {
		for _, r := range o.rules {
			if r.host != base.Host {
				continue
			}
			for _, p := range r.paths {
				if p.path == "/.well-known" || strings.HasPrefix(p.path, "/.well-known/") {
					t.Errorf("%s (%s) declares %s on %s: the app serves /.well-known itself and the ACME solver adds its own path",
						o.name, o.file, p.path, base.Host)
				}
			}
		}
	}

	for _, file := range appLinksPaths {
		var winner ingressObject
		var best ingressPath
		for _, o := range all {
			for _, r := range o.rules {
				if r.host != base.Host {
					continue
				}
				for _, p := range r.paths {
					if ingressPathMatches(p, file) && len(p.path) > len(best.path) {
						winner, best = o, p
					}
				}
			}
		}
		if winner.name == "" {
			t.Errorf("no Ingress routes %s on %s", file, base.Host)
			continue
		}
		if best.service != "tappa" || best.port != "http" {
			t.Errorf("%s on %s is routed by %s to %s:%s, want tappa:http", file, base.Host, winner.name, best.service, best.port)
		}
		tls := false
		for _, x := range winner.tls {
			for _, h := range x.hosts {
				tls = tls || h == base.Host
			}
		}
		if !tls {
			t.Errorf("%s routes %s without TLS for %s", winner.name, file, base.Host)
		}
		for k := range winner.annotations {
			if strings.HasSuffix(k, "ssl-redirect") {
				continue
			}
			if strings.Contains(k, "redirect") || strings.Contains(k, "rewrite") || strings.Contains(k, "auth-") {
				t.Errorf("%s routes %s with %s: the file must reach the app unredirected and unchanged", winner.name, file, k)
			}
		}
	}
}

// ingressPathMatches is Kubernetes path matching as ingress-nginx applies it: Exact is equality;
// Prefix (and ImplementationSpecific, which this controller treats as a prefix) matches path
// ELEMENT by element, so /v1 matches /v1 and /v1/x but not /v1x.
func ingressPathMatches(p ingressPath, path string) bool {
	if p.pathType == "Exact" {
		return p.path == path
	}
	prefix := strings.TrimSuffix(p.path, "/")
	return prefix == "" || path == prefix || strings.HasPrefix(path, prefix+"/")
}
