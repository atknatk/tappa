package main

// operatoringress_test.go — backlog T112: the operator host's two Ingresses
// (deploy/k8s/41-operator-ingress.yaml), which lived only in the cluster from 2026-10-09
// until this file's manifest was written.
//
// THREAT MODEL, stated once: these pins are against ACCIDENTAL drift — the manifest
// wandering away from what was dumped from the cluster, a body limit falling under the
// handler's own bound, a host routed twice, an Ingress file the deploy never applies, a
// README count nobody updated. A deliberate change edits the pin with it, in review.
// The cluster itself is not read: what the controller made of the two objects (one
// server block, two locations, two client_max_body_size) is the orchestrator's nginx -T
// measurement in deploy/README.md.

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/handler/operator"
)

const (
	operatorIngressFile = "41-operator-ingress.yaml"
	customerIngressFile = "40-ingress.yaml"
)

// ------------------------------------------------------------ a YAML subset parser --

// yamlLine is one comment-stripped, non-blank line of a manifest.
type yamlLine struct {
	indent int
	text   string
	no     int
}

// parseYAMLSubset parses the block-style subset the Ingress manifests are written in:
// maps, lists (`- ` items, a map item's first key on the dash line), and plain or quoted
// scalars. Anything else — a flow collection, a block scalar, an anchor, a tag, a tab —
// is an ERROR rather than a guess, so a manifest that leaves the subset turns these tests
// red instead of being read wrong. Comments go first (stripYAMLComments, whose cut at the
// first '#' is bounded for the files read here by TestPackaging_TheIngressFilesParseAsWritten).
func parseYAMLSubset(src string) (any, error) {
	var lines []yamlLine
	for i, l := range strings.Split(stripYAMLComments(src), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.Contains(l, "\t") {
			return nil, fmt.Errorf("line %d: a tab", i+1)
		}
		lines = append(lines, yamlLine{indentOf(l), strings.TrimSpace(l), i + 1})
	}
	if len(lines) == 0 {
		return nil, nil
	}
	v, next, err := parseYAMLNode(lines, 0, lines[0].indent)
	if err != nil {
		return nil, err
	}
	if next != len(lines) {
		return nil, fmt.Errorf("line %d: %q was not consumed (indentation the subset does not read)",
			lines[next].no, lines[next].text)
	}
	return v, nil
}

func isListItem(s string) bool { return s == "-" || strings.HasPrefix(s, "- ") }

func parseYAMLNode(lines []yamlLine, i, indent int) (any, int, error) {
	if isListItem(lines[i].text) {
		return parseYAMLList(lines, i, indent)
	}
	return parseYAMLMap(lines, i, indent)
}

func parseYAMLList(lines []yamlLine, i, indent int) (any, int, error) {
	var out []any
	for i < len(lines) && lines[i].indent == indent && isListItem(lines[i].text) {
		rest := strings.TrimSpace(strings.TrimPrefix(lines[i].text, "-"))
		if rest == "" {
			return nil, i, fmt.Errorf("line %d: an empty list item", lines[i].no)
		}
		if _, _, isKey := splitYAMLKey(rest); isKey {
			// A map item: its first key sits on the dash line, two columns in.
			shifted := append([]yamlLine(nil), lines...)
			shifted[i] = yamlLine{indent + 2, rest, lines[i].no}
			v, next, err := parseYAMLMap(shifted, i, indent+2)
			if err != nil {
				return nil, i, err
			}
			out = append(out, v)
			i = next
			continue
		}
		s, err := yamlScalar(rest, lines[i].no)
		if err != nil {
			return nil, i, err
		}
		out = append(out, s)
		i++
	}
	if i < len(lines) && lines[i].indent > indent {
		return nil, i, fmt.Errorf("line %d: %q is indented under a scalar list item", lines[i].no, lines[i].text)
	}
	return out, i, nil
}

func parseYAMLMap(lines []yamlLine, i, indent int) (any, int, error) {
	out := map[string]any{}
	for i < len(lines) && lines[i].indent == indent && !isListItem(lines[i].text) {
		key, val, ok := splitYAMLKey(lines[i].text)
		if !ok {
			return nil, i, fmt.Errorf("line %d: %q is not `key: value`", lines[i].no, lines[i].text)
		}
		if _, dup := out[key]; dup {
			return nil, i, fmt.Errorf("line %d: key %q twice", lines[i].no, key)
		}
		no := lines[i].no
		i++
		if val != "" {
			s, err := yamlScalar(val, no)
			if err != nil {
				return nil, i, err
			}
			out[key] = s
			continue
		}
		switch {
		case i < len(lines) && lines[i].indent > indent:
			v, next, err := parseYAMLNode(lines, i, lines[i].indent)
			if err != nil {
				return nil, i, err
			}
			out[key], i = v, next
		case i < len(lines) && lines[i].indent == indent && isListItem(lines[i].text):
			v, next, err := parseYAMLList(lines, i, indent)
			if err != nil {
				return nil, i, err
			}
			out[key], i = v, next
		default:
			out[key] = nil
		}
	}
	if i < len(lines) && lines[i].indent > indent {
		return nil, i, fmt.Errorf("line %d: %q is indented under a scalar", lines[i].no, lines[i].text)
	}
	return out, i, nil
}

