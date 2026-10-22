package web_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"testing/fstest"

	"github.com/atknatk/tappa/web"
)

// TestAssetURL_VersionsOnlyFilesOfTheTableItWasGiven drives web.AssetURL over a tree
// whose bytes the test chose, so the version is known in advance and the "no table"
// row is the one every page test renders under.
func TestAssetURL_VersionsOnlyFilesOfTheTableItWasGiven(t *testing.T) {
	tree := fstest.MapFS{
		"css/app.css":  {Data: []byte("body{}")},
		"js/tap.js":    {Data: []byte("tap()")},
		"fonts/a.woff": {Data: []byte("font")},
	}
	assets, err := web.HashAssets(tree)
	if err != nil {
		t.Fatal(err)
	}
	version := func(b string) string {
		sum := sha256.Sum256([]byte(b))
		return hex.EncodeToString(sum[:])[:16]
	}
	withTable := web.WithAssets(t.Context(), assets)

	tests := []struct {
		name     string
		versions bool
		in, want string
	}{
		{"stylesheet with the table", true, "/static/css/app.css", "/static/css/app.css?v=" + version("body{}")},
		{"script with the table", true, "/static/js/tap.js", "/static/js/tap.js?v=" + version("tap()")},
		{"stylesheet without a table", false, "/static/css/app.css", "/static/css/app.css"},
		{"script without a table", false, "/static/js/tap.js", "/static/js/tap.js"},
		{"a name the tree does not hold", true, "/static/js/missing.js", "/static/js/missing.js"},
		{"a directory", true, "/static/js", "/static/js"},
		{"a path outside /static", true, "/brand/theme/DA291C.css", "/brand/theme/DA291C.css"},
		{"a path that already has a query", true, "/static/js/tap.js?x=1", "/static/js/tap.js?x=1"},
		{"a path with a fragment", true, "/static/js/tap.js#x", "/static/js/tap.js#x"},
		{"the empty string (no script)", true, "", ""},
		{"a relative name", true, "css/app.css", "css/app.css"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.versions {
				ctx = withTable
			}
			if got := web.AssetURL(ctx, tc.in); got != tc.want {
				t.Errorf("AssetURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	if sum, ok := assets.Sum("css/app.css"); !ok || sum[:16] != version("body{}") {
		t.Errorf("Sum(css/app.css) = %q, %v; want the hex sha256 of the bytes", sum, ok)
	}
	if v, ok := assets.Version("js/tap.js"); !ok || v != version("tap()") {
		t.Errorf("Version(js/tap.js) = %q, %v", v, ok)
	}
	if _, ok := assets.Version("js"); ok {
		t.Error("Version(js) answered for a directory")
	}
	var none *web.Assets
	if _, ok := none.Version("css/app.css"); ok {
		t.Error("a nil table answered a version")
	}
}
