//go:build !sandbox

package main

import (
	"log/slog"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/api"
	"github.com/atknatk/tappa/internal/webhook"
)

// THE PRODUCTION BUILD'S HALF of the build split (M11 API-11, ADR 0028 §11; its twin is sandbox.go,
// `//go:build sandbox`). This file is what an untagged `go build ./cmd/tappa` compiles: the
// integration keys are live keys, and /v1 has no extension -- internal/sandbox is not imported
// anywhere in this build, so the binary does not contain the simulation
// (TestPackaging_ProdBuildHasNoSandboxPackage).

// apiKeyPrefix is the prefix the owner's panel issues and the /v1 chain accepts (K-10): tt_live_.
// A tt_test_ key is refused by the chain's shape check, before the database (ADR 0028 §3).
const apiKeyPrefix = apikey.LivePrefix

// sandboxExtensions is the sandbox build's /v1 extension; the production build has none.
func sandboxExtensions(*config.Config, *db.DB, *webhook.Worker, *slog.Logger) ([]api.Extension, error) {
	return nil, nil
}
