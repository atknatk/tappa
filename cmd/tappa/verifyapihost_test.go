package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// verifyapihost_test.go -- scripts/verify-deployment.sh's api-host mode (M11 API-13a), run for real
// with a stub curl and a stub sleep first in PATH: every branch of check_api_host, judged by its exit
// code and its annotation, with no network. The stub answers from files the test writes: the guide's
// "<status> <content type>" lines (one per call, the last repeated) and exit code, the root's status
// and exit code; it logs every URL it was asked.

const stubCurl = `#!/bin/sh
dir=$(dirname "$0")
for a in "$@"; do url=$a; done
echo "$url" >> "$dir/asked"
case $url in
  */v1/guide.md)
    rc=$(cat "$dir/guide.rc"); [ "$rc" = 0 ] || exit "$rc"
    n=$(grep -c guide.md "$dir/asked")
    line=$(sed -n "${n}p" "$dir/guide.answers"); [ -n "$line" ] || line=$(tail -n 1 "$dir/guide.answers")
    printf '%s' "$line" ;;
  */)
    rc=$(cat "$dir/root.rc"); [ "$rc" = 0 ] || exit "$rc"
    printf '%s' "$(cat "$dir/root.answer")" ;;
esac
`

// TestVerifyDeployment_TheAPIHostModeJudgesEveryAnswer: the guide's 200 text/markdown with a 404 root
// passes; a 404 guide is asked again (18 times in all) and then a violation, a 404 that turns into a
// 200 passes; a guide in another type, a root that answers 200 or 302, are violations (exit 1); a name
// that does not resolve is a warning and exit 0; any other curl failure is exit 2 (not checked).
func TestVerifyDeployment_TheAPIHostModeJudgesEveryAnswer(t *testing.T) {
	script := filepath.Join(repoRoot, "scripts", "verify-deployment.sh")
	md := "200 text/markdown; charset=utf-8"
	for _, c := range []struct {
		name      string
		guide     []string
		guideRC   string
		root      string
		rootRC    string
		exit      int
		guideAsks int
		mustSay   string
	}{
		{"served", []string{md}, "0", "404", "0", 0, 1, "api-host: api.taptime.mt serves /v1"},
		{"the ingress picks it up late", []string{"404 text/plain", "404 text/plain", md}, "0", "404", "0", 0, 3, "serves /v1"},
		{"never served", []string{"404 text/plain"}, "0", "404", "0", 1, 18, "is not served on its host"},
		{"another type", []string{"200 text/html; charset=utf-8"}, "0", "404", "0", 1, 1, "want 200 text/markdown"},
		{"a 503 is final", []string{"503 application/json"}, "0", "404", "0", 1, 1, "answered 503"},
		{"the root serves something", []string{md}, "0", "200", "0", 1, 1, "something other than the API"},
		{"the root redirects", []string{md}, "0", "302", "0", 1, 1, "something other than the API"},
		{"no DNS record yet", []string{md}, "6", "404", "0", 0, 1, "does not resolve"},
		{"a timeout", []string{md}, "28", "404", "0", 2, 1, "NOT checked"},
		{"the root times out", []string{md}, "0", "404", "28", 2, 1, "NOT fully checked"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, body string, mode os.FileMode) {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), mode); err != nil {
					t.Fatal(err)
				}
			}
			write("curl", stubCurl, 0o755)
			write("sleep", "#!/bin/sh\nexit 0\n", 0o755)
			write("guide.answers", strings.Join(c.guide, "\n")+"\n", 0o644)
			write("guide.rc", c.guideRC, 0o644)
			write("root.answer", c.root, 0o644)
			write("root.rc", c.rootRC, 0o644)
			write("asked", "", 0o644)
			cmd := exec.Command("bash", script, "api-host", "api.taptime.mt")
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			exit := 0
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				exit = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if exit != c.exit || !strings.Contains(string(out), c.mustSay) {
				t.Errorf("exit %d, output %q; want exit %d saying %q", exit, out, c.exit, c.mustSay)
			}
			asked, _ := os.ReadFile(filepath.Join(dir, "asked"))
			if n := strings.Count(string(asked), "https://api.taptime.mt/v1/guide.md"); n != c.guideAsks {
				t.Errorf("the guide was asked %d time(s), want %d", n, c.guideAsks)
			}
		})
	}
}
