package handler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/legal"
)

// fakeTexts is the double for internal/domain/legal.Store.
//
// 🔴 IT STARTS EMPTY, WHICH IS THE STATE THE PRODUCT SHIPPED IN. A double that
// pre-populated the four documents would make every "the placeholder is still
// showing" assertion vacuous — and that placeholder is the sentence M7-01 shipped
// and M7-06 has to remove exactly once, for exactly the document that was published.
type fakeTexts struct {
	mu sync.Mutex
	// docs is the snapshot.
	docs map[string]legal.Doc
	// calls records every Publish that reached this double, in order, so a test can
	// assert what the handler passed rather than only what came back.
	calls []fakePublish
	// err, when set, is what Publish returns instead of writing.
	err error
}

// fakePublish is one recorded call. The TENANT AND ACTOR ARE RECORDED because the
// §4.5 story of this whole feature is that they are the caller's own and nobody
// else's; a test that only checked the body would not notice them being wrong.
type fakePublish struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	Slug     string
	Body     string
}

func newFakeTexts() *fakeTexts { return &fakeTexts{docs: map[string]legal.Doc{}} }

func (f *fakeTexts) Published() map[string]legal.Doc {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]legal.Doc, len(f.docs))
	for k, v := range f.docs {
		out[k] = v
	}
	return out
}

func (f *fakeTexts) Publish(_ context.Context, tenantID, actorID uuid.UUID, slug, body string) (legal.Doc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakePublish{TenantID: tenantID, ActorID: actorID, Slug: slug, Body: body})
	if f.err != nil {
		return legal.Doc{}, f.err
	}
	if !legal.Valid(slug) {
		return legal.Doc{}, errors.New("fake: unknown slug")
	}
	d := legal.Doc{
		Slug:        slug,
		Body:        body,
		PublishedAt: time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC),
		Paragraphs:  legal.Paragraphs(body),
	}
	f.docs[slug] = d
	return d, nil
}

// put installs a published document without going through Publish, for tests whose
// subject is the READ.
func (f *fakeTexts) put(slug, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs[slug] = legal.Doc{
		Slug:        slug,
		Body:        body,
		PublishedAt: time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC),
		// Precomputed exactly as the real Store does, so a handler that read Body and
		// split it again would still pass here and the difference would go unnoticed.
		Paragraphs: legal.Paragraphs(body),
	}
}

func (f *fakeTexts) published() []fakePublish {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakePublish(nil), f.calls...)
}
