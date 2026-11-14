package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/httpx"
)

// THE APP ASSOCIATION FILES (M16-08, ADR 0045). A phone with the native app installed opens a
// plaque's URL in the app instead of the browser only if the plaque's host vouches for the app:
// iOS reads /.well-known/apple-app-site-association (Universal Links), Android reads
// /.well-known/assetlinks.json (App Links). These two routes publish them. Nothing else changes
// for a plaque: without the app, or without these files, the URL opens the browser exactly as
// before -- the plaque is never re-encoded (ADR 0045).
//
// WHAT IS LINKED IS appLinkPaths AND NOTHING ELSE: the plaque tap path, TapPath. /activate is NOT
// (Q40, user decision 2026-11-14): an invitation link opens in the browser and the app takes the
// invitation code on its own screen, so browser activation does not change because an app happens
// to be installed. Phase D's live-QR path is added to this list by M16-20, on that card.
//
// EACH FILE IS SERVED ONLY WHILE ITS PLATFORM IS CONFIGURED (internal/config: Config.AppAppleTeamID,
// Config.AppAndroidSignerSHA256, each optional on its own). Unconfigured, the route is not mounted and
// the path answers the router's plain 404 (".well-known" is one of plainNotFoundRoots), and the
// server starts as before.
//
// ONLY ON THE CANONICAL HOST, TAPPA_BASE_URL's -- the host a plaque's URL names (PlaqueTapBase).
// Any other host reaching these routes gets http.NotFound: the operator host and the API host never
// get this far (their gates in internal/httpx answer every path outside their own surface with 404),
// and the redirect hosts (www.taptime.mt, tappa.everva.com.tr) answer a 301 before any route runs.
// A redirect is no association -- Apple: "You must host the file using https:// with a valid
// certificate and with no redirects"; Android: "must be accessible without any redirects" -- so
// Universal Links and App Links work for taptime.mt alone, which is the only host a plaque carries.
//
// The answer is the same bytes for every request: built once, at construction, from configuration.
// It reads no cookie, sets none, resolves no identity, touches no database and logs nothing.
type AppLinks struct {
	host  string
	apple []byte // nil when no Team ID is configured
	links []byte // nil when no Android certificate is configured
}

// The two well-known paths. Their spelling is the platforms' (no extension on Apple's file).
const (
	AppleAppSiteAssociationPath = "/.well-known/apple-app-site-association"
	AssetLinksPath              = "/.well-known/assetlinks.json"
)

// AppBundleID is the native app's identity in both stores: the iOS bundle ID and the Android
// package name (Q35, user decision 2026-11-14; publisher Everva Bilisim). It is a decision, not
// configuration -- a store identity cannot be changed after the first upload -- so it is a constant.
const AppBundleID = "mt.taptime.app"

// appLinkPaths are the URL paths the app may open (the package doc). Both files are built from
// this one list, so the two platforms cannot link different paths.
var appLinkPaths = []string{TapPath}

// appLinksCacheControl lets a cache keep the file for five minutes. It is short because the body
// changes with a deploy (a new certificate, a path M16-20 adds) and a stale association is
// invisible: a phone that read an old file just opens the browser. Apple fetches through its own
// CDN and devices re-check about once a week whatever this says, so a longer value buys nothing.
const appLinksCacheControl = "public, max-age=300"

