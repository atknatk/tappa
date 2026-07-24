// Package web embeds static assets into the binary so deployment stays a
// single artifact with no runtime file dependencies.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var staticFS embed.FS

// Static returns the asset tree rooted at web/static.
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embed layout is compile-time constant; this cannot fail at runtime
	}
	return sub
}
