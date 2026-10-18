package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// THE SANDBOX'S MANIFESTS (M11 API-11, ADR 0028 §11), deploy/sandbox/*.yaml. They are NOT applied
// (the sandbox's domain is the user's to choose); these tests hold what they say so that the day
// they are applied they say it: a namespace of their own with Pod Security `restricted`, a quota and
// limits, their own Postgres behind their own NetworkPolicy, the sandbox image with production's
// hardening, a configuration only the sandbox build starts with, no real e-mail, no taptime.mt
// host, and a Secret template of placeholders whose keys are the ones the server reads.

const sandboxNamespace = "tappa-sandbox"

type sandboxDoc struct {
	file, kind, name, namespace, body string
}

func sandboxDocs(t *testing.T) []sandboxDoc {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, "deploy", "sandbox", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("CONTROL FAILED: deploy/sandbox has no manifests (%v)", err)
	}
	topKind := regexp.MustCompile(`(?m)^kind:\s*(\S+)\s*$`)
	metaField := func(body, field string) string {
		meta := blockUnder(body, "metadata:")
		for _, l := range meta {
			if indentOf(l) == 2 {
				if v, ok := strings.CutPrefix(strings.TrimSpace(l), field+":"); ok {
					return unquote(strings.TrimSpace(v))
				}
			}
		}
		return ""
	}
	var out []sandboxDoc
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range regexp.MustCompile(`(?m)^---\s*$`).Split(stripYAMLComments(string(b)), -1) {
			if strings.TrimSpace(doc) == "" {
				continue
			}
			m := topKind.FindStringSubmatch(doc)
			if m == nil {
				t.Fatalf("%s: a document without a kind", filepath.Base(f))
			}
			out = append(out, sandboxDoc{file: filepath.Base(f), kind: m[1], name: metaField(doc, "name"), namespace: metaField(doc, "namespace"), body: doc})
		}
	}
	return out
}

func sandboxDoc1(t *testing.T, docs []sandboxDoc, kind, name string) sandboxDoc {
	t.Helper()
	for _, d := range docs {
		if d.kind == kind && d.name == name {
			return d
		}
	}
	t.Fatalf("deploy/sandbox has no %s %s", kind, name)
	return sandboxDoc{}
}

// TestSandboxManifests_AreTheSandboxsOwn: every object is in the sandbox's namespace and the
// namespace enforces, audits and warns `restricted`; the quota and the limit range exist; every pod
// is production's hardening; the server is the sandbox image and its secrets are the ones it reads
// (the operator's four required, no SMTP credential, no migration DSN); no host is taptime.mt's and
// every host is the SANDBOX_DOMAIN placeholder's; the Secret template holds only placeholders; and
// neither workflow applies, builds or pushes any of it (the deploy path comes with the domain).
//
// MUTANTS (measured): one object's namespace set to tappa; the warn label dropped; the LimitRange
// deleted; the server's image set to docker.io/atknatk/tappa; TAPPA_OPERATOR_HOST made optional; an
// Ingress host that is not the placeholder's; `docker build --target app-sandbox` added to
// deploy.yml -- each red.
func TestSandboxManifests_AreTheSandboxsOwn(t *testing.T) {
	docs := sandboxDocs(t)
	var kinds []string
	for _, d := range docs {
		kinds = append(kinds, d.kind)
		switch {
		case d.kind == "Namespace":
			if d.name != sandboxNamespace {
				t.Errorf("%s: the namespace is %q", d.file, d.name)
			}
			for _, mode := range []string{"enforce", "audit", "warn"} {
				if !strings.Contains(d.body, "pod-security.kubernetes.io/"+mode+": restricted") {
					t.Errorf("%s: Pod Security %s is not restricted", d.file, mode)
				}
			}
		case d.namespace != sandboxNamespace:
			t.Errorf("%s: %s %s is in namespace %q, not %s", d.file, d.kind, d.name, d.namespace, sandboxNamespace)
		}
		if strings.Contains(d.body, "taptime.mt") {
			t.Errorf("%s: %s %s names a taptime.mt host; the sandbox's domain is its own (K-3)", d.file, d.kind, d.name)
		}
		if strings.Contains(d.body, "template:") || d.kind == "Deployment" || d.kind == "StatefulSet" || d.kind == "Job" {
			for _, want := range []string{"runAsNonRoot: true", "type: RuntimeDefault", "automountServiceAccountToken: false",
				"allowPrivilegeEscalation: false", "readOnlyRootFilesystem: true", `drop: ["ALL"]`} {
				if !strings.Contains(d.body, want) {
					t.Errorf("%s: %s %s lacks %q", d.file, d.kind, d.name, want)
				}
			}
		}
	}
	sort.Strings(kinds)
	want := []string{"ConfigMap", "Deployment", "Ingress", "Ingress", "Job", "LimitRange", "Namespace", "NetworkPolicy",
		"ResourceQuota", "Secret", "Service", "Service", "StatefulSet"}
	if !slices.Equal(kinds, want) {
		t.Errorf("deploy/sandbox's objects are %v, want %v", kinds, want)
	}

	hosts := 0
	for _, d := range docs {
		if d.kind != "Ingress" {
			continue
		}
		for _, m := range regexp.MustCompile(`(?m)^\s*(?:- )?host:\s*(\S+)|^\s*- (\S+\.\S+|SANDBOX_DOMAIN)\s*$`).FindAllStringSubmatch(d.body, -1) {
			h := m[1] + m[2]
			hosts++
			if h != "SANDBOX_DOMAIN" && !strings.HasSuffix(h, ".SANDBOX_DOMAIN") {
				t.Errorf("%s: the Ingress %s names the host %q, not the SANDBOX_DOMAIN placeholder", d.file, d.name, h)
			}
		}
	}

	if hosts < 4 {
		t.Fatalf("CONTROL FAILED: the Ingresses' hosts read as %d entries", hosts)
	}

	app := sandboxDoc1(t, docs, "Deployment", "tappa")
	if !strings.Contains(app.body, "image: docker.io/atknatk/tappa-sandbox:") || strings.Contains(app.body, "image: docker.io/atknatk/tappa:") {
		t.Errorf("the sandbox server's image is not the sandbox image")
	}
	env := containerEnvEntries(t, app.body, "tappa")
	required := []string{"DATABASE_URL", "TAPPA_SESSION_HMAC_KEY", "TAPPA_TAG_KEK", "TAPPA_INVITE_HMAC_KEY", "TAPPA_API_KEY_HMAC_KEY", "TAPPA_WEBHOOK_KEK"}
	required = append(required, config.OperatorSurfaceVariables()...)
	for _, name := range required {
		if e, ok := env[name]; !ok || !e.hasSource || e.optional {
			t.Errorf("the sandbox server does not REQUIRE %s from its Secret (%+v, present %v)", name, e, ok)
		}
	}
	for _, name := range append(config.SMTPCredentialVariables(), "DATABASE_MIGRATE_URL") {
		if _, ok := env[name]; ok {
			t.Errorf("the sandbox server is given %s", name)
		}
	}

	secret := sandboxDoc1(t, docs, "Secret", "tappa-secrets-example-do-not-apply")
	values := configMapValues(t, secret.body)
	for _, name := range required {
		if _, ok := values[name]; !ok {
			t.Errorf("the Secret template has no %s", name)
		}
	}
	for k, v := range values {
		if !strings.Contains(v, "REPLACE") && !strings.Contains(v, "SANDBOX_DOMAIN") {
			t.Errorf("the Secret template's %s is not a placeholder", k)
		}
	}

	for _, wf := range []string{"deploy.yml", "ci.yml"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", wf))
		if err != nil {
			t.Fatal(err)
		}
		code := strings.Join(codeLinesOf(string(b)), "\n")
		for _, never := range []string{"deploy/sandbox", "app-sandbox", "build-sandbox", "tappa-sandbox"} {
			if strings.Contains(code, never) {
				t.Errorf("%s names %q: the sandbox's build, push and apply are not wired before its domain exists", wf, never)
			}
		}
	}
}