// splitYAMLKey splits `key: value` or `key:`; a key is never quoted in these manifests.
func splitYAMLKey(s string) (key, val string, ok bool) {
	if strings.HasPrefix(s, `"`) || strings.HasPrefix(s, `'`) {
		return "", "", false
	}
	if k, ok := strings.CutSuffix(s, ":"); ok && !strings.Contains(k, ": ") {
		return k, "", k != ""
	}
	k, v, ok := strings.Cut(s, ": ")
	if !ok || k == "" {
		return "", "", false
	}
	return k, strings.TrimSpace(v), true
}

// yamlScalar is a plain or quoted scalar; the syntaxes outside the subset are refused.
func yamlScalar(s string, no int) (string, error) {
	for _, bad := range []string{"[", "{", "|", ">", "&", "*", "!", "%", "@", "`"} {
		if strings.HasPrefix(s, bad) {
			return "", fmt.Errorf("line %d: %q starts with %q, outside the subset", no, s, bad)
		}
	}
	return unquote(s), nil
}

// flattenYAML renders a parsed document as sorted `path=value` lines: map keys holding a
// dot or a slash are bracketed (annotations), list items are indexed. Two documents are
// the same object exactly when their flattenings are equal.
func flattenYAML(v any) []string {
	var out []string
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				p := k
				if strings.ContainsAny(k, "./") {
					p = `["` + k + `"]`
				}
				if prefix != "" && !strings.HasPrefix(p, "[") {
					p = "." + p
				}
				walk(prefix+p, c)
			}
		case []any:
			for i, c := range x {
				walk(fmt.Sprintf("%s[%d]", prefix, i), c)
			}
		case nil:
			out = append(out, prefix+"=<null>")
		default:
			out = append(out, fmt.Sprintf("%s=%v", prefix, x))
		}
	}
	walk("", v)
	sort.Strings(out)
	return out
}

// -------------------------------------------------------------- the Ingress objects --

// ingressObject is the slice of an Ingress these tests read.
type ingressObject struct {
	file        string
	name        string
	namespace   string
	class       string
	annotations map[string]string
	labels      map[string]string
	tls         []ingressTLS
	rules       []ingressRule
	flat        []string
}

type ingressTLS struct {
	hosts  []string
	secret string
}

type ingressRule struct {
	host  string
	paths []ingressPath
}

type ingressPath struct {
	path, pathType, service, port string
}

// manifestDocuments splits a manifest at its `---` lines.
func manifestDocuments(src string) []string {
	return regexp.MustCompile(`(?m)^---[ \t]*$`).Split(src, -1)
}

var kindIngressLine = regexp.MustCompile(`(?m)^kind:[ \t]*Ingress[ \t]*$`)

// ingressesIn parses every `kind: Ingress` document of one top-level manifest.
func ingressesIn(t *testing.T, file string) []ingressObject {
	t.Helper()
	var out []ingressObject
	for n, doc := range manifestDocuments(readK8s(t, file)) {
		if !kindIngressLine.MatchString(stripYAMLComments(doc)) {
			continue
		}
		v, err := parseYAMLSubset(doc)
		if err != nil {
			t.Fatalf("%s, document %d: %v", file, n+1, err)
		}
		root, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%s, document %d is not a map", file, n+1)
		}
		out = append(out, ingressFrom(t, file, root))
	}
	return out
}

func ingressFrom(t *testing.T, file string, root map[string]any) ingressObject {
	t.Helper()
	str := func(v any) string {
		s, _ := v.(string)
		return s
	}
	mapOf := func(v any) map[string]any {
		m, _ := v.(map[string]any)
		return m
	}
	listOf := func(v any) []any {
		l, _ := v.([]any)
		return l
	}
	strMap := func(v any) map[string]string {
		out := map[string]string{}
		for k, x := range mapOf(v) {
			out[k] = str(x)
		}
		return out
	}
	meta, spec := mapOf(root["metadata"]), mapOf(root["spec"])
	o := ingressObject{
		file:        file,
		name:        str(meta["name"]),
		namespace:   str(meta["namespace"]),
		class:       str(spec["ingressClassName"]),
		annotations: strMap(meta["annotations"]),
		labels:      strMap(meta["labels"]),
		flat:        flattenYAML(root),
	}
	for _, x := range listOf(spec["tls"]) {
		e := ingressTLS{secret: str(mapOf(x)["secretName"])}
		for _, h := range listOf(mapOf(x)["hosts"]) {
			e.hosts = append(e.hosts, str(h))
		}
		o.tls = append(o.tls, e)
	}
	for _, x := range listOf(spec["rules"]) {
		r := ingressRule{host: str(mapOf(x)["host"])}
		for _, p := range listOf(mapOf(mapOf(x)["http"])["paths"]) {
			svc := mapOf(mapOf(mapOf(p)["backend"])["service"])
			r.paths = append(r.paths, ingressPath{
				path:     str(mapOf(p)["path"]),
				pathType: str(mapOf(p)["pathType"]),
				service:  str(svc["name"]),
				port:     str(mapOf(svc["port"])["name"]),
			})
		}
		o.rules = append(o.rules, r)
	}
	if o.name == "" || len(o.rules) == 0 {
		t.Fatalf("%s: an Ingress parsed without a name or a rule (%+v); the parse has gone blind", file, o)
	}
	return o
}

