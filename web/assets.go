package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"
)

// StaticPrefix is the URL path the asset tree is served under (internal/httpx mounts
// "/static/*" and strips this prefix).
const StaticPrefix = "/static/"

// versionLen is how many hex digits of a file's SHA-256 its ?v= carries. 16 digits are
// 64 bits: the version only has to tell this deploy's bytes from an earlier deploy's,
// and a page names a handful of assets, so the full 64 digits would only lengthen every
// <head> without changing what a mismatch can be.
const versionLen = 16

// Assets is the content table of an asset tree: every file's SHA-256, computed once.
// It is the ONE place both halves of the versioned-URL rule read from -- the templates
// write /static/<name>?v=<Version(name)>, and internal/httpx's file server pins exactly
// that URL for a year -- so the two cannot disagree about what a version is.
type Assets struct {
	sums map[string]string // name relative to the tree root -> hex sha256 of its bytes
}

// HashAssets reads every file under fsys and returns its table.
func HashAssets(fsys fs.FS) (*Assets, error) {
	sums := make(map[string]string)
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		sums[name] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("assets: hashing the tree: %w", err)
	}
	return &Assets{sums: sums}, nil
}

// Sum is the hex SHA-256 of the file at name (relative to the tree root), and false
// when the tree has no such file.
func (a *Assets) Sum(name string) (string, bool) {
	if a == nil {
		return "", false
	}
	s, ok := a.sums[name]
	return s, ok
}

// Version is the ?v= value of the file at name, and false when the tree has no such
// file.
func (a *Assets) Version(name string) (string, bool) {
	s, ok := a.Sum(name)
	if !ok {
		return "", false
	}
	return s[:versionLen], true
}

// URL is p with its version appended when p is a file in this tree
// ("/static/css/app.css" -> "/static/css/app.css?v=0123456789abcdef"), and p unchanged
// otherwise: a path outside /static, a path that already carries a query, or a name
// the tree does not hold. Unchanged is the old URL, which is still served -- under
// revalidation instead of a year's pin -- so a miss here costs a round trip, never a
// broken page.
func (a *Assets) URL(p string) string {
	name, ok := strings.CutPrefix(p, StaticPrefix)
	if !ok || strings.ContainsAny(name, "?#") {
		return p
	}
	v, ok := a.Version(name)
	if !ok {
		return p
	}
	return p + "?v=" + v
}

// assetsKey is the context key WithAssets stores the table under.
type assetsKey struct{}

// WithAssets returns ctx carrying a. cmd/tappa puts the binary's table into every
// request's context through http.Server.BaseContext; AssetURL reads it back while a
// page renders.
func WithAssets(ctx context.Context, a *Assets) context.Context {
	return context.WithValue(ctx, assetsKey{}, a)
}

// AssetURL is the URL a page writes for the asset at p: versioned when ctx carries a
// table (WithAssets) that holds p, and p unchanged otherwise.
//
// 🔴 UNCHANGED WITHOUT A TABLE IS THE POINT, NOT A FALLBACK. Only the serving process
// puts a table in the context, so every test that renders a page -- the byte goldens
// under internal/handler/testdata among them -- sees the bare URL, and an edit to
// app.css does not rewrite them. What the versioned URL costs and buys is asserted
// where the table IS installed: internal/httpx's static cache tests and cmd/tappa's
// end-to-end test over the server run() builds.
func AssetURL(ctx context.Context, p string) string {
	a, _ := ctx.Value(assetsKey{}).(*Assets)
	if a == nil {
		return p
	}
	return a.URL(p)
}
