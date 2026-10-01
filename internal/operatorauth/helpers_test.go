package operatorauth

import (
	"crypto/sha1"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// sha1New is the production HOTP hash, named for the tests that compute codes.
var sha1New = sha1.New

// lockedWriter makes a strings.Builder safe for a logger shared by racing goroutines.
type lockedWriter struct {
	mu sync.Mutex
	b  *strings.Builder
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

// newTestLogger captures EVERY level, Debug included: the product can run at Debug
// (TAPPA_LOG_LEVEL), and a capture at the default Info level would not see a Debug line
// that carries a secret -- measured (OP-6 verification, 4th round): a Debug line with
// the typed code stayed green while the captures were at Info.
func newTestLogger(b *strings.Builder) *slog.Logger {
	var w io.Writer = &lockedWriter{b: b}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
