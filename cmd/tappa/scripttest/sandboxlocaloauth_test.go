package scripttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// oauthScriptTree copies scripts/sandbox-local-oauth.sh into a temporary tree of its own (the script
// cds to its parent's parent and reads .sandbox-local/ there), with the env file and the flow's
// secrets when asked. Nothing of the repository's own .sandbox-local/ is touched.
func oauthScriptTree(t *testing.T, env, secrets bool) (root, script string) {
	return oauthScriptTreeWith(t, env, secrets, "")
}

// oauthScriptTreeWith is oauthScriptTree with extra lines appended to the env file.
func oauthScriptTreeWith(t *testing.T, env, secrets bool, extra string) (root, script string) {
	t.Helper()
	root = t.TempDir()
	src, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "sandbox-local-oauth.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script = filepath.Join(root, "scripts", "sandbox-local-oauth.sh")
	for _, d := range []string{filepath.Dir(script), filepath.Join(root, ".sandbox-local")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(script, src, 0o755); err != nil {
		t.Fatal(err)
	}
	if env {
		body := "TAPPA_ADDR=127.0.0.1:1\nTAPPA_API_HOST=api.localhost\nTAPPA_OPERATOR_HOST=ops.localhost\nTAPPA_BASE_URL=https://localhost:1\n" + extra
		if err := os.WriteFile(filepath.Join(root, ".sandbox-local", "tappa_sbx_probe.env"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if secrets {
		s := filepath.Join(root, ".sandbox-local", "tappa_sbx_probe.secrets")
		if err := os.MkdirAll(s, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"owner-password", "operator-password", "operator-totp-key"} {
			if err := os.WriteFile(filepath.Join(s, f), []byte("placeholder"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root, script
}

// oauthScriptTreeHosts is a tree whose env file names the API and operator hosts given.
func oauthScriptTreeHosts(t *testing.T, api, ops string) (root, script string) {
	t.Helper()
	return oauthScriptTreeEnv(t, "127.0.0.1:1", api, ops, "https://localhost:1")
}

// oauthScriptTreeEnv is a tree whose env file holds the four values given.
func oauthScriptTreeEnv(t *testing.T, addr, api, ops, base string) (root, script string) {
	t.Helper()
	root, script = oauthScriptTree(t, false, true)
	body := "TAPPA_ADDR=" + addr + "\nTAPPA_API_HOST=" + api + "\nTAPPA_OPERATOR_HOST=" + ops + "\nTAPPA_BASE_URL=" + base + "\n"
	if err := os.WriteFile(filepath.Join(root, ".sandbox-local", "tappa_sbx_probe.env"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, script
}

// TestSandboxLocalOAuth_RefusesBeforeAnyRequest (issue #37 phase 4; the shape of
// TestSandboxLocal_RefusesADSNThatIsNotOneLoopbackHost): scripts/sandbox-local-oauth.sh checks every
// precondition before a request leaves -- an unknown mode, no env file, no flow secrets, an API or
// operator host that is not a loopback name, and (register) an output file that exists, which is NEVER
// overwritten, or one inside the repository tree. The env file is read, never sourced: a line of it
// that would run when sourced does not. A curl stub first on PATH records any real
// request; the positive control reaches it.
func TestSandboxLocalOAuth_RefusesBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	stub := t.TempDir()
	reachedFile := filepath.Join(stub, "reached")
	// The script's own capability probe (curl -K /dev/null --version) is not a request.
	curl := "#!/bin/sh\ncase \"$*\" in *--version*) exit 0 ;; esac\n: > \"" + reachedFile + "\"\nexit 7\n"
	if err := os.WriteFile(filepath.Join(stub, "curl"), []byte(curl), 0o755); err != nil {
		t.Fatal(err)
	}
	path := "PATH=" + stub + ":/usr/bin:/bin"
	reached := func() bool { _, err := os.Stat(reachedFile); return err == nil }
	register := func(out string) []string {
		return []string{path, "SANDBOX_DB=tappa_sbx_probe", "SANDBOX_APP_NAME=Probe App",
			"SANDBOX_APP_REDIRECT=https://app.example.com/cb", "SANDBOX_APP_OUT=" + out}
	}

	// Positive control: everything present, a new output file outside the tree -> the script goes on to
	// its first request. The env file also holds a line that RUNS when the file is sourced: the script
	// reads its four values and must not run it.
	sourced := filepath.Join(stub, "sourced")
	_, script := oauthScriptTreeWith(t, true, true, "TAPPA_PROBE=$(: > \""+sourced+"\")\n")
	outside := filepath.Join(t.TempDir(), "new.credentials")
	ctl := exec.Command("bash", script, "register")
	ctl.Env = register(outside)
	if out, _ := ctl.CombinedOutput(); !reached() {
		t.Fatalf("positive control: the preconditions held and no request was made: %q", out)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("positive control: a credentials file was written although no app was registered")
	}
	if _, err := os.Stat(sourced); err == nil {
		t.Fatal("the env file was sourced (a line of it ran): the script must read its four values only")
	}
	if err := os.Remove(reachedFile); err != nil {
		t.Fatal(err)
	}

	existing := filepath.Join(t.TempDir(), "kept.credentials")
	if err := os.WriteFile(existing, []byte("client_id=placeholder\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name          string
		env, secrets  bool
		args, environ []string
		says          string
		inTree        bool // SANDBOX_APP_OUT inside the script's tree
	}{
		{"an unknown mode", true, true, []string{"bogus"}, []string{path, "SANDBOX_DB=tappa_sbx_probe"}, "the mode is flow or register", false},
		{"no env file", false, false, []string{"flow"}, []string{path, "SANDBOX_DB=tappa_sbx_probe"}, "make sandbox-local SANDBOX_DB=tappa_sbx_probe first", false},
		{"no flow secrets", true, false, []string{"flow"}, []string{path, "SANDBOX_DB=tappa_sbx_probe"}, "make sandbox-local-flow SANDBOX_DB=tappa_sbx_probe first", false},
		{"an output file that exists", true, true, []string{"register"}, register(existing), "nothing is overwritten", false},
		{"register without an output file", true, true, []string{"register"}, register("")[:4], "SANDBOX_APP_OUT", false},
		{"an output file inside the tree", true, true, []string{"register"}, nil, "inside the repository tree", true},
		{"an API host that is not loopback", true, true, []string{"flow"}, []string{path, "SANDBOX_DB=tappa_sbx_probe"}, "TAPPA_API_HOST", false},
		{"an operator host that is not loopback", true, true, []string{"flow"}, []string{path, "SANDBOX_DB=tappa_sbx_probe"}, "TAPPA_OPERATOR_HOST", false},
		{"a server address that is not loopback", true, true, []string{"flow"}, []string{path, "SANDBOX_DB=tappa_sbx_probe"}, "TAPPA_ADDR in", false},
		{"a base URL that is not this machine's", true, true, []string{"flow"}, []string{path, "SANDBOX_DB=tappa_sbx_probe"}, "TAPPA_BASE_URL in", false},
	} {
		root, script := oauthScriptTree(t, c.env, c.secrets)
		switch c.name {
		case "an API host that is not loopback":
			root, script = oauthScriptTreeHosts(t, "api.example.com", "ops.localhost")
		case "an operator host that is not loopback":
			root, script = oauthScriptTreeHosts(t, "api.localhost", "ops.example.com")
		case "a server address that is not loopback":
			root, script = oauthScriptTreeEnv(t, "0.0.0.0:1", "api.localhost", "ops.localhost", "https://localhost:1")
		case "a base URL that is not this machine's":
			root, script = oauthScriptTreeEnv(t, "127.0.0.1:1", "api.localhost", "ops.localhost", "https://taptime.example.com")
		}
		if c.inTree {
			c.environ = register(filepath.Join(root, "credentials"))
		}
		cmd := exec.Command("bash", append([]string{script}, c.args...)...)
		cmd.Env = c.environ
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), c.says) {
			t.Errorf("%s: err %v, output %q -- want a refusal saying %q", c.name, err, out, c.says)
		}
		if reached() {
			t.Errorf("%s: the script made a request before refusing", c.name)
			_ = os.Remove(reachedFile)
		}
	}
	if b, err := os.ReadFile(existing); err != nil || string(b) != "client_id=placeholder\n" {
		t.Fatalf("the existing output file changed: %q %v", b, err)
	}
}

// TestSandboxLocalOAuth_NoSecretOnACommandLine reads the script's code (comments left out): every
// value that is a secret -- the passwords, the TOTP code, the PKCE verifier, the code, the tokens, the
// form binding and rd -- travels as `--data-urlencode name@file`; the Basic and Bearer credentials as
// `-H @file`; curl's -u/--user (the secret on argv) is never used -- on a continuation line either
// (every command is read with its continuation lines joined); a file is never expanded into an
// argument ($(cat …), $(< …)); python3 runs isolated (-I) with file paths for arguments. The flow's
// rule (sandbox-local-flow.sh's header) for the OAuth half.
func TestSandboxLocalOAuth_NoSecretOnACommandLine(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "sandbox-local-oauth.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var code []string
	inHeredoc := false
	for _, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inHeredoc:
			if line == "PY" {
				inHeredoc = false
			}
			continue
		case strings.HasPrefix(trimmed, "#"):
			continue
		}
		if strings.Contains(line, "<<'PY'") {
			inHeredoc = true
		}
		code = append(code, line)
	}
	// A command's continuation lines are one command: joined, a credential on its third line is on the
	// same argv as the curl on its first.
	text := strings.ReplaceAll(strings.Join(code, "\n"), "\\\n", " ")
	secretFields := []string{"password", "code", "code_verifier", "refresh_token", "token", "rd", "csrf", "client_secret"}
	urlencoded := regexp.MustCompile(`--data-urlencode "?([a-z_]+)([=@])`)
	fields := 0
	for _, m := range urlencoded.FindAllStringSubmatch(text, -1) {
		fields++
		for _, f := range secretFields {
			if m[1] == f && m[2] != "@" {
				t.Errorf("the secret field %s is sent as a value on the command line (%s)", f, m[0])
			}
		}
	}
	if fields < 12 {
		t.Fatalf("anti-vacuity: %d form fields read", fields)
	}
	for _, bad := range []*regexp.Regexp{
		regexp.MustCompile(`\$\(\s*cat\b`), regexp.MustCompile(`\$\(\s*<`), regexp.MustCompile(`\bcurl\b[^\n]*\s(-u|--user)(\s|=|"|')`), regexp.MustCompile(`\bcurl\b[^\n]*\s-u\S`),
		regexp.MustCompile(`-H\s+"Authorization`), regexp.MustCompile(`-H\s+'Authorization`), regexp.MustCompile(`client_secret=\$`),
	} {
		if loc := bad.FindString(text); loc != "" {
			t.Errorf("the script puts a secret on a command line: %q", loc)
		}
	}
	if n := strings.Count(text, "-H @\"$O/basic.header\""); n < 3 {
		t.Errorf("the token endpoint is called %d times with the Basic header file, want every call (3)", n)
	}
	// The live credentials go at the end, a failure included (an EXIT trap set once the run's directory
	// exists): the page that showed the secret, both sessions' cookies, the flow's test credentials.
	cleanup := regexp.MustCompile(`(?s)cleanup\(\) \{(.*?)\n\}`).FindStringSubmatch(text)
	if cleanup == nil || !strings.Contains(text, "trap cleanup EXIT") {
		t.Fatal("the script has no cleanup function on an EXIT trap")
	}
	for _, f := range []string{"app.html", "op-challenge.header", "operator.header", "panel.cookies", "client.credentials", "basic.header"} {
		if !strings.Contains(cleanup[1], `"$O/`+f+`"`) {
			t.Errorf("the cleanup does not delete %s", f)
		}
	}
	// Every response header file curl writes (-D) can carry a Set-Cookie -- the operator's sign-in
	// challenge and session among them: each -D path of the script, read off it (a loop's $n is any
	// number), is a path the cleanup deletes.
	quoted := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(cleanup[1], -1)
	headers := regexp.MustCompile(`-D "([^"]+)"`).FindAllStringSubmatch(text, -1)
	if len(headers) < 6 {
		t.Fatalf("anti-vacuity: %d -D header files read off the script", len(headers))
	}
	// A path of a loop ("…-$n.h") is each path the script's own `for n in …` loop makes.
	loop := regexp.MustCompile(`for n in ([0-9 ]+); do`).FindStringSubmatch(text)
	inCleanup := map[string]bool{}
	for _, q := range quoted {
		inCleanup[q[1]] = true
	}
	for _, h := range headers {
		paths := []string{h[1]}
		if strings.Contains(h[1], "$n") {
			if loop == nil {
				t.Fatalf("%s is a loop's path and the script has no `for n in …` loop to expand it", h[1])
			}
			paths = nil
			for _, v := range strings.Fields(loop[1]) {
				paths = append(paths, strings.ReplaceAll(h[1], "$n", v))
			}
		}
		for _, p := range paths {
			if !inCleanup[p] {
				t.Errorf("the header file %s (-D) is not deleted by the cleanup: a Set-Cookie may stay on disk", p)
			}
		}
	}
	if !strings.Contains(cleanup[1], `"$OPS/operator/logout"`) {
		t.Error("the cleanup does not sign the operator out")
	}
	if strings.Index(text, "trap cleanup EXIT") < strings.Index(text, `mkdir "$O"`) {
		t.Error("the trap is set before the run's directory exists (its paths would be unset)")
	}
	if regexp.MustCompile(`(?m)^\s*(\.|source)\s+"?\./?\$ENV_FILE`).MatchString(text) || strings.Contains(text, "set -a") {
		t.Error("the script sources or exports its env file: it reads four values of it")
	}
	for _, line := range code {
		if strings.Contains(line, "python3") && !strings.Contains(line, "python3 -I") && !strings.Contains(line, "command -v") && !strings.Contains(line, "for c in") {
			t.Errorf("python3 not isolated: %q", strings.TrimSpace(line))
		}
	}
}
