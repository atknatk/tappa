package api

import (
	_ "embed"
	"net/http"
)

// THE OPENAPI DOCUMENT (ADR 0028 §8): OpenAPI 3.1, written by hand (no generator, no Node --
// CLAUDE.md §1), embedded in the binary and served at GET /v1/openapi.yaml without a key -- it
// describes the contract and holds no tenant's data. Its paths are the router's /v1 routes and its
// Error schema is errors.go's, both ways (TestOpenAPI_PathsAreTheRoutersAndTheErrorIsSection8):
// an endpoint added without its paragraph here, or a code added on one side only, is a red test.
// Like every /v1 GET it takes only the query parameters it names -- none (readQuery).

//go:embed openapi.yaml
var openAPIDocument []byte

// openAPIPath is the document's route under Prefix.
const openAPIPath = "/openapi.yaml"

// OpenAPI returns a copy of the embedded document.
func OpenAPI() []byte { return append([]byte(nil), openAPIDocument...) }

func (s *Surface) openAPI(w http.ResponseWriter, r *http.Request) {
	if _, prob := readQuery(r, nil, problemNoQuery); prob != nil {
		writeProblem(w, prob)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPIDocument)
}