// operatorIngresses returns 41's two objects by name — the console's ("/") and the legal
// publication's — failing unless there are exactly those two.
func operatorIngresses(t *testing.T) (console, legal ingressObject) {
	t.Helper()
	got := ingressesIn(t, operatorIngressFile)
	byName := map[string]ingressObject{}
	for _, o := range got {
		byName[o.name] = o
	}
	console, okConsole := byName["tappa-operator"]
	legal, okLegal := byName["tappa-operator-legal"]
	if len(got) != 2 || !okConsole || !okLegal {
		t.Fatalf("%s carries %d Ingress object(s) %v, want exactly tappa-operator and tappa-operator-legal",
			operatorIngressFile, len(got), byName)
	}
	return console, legal
}

// TestYAMLSubset_RefusesWhatItDoesNotRead is the parser's own table: the forms it reads
// come back as the structure written, and every form outside the subset is an error —
// a parser that guessed would make every pin below as good as its guess.
func TestYAMLSubset_RefusesWhatItDoesNotRead(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []string // nil: an error is wanted
	}{
		{"a map with a nested list of maps",
			"a: 1\nb:\n  - c: x\n    d: \"y\"\n  - c: z\n",
			[]string{"a=1", "b[0].c=x", "b[0].d=y", "b[1].c=z"}},
		{"a list at the key's own indent", "a:\n- x\n- 'y'\n", []string{"a[0]=x", "a[1]=y"}},
		{"an annotation key is bracketed", "m:\n  example.com/k: v\n", []string{`m["example.com/k"]=v`}},
		{"a comment is not data", "a: 1 # b: 2\n# c: 3\n", []string{"a=1"}},
		{"an empty value", "a:\nb: 1\n", []string{"a=<null>", "b=1"}},
		{"a flow list", "a: [x, y]\n", nil},
		{"a flow map", "a: {x: y}\n", nil},
		{"a block scalar", "a: |\n  text\n", nil},
		{"a tag", "a: !!str x\n", nil},
		{"an anchor", "a: &x y\n", nil},
		{"a duplicate key", "a: 1\na: 2\n", nil},
		{"a tab", "a:\n\tb: 1\n", nil},
		{"a stray deeper line", "a: 1\n    b: 2\n", nil},
		{"a line that is not a key", "a: 1\nb\n", nil},
	} {
		v, err := parseYAMLSubset(tc.src)
		switch {
		case tc.want == nil && err == nil:
			t.Errorf("%s: parsed as %v, want an error", tc.name, flattenYAML(v))
		case tc.want != nil && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.want != nil && strings.Join(flattenYAML(v), "\n") != strings.Join(tc.want, "\n"):
			t.Errorf("%s: got %q, want %q", tc.name, flattenYAML(v), tc.want)
		}
	}
}

// TestPackaging_TheIngressFilesParseAsWritten bounds the comment stripper's naivety for
// the two Ingress files (no '#' inside a quoted string, the shape the other manifest tests
// check for 20-app.yaml and 05-config.yaml) and is the CONTROL that the parse sees what a
// reader sees: 40's three hosts and 41's one.
func TestPackaging_TheIngressFilesParseAsWritten(t *testing.T) {
	for _, f := range []string{customerIngressFile, operatorIngressFile} {
		for i, line := range strings.Split(readK8s(t, f), "\n") {
			if h := strings.Index(line, "#"); h >= 0 && (strings.Count(line[:h], `"`)%2 == 1 || strings.Count(line[:h], `'`)%2 == 1) {
				t.Errorf("%s:%d has a '#' inside a quoted string; the comment stripper would truncate it", f, i+1)
			}
		}
	}
	hosts := func(file string) []string {
		var out []string
		for _, o := range ingressesIn(t, file) {
			for _, r := range o.rules {
				out = append(out, r.host)
			}
		}
		sort.Strings(out)
		return out
	}
	if got := hosts(customerIngressFile); strings.Join(got, ",") != "tappa.everva.com.tr,taptime.mt,www.taptime.mt" {
		t.Errorf("CONTROL FAILED: %s parsed to hosts %v", customerIngressFile, got)
	}
	if got := hosts(operatorIngressFile); strings.Join(got, ",") != "ops.taptime.mt,ops.taptime.mt" {
		t.Errorf("CONTROL FAILED: %s parsed to hosts %v", operatorIngressFile, got)
	}
}

// TestPackaging_TheOperatorIngressesAreTheLiveObjects pins 41 to the live pair as the
// orchestrator dumped it on 2026-10-10 (kubectl get -o yaml, with status, managedFields
// and the last-applied annotation removed): every field, nothing more. The dump and the
// manifest were also compared as whole documents (PyYAML equality: EQUAL for both, the
// card). A field added here — a label, the K4 allow-list — is a change to the LIVE
// objects on the next deploy, so it turns this red on purpose and is edited with it.
func TestPackaging_TheOperatorIngressesAreTheLiveObjects(t *testing.T) {
	console, legal := operatorIngresses(t)
	common := func(name, issuer, bodySize, path, pathType string) []string {
		out := []string{"apiVersion=networking.k8s.io/v1", "kind=Ingress"}
		if issuer != "" {
			out = append(out, `metadata.annotations["cert-manager.io/cluster-issuer"]=`+issuer)
		}
		out = append(out,
			`metadata.annotations["nginx.ingress.kubernetes.io/force-ssl-redirect"]=true`,
			`metadata.annotations["nginx.ingress.kubernetes.io/proxy-body-size"]=`+bodySize,
			`metadata.annotations["nginx.ingress.kubernetes.io/ssl-redirect"]=true`,
			"metadata.name="+name,
			"metadata.namespace=tappa",
			"spec.ingressClassName=nginx",
			"spec.rules[0].host=ops.taptime.mt",
			"spec.rules[0].http.paths[0].backend.service.name=tappa",
			"spec.rules[0].http.paths[0].backend.service.port.name=http",
			"spec.rules[0].http.paths[0].path="+path,
			"spec.rules[0].http.paths[0].pathType="+pathType,
			"spec.tls[0].hosts[0]=ops.taptime.mt",
			"spec.tls[0].secretName=ops-taptime-tls",
		)
		sort.Strings(out)
		return out
	}
	for _, c := range []struct {
		got  ingressObject
		want []string
	}{
		{console, common("tappa-operator", "letsencrypt-prod", "24k", "/", "Prefix")},
		{legal, common("tappa-operator-legal", "", "320k", "/operator/legal", "Exact")},
	} {
		if g, w := strings.Join(c.got.flat, "\n"), strings.Join(c.want, "\n"); g != w {
			t.Errorf("%s is not the live object as dumped.\n--- manifest:\n%s\n--- live (2026-10-10):\n%s", c.got.name, g, w)
		}
	}
}

