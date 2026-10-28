package main

import (
	"maps"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/adminauth"
)

// panelIngressName is the Ingress that routes the panel's prefix without an access log
// (40-ingress.yaml, M14-10a).
const panelIngressName = "tappa-panel"

const (
	accessLogAnnotation = "nginx.ingress.kubernetes.io/enable-access-log"
	issuerAnnotation    = "cert-manager.io/cluster-issuer"
)

// TestPackaging_ThePanelPathsAreRoutedWithoutAnAccessLog (M14-10a, ADR 0031 §5.5 S14; user
// decision 2026-10-26): every panel POST carries its tenant binding in the query, and this
// cluster's ingress-nginx logs the full request line. So, across EVERY Ingress in the tree, on
// the customer origin (05-config's TAPPA_BASE_URL host):
//
//   - the panel's prefix (adminauth.CookiePath -- the panel cookie's scope, so every panel route
//     is under it) is routed by exactly one path, a Prefix, in exactly one object, and that
//     object carries enable-access-log "false";
//   - no other path at or under the prefix exists anywhere (a more specific path on a logging
//     object -- an Exact /admin/login, say -- would win the match and log the query again);
//   - the panel object is the `tappa` Ingress field for field (host, TLS, class, namespace,
//     labels, backend and every annotation) except two: it adds enable-access-log "false" and
//     drops cert-manager's issuer, so one Certificate owns the shared secret (exactly one issuer
//     across the objects naming it);
//   - the `tappa` Ingress keeps its access log (it routes everything else).
//
// MUTANT (measured, M14-10a): enable-access-log "true" -> red; the panel path moved back onto
// `tappa` (and the panel object deleted) -> red; an Exact /admin/login added to `tappa` -> red;
// the issuer copied onto the panel object -> red; proxy-body-size 64k on the panel object -> red.
func TestPackaging_ThePanelPathsAreRoutedWithoutAnAccessLog(t *testing.T) {
	prefix := adminauth.CookiePath
	if prefix != "/admin" {
		t.Fatalf("PREMISE: the panel cookie's path is %q; this test was written for /admin", prefix)
	}
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

	var panelRoutes []string
	var panel, customer ingressObject
	for _, o := range all {
		switch o.name {
		case panelIngressName:
			panel = o
		case "tappa":
			customer = o
		}
		for _, r := range o.rules {
			if r.host != base.Host {
				continue
			}
			for _, p := range r.paths {
				if p.path == prefix || strings.HasPrefix(p.path, prefix+"/") {
					panelRoutes = append(panelRoutes, o.name+" "+p.path+" ("+p.pathType+")")
					if o.annotations[accessLogAnnotation] != "false" {
						t.Errorf("%s routes the panel's %s on %s with the access log on: the binding in the query is "+
							"written to the controller's log", o.name, p.path, base.Host)
					}
				}
			}
		}
	}
	if want := panelIngressName + " " + prefix + " (Prefix)"; len(panelRoutes) != 1 || panelRoutes[0] != want {
		t.Errorf("the panel's paths on %s are routed as %v, want exactly [%s]", base.Host, panelRoutes, want)
	}
	if panel.name == "" || customer.name == "" {
		t.Fatalf("CONTROL FAILED: %s or tappa is missing from %s", panelIngressName, customerIngressFile)
	}
	if panel.file != customerIngressFile {
		t.Errorf("%s is in %s; it belongs beside `tappa` in %s (one file per host)", panel.name, panel.file, customerIngressFile)
	}

	// The panel object is `tappa` but for the two annotations.
	want := maps.Clone(customer.annotations)
	delete(want, issuerAnnotation)
	want[accessLogAnnotation] = "false"
	if !maps.Equal(panel.annotations, want) {
		t.Errorf("%s's annotations are %v, want tappa's minus the issuer plus the access log off: %v",
			panel.name, sortedPairs(panel.annotations), sortedPairs(want))
	}
	if _, ok := customer.annotations[accessLogAnnotation]; ok {
		t.Errorf("tappa carries %s: everything but the panel keeps its access log", accessLogAnnotation)
	}
	if !maps.Equal(panel.labels, customer.labels) || panel.class != customer.class || panel.namespace != customer.namespace {
		t.Errorf("%s: labels %v class %q namespace %q, want tappa's %v %q %q", panel.name, panel.labels, panel.class,
			panel.namespace, customer.labels, customer.class, customer.namespace)
	}
	if len(panel.tls) != 1 || len(customer.tls) != 1 || panel.tls[0].secret != customer.tls[0].secret ||
		strings.Join(panel.tls[0].hosts, ",") != strings.Join(customer.tls[0].hosts, ",") {
		t.Errorf("%s's TLS %+v is not tappa's %+v: two secrets for one host make the controller pick one", panel.name,
			panel.tls, customer.tls)
	}
	if len(panel.rules) != 1 || len(panel.rules[0].paths) != 1 || len(customer.rules) != 1 || len(customer.rules[0].paths) != 1 {
		t.Fatalf("%s or tappa is not one rule with one path: %+v / %+v", panel.name, panel.rules, customer.rules)
	}
	pp, cp := panel.rules[0].paths[0], customer.rules[0].paths[0]
	if panel.rules[0].host != customer.rules[0].host || pp.service != cp.service || pp.port != cp.port {
		t.Errorf("%s routes %s to %s:%s, want tappa's %s to %s:%s", panel.name, panel.rules[0].host, pp.service,
			pp.port, customer.rules[0].host, cp.service, cp.port)
	}
	if cp.path != "/" || cp.pathType != "Prefix" {
		t.Errorf("tappa routes %s (%s), want / (Prefix): everything that is not the panel", cp.path, cp.pathType)
	}

	// One Certificate for the shared secret.
	issuers := 0
	for _, o := range all {
		for _, x := range o.tls {
			if x.secret == customer.tls[0].secret {
				if _, ok := o.annotations[issuerAnnotation]; ok {
					issuers++
				}
			}
		}
	}
	if issuers != 1 {
		t.Errorf("%d Ingresses naming %s carry %s, want exactly 1: one secret, one Certificate", issuers,
			customer.tls[0].secret, issuerAnnotation)
	}
}

// sortedPairs prints a map in a stable order for a failure message.
func sortedPairs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
