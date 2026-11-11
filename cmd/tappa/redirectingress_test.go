package main

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// TestPackaging_TheRedirectIngressRoutesExactlyTheRedirectHosts (backlog T71): 43-redirect-ingress.yaml
// is ONE Ingress named tappa-redirects -- the name 40 carried, so the deploy's apply patches the live
// object and a rollback's 40 puts the ingress-level redirect back -- whose rules are exactly the hosts
// config.Load gives a production pod as RedirectHosts (shippedPodEnv), each "/" Prefix to the tappa
// Service's http port; it carries no ingress-level redirect any more (the application answers), keeps
// one certificate per host (tappa-tls, taptime-www-tls), and its annotations are EXACTLY the cluster
// issuer and forced HTTPS (T257: nothing else, so no snippet or auth-* can answer first). And the
// router built from that same configuration answers each host with a redirect to the base URL that
// keeps the path and the query.
//
// MUTANTS (measured): permanent-redirect put back -> red; a third host added to 43 -> red (not the
// pod's list); www.taptime.mt removed from redirectHostList -> red (43 routes a host the pod serves).
// T257 (measured): a configuration-snippet annotation added to 43 -> red; force-ssl-redirect removed
// -> red.
func TestPackaging_TheRedirectIngressRoutesExactlyTheRedirectHosts(t *testing.T) {
	productionBuildOnly(t)
	objs := ingressesIn(t, redirectIngressFile)
	if len(objs) != 1 {
		t.Fatalf("%s holds %d Ingress(es), want 1", redirectIngressFile, len(objs))
	}
	o := objs[0]
	_, env := shippedPodEnv(t, "postgres://tappa_app@127.0.0.1:1/tappa?sslmode=disable")
	for k, v := range env {
		t.Setenv(k, v)
	}
	for _, name := range config.OperatorSurfaceVariables() {
		if _, set := env[name]; !set {
			t.Setenv(name, "")
		}
	}
	t.Setenv("DATABASE_MIGRATE_URL", "")
	t.Setenv("TAPPA_TAG_KEK_PREVIOUS", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load refused the shipped ConfigMap: %v", err)
	}

	if o.name != "tappa-redirects" || o.namespace != "tappa" || o.class != "nginx" {
		t.Errorf("name %q, namespace %q, class %q; want tappa-redirects, tappa, nginx", o.name, o.namespace, o.class)
	}
	var hosts []string
	want := fmt.Sprint([]ingressPath{{path: "/", pathType: "Prefix", service: "tappa", port: "http"}})
	for _, r := range o.rules {
		hosts = append(hosts, r.host)
		if fmt.Sprint(r.paths) != want {
			t.Errorf("%s: paths %+v, want %s alone", r.host, r.paths, want)
		}
	}
	slices.Sort(hosts)
	pod := slices.Sorted(slices.Values(cfg.RedirectHosts))
	if len(pod) == 0 || !slices.Equal(hosts, pod) {
		t.Errorf("%s routes %v, the pod redirects %v: they must be one list", redirectIngressFile, hosts, pod)
	}
	// The annotation set is pinned EXACTLY (backlog T257): any annotation beyond these three --
	// a configuration-snippet, server-snippet or auth-* -- would let the controller answer or gate the
	// request before the application's redirect runs, and a test that only refused names containing
	// "redirect" stayed green for every one of them.
	wantAnnotations := map[string]string{
		"cert-manager.io/cluster-issuer":                 "letsencrypt-prod",
		"nginx.ingress.kubernetes.io/ssl-redirect":       "true",
		"nginx.ingress.kubernetes.io/force-ssl-redirect": "true",
	}
	if !maps.Equal(o.annotations, wantAnnotations) {
		t.Errorf("%s's annotations are %v, want exactly %v: the ingress must neither answer the request itself "+
			"(the application never sees it) nor drop the issuer or forced HTTPS", redirectIngressFile, o.annotations, wantAnnotations)
	}
	tls := map[string]string{}
	for _, x := range o.tls {
		if len(x.hosts) != 1 {
			t.Errorf("tls %+v: one host per certificate, so neither can hold the other hostage", x)
			continue
		}
		tls[x.hosts[0]] = x.secret
	}
	if fmt.Sprint(tls) != fmt.Sprint(map[string]string{"tappa.everva.com.tr": "tappa-tls", "www.taptime.mt": "taptime-www-tls"}) {
		t.Errorf("tls %v, want tappa-tls and taptime-www-tls unchanged", tls)
	}

	h := httpx.NewRouter(cfg, nil)
	for _, host := range cfg.RedirectHosts {
		r := httptest.NewRequest(http.MethodGet, "https://"+host+"/activate?x=1", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if loc := w.Result().Header.Get("Location"); w.Code != http.StatusMovedPermanently || loc != cfg.BaseURL+"/activate?x=1" {
			t.Errorf("GET %s/activate?x=1 with the shipped configuration: %d %q, want 301 to %s/activate?x=1",
				host, w.Code, loc, cfg.BaseURL)
		}
	}
}