// TestPackaging_TheOperatorIngressesShareOneHostAndOneCertificate states the pair's
// invariants independently of the snapshot: one host, the same TLS block on both (one
// secret), exactly one cert-manager issuer annotation (one Certificate for the secret),
// HTTPS forced on both, the class and backend 40's customer Ingress uses, the console
// under a Prefix "/" and the legal publication alone under an Exact path that is the
// router's own (operator.Prefix + "/legal", the route publishLegal is registered on —
// TestPackaging_TheOperatorBodyLimitsFollowTheHandlersBounds reads that registration).
func TestPackaging_TheOperatorIngressesShareOneHostAndOneCertificate(t *testing.T) {
	console, legal := operatorIngresses(t)
	var customer ingressObject
	for _, o := range ingressesIn(t, customerIngressFile) {
		if o.name == "tappa" {
			customer = o
		}
	}
	if customer.name == "" || len(customer.rules) == 0 || len(customer.rules[0].paths) == 0 {
		t.Fatalf("CONTROL FAILED: %s has no `tappa` Ingress with a path", customerIngressFile)
	}
	backend := customer.rules[0].paths[0]

	host, secret := "", ""
	issuers := 0
	for _, o := range []ingressObject{console, legal} {
		if len(o.tls) != 1 || len(o.tls[0].hosts) != 1 || o.tls[0].secret == "" {
			t.Fatalf("%s: tls = %+v, want one entry with one host and a secret", o.name, o.tls)
		}
		if host == "" {
			host, secret = o.tls[0].hosts[0], o.tls[0].secret
		}
		if o.tls[0].hosts[0] != host || o.tls[0].secret != secret {
			t.Errorf("%s: tls %+v differs from the pair's %s/%s: two secrets for one host make the controller "+
				"pick one", o.name, o.tls[0], host, secret)
		}
		if len(o.rules) != 1 || o.rules[0].host != host || len(o.rules[0].paths) != 1 {
			t.Errorf("%s: rules %+v, want one rule for %s with one path", o.name, o.rules, host)
		}
		if _, ok := o.annotations["cert-manager.io/cluster-issuer"]; ok {
			issuers++
		}
		if _, ok := o.annotations["cert-manager.io/issuer"]; ok {
			t.Errorf("%s: a namespaced cert-manager issuer; the customer hosts use the cluster issuer", o.name)
		}
		for _, k := range []string{"nginx.ingress.kubernetes.io/ssl-redirect", "nginx.ingress.kubernetes.io/force-ssl-redirect"} {
			if o.annotations[k] != "true" {
				t.Errorf("%s: %s = %q, want \"true\" (plain HTTP must not reach the operator's forms)", o.name, k, o.annotations[k])
			}
		}
		if o.class != customer.class || o.namespace != customer.namespace {
			t.Errorf("%s: class %q namespace %q, want %s's %q %q", o.name, o.class, o.namespace, customer.name,
				customer.class, customer.namespace)
		}
		if p := o.rules[0].paths[0]; p.service != backend.service || p.port != backend.port {
			t.Errorf("%s: backend %s:%s, want the customer Ingress's %s:%s (one Service, two hosts)", o.name,
				p.service, p.port, backend.service, backend.port)
		}
	}
	if issuers != 1 {
		t.Errorf("%d of the two objects carry cert-manager.io/cluster-issuer, want exactly 1: one secret, one "+
			"Certificate", issuers)
	}
	// K4, whenever it lands: an address list is per object, so one carried by a single
	// object leaves the other path open to every address. Any *source-range annotation
	// (whitelist- or allowlist-, by controller version) must be the same on both.
	for _, o := range []ingressObject{console, legal} {
		for k := range o.annotations {
			if strings.Contains(k, "source-range") && console.annotations[k] != legal.annotations[k] {
				t.Errorf("%s = %q on tappa-operator but %q on tappa-operator-legal: the path without it is open to "+
					"every address", k, console.annotations[k], legal.annotations[k])
			}
		}
	}
	if console.annotations["cert-manager.io/cluster-issuer"] != customer.annotations["cert-manager.io/cluster-issuer"] {
		t.Errorf("tappa-operator's issuer %q is not the customer host's %q", console.annotations["cert-manager.io/cluster-issuer"],
			customer.annotations["cert-manager.io/cluster-issuer"])
	}
	if p := console.rules[0].paths[0]; p.path != "/" || p.pathType != "Prefix" {
		t.Errorf("tappa-operator routes %s (%s), want / (Prefix)", p.path, p.pathType)
	}
	if p := legal.rules[0].paths[0]; p.path != operator.Prefix+"/legal" || p.pathType != "Exact" {
		t.Errorf("tappa-operator-legal routes %s (%s), want %s (Exact): only the publication gets the larger "+
			"limit", p.path, p.pathType, operator.Prefix+"/legal")
	}
}

