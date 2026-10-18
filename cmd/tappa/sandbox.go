//go:build sandbox

package main

import (
	"log/slog"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/api"
	"github.com/atknatk/tappa/internal/sandbox"
	"github.com/atknatk/tappa/internal/webhook"
)

// THE SANDBOX BUILD'S HALF (M11 API-11, ADR 0028 §11, K-3), compiled only by `go build -tags
// sandbox` (make build-sandbox; the Dockerfile's app-sandbox target). It is the ONE importer of
// internal/sandbox (TestPackaging_SandboxBuildHasIt; the untagged twin is nosandbox.go). The same
// build's internal/config starts only with TAPPA_ENV=sandbox, so this binary is never a production
// server, and the production binary refuses TAPPA_ENV=sandbox.

// apiKeyPrefix is the sandbox's key prefix (K-10): tt_test_, for the panel that issues keys and the
// /v1 chain that accepts them alike. A tt_live_ key is refused by the shape check, before the
// database.
const apiKeyPrefix = apikey.SandboxPrefix

// sandboxExtensions builds the tap simulation (POST /v1/sandbox/taps) over run()'s own pool,
// configuration and K-19 worker. The simulation builds its own SUN pre-check, tap recorder and trail
// with the tap page's constructors over the /v1 write runner's transaction (internal/sandbox's
// runner.go: one connection per simulation -- API-11 fix 1); the tap page keeps its own on the pool.
func sandboxExtensions(cfg *config.Config, data *db.DB, work *webhook.Worker, log *slog.Logger) ([]api.Extension, error) {
	sim, err := sandbox.New(sandbox.Config{App: cfg, Data: data, Work: work, Log: log})
	if err != nil {
		return nil, err
	}
	log.Info("the sandbox build: /v1/sandbox/taps simulates plaque taps on virtual plaques", "sandbox", "on")
	return []api.Extension{sim}, nil
}
