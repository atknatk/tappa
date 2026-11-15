package oauth

import "github.com/atknatk/tappa/internal/config"

// THE BUILD SPLIT (ADR 0047 §7.1, §8.6, §10.4; the API key's K-10 reason): every OAuth value a build
// issues and accepts carries its environment word -- tt_<kind>_live_ in production, tt_<kind>_test_ in
// the sandbox build (`go build -tags sandbox`) -- and only the sandbox build registers and redirects
// to a loopback redirect URI (http://localhost:<port>/... or http://127.0.0.1:<port>/..., port
// included and matched byte for byte), whatever the registry row says: the schema CHECK takes both
// forms because one set of migrations runs under both builds.
//
// 🔴 BOTH ARE COMPILE-TIME CONSTANTS, from internal/config's two build files -- the module's only
// build-constrained product files (internal/db's TestProductCode_CarriesNoBuildConstraint). No
// configuration, variable or request of a production binary can open a loopback redirect or accept a
// sandbox credential: TestOAuthRedirect_ProductionV2RefusesLoopback, the untagged build's
// prefix tests and the sandbox build's TestOAuthRedirect_SandboxAcceptsLoopback pin both sides.
const (
	// Env is the environment word inside every prefix: tt_<kind>_<Env>_.
	Env = config.CredentialWord
	// LoopbackRedirects is whether a loopback redirect URI is one this build registers and redirects
	// to. Never in production.
	LoopbackRedirects = config.IsSandboxBuild
)
