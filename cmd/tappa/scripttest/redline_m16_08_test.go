package scripttest

import (
	"strings"
	"testing"
)

// TestRedline_M16_08_TheAssetLinksKeyIsTheOnlyWaiver pins R1's sixth waiver phrase (M16-08, ADR
// 0012's M16-08 note): Android's Digital Asset Links key for the signing certificate's SHA-256 is
// waived ONLY as the quoted key, ONLY in internal/handler/applinks*.go, and only while nothing else
// on the line is a trigger. Spellings are masked ("~~", m16Unmask) because this file is scanned too.
//
// Red: the key in a file outside the waived path; the bare word (no quotes) in a waived file; an
// identifier that carries the word in a waived file; the key beside another trigger on one line.
// Green (and reported as a waiver, not silent): the struct tag and the golden line the handler and
// its test carry.
func TestRedline_M16_08_TheAssetLinksKeyIsTheOnlyWaiver(t *testing.T) {
	t.Parallel()
	const key = `"sha256_cert_finger~~prints"`
	red := map[string]string{
		"internal/domain/tap/r1_key.go":       "package tap\n\nconst k = " + key + "\n",
		"internal/handler/applinks_bare.go":   "package handler\n\n// the sha256_cert_finger~~prints list\n",
		"internal/handler/applinks_ident.go":  "package handler\n\nvar finger~~printTemplate []byte\n",
		"internal/handler/applinks_beside.go": "package handler\n\nvar x = `json:" + key + "` // web~~authn\n",
	}
	green := map[string]string{
		"internal/handler/applinks.go": "package handler\n\ntype t struct {\n\tCertSHA256 []string `json:" + key + "`\n}\n",
		"internal/handler/applinks_test.go": "package handler\n\nconst g = `\"target\":{\"namespace\":\"android_app\"," + key +
			":[`\n",
	}
	m16RedGreen(t, "R1", red, green)

	out, _ := redlineRun(t, m16Tree(green))
	for rel := range green {
		if !strings.Contains(out, rel+":") {
			t.Errorf("the waived line in %s is not reported as a waiver:\n%s", rel, out)
		}
	}
}