// TestSandboxManifests_TheConfigMapStartsOnlyTheSandboxBuild: the ConfigMap, with the placeholder
// replaced and a distinct dummy for each Secret key, is a configuration the sandbox build loads --
// hardened, both e-mail flows off, the API on api.<domain> -- and the untagged build refuses. Read
// in whichever build runs it.
func TestSandboxManifests_TheConfigMapStartsOnlyTheSandboxBuild(t *testing.T) {
	docs := sandboxDocs(t)
	cm := configMapValues(t, sandboxDoc1(t, docs, "ConfigMap", "tappa-config").body)
	if cm["TAPPA_ENV"] != config.EnvSandbox || cm["TAPPA_RESET_DELIVERY"] != config.ResetDeliveryNone || cm["TAPPA_INVITE_DELIVERY"] != config.InviteDeliveryPanel {
		t.Fatalf("the ConfigMap: env %q reset %q invite %q", cm["TAPPA_ENV"], cm["TAPPA_RESET_DELIVERY"], cm["TAPPA_INVITE_DELIVERY"])
	}
	for k := range cm {
		if strings.HasPrefix(k, "TAPPA_SMTP_") || strings.HasPrefix(k, "TAPPA_MAIL_") {
			t.Errorf("the sandbox ConfigMap configures e-mail (%s)", k)
		}
	}
	for _, name := range append(append([]string{"DATABASE_MIGRATE_URL", "TAPPA_TAG_KEK_PREVIOUS", "TAPPA_DEV_TOOLS"},
		config.SMTPCredentialVariables()...), "TAPPA_SMTP_HOST", "TAPPA_SMTP_PORT", "TAPPA_MAIL_FROM", "TAPPA_MAIL_REPLY_TO") {
		t.Setenv(name, "")
	}
	for k, v := range cm {
		t.Setenv(k, strings.ReplaceAll(v, "SANDBOX_DOMAIN", "sandbox.example"))
	}
	key := func(c byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{c}, 32)) }
	for name, v := range map[string]string{
		"DATABASE_URL": "postgres://tappa_app@127.0.0.1:1/tappa", "TAPPA_SESSION_HMAC_KEY": key('S'), "TAPPA_TAG_KEK": key('K'),
		"TAPPA_INVITE_HMAC_KEY": key('I'), "TAPPA_API_KEY_HMAC_KEY": key('A'), "TAPPA_WEBHOOK_KEK": key('W'),
		"TAPPA_OPERATOR_DATABASE_URL": "postgres://tappa_operator@127.0.0.1:1/tappa", "TAPPA_OPERATOR_TOTP_KEK": key('T'),
		"TAPPA_OPERATOR_TOKEN_HMAC_KEY": key('O'), "TAPPA_OPERATOR_HOST": "ops.sandbox.example",
	} {
		t.Setenv(name, v)
	}
	c, err := config.Load()
	if !config.SandboxBuild() {
		if err == nil || !strings.Contains(err.Error(), "built without the sandbox tag") {
			t.Fatalf("the untagged build loaded the sandbox's ConfigMap: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("the sandbox build refuses the sandbox's ConfigMap: %v", err)
	}
	if !c.IsHardened() || c.APIHost != "api.sandbox.example" || c.OperatorHost != "ops.sandbox.example" {
		t.Errorf("the sandbox loaded as hardened=%v api=%q ops=%q", c.IsHardened(), c.APIHost, c.OperatorHost)
	}
}