// ------------------------------------------------------------------- body limits --

// nginxSize is client_max_body_size's syntax (bytes, or a k/m/g suffix of 1024's powers).
// "0" — nginx's "no limit" — is an error here: an operator Ingress without a limit is not
// a limit this file accepts.
func nginxSize(s string) (int64, error) {
	m := regexp.MustCompile(`^([0-9]+)([kKmMgG]?)$`).FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("%q is not an nginx size", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("%q: zero or unreadable (0 disables nginx's check)", s)
	}
	switch strings.ToLower(m[2]) {
	case "k":
		n <<= 10
	case "m":
		n <<= 20
	case "g":
		n <<= 30
	}
	return n, nil
}

// operatorBodyFacts is what internal/handler/operator's production sources say about
// request bodies, read from the AST (comments are not code, so prose cannot satisfy it).
type operatorBodyFacts struct {
	consts       map[string]int64    // maxFormBytes, maxLegalBody — evaluated
	limitUsers   map[string][]string // readForm's limit identifier -> the functions passing it
	badLimits    []string            // a readForm call whose limit is not an identifier
	otherReads   []string            // a request-body read outside readForm
	legalRoutes  []string            // "Method path under [route prefixes]" registering publishLegal
	readFormSeen int
}

// requestBodyReaders are *http.Request's members that read (or hand out) the body.
var requestBodyReaders = map[string]bool{
	"Body": true, "GetBody": true, "ParseForm": true, "ParseMultipartForm": true, "MultipartReader": true,
	"FormValue": true, "PostFormValue": true, "FormFile": true,
}

func scanOperatorBodyFacts(t *testing.T) operatorBodyFacts {
	t.Helper()
	dir := filepath.Join(repoRoot, "internal", "handler", "operator")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	facts := operatorBodyFacts{consts: map[string]int64{}, limitUsers: map[string][]string{}}
	fset := token.NewFileSet()
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files++
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				if d.Tok != token.CONST {
					continue
				}
				for _, spec := range d.Specs {
					vs := spec.(*ast.ValueSpec)
					for i, n := range vs.Names {
						if (n.Name != "maxFormBytes" && n.Name != "maxLegalBody") || i >= len(vs.Values) {
							continue
						}
						expr := string(src[fset.Position(vs.Values[i].Pos()).Offset:fset.Position(vs.Values[i].End()).Offset])
						tv, err := types.Eval(token.NewFileSet(), nil, token.NoPos, expr)
						if err != nil {
							t.Fatalf("%s: %s = %s does not evaluate on its own (%v); this pin reads literal "+
								"constant expressions only", name, n.Name, expr, err)
						}
						v, exact := constant.Int64Val(tv.Value)
						if !exact {
							t.Fatalf("%s: %s = %s is not an int64", name, n.Name, expr)
						}
						facts.consts[n.Name] = v
					}
				}
			case *ast.FuncDecl:
				scanOperatorFunc(d, &facts)
			}
		}
		scanLegalRoutes(f, nil, &facts)
	}
	if files < 5 {
		t.Fatalf("read %d production files of internal/handler/operator; the scan has gone blind", files)
	}
	return facts
}

// scanOperatorFunc records fd's readForm calls and every body read on a *http.Request
// parameter of fd or of a closure inside it.
func scanOperatorFunc(fd *ast.FuncDecl, facts *operatorBodyFacts) {
	if fd.Body == nil {
		return
	}
	requests := map[string]bool{}
	addParams := func(ft *ast.FuncType) {
		if ft == nil || ft.Params == nil {
			return
		}
		for _, p := range ft.Params.List {
			if star, ok := p.Type.(*ast.StarExpr); ok && types.ExprString(star.X) == "http.Request" {
				for _, n := range p.Names {
					requests[n.Name] = true
				}
			}
		}
	}
	addParams(fd.Type)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok {
			addParams(fl.Type)
		}
		return true
	})
	fn := fd.Name.Name
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "readForm" {
				facts.readFormSeen++
				if len(x.Args) < 3 {
					facts.badLimits = append(facts.badLimits, fn+": readForm with "+strconv.Itoa(len(x.Args))+" arguments")
					break
				}
				id, ok := x.Args[2].(*ast.Ident)
				if !ok {
					facts.badLimits = append(facts.badLimits, fn+": readForm(…, "+types.ExprString(x.Args[2])+", …)")
					break
				}
				facts.limitUsers[id.Name] = append(facts.limitUsers[id.Name], fn)
			}
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && requests[id.Name] && requestBodyReaders[x.Sel.Name] && fn != "readForm" {
				facts.otherReads = append(facts.otherReads, fn+": "+id.Name+"."+x.Sel.Name)
			}
		}
		return true
	})
}

