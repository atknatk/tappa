// Package apidocs embeds the integration guide -- the integrator's prose beside the OpenAPI document
// (M11 API-12; ADR 0028 §8, "API-12 uygulama notu") -- so the binary serves the very bytes this
// directory holds: GET /v1/guide.md is integration-guide.md, GET /v1/guide.tr.md its Turkish
// translation (internal/handler/api's guide.go). It is a package of its own, in the guide's own
// directory, for web's reason (web/embed.go): go:embed does not reach a parent directory, and the
// guide belongs with the documents, not inside a handler package.
//
// The two files are one document in two languages: their heading tree, their numbered items, their
// code blocks and their link targets are the same, byte for byte where they are not prose -- the
// API package's tests hold that (TestGuide_TheTwoLanguagesAreOneDocument).
package apidocs

import _ "embed"

//go:embed integration-guide.md
var english []byte

//go:embed integration-guide.tr.md
var turkish []byte

// English is the guide in English, the original.
func English() []byte { return append([]byte(nil), english...) }

// Turkish is the guide's Turkish translation.
func Turkish() []byte { return append([]byte(nil), turkish...) }
