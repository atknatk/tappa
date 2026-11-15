package scripttest

import (
	"strings"
	"testing"
)

// TestRedline_M16_08_TheAssetLinksKeyIsTheOnlyWaiver pins R1's protocol waiver (M16-08, ADR 0012's
// M16-08 note): Android's Digital Asset Links key for the signing certificate's SHA-256 is waived
// ONLY as the quoted key, ONLY in internal/handler/applinks.go and applinks_test.go (no other file
// of the package, no applinks* prefix, no subdirectory), and only while nothing else on the line is
// a trigger. It is its own phrase-path pair: the marketing phrases are not waived in those two files
// and the key is not waived in the marketing files. Spellings are masked ("~~", m16Unmask) because
// this file is scanned too.
//
// Every red case runs in a tree of its own, so a file that is red for an unrelated reason cannot
// stand in for another.
//
// MUTANT (measured 2026-11-14, reverted): R1_PROTOCOL_PATHS widened to internal/handler/[^:]*[.]go
// -> red here (tap_r1.go, applink.go, applinks_extra.go stop failing).
func TestRedline_M16_08_TheAssetLinksKeyIsTheOnlyWaiver(t *testing.T) {
	t.Parallel()
	const key = `"sha256_cert_finger~~prints"`
	tagLine := "package handler\n\nvar x = `json:" + key + "`\n"
	red := []struct{ name, rel, src string }{
		{"outside the package", "internal/domain/tap/r1_key.go", "package tap\n\nconst k = " + key + "\n"},
		{"a neighbouring handler file", "internal/handler/tap_r1.go", tagLine},
		{"a near-miss name", "internal/handler/applink.go", tagLine},
		{"an applinks prefix", "internal/handler/applinks_extra.go", tagLine},
		{"a subdirectory", "internal/handler/applinks/sub/x.go", "package sub\n\nvar x = `json:" + key + "`\n"},
		{"the key in a marketing file (no cross-product)", "internal/handler/marketing_r1.go", tagLine},
		{"a marketing phrase in the waived file (no cross-product)", "internal/handler/applinks.go",
			"package handler\n\n// safer than a finger~~print terminal\n"},
		{"the bare word in the waived file", "internal/handler/applinks.go", "package handler\n\n// the sha256_cert_finger~~prints list\n"},
		{"an identifier in the waived file", "internal/handler/applinks_test.go", "package handler\n\nvar finger~~printTemplate []byte\n"},
		{"the key beside another trigger", "internal/handler/applinks.go", "package handler\n\nvar x = `json:" + key + "` // web~~authn\n"},
	}
	for _, c := range red {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := redlineRun(t, m16Tree(map[string]string{c.rel: c.src}))
			if code == 0 || !strings.Contains(failSection(out, "R1"), c.rel+":") {
				t.Errorf("%s: want an R1 FAIL naming %s, got exit %d:\n%s", c.name, c.rel, code, out)
			}
		})
	}

	green := map[string]string{
		"internal/handler/applinks.go": "package handler\n\ntype t struct {\n\tCertSHA256 []string `json:" + key + "`\n}\n",
		"internal/handler/applinks_test.go": "package handler\n\nconst g = `\"target\":{\"namespace\":\"android_app\"," + key +
			":[`\n",
	}
	out, code := redlineRun(t, m16Tree(green))
	if code != 0 || strings.Contains(out, "· FAIL]") {
		t.Errorf("the waived lines: want exit 0 and no FAIL, got exit %d:\n%s", code, out)
	}
	for rel := range green {
		if !strings.Contains(out, rel+":") {
			t.Errorf("the waived line in %s is not reported as a waiver:\n%s", rel, out)
		}
	}
}