// scanLegalRoutes records every registration of publishLegal with the path literal and
// the chain of Route/Mount prefixes it is nested in.
func scanLegalRoutes(n ast.Node, prefixes []string, facts *operatorBodyFacts) {
	ast.Inspect(n, func(m ast.Node) bool {
		call, ok := m.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if (sel.Sel.Name == "Route" || sel.Sel.Name == "Mount") && len(call.Args) > 0 {
			inner := append(append([]string(nil), prefixes...), types.ExprString(call.Args[0]))
			for _, a := range call.Args[1:] {
				scanLegalRoutes(a, inner, facts)
			}
			return false
		}
		for _, a := range call.Args {
			if s, ok := a.(*ast.SelectorExpr); ok && s.Sel.Name == "publishLegal" {
				path := "?"
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					path, _ = strconv.Unquote(lit.Value)
				}
				facts.legalRoutes = append(facts.legalRoutes, fmt.Sprintf("%s %s under %v", sel.Sel.Name, path, prefixes))
			}
		}
		return true
	})
}

// TestPackaging_TheOperatorBodyLimitsFollowTheHandlersBounds ties 41's two
// proxy-body-size values to the bounds the operator handlers set themselves, read from
// the package's AST:
//
//   - every body the surface reads goes through readForm (no other r.Body / ParseForm /
//     FormValue / multipart read), and readForm's limit is one of two constants;
//   - maxLegalBody is passed by publishLegal ALONE, which is registered as POST "/legal"
//     under Route(Prefix) and nowhere else — the Exact path of the legal Ingress;
//   - so the "/" Ingress must admit maxFormBytes and the legal one maxLegalBody: each
//     limit is ABOVE its bound (the request just past the bound gets the application's own
//     413 page), at most twice it (the margin never outgrows the bound), and the "/"
//     limit stays under maxLegalBody (the sign-in paths, before any session, cannot make
//     the controller buffer what only the publication needs).
//
// The units agree: readForm's MaxBytesReader counts the URL-encoded request body, the
// bytes client_max_body_size counts.
func TestPackaging_TheOperatorBodyLimitsFollowTheHandlersBounds(t *testing.T) {
	facts := scanOperatorBodyFacts(t)
	form, legalMax := facts.consts["maxFormBytes"], facts.consts["maxLegalBody"]
	if form <= 0 || legalMax <= 0 {
		t.Fatalf("maxFormBytes = %d, maxLegalBody = %d: a constant was not found", form, legalMax)
	}
	if len(facts.badLimits) > 0 {
		t.Errorf("readForm calls whose limit this pin cannot name: %v", facts.badLimits)
	}
	if len(facts.otherReads) > 0 {
		t.Errorf("request-body reads outside readForm, under no limit the Ingress is sized for: %v", facts.otherReads)
	}
	var limits []string
	for id := range facts.limitUsers {
		limits = append(limits, id)
	}
	sort.Strings(limits)
	if strings.Join(limits, ",") != "maxFormBytes,maxLegalBody" {
		t.Errorf("readForm is called with the limits %v, want exactly maxFormBytes and maxLegalBody", limits)
	}
	if users := facts.limitUsers["maxLegalBody"]; len(users) != 1 || users[0] != "publishLegal" {
		t.Errorf("maxLegalBody is passed by %v, want publishLegal alone: any other route under the 24k Ingress "+
			"would refuse at the controller what its handler accepts", users)
	}
	// CONTROL: the scan saw the sign-in path's readForm, so silence above is not blindness.
	if n := len(facts.limitUsers["maxFormBytes"]); n < 3 || facts.readFormSeen < 4 {
		t.Errorf("CONTROL FAILED: %d readForm calls in all, %d with maxFormBytes; the scan has gone blind",
			facts.readFormSeen, n)
	}
	if strings.Join(facts.legalRoutes, ";") != "Post /legal under [Prefix]" {
		t.Errorf("publishLegal is registered as %v, want exactly [Post /legal under [Prefix]] — the Exact path "+
			"the legal Ingress routes", facts.legalRoutes)
	}

	console, legal := operatorIngresses(t)
	size := func(o ingressObject) int64 {
		n, err := nginxSize(o.annotations["nginx.ingress.kubernetes.io/proxy-body-size"])
		if err != nil {
			t.Fatalf("%s: proxy-body-size: %v", o.name, err)
		}
		return n
	}
	consoleLimit, legalLimit := size(console), size(legal)
	for _, c := range []struct {
		name         string
		limit, bound int64
		boundName    string
	}{
		{console.name, consoleLimit, form, "maxFormBytes"},
		{legal.name, legalLimit, legalMax, "maxLegalBody"},
	} {
		if c.limit <= c.bound {
			t.Errorf("%s: proxy-body-size %d bytes is not above %s = %d: the controller would refuse, with its "+
				"own unbranded 413, bodies the handler accepts", c.name, c.limit, c.boundName, c.bound)
		}
		if c.limit > 2*c.bound {
			t.Errorf("%s: proxy-body-size %d bytes is more than twice %s = %d: the margin has outgrown the bound",
				c.name, c.limit, c.boundName, c.bound)
		}
	}
	if consoleLimit >= legalMax {
		t.Errorf("tappa-operator's limit %d is not under maxLegalBody = %d: every path, the sign-in ones before "+
			"any session included, could make the controller buffer what only the publication needs",
			consoleLimit, legalMax)
	}
}

