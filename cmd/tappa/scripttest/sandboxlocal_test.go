package scripttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSandboxLocal_RefusesADSNThatIsNotOneLoopbackHost (M11 API-14 review, round 2): scripts/sandbox-local.sh
// gates on what libpq CONNECTS to, not on the URL's authority alone. A query host= or hostaddr= (which
// overrides the authority), a host list and a keyword=value string are refused -- before psql, docker or
// anything that writes is reached (the refusal comes first, so these cases need no database).
func TestSandboxLocal_RefusesADSNThatIsNotOneLoopbackHost(t *testing.T) {
	t.Parallel()
	good := "postgres://tappa_owner:x@127.0.0.1:55610/tappa?sslmode=disable"
	// A psql stub first on PATH: reaching it means the gate let the DSN through. Nothing connects.
	stub := t.TempDir()
	if err := os.WriteFile(filepath.Join(stub, "psql"), []byte("#!/bin/sh\n: > \""+filepath.Join(stub, "reached")+"\"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := "PATH=" + stub + ":/usr/bin:/bin"
	// Positive control: two good DSNs pass the gate and reach psql, so the refusals below are the
	// gate's and not a message that always appears.
	ctl := exec.Command("bash", filepath.Join(repoRoot, "scripts", "sandbox-local.sh"))
	ctl.Env = []string{path, "SANDBOX_DB=tappa_sbx_refusal_probe", "APP_DSN=" + good, "OWNER_DSN=" + good}
	reached := func() bool { _, err := os.Stat(filepath.Join(stub, "reached")); return err == nil }
	if out, _ := ctl.CombinedOutput(); !reached() {
		t.Fatalf("positive control: two loopback DSNs did not pass the gate: %q", out)
	}
	if err := os.Remove(filepath.Join(stub, "reached")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"postgres://tappa_owner:x@localhost:1/tappa?host=127.0.0.1&port=55610",
		"postgres://tappa_owner:x@localhost/tappa?sslmode=disable&HostAddr=10.0.0.5",
		"postgres://tappa_owner:x@localhost,db.example.com/tappa",
		"host=127.0.0.1 port=55610 user=tappa_owner dbname=tappa",
		"postgres://tappa_owner:x@db.example.com/tappa",
	} {
		for _, side := range []string{"OWNER_DSN", "APP_DSN"} {
			cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "sandbox-local.sh"))
			cmd.Env = []string{path, "SANDBOX_DB=tappa_sbx_refusal_probe",
				"APP_DSN=" + good, "OWNER_DSN=" + good, side + "=" + bad}
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), side+" is not a postgres:// URL on one loopback host") {
				t.Errorf("%s=%q: err %v, output %q -- want the loopback refusal", side, bad, err, out)
			}
			if reached() {
				t.Errorf("%s=%q: the script went on to psql", side, bad)
			}
		}
	}
}
