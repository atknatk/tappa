package pages

import "github.com/atknatk/tappa/internal/apiguide"

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