// TestNginxSize is the size parser's table, including the value that must never pass.
func TestNginxSize(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64 // 0: an error
	}{
		{"24k", 24 << 10}, {"320k", 320 << 10}, {"1m", 1 << 20}, {"16384", 16384}, {"2G", 2 << 30}, {"24K", 24 << 10},
		{"0", 0}, {"", 0}, {"24 k", 0}, {"24kb", 0}, {"-1", 0}, {"1.5m", 0},
	} {
		got, err := nginxSize(tc.in)
		if tc.want == 0 && err == nil {
			t.Errorf("nginxSize(%q) = %d, want an error", tc.in, got)
		}
		if tc.want != 0 && (err != nil || got != tc.want) {
			t.Errorf("nginxSize(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
}

// ---------------------------------------------------------- hosts across the tree --

// TestPackaging_EveryIngressHostIsRoutedByOneFile: across every top-level manifest, a
// host (rule or TLS) belongs to exactly one file and no host is a wildcard, the operator
// file carries the operator host alone, the customer origin (05-config's TAPPA_BASE_URL)
// is 40's and never 41's (config.Load refuses an operator host equal to it), and the
// operator's TLS secret is no customer host's — a stuck ops challenge cannot hold a
// customer certificate hostage. Before T112 the tree had no ops host at all, and ADR 0020's
// inventory recorded exactly that.
func TestPackaging_EveryIngressHostIsRoutedByOneFile(t *testing.T) {
	manifests, err := filepath.Glob(filepath.Join(repoRoot, "deploy", "k8s", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	hostFiles := map[string]map[string]bool{}
	secretFiles := map[string]map[string]bool{}
	note := func(m map[string]map[string]bool, k, file string) {
		if m[k] == nil {
			m[k] = map[string]bool{}
		}
		m[k][file] = true
	}
	ingressFiles := 0
	for _, path := range manifests {
		file := filepath.Base(path)
		objs := ingressesIn(t, file)
		if len(objs) > 0 {
			ingressFiles++
		}
		for _, o := range objs {
			for _, r := range o.rules {
				note(hostFiles, r.host, file)
			}
			for _, x := range o.tls {
				note(secretFiles, x.secret, file)
				for _, h := range x.hosts {
					note(hostFiles, h, file)
				}
			}
		}
	}
	if ingressFiles != 2 || len(hostFiles) < 4 {
		t.Fatalf("CONTROL FAILED: %d Ingress files and %d hosts found, want 2 files and at least 4 hosts", ingressFiles,
			len(hostFiles))
	}
	for h, files := range hostFiles {
		if len(files) != 1 {
			t.Errorf("host %q is routed by %d files %v: two Ingress files competing for one host", h, len(files), files)
		}
		if strings.HasPrefix(h, "*") || h == "" {
			t.Errorf("host %q: a wildcard or empty host would also answer for the operator host", h)
		}
	}
	var opHosts []string
	for h, files := range hostFiles {
		if files[operatorIngressFile] {
			opHosts = append(opHosts, h)
		}
	}
	if strings.Join(opHosts, ",") != "ops.taptime.mt" {
		t.Errorf("%s routes %v, want ops.taptime.mt alone", operatorIngressFile, opHosts)
	}
	base, err := url.Parse(configMapValues(t, readK8s(t, "05-config.yaml"))["TAPPA_BASE_URL"])
	if err != nil || base.Host == "" {
		t.Fatalf("05-config.yaml's TAPPA_BASE_URL does not parse: %v", err)
	}
	if !hostFiles[base.Host][customerIngressFile] || hostFiles[base.Host][operatorIngressFile] {
		t.Errorf("the customer origin %q is routed by %v, want %s alone", base.Host, hostFiles[base.Host], customerIngressFile)
	}
	if files := secretFiles["ops-taptime-tls"]; len(files) != 1 || !files[operatorIngressFile] {
		t.Errorf("secret ops-taptime-tls is used by %v, want %s alone", files, operatorIngressFile)
	}
}

// ------------------------------------------------------------- deploy and README --

var deployApplyLine = regexp.MustCompile(`kubectl apply -f deploy/k8s/([A-Za-z0-9._-]+\.yaml)`)

// codeLinesOf drops the whole-line comments of a workflow or script.
func codeLinesOf(src string) []string {
	var out []string
	for _, l := range strings.Split(src, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			out = append(out, l)
		}
	}
	return out
}

// TestPackaging_TheDeployAppliesEveryIngressFile closes T112's class for Ingresses: a
// top-level manifest carrying a `kind: Ingress` that deploy.yml does not apply is the
// cluster-only object this backlog item was. 41 is applied in "Roll out the server",
// after 40 and before the rollout wait, and the deploy identity's ingresses rule in
// 01-rbac.yaml carries the verbs `kubectl apply` needs on an existing or a new object
// (get, create, patch) — and no delete, which is why closing the surface is the
// operator's (41's header).
func TestPackaging_TheDeployAppliesEveryIngressFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	code := codeLinesOf(string(b))
	applied := map[string]int{}
	step, line40, line41, rollout := "", -1, -1, -1
	for i, l := range code {
		if s, ok := strings.CutPrefix(strings.TrimSpace(l), "- name: "); ok {
			step = s
		}
		for _, m := range deployApplyLine.FindAllStringSubmatch(l, -1) {
			applied[m[1]]++
			if step == "Roll out the server" {
				switch m[1] {
				case customerIngressFile:
					line40 = i
				case operatorIngressFile:
					line41 = i
				}
			}
		}
		if step == "Roll out the server" && strings.Contains(l, "rollout status deployment/tappa") {
			rollout = i
		}
	}
	manifests, err := filepath.Glob(filepath.Join(repoRoot, "deploy", "k8s", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ingressFiles := 0
	for _, path := range manifests {
		file := filepath.Base(path)
		if !kindIngressLine.MatchString(stripYAMLComments(readK8s(t, file))) {
			continue
		}
		ingressFiles++
		if applied[file] != 1 {
			t.Errorf("deploy.yml applies %s %d time(s), want once: an Ingress the deploy does not apply lives only "+
				"in the cluster (T112)", file, applied[file])
		}
	}
	if ingressFiles != 2 {
		t.Errorf("CONTROL FAILED: %d Ingress files found, want 2 (40 and 41)", ingressFiles)
	}
	if line40 < 0 || line41 < 0 || rollout < 0 || line40 > line41 || line41 > rollout {
		t.Errorf("in \"Roll out the server\": 40 at %d, 41 at %d, rollout wait at %d; want 40 < 41 < the wait",
			line40, line41, rollout)
	}

	rbac := stripYAMLComments(readK8s(t, "01-rbac.yaml"))
	m := regexp.MustCompile(`resources:\s*\["ingresses"\]\s*\n\s*verbs:\s*\[([^\]]*)\]`).FindStringSubmatch(rbac)
	if m == nil {
		t.Fatal("01-rbac.yaml has no `resources: [\"ingresses\"]` rule followed by its verbs")
	}
	verbs := map[string]bool{}
	for _, v := range strings.Split(m[1], ",") {
		verbs[unquote(strings.TrimSpace(v))] = true
	}
	for _, need := range []string{"get", "create", "patch"} {
		if !verbs[need] {
			t.Errorf("01-rbac.yaml's ingresses rule lacks %q; deploy.yml's apply of 40 and 41 would be Forbidden", need)
		}
	}
	if verbs["delete"] || verbs["*"] {
		t.Errorf("01-rbac.yaml's ingresses rule grants %v: the deploy identity was never meant to delete an Ingress", m[1])
	}
}

// clusterScopedKinds and namespacedKinds classify every kind the manifests carry; a kind
// in neither list stops the count rather than being guessed.
var (
	clusterScopedKinds = map[string]bool{"Namespace": true, "ClusterRole": true, "ClusterRoleBinding": true,
		"PersistentVolume": true, "StorageClass": true, "IngressClass": true, "ClusterIssuer": true,
		"CustomResourceDefinition": true, "PriorityClass": true}
	namespacedKinds = map[string]bool{"ServiceAccount": true, "Role": true, "RoleBinding": true, "ConfigMap": true,
		"Secret": true, "Service": true, "Deployment": true, "StatefulSet": true, "Job": true, "CronJob": true,
		"Ingress": true, "NetworkPolicy": true, "PersistentVolumeClaim": true}
)

// readmeCountAnchor is the one sentence of deploy/README.md that carries the current
// total/cluster-scoped/namespaced object count of deploy/k8s/*.yaml.
const readmeCountAnchor = "**Güncel sayım: "

// TestPackaging_TheReadmeCountsTheManifestObjects: deploy/README.md's object count went
// 14 -> 15 -> 16 -> 17 with a by-hand recount each time, and one step (40-ingress.yaml's
// second Ingress) was missed for a month. The count is now derived — `kind:` lines at
// column 0 outside comments, the README's own `grep -h '^kind:'` — and the README's
// anchored sentence must say it; the README's "no Secret" claim and its file table
// (every top-level manifest named as `k8s/<file>`) are read with it.
func TestPackaging_TheReadmeCountsTheManifestObjects(t *testing.T) {
	manifests, err := filepath.Glob(filepath.Join(repoRoot, "deploy", "k8s", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	readmeBytes, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(readmeBytes)
	total, cluster, namespaced := 0, 0, 0
	kindLine := regexp.MustCompile(`(?m)^kind:[ \t]*(\S+)[ \t]*$`)
	for _, path := range manifests {
		file := filepath.Base(path)
		for _, m := range kindLine.FindAllStringSubmatch(stripYAMLComments(readK8s(t, file)), -1) {
			total++
			switch {
			case m[1] == "Secret":
				t.Errorf("%s carries a Secret; the README says the directory has none, and a Secret in the tree is "+
					"§4.7's subject", file)
				namespaced++
			case clusterScopedKinds[m[1]]:
				cluster++
			case namespacedKinds[m[1]]:
				namespaced++
			default:
				t.Errorf("%s: kind %q is in neither scope list of this test; classify it (kubectl api-resources) "+
					"before it is counted", file, m[1])
			}
		}
		if !strings.Contains(readme, "`k8s/"+file+"`") {
			t.Errorf("deploy/README.md's file table does not name `k8s/%s`: a manifest with no row says nobody "+
				"recorded who applies it", file)
		}
	}
	if total < 17 {
		t.Fatalf("CONTROL FAILED: counted %d objects; the directory held 17 before T112", total)
	}
	want := fmt.Sprintf("%s%d/%d/%d**", readmeCountAnchor, total, cluster, namespaced)
	if n := strings.Count(readme, readmeCountAnchor); n != 1 {
		t.Errorf("deploy/README.md carries the count anchor %q %d times, want once", readmeCountAnchor, n)
	}
	if !strings.Contains(readme, want) {
		t.Errorf("deploy/README.md does not say %q: the tree holds %d objects, %d cluster-scoped, %d namespaced",
			want, total, cluster, namespaced)
	}
}
