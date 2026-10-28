package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestPackaging_TheIdentityFlagShipsOn (ADR 0031 §8, M14-08; M14-19): the ConfigMap carries
// TAPPA_ADMIN_IDENTITY and ships it "on", and no env entry of the serving container overrides it
// (an entry wins over envFrom, so the ConfigMap -- the one place the flag is decided -- would not
// be what the pod runs with). It shipped "off" until M14-19's commit, the user's decision, which
// updated this test on purpose; the way back ("off", deploy/README.md "M14-19 — bayrak" (f)) updates
// it again: a stray edit of the line does not pass unnoticed.
func TestPackaging_TheIdentityFlagShipsOn(t *testing.T) {
	cfgSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cm := configMapValues(t, string(cfgSrc))
	if cm["TAPPA_ENV"] != "prod" {
		t.Fatalf("CONTROL FAILED: the ConfigMap parse reads TAPPA_ENV as %q", cm["TAPPA_ENV"])
	}
	got, ok := cm[config.EnvAdminIdentity]
	switch {
	case !ok:
		t.Errorf("%s is not a ConfigMap key: the flag's state would be an unwritten default", config.EnvAdminIdentity)
	case got != config.AdminIdentityOn:
		t.Errorf("the ConfigMap ships %s=%q; it ships %q since M14-19, the user's decision (ADR 0031 §8)",
			config.EnvAdminIdentity, got, config.AdminIdentityOn)
	}
	if _, overridden := containerEnvEntries(t, string(appSrc), servingContainer)[config.EnvAdminIdentity]; overridden {
		t.Errorf("%s is an env entry of the serving container: it would override the ConfigMap's value", config.EnvAdminIdentity)
	}
}
