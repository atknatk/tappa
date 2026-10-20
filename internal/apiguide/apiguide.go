// Package apiguide draws the integration guide (docs/api/integration-guide.md and its Turkish
// translation) as an HTML page for the product site (M11 DOCS-1, ADR 0030). The Markdown files stay
// the one source: GET /v1/guide.md serves their bytes unchanged, and this package turns the same
// bytes into the body of /docs/api and /docs/api/tr.
//
// The conversion is goldmark's CommonMark core plus its own GFM table extension and nothing else
// (ADR 0030 lists the boundary). Four rules hold it to what a public page on this site may carry:
//
//   - RAW HTML IS NEVER WRITTEN. goldmark's renderer is built without html.WithUnsafe, so a raw HTML
//     block or tag in the source becomes the renderer's "raw HTML omitted" comment. The one HTML
//     form the guide uses on purpose -- the anchor line `<a id="x"></a>` before a heading -- is not
//     passed through either: it is read here and becomes that heading's id attribute.
//   - A LINK IS DRAWN ONLY TO http(s), TO THIS SITE OR TO THIS PAGE. A link whose target has any
//     other scheme (javascript:, data:, mailto:, ftp:...) is drawn as its text, without <a>; so is
//     an http(s) target without a host, a scheme-relative //host or /\host, and a file-relative
//     link the caller did not map to a site address (a path into the repository resolves nowhere
//     on the site). The target judged is the one the renderer writes -- backslash escapes removed
//     and entities resolved -- not the source spelling. Images are drawn as their text: the
//     page's policy names no img-src.
//   - AN HTML COMMENT BLOCK IS DROPPED (the guide's <!-- example: ... --> test markers): its text
//     never reaches the page. The only comment the page can carry is the renderer's own
//     "raw HTML omitted", where refused raw HTML stood -- and the guide has none.
//   - The conversion runs once per process, at construction, not per request.
package apiguide

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/a-h/templ"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// ContentsID is the anchor of the guide's own table of contents (the section after the title). The
// page's side navigation lists every other section and leaves this one out: it is the list the
// side navigation already is.
const ContentsID = "contents"

// Section is one heading with an anchor, in document order.
type Section struct {
	ID    string
	Title string
	Level int
}

// Guide is one language's guide as the site draws it.
type Guide struct {
	// Title is the guide's level-1 heading. It is not in the body: the page draws it.
	Title string
	// Sections are the level-2 and level-3 headings that carry an anchor.
	Sections []Section
	// body is goldmark's output. It is unexported so that the only way to hold one is Render: the
	// page writes it without escaping (Body), and nothing but this package's conversion may decide
	// what those bytes are.
	body string
}

// Body writes the converted guide. It is the one place the page writes markup it did not escape,
// and what it writes is Render's output: no raw HTML, filtered link targets.
func (g Guide) Body() templ.Component {
	body := g.body
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, body)
		return err
	})
}

// Options are what the site knows that the Markdown does not.
type Options struct {
	// Links maps a file-relative link target in the Markdown (its path, without the fragment) to
	// the address that file has on the site; a fragment on the source link is kept. A target mapped
	// to "" is drawn as text. A file-relative target that is not in the map is drawn as text too.
	Links map[string]string
}

// anchorTag is the guide's anchor line: <a id="x"></a>, alone in a paragraph before a heading.
var anchorTag = regexp.MustCompile(`^<a id="([a-z0-9-]+)">$`)

// Render converts one guide.
func Render(src []byte, opt Options) (Guide, error) {
	md := goldmark.New(goldmark.WithExtensions(
		// Alignment is never written: TableCellAlignNone emits neither the align attribute nor a
		// style attribute (the page's policy refuses inline style).
		extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignNone)),
	))
	doc := md.Parser().Parse(text.NewReader(src))
	g := Guide{}
	if err := transform(doc, src, opt, &g); err != nil {
		return Guide{}, err
	}
	var out bytes.Buffer
	if err := md.Renderer().Render(&out, src, doc); err != nil {
		return Guide{}, fmt.Errorf("apiguide: render: %w", err)
	}
	g.body = out.String()
	return g, nil
}

