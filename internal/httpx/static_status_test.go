package httpx

import (
	"net/http"
	"testing"
)

// statusLog is a ResponseWriter that records, for every WriteHeader, the status and the
// two asset headers as they stood at that moment.
type statusLog struct {
	h     http.Header
	calls []statusCall
}

type statusCall struct {
	code      int
	cache, et string
}

func (s *statusLog) Header() http.Header         { return s.h }
func (s *statusLog) Write(b []byte) (int, error) { return len(b), nil }
func (s *statusLog) WriteHeader(code int) {
	s.calls = append(s.calls, statusCall{code, s.h.Get("Cache-Control"), s.h.Get("ETag")})
}

// TestStaticAssetStatus_InformationalStatusesPassThrough: assetStatus decides the asset
// headers on the FINAL status. A 103 before a 200 is passed through untouched, and the
// 200 still carries the pin and the ETag; 101 is final and is treated as a refusal.
func TestStaticAssetStatus_InformationalStatusesPassThrough(t *testing.T) {
	const etag = `"abc"`
	tests := []struct {
		name  string
		codes []int
		want  []statusCall
	}{
		{"103 then 200", []int{http.StatusEarlyHints, http.StatusOK}, []statusCall{
			{http.StatusEarlyHints, cacheImmutable, etag}, {http.StatusOK, cacheImmutable, etag}}},
		{"103 then 412", []int{http.StatusEarlyHints, http.StatusPreconditionFailed}, []statusCall{
			{http.StatusEarlyHints, cacheImmutable, etag}, {http.StatusPreconditionFailed, "", ""}}},
		{"100 then 304", []int{http.StatusContinue, http.StatusNotModified}, []statusCall{
			{http.StatusContinue, cacheImmutable, etag}, {http.StatusNotModified, cacheImmutable, etag}}},
		{"101 is final", []int{http.StatusSwitchingProtocols}, []statusCall{
			{http.StatusSwitchingProtocols, "", ""}}},
		{"200 alone", []int{http.StatusOK}, []statusCall{{http.StatusOK, cacheImmutable, etag}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log := &statusLog{h: http.Header{}}
			log.h.Set("Cache-Control", cacheImmutable)
			log.h.Set("ETag", etag)
			w := &assetStatus{ResponseWriter: log}
			for _, c := range tc.codes {
				w.WriteHeader(c)
			}
			if len(log.calls) != len(tc.want) {
				t.Fatalf("recorded %v, want %v", log.calls, tc.want)
			}
			for i := range tc.want {
				if log.calls[i] != tc.want[i] {
					t.Errorf("WriteHeader #%d = %+v, want %+v", i, log.calls[i], tc.want[i])
				}
			}
		})
	}
}
