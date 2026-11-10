package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// TestPackaging_TheRedirectIngressRoutesExactlyTheRedirectHosts (backlog T71): 43-redirect-ingress.yaml
// is ONE Ingress named tappa-redirects -- the name 40 carried, so the deploy's apply patches the live
// object and a rollback's 40 puts the ingress-level redirect back -- whose rules are exactly the hosts
// config.Load gives a production pod as RedirectHosts (shippedPodEnv), each "/" Prefix to the tappa
// Service's http port; it carries no ingress-level redirect any more (the application answers), keeps
// one certificate per host (tappa-tls, taptime-www-tls), the cluster issuer and forced HTTPS. And the
// router built from that same configuration answers each host with a redirect to the base URL that
// keeps the path and the query.
//
// MUTANTS (measured): permanent-redirect put back -> red; a third host added to 43 -> red (not the
// pod's list); www.taptime.mt removed from redirectHostList -> red (43 routes a host the pod serves).
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
	for k := range o.annotations {
		if strings.Contains(k, "redirect") && !strings.HasSuffix(k, "/ssl-redirect") && !strings.HasSuffix(k, "/force-ssl-redirect") {
			t.Errorf("annotation %s: the ingress answers the request itself and the application never sees it", k)
		}
	}
	for _, k := range []string{"nginx.ingress.kubernetes.io/ssl-redirect", "nginx.ingress.kubernetes.io/force-ssl-redirect"} {
		if o.annotations[k] != "true" {
			t.Errorf("%s = %q, want \"true\"", k, o.annotations[k])
		}
	}
	if o.annotations["cert-manager.io/cluster-issuer"] != "letsencrypt-prod" {
		t.Errorf("cluster-issuer %q, want letsencrypt-prod", o.annotations["cert-manager.io/cluster-issuer"])
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