// transform rewrites the parsed document in place: anchors onto headings, the title out of the
// body, link targets filtered, headings made their own permalinks.
func transform(doc ast.Node, src []byte, opt Options, g *Guide) error {
	var (
		anchors  []*ast.Paragraph
		comments []*ast.HTMLBlock
		links    []*ast.Link
		autos    []*ast.AutoLink
		images   []*ast.Image
	)
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Paragraph:
			if anchorOf(v, src) != "" {
				anchors = append(anchors, v)
			}
		case *ast.HTMLBlock:
			// The guide's markers (<!-- example: ... -->) are for its tests; on the page they would be
			// the renderer's "raw HTML omitted" comment. A comment carries nothing, so it is dropped.
			// Every other raw block is left to the renderer, which does not write it.
			if v.HTMLBlockType == ast.HTMLBlockType2 {
				comments = append(comments, v)
			}
		case *ast.Link:
			links = append(links, v)
		case *ast.AutoLink:
			autos = append(autos, v)
		case *ast.Image:
			images = append(images, v)
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return fmt.Errorf("apiguide: walk: %w", err)
	}

	for _, p := range anchors {
		id := anchorOf(p, src)
		h, ok := p.NextSibling().(*ast.Heading)
		if !ok {
			return fmt.Errorf("apiguide: the anchor %q is not followed by a heading", id)
		}
		h.SetAttributeString("id", []byte(id))
		p.Parent().RemoveChild(p.Parent(), p)
	}
	for _, c := range comments {
		c.Parent().RemoveChild(c.Parent(), c)
	}
	for _, l := range links {
		// The decision is made on the target the renderer WRITES, not on the source spelling:
		// goldmark's renderLink writes util.URLEscape(Destination, true), which removes backslash
		// escapes and resolves entities, so `/\/evil.example` and `/&#47;evil.example` are both
		// written as //evil.example. Judging the source spelling let them through (DOCS-1 audit).
		written := string(util.URLEscape(l.Destination, true))
		dest, ok := siteTarget(written, opt.Links)
		if !ok {
			unwrap(l)
			continue
		}
		if dest != written {
			// A mapped file: the site address replaces the target. It is this package's own
			// constant (plus the source's fragment), and it carries nothing the renderer resolves.
			l.Destination = []byte(dest)
		}
		// Otherwise the source spelling stays: resolving it again would be a second decoding
		// (`&amp;#47;` -> `&#47;` -> `/`), and the renderer's one decoding is what was judged.
	}
	for _, a := range autos {
		// renderAutoLink writes util.URLEscape(URL, false): no resolution, judged as written.
		written := string(util.URLEscape(a.URL(src), false))
		if _, ok := siteTarget(written, nil); ok && a.AutoLinkType == ast.AutoLinkURL {
			continue
		}
		a.Parent().ReplaceChild(a.Parent(), a, ast.NewString(a.Label(src)))
	}
	for _, im := range images {
		unwrap(im)
	}

	// The headings, after the anchors are on them.
	var headings []*ast.Heading
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if h, ok := n.(*ast.Heading); ok {
			headings = append(headings, h)
		}
	}
	for _, h := range headings {
		if h.Level == 1 && g.Title == "" {
			g.Title = plainText(h, src)
			h.Parent().RemoveChild(h.Parent(), h)
			continue
		}
		id, ok := h.AttributeString("id")
		if !ok {
			continue
		}
		ids, _ := id.([]byte)
		if h.Level <= 3 {
			g.Sections = append(g.Sections, Section{ID: string(ids), Title: plainText(h, src), Level: h.Level})
		}
		permalink(h, string(ids))
	}
	if g.Title == "" {
		return errors.New("apiguide: the guide has no level-1 heading")
	}
	return nil
}

// anchorOf returns x when p is exactly the anchor line <a id="x"></a>, else "".
func anchorOf(p *ast.Paragraph, src []byte) string {
	open, ok := p.FirstChild().(*ast.RawHTML)
	if !ok || p.ChildCount() != 2 {
		return ""
	}
	closing, ok := open.NextSibling().(*ast.RawHTML)
	if !ok || rawText(closing, src) != "</a>" {
		return ""
	}
	m := anchorTag.FindStringSubmatch(rawText(open, src))
	if m == nil {
		return ""
	}
	return m[1]
}

func rawText(r *ast.RawHTML, src []byte) string {
	var b strings.Builder
	for i := 0; i < r.Segments.Len(); i++ {
		s := r.Segments.At(i)
		b.Write(s.Value(src))
	}
	return b.String()
}

// siteTarget decides a link target, given as the renderer writes it (escapes removed, entities
// resolved): ("#x", true) for this page, an http(s) URL with a host or a site path as it is, a
// mapped file-relative path as its site address (fragment kept), and false for anything else --
// another scheme, an http(s) target without a host (a browser reads `http:x` on an https page as
// http://x/), a scheme-relative //host or /\host (a browser reads a backslash as a slash), an
// unmapped file-relative path, a target that does not parse.
func siteTarget(dest string, links map[string]string) (string, bool) {
	if dest == "" {
		return "", false
	}
	if strings.HasPrefix(dest, "#") {
		return dest, true
	}
	u, err := url.Parse(dest)
	if err != nil {
		return "", false
	}
	switch {
	case u.Scheme != "":
		s := strings.ToLower(u.Scheme)
		if (s == "http" || s == "https") && u.Host != "" {
			return dest, true
		}
		return "", false
	case strings.HasPrefix(dest, "//"), strings.HasPrefix(dest, `/\`):
		return "", false
	case strings.HasPrefix(dest, "/"):
		return dest, true
	}
	path, frag, _ := strings.Cut(dest, "#")
	to, ok := links[path]
	if !ok || to == "" {
		return "", false
	}
	if frag != "" {
		return to + "#" + frag, true
	}
	return to, true
}

// unwrap replaces n with its children: a refused link is drawn as its text.
func unwrap(n ast.Node) {
	parent := n.Parent()
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		parent.InsertBefore(parent, n, c)
		c = next
	}
	parent.RemoveChild(parent, n)
}

// permalink makes the heading's text a link to the heading itself, so a reader can copy the
// section's address (/docs/api#webhooks). A heading that already holds a link is left alone: a
// link inside a link is not valid HTML.
func permalink(h *ast.Heading, id string) {
	for c := h.FirstChild(); c != nil; c = c.NextSibling() {
		if _, ok := c.(*ast.Link); ok {
			return
		}
	}
	l := ast.NewLink()
	l.Destination = []byte("#" + id)
	for c := h.FirstChild(); c != nil; {
		next := c.NextSibling()
		l.AppendChild(l, c)
		c = next
	}
	h.AppendChild(h, l)
}

// plainText is a heading's text without markup: its text and code spans, in order.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch v := c.(type) {
			case *ast.Text:
				b.Write(v.Segment.Value(src))
				if v.SoftLineBreak() || v.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(v.Value)
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}
