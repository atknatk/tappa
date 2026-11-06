package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// baseURLCanary is in every value that carries a user part, to prove no message repeats it.
const baseURLCanary = "CANARY-not-a-real-password"

// baseURLRefused loads the configuration with TAPPA_BASE_URL set to raw and reports whether the
// variable was the one refused. Other refusals (prod's own required settings) do not count.
func baseURLRefused(t *testing.T, env, raw string) (bool, string) {
	t.Helper()
	setRequired(t)
	t.Setenv("TAPPA_ENV", env)
	t.Setenv("TAPPA_BASE_URL", raw)
	_, err := config.Load()
	if err == nil {
		return false, ""
	}
	return strings.Contains(err.Error(), "TAPPA_BASE_URL"), err.Error()
}

// TestLoad_BaseURLShape is backlog T5's table, through Load. Every refused value is refused in every
// environment, because no reader can use it; the hardened rows add https.
//
// MUTANTS (measured, 2026-11-06, each reverted; the rows that go red are named): the push of
// validBaseURL removed (every refused row and the prod-only rows); the lower-case prefix made
// case-insensitive (the three upper-case scheme rows); the prefix check removed (those and
// scheme-relative, another scheme); the query/fragment/whitespace check (the query and fragment
// rows); the user-part check (the three user rows); the host check (no host, every port and label
// row); the default-port rule (the three default-port rows); the leading-zero rule (zero on another
// port); the port range; the empty-port rule; the DNS label rule reduced to a non-empty host (the dot
// and hyphen rows, non-ASCII, punctuation); the label rule without case-folding (the upper-case host,
// the sandbox placeholder); without the underscore (the sandbox placeholder); the path rule (the
// path rows); the path rule trimming slashes (two slashes, an escaped slash); the hardened https
// check (the prod-only rows).
func TestLoad_BaseURLShape(t *testing.T) {
	untaggedOnly(t)
	for _, tc := range []struct {
		name, raw string
		dev, prod bool // accepted there
	}{
		// The values the deployments and tests use.
		{"the live value", "https://taptime.mt", true, true},
		{"a trailing slash", "https://taptime.mt/", true, true},
		{"the sandbox placeholder", "https://SANDBOX_DOMAIN", true, true},
		{"the dev default, spelled", "http://localhost:8080", true, false},
		{"loopback with a port", "http://127.0.0.1:8080", true, false},
		{"plain http on a real name", "http://taptime.mt", true, false},
		{"the default when unset", "", true, false},
		{"a port", "https://taptime.mt:8443", true, true},
		{"the other scheme's default port", "https://taptime.mt:80", true, true},
		{"an upper-case host, which an Origin still equals", "https://TapTime.MT", true, true},
		{"punycode", "https://xn--tptime-cua.mt", true, true},
		{"localhost by name", "http://localhost", true, false},
		{"a single label over https", "https://localhost", true, true},
		{"IPv6 loopback", "http://[::1]:8080", true, false},
		{"IPv6 over https", "https://[2001:db8::1]:8443/", true, true},
		// Not a URL with a scheme and a host.
		{"whitespace alone", " ", false, false},
		{"no scheme", "taptime.mt", false, false},
		{"host and port, which parses as a scheme", "localhost:8080", false, false},
		{"scheme-relative", "//taptime.mt", false, false},
		{"javascript", "javascript:alert(1)", false, false},
		{"another scheme", "ftp://taptime.mt", false, false},
		{"a scheme alone", "https://", false, false},
		{"a path and no host", "https:///admin", false, false},
		{"a port and no host", "https://:8443", false, false},
		{"opaque", "https:taptime.mt", false, false},
		// What an origin has no place for.
		{"an upper-case scheme, which the NDEF builder and the heartbeat refuse", "HTTPS://taptime.mt", false, false},
		{"a mixed-case scheme", "Http://localhost:8080", false, false},
		{"an upper-case http", "HTTP://localhost:8080", false, false},
		{"a query", "https://taptime.mt?a=1", false, false},
		{"an empty query", "https://taptime.mt?", false, false},
		{"a fragment", "https://taptime.mt#f", false, false},
		{"an empty fragment", "https://taptime.mt#", false, false},
		{"a query after a path", "https://taptime.mt/a?b", false, false},
		{"a password", "https://user:" + baseURLCanary + "@taptime.mt", false, false},
		{"a user alone", "https://" + baseURLCanary + "@taptime.mt", false, false},
		{"an empty user", "https://@taptime.mt", false, false},
		// Whitespace and hosts no Origin header could match.
		{"leading space", " https://taptime.mt", false, false},
		{"trailing space", "https://taptime.mt ", false, false},
		{"a space in the path", "https://taptime.mt/a b", false, false},
		{"a tab in the path", "https://taptime.mt/a\tb", false, false},
		{"a newline", "https://taptime.mt\n", false, false},
		// Hosts and ports a browser spells differently, so sameOrigin is false on every POST.
		{"https's default port", "https://taptime.mt:443", false, false},
		{"http's default port", "http://localhost:80", false, false},
		{"https's default port on IPv6", "https://[2001:db8::1]:443", false, false},
		{"a leading zero on the default port", "https://taptime.mt:0443", false, false},
		{"a leading zero on another port", "https://taptime.mt:08443", false, false},
		{"a trailing dot", "https://taptime.mt.", false, false},
		{"a trailing dot before a port", "https://taptime.mt.:8443", false, false},
		{"a dot alone", "https://.", false, false},
		{"two dots", "https://..", false, false},
		{"a leading dot", "https://.taptime.mt", false, false},
		{"a doubled dot", "https://taptime..mt", false, false},
		{"a hyphen alone", "https://-", false, false},
		{"a label starting with a hyphen", "https://-taptime.mt", false, false},
		{"a label ending with a hyphen", "https://taptime-.mt", false, false},
		// A path: every link it mints is under a prefix nothing mounts.
		{"a path", "https://taptime.mt/app", false, false},
		{"a path with a trailing slash", "https://taptime.mt/app/", false, false},
		{"two slashes", "https://taptime.mt//", false, false},
		{"an escaped slash", "https://taptime.mt/%2f", false, false},
		{"markup in the path", `https://taptime.mt/"><b>`, false, false},
		{"a port out of range", "https://taptime.mt:99999", false, false},
		{"port zero", "https://taptime.mt:0", false, false},
		{"an empty port", "https://taptime.mt:", false, false},
		{"a port that is a word", "https://taptime.mt:abc", false, false},
		{"an open bracket", "https://[::1", false, false},
		{"brackets around a name", "https://[taptime.mt]", false, false},
		{"brackets around IPv4", "https://[127.0.0.1]", false, false},
		{"a non-ASCII host", "https://täptime.mt", false, false},
		{"punctuation in the host", `https://taptime.mt"><b`, false, false},
		{"a backslash before the at sign", `https://evil.test\@taptime.mt`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for env, accepted := range map[string]bool{config.EnvDev: tc.dev, config.EnvStaging: tc.dev, config.EnvProd: tc.prod} {
				refused, msg := baseURLRefused(t, env, tc.raw)
				if refused == accepted {
					t.Errorf("TAPPA_ENV=%s: TAPPA_BASE_URL refused = %v, want %v\n%s", env, refused, !accepted, msg)
				}
				if refused && tc.raw != "" && strings.TrimSpace(tc.raw) != "" && strings.Contains(msg, tc.raw) {
					t.Errorf("TAPPA_ENV=%s: the refusal repeats the value", env)
				}
				if strings.Contains(msg, baseURLCanary) {
					t.Errorf("TAPPA_ENV=%s: the refusal carries the value's user part", env)
				}
			}
		})
	}
}

// TestLoad_TheShippedBaseURLsPass reads the two ConfigMaps rather than a copy of their values: a
// refusal of the live value would stop every pod at the next rollout.
func TestLoad_TheShippedBaseURLsPass(t *testing.T) {
	untaggedOnly(t)
	line := regexp.MustCompile(`(?m)^\s*TAPPA_BASE_URL:\s*"([^"]+)"`)
	for _, file := range []string{"k8s/05-config.yaml", "sandbox/05-config.yaml"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		m := line.FindSubmatch(raw)
		if m == nil {
			t.Fatalf("%s names no TAPPA_BASE_URL", file)
		}
		if refused, msg := baseURLRefused(t, config.EnvProd, string(m[1])); refused {
			t.Errorf("%s: the shipped value is refused:\n%s", file, msg)
		}
	}
}
