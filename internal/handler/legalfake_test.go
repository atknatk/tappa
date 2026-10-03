package handler

import (
	"context"
	"sync"
	"time"

	"github.com/atknatk/tappa/internal/domain/legal"
)

// fakeTexts is the double for internal/domain/legal.Store, as the PUBLIC surface sees
// it (legalReader: Published) -- and with the store's other method, Refresh, COUNTED, so
// a test can say the public pages never reached it.
//
// 🔴 IT STARTS EMPTY, WHICH IS THE STATE THE PRODUCT SHIPPED IN. A double that
// pre-populated the four documents would make every "the placeholder is still
// showing" assertion vacuous.
//
// (Until M10 OP-10 it was also the M7-06 panel's writer, with a Publish that recorded
// the publisher's tenant and admin id. The panel's legal screen is gone -- the platform
// operator publishes through op_publish_legal -- and so is that method.)
type fakeTexts struct {
	mu sync.Mutex
	// docs is the snapshot.
	docs map[string]legal.Doc
	// reads counts Published calls; refreshes counts Refresh calls.
	reads, refreshes int
}

func newFakeTexts() *fakeTexts { return &fakeTexts{docs: map[string]legal.Doc{}} }

func (f *fakeTexts) Published() map[string]legal.Doc {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	out := make(map[string]legal.Doc, len(f.docs))
	for k, v := range f.docs {
		out[k] = v
	}
	return out
}

// Refresh is the store's read of the database; the public surface's reader has no such
// method, and this one only counts.
func (f *fakeTexts) Refresh(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	return nil
}

// put installs a published document, for tests whose subject is the READ.
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

func (f *fakeTexts) counts() (reads, refreshes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads, f.refreshes
}