// NewAppLinks builds the two answers. baseURL is TAPPA_BASE_URL (internal/config has validated its
// shape); teamID and certs are internal/config's validated values, empty when unset.
func NewAppLinks(baseURL, teamID string, certs []string) (*AppLinks, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("handler: app links: the base URL has no host")
	}
	a := &AppLinks{host: u.Hostname()}
	if teamID != "" {
		if a.apple, err = appleAppSiteAssociation(teamID); err != nil {
			return nil, err
		}
	}
	if len(certs) > 0 {
		if a.links, err = assetLinks(certs); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// Mount registers each configured file for GET and HEAD (BrandTheme.Mount's reason for HEAD: the
// answer has no identity, budget or query behind it, so admitting HEAD has no blast radius).
func (a *AppLinks) Mount(r chi.Router) {
	if a.apple != nil {
		h := a.serve(a.apple)
		r.Get(AppleAppSiteAssociationPath, h)
		r.Head(AppleAppSiteAssociationPath, h)
	}
	if a.links != nil {
		h := a.serve(a.links)
		r.Get(AssetLinksPath, h)
		r.Head(AssetLinksPath, h)
	}
}

// serve answers body on the canonical host and http.NotFound -- the router's own 404 -- on any
// other. Status 200 directly, never a redirect.
func (a *AppLinks) serve(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !httpx.OnHost(r, a.host) {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "application/json")
		h.Set("Cache-Control", appLinksCacheControl)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		// A failed write is the client gone; there is nobody left to answer.
		_, _ = w.Write(body)
	}
}

// The apple-app-site-association shape (Apple, "Supporting associated domains": applinks.details,
// each with appIDs and components; a component's "/" key is a path pattern, and a component with
// no "?" key matches any query -- the plaque URL's query is the SUN data).
type appleAssociation struct {
	AppLinks appleAppLinks `json:"applinks"`
}

type appleAppLinks struct {
	Details []appleDetail `json:"details"`
}

type appleDetail struct {
	AppIDs     []string         `json:"appIDs"`
	Components []appleComponent `json:"components"`
}

type appleComponent struct {
	Path string `json:"/"`
}

func appleAppSiteAssociation(teamID string) ([]byte, error) {
	var components []appleComponent
	for _, p := range appLinkPaths {
		components = append(components, appleComponent{Path: p})
	}
	body, err := json.Marshal(appleAssociation{AppLinks: appleAppLinks{Details: []appleDetail{{
		AppIDs:     []string{teamID + "." + AppBundleID},
		Components: components,
	}}}})
	if err != nil {
		return nil, fmt.Errorf("handler: app links: apple-app-site-association: %w", err)
	}
	return body, nil
}

// The Digital Asset Links statement list (Android, "Configure the assetlinks.json file").
//
// relation_extensions carries the same path list as Dynamic App Links: Android 15+ (with Google
// services) merges it with the app manifest's intent filters and opens the app only for a matching
// path ("If no matches are found in the list of declared components, the URL won't open the app").
// Android 14 and earlier ignore the extension, so the manifest's own intent filter (M16-11) must
// name the same paths; the extension can narrow the manifest's scope, never widen it.
type assetStatement struct {
	Relation           []string                   `json:"relation"`
	Target             assetTarget                `json:"target"`
	RelationExtensions map[string]assetExtensions `json:"relation_extensions"`
}

type assetTarget struct {
	Namespace   string   `json:"namespace"`
	PackageName string   `json:"package_name"`
	CertSHA256  []string `json:"sha256_cert_fingerprints"`
}

type assetExtensions struct {
	Components []assetComponent `json:"dynamic_app_link_components"`
}

type assetComponent struct {
	Path string `json:"/"`
}

const handleAllURLs = "delegate_permission/common.handle_all_urls"

func assetLinks(certs []string) ([]byte, error) {
	var components []assetComponent
	for _, p := range appLinkPaths {
		components = append(components, assetComponent{Path: p})
	}
	body, err := json.Marshal([]assetStatement{{
		Relation: []string{handleAllURLs},
		Target: assetTarget{
			Namespace:   "android_app",
			PackageName: AppBundleID,
			CertSHA256:  append([]string(nil), certs...),
		},
		RelationExtensions: map[string]assetExtensions{handleAllURLs: {Components: components}},
	}})
	if err != nil {
		return nil, fmt.Errorf("handler: app links: assetlinks.json: %w", err)
	}
	return body, nil
}
