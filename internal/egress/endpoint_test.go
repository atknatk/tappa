package egress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// TestEndpoint_TheURLRule is ADR 0028 §6's URL rule, one refusal at a time, and the one canonical
// spelling of what passes. Every refusal is a fixed sentinel whose text never holds the address.
func TestEndpoint_TheURLRule(t *testing.T) {
	refused := []struct {
		raw  string
		want error
	}{
		{"", ErrNotAnswered},
		{"http://hooks.example.com/in", ErrNotHTTPS},
		{"HTTPS://hooks.example.com/in", ErrNotHTTPS},
		{"ftp://hooks.example.com/in", ErrNotHTTPS},
		{"https:hooks.example.com", ErrNotHTTPS},
		{"//hooks.example.com/in", ErrNotHTTPS},
		{"https://hooks.example.com:8443/in", ErrPort},
		{"https://hooks.example.com:80/in", ErrPort},
		{"https://hooks.example.com:/in", ErrPort},
		{"https://hooks.example.com:0443/in", ErrPort},
		{"https://203.0.113.10/in", ErrIPLiteral},
		{"https://8.8.8.8/in", ErrIPLiteral},
		{"https://[2001:4860::8888]/in", ErrIPLiteral},
		{"https://[::ffff:10.0.0.1]/in", ErrIPLiteral},
		{"https://[fe80::1%25eth0]/in", ErrIPLiteral},
		{"https://2130706433/in", ErrHost},
		{"https://127.1/in", ErrIPLiteral},
		{"https://0x7f.0x1/in", ErrIPLiteral},
		{"https://user:pass@hooks.example.com/in", ErrUserinfo},
		{"https://user@hooks.example.com/in", ErrUserinfo},
		{"https://hooks.example.com/in?token=abc", ErrQuery},
		{"https://hooks.example.com/in?", ErrQuery},
		{"https://hooks.example.com/in#frag", ErrFragment},
		{"https://hooks.example.com/in#", ErrFragment},
		{"https://xn--bcher-kva.example/in", ErrHost},
		{"https://hooks.xn--p1ai/in", ErrHost},
		{"https://bücher.example/in", ErrCharacters},
		{"https://hooks.example.com./in", ErrHost},
		{"https://hooks..example.com/in", ErrHost},
		{"https://-hooks.example.com/in", ErrHost},
		{"https://hooks-.example.com/in", ErrHost},
		{"https://hooks_x.example.com/in", ErrHost},
		{"https://localhost/in", ErrHost},
		{"https://api.localhost/in", ErrHost},
		{"https://kubernetes.default.svc.cluster.local/in", ErrHost},
		{"https://metadata.google.internal/in", ErrHost},
		{"https://1.0.0.10.in-addr.arpa/in", ErrHost},
		{"https://hooks/in", ErrHost},
		{" https://hooks.example.com/in", ErrCharacters},
		{"https://hooks.example.com/in ", ErrCharacters},
		{"https://hooks.example.com/a b", ErrCharacters},
		{"https://hooks.example.com/in\r\nX-Evil: 1", ErrCharacters},
		{"https://hooks.example.com/in\t", ErrCharacters},
		{"https://hooks.example.com/in\x00", ErrCharacters},
		{"https://hooks.example.com/\x7f", ErrCharacters},
		{"https://hooks.example.com/" + strings.Repeat("a", MaxEndpointLength), ErrTooLong},
		{"https://" + strings.Repeat("a.", 130) + "com/", ErrHost},
		{"https://" + strings.Repeat("a", 64) + ".example.com/", ErrHost},
	}
	for _, c := range refused {
		e, err := ParseEndpoint(c.raw)
		// The sentinel itself, not a wrapping of it: a wrapped error could carry the input.
		if err != c.want || !e.IsZero() {
			t.Errorf("%q: %v, want %v", c.raw, err, c.want)
		}
	}
	accepted := []struct{ raw, canonical string }{
		{"https://hooks.example.com", "https://hooks.example.com/"},
		{"https://hooks.example.com/", "https://hooks.example.com/"},
		{"https://hooks.example.com:443/in", "https://hooks.example.com/in"},
		{"https://Hooks.Example.COM/In/Path", "https://hooks.example.com/In/Path"},
		{"https://rms.kebabfactory.com.mt/api/taptime/webhook", "https://rms.kebabfactory.com.mt/api/taptime/webhook"},
		{"https://a-b.c9.example.co.uk/x%20y/%2F", "https://a-b.c9.example.co.uk/x%20y/%2F"},
		{"https://10-0-0-1.example.com/in", "https://10-0-0-1.example.com/in"},
	}
	for _, c := range accepted {
		e, err := ParseEndpoint(c.raw)
		if err != nil {
			t.Errorf("%q: refused (%v)", c.raw, err)
			continue
		}
		if e.StoredForm() != c.canonical || e.ShowToTheOwner() != c.canonical {
			t.Errorf("%q: canonical %q, want %q", c.raw, e.StoredForm(), c.canonical)
		}
		again, err := EndpointFromStore(e.StoredForm())
		if err != nil || again.StoredForm() != c.canonical {
			t.Errorf("%q: the stored form does not read back as itself", c.raw)
		}
	}
}

// TestEndpoint_PrintsAPlaceholderEverywhere: an Endpoint printed through fmt's verbs, inside a struct,
// a slice and a map, through slog (text and JSON) and encoding/json shows the placeholder and never
// the address -- a log line that names an endpoint value cannot leak it (CLAUDE.md §7).
func TestEndpoint_PrintsAPlaceholderEverywhere(t *testing.T) {
	e, err := ParseEndpoint("https://canary-host.example.com/secret-token-path")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		out = append(out, fmt.Sprintf(verb, e), fmt.Sprintf(verb, struct{ E Endpoint }{e}),
			fmt.Sprintf(verb, []Endpoint{e}), fmt.Sprintf(verb, map[string]Endpoint{"k": e}))
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "endpoint", e)
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "endpoint", e)
	out = append(out, buf.String())
	j, err := json.Marshal(map[string]any{"e": e})
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, string(j))
	for _, o := range out {
		if strings.Contains(o, "canary") || strings.Contains(o, "secret-token") || strings.Contains(o, "example.com") {
			t.Fatalf("an Endpoint printed its address: %s", o)
		}
	}
	if !strings.Contains(out[0], "redacted") {
		t.Fatalf("CONTROL: %q", out[0])
	}
}
