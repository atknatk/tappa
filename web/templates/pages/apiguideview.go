package pages

import (
	"strings"

	"github.com/atknatk/tappa/internal/apiguide"
	"github.com/atknatk/tappa/web/templates/layout"
)

// The integration guide's two pages on the product site (M11 DOCS-1, ADR 0030). internal/handler
// mounts them at these paths, the marketing footer and the panel's API keys screen link to them, and
// the page links each language to the other: one set of constants, so a link cannot name a path
// nothing serves.
const (
	// APIGuidePath is the guide in English, the original.
	APIGuidePath = "/docs/api"
	// APIGuideTRPath is its Turkish translation.
	APIGuideTRPath = "/docs/api/tr"
	// APIGuideWebhooksHref is the guide's section 9, the webhook documentation. The fragment is the
	// guide's own anchor for that section (<a id="webhooks"></a> in both languages).
	APIGuideWebhooksHref = APIGuidePath + "#webhooks"
)

// APIGuideView is one language's guide page.
type APIGuideView struct {
	// Guide is the converted Markdown (internal/apiguide.Render), converted once per process.
	Guide apiguide.Guide
	// Turkish selects the Turkish page: its lang attribute, its few words of chrome, and the link
	// back to the English original.
	Turkish bool
	// OpenAPIHref is where the OpenAPI document can be downloaded -- the API host's
	// /v1/openapi.yaml -- or "" where this deployment has no API host, and then no link is drawn.
	OpenAPIHref string
	// SignInHref is the panel's sign-in page, for the shared chrome.
	SignInHref string
	// SignupHref is the wizard, for the chrome's Start free ("" draws none).
	SignupHref string
}

// path is the page's own route, the chrome's current page.
func (v APIGuideView) path() string {
	if v.Turkish {
		return APIGuideTRPath
	}
	return APIGuidePath
}

// lang is the page's language tag.
func (v APIGuideView) lang() string {
	if v.Turkish {
		return "tr"
	}
	return "en"
}

// say picks the page's language.
func (v APIGuideView) say(en, tr string) string {
	if v.Turkish {
		return tr
	}
	return en
}

// sideSections is the side navigation: every anchored section but the guide's own contents list.
func (v APIGuideView) sideSections() []apiguide.Section {
	out := make([]apiguide.Section, 0, len(v.Guide.Sections))
	for _, s := range v.Guide.Sections {
		if s.ID != apiguide.ContentsID {
			out = append(out, s)
		}
	}
	return out
}

// pageTitle is the document's <title>: the guide's own title, which already names Taptime
// ("Taptime integration API — integration guide"), and the product's name after it only
// when it does not.
func (v APIGuideView) pageTitle() string {
	if strings.Contains(v.Guide.Title, "Taptime") {
		return v.Guide.Title
	}
	return v.Guide.Title + " — Taptime"
}

// alternates are the guide's two translations, for the head: the same pair on both pages,
// each naming itself too (hreflang is read as a set).
func (v APIGuideView) alternates() []layout.Alternate {
	return []layout.Alternate{{Lang: "en", Href: APIGuidePath}, {Lang: "tr", Href: APIGuideTRPath}}
}
