package main

import (
	"log/slog"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/handler/oauthapi"
	"github.com/atknatk/tappa/internal/oauth"
)

// openOAuth builds the "Connected apps" authorization server (issue #37, ADR 0047) over the pool and
// the trail, under TAPPA_OAUTH_HMAC_KEY (required -- config.Load refuses to start without it): the
// server the panel's consent (handler.OAuthConsent) and the token endpoint's surface share, and that
// surface (POST /oauth/token, POST /oauth/revoke and the front door GET /oauth/authorize). Both are on
// every configuration; the operator surface's registry mints its client secrets with the same key
// (clientSecretMinter).
func openOAuth(cfg *config.Config, store oauth.Store, trail oauth.Trail, log *slog.Logger) (*oauth.Server, *oauthapi.Surface, error) {
	srv, err := oauth.NewServer(cfg.OAuthHMACKey.Reveal(), store, trail, log)
	if err != nil {
		return nil, nil, err
	}
	surface, err := oauthapi.New(oauthapi.Config{Authority: srv, Log: log})
	if err != nil {
		return nil, nil, err
	}
	return srv, surface, nil
}

// clientSecretMinter is the operator registry's client secret minter: the hash is TAPPA_OAUTH_HMAC_KEY's,
// the same key the token endpoint compares under (a secret minted under another key would never
// authenticate).
func clientSecretMinter(cfg *config.Config) (*oauth.Hasher, error) {
	return oauth.NewHasher(cfg.OAuthHMACKey.Reveal())
}
