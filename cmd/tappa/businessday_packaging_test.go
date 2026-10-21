package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
)

// TestPackaging_TheBusinessDaySwitchInTheConfigMapIsOneTheServerStartsWith (ADR 0032 §A.8, security
// lens F-3): the cluster runs ONE replica (deploy/k8s/20-app.yaml), so a TAPPA_BUSINESS_DAY_FROM the
// server refuses is a restart into no server — no tap recorded until somebody reads the error. The
// shipped ConfigMap's value is therefore read here with the parser config.Load boots with
// (config.ParseBusinessDayFrom: RFC 3339, at most 30 days ahead). The key itself must be there —
// TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest requires every variable config.go
// names to reach the serving container — and an EMPTY value is the switch off: the test says so
// and passes, because the value is a release decision (M15-08), not this test's.
func TestPackaging_TheBusinessDaySwitchInTheConfigMapIsOneTheServerStartsWith(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	values := configMapValues(t, string(src))
	if len(values) < 5 {
		t.Fatalf("only %d ConfigMap values parsed; the parse has gone blind", len(values))
	}
	raw, ok := values[config.EnvBusinessDayFrom]
	if !ok {
		t.Fatalf("05-config.yaml has no %s key: the serving container would never receive the switch", config.EnvBusinessDayFrom)
	}
	from, err := config.ParseBusinessDayFrom(raw, time.Now())
	if err != nil {
		t.Fatalf("the shipped %s would stop the server from starting: %v", config.EnvBusinessDayFrom, err)
	}
	if from == nil {
		t.Logf("%s is empty in 05-config.yaml: the business-day switch ships OFF (its value is M15-08's release decision)",
			config.EnvBusinessDayFrom)
		return
	}
	t.Logf("%s ships %s: on from then, by the server's clock", config.EnvBusinessDayFrom, from.Format(time.RFC3339))
}
