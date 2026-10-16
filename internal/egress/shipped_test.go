package egress

import (
	"bytes"
	"encoding/base64"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// shipped_test.go -- the node/ingress list AS IT SHIPS (deploy/k8s/05-config.yaml), through the path a
// production pod takes: the ConfigMap's value into config.Load, its parsed prefixes into New, and the
// dial-time gate asked about the addresses the list exists for (M11 API-13a).

// shippedConfigMap returns the tappa-config ConfigMap's keys with their values, one layer of quotes
// removed. Comment lines are dropped first (a commented-out key is not a key).
func shippedConfigMap(t *testing.T) map[string]string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s{2}([A-Z][A-Z0-9_]*):[ \t]*(.*?)[ \t]*$`).
		FindAllStringSubmatch(strings.Join(kept, "\n"), -1) {
		v := m[2]
		if u, err := strconv.Unquote(v); err == nil {
			v = u
		}
		out[m[1]] = v
	}
	return out
}

// TestEgress_TheShippedListRefusesTheNodesIPv6Network: the ConfigMap's TAPPA_WEBHOOK_BLOCKED_ADDRESSES,
// loaded by config.Load as a production pod loads it (every ConfigMap key in the environment, a
// stand-in for each Secret value), gives a Client whose gate refuses the node's measured IPv6 address
// (2a01:4f8:200:3164::2, eno1, 2026-10-16), another address of the same /64 -- Hetzner routes the
// whole /64 to this one server -- and the apex's IPv4 origin; the neighbouring /64 and the
// neighbouring IPv4 address are CONTROLS and pass. A ConfigMap that drops the IPv6 entry, narrows it
// to the one address or misspells it so config.Load refuses it is red here, before a deploy.
//
// MUTANTS (measured): the entry written as 2a01:4f8:200:3164::2 alone -> the second /64 address is
// allowed, red; the entry removed -> both IPv6 rows allowed, red.
func TestEgress_TheShippedListRefusesTheNodesIPv6Network(t *testing.T) {
	cm := shippedConfigMap(t)
	if cm["TAPPA_ENV"] != config.EnvProd {
		t.Fatalf("PREMISE: the ConfigMap parse reads TAPPA_ENV as %q, want prod", cm["TAPPA_ENV"])
	}
	if _, ok := cm["TAPPA_WEBHOOK_BLOCKED_ADDRESSES"]; !ok {
		t.Fatal("TAPPA_WEBHOOK_BLOCKED_ADDRESSES is not a ConfigMap key")
	}
	for k, v := range cm {
		t.Setenv(k, v)
	}
	key := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	t.Setenv("DATABASE_URL", "postgres://tappa_app@127.0.0.1:1/tappa?sslmode=disable")
	t.Setenv("DATABASE_MIGRATE_URL", "")
	t.Setenv("TAPPA_SESSION_HMAC_KEY", key('S'))
	t.Setenv("TAPPA_TAG_KEK", key('K'))
	t.Setenv("TAPPA_TAG_KEK_PREVIOUS", "")
	t.Setenv("TAPPA_INVITE_HMAC_KEY", key('I'))
	t.Setenv("TAPPA_API_KEY_HMAC_KEY", key('A'))
	t.Setenv("TAPPA_WEBHOOK_KEK", key('W'))
	creds := config.SMTPCredentialVariables()
	t.Setenv(creds[0], "jkq4wvz8xqp2zkv6")
	t.Setenv(creds[1], "qzx7vjw9kpq3xzt8")
	for _, name := range config.OperatorSurfaceVariables() {
		t.Setenv(name, "")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load refused the shipped ConfigMap: %v", err)
	}
	c, err := New(Options{Blocked: cfg.WebhookBlockedAddresses})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"2a01:4f8:200:3164::2",                  // the node's measured address
		"2a01:4f8:200:3164:9a3c:1:2:3",          // another address of the same /64
		"2a01:4f8:200:3164:ffff:ffff:ffff:ffff", // the /64's last address
		"144.76.158.60", "::ffff:144.76.158.60", // the apex origin, both spellings
	} {
		if c.gate.allowed(on443(netip.MustParseAddr(s))) {
			t.Errorf("%s is allowed by the shipped list: it is this node's own", s)
		}
	}
	for _, s := range []string{"2a01:4f8:200:3165::2", "2a01:4f8:200:3163:ffff:ffff:ffff:ffff", "144.76.158.61"} {
		if !c.gate.allowed(on443(netip.MustParseAddr(s))) {
			t.Errorf("%s is refused: CONTROL -- it is not the node's", s)
		}
	}
}
