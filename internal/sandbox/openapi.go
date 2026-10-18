package sandbox

import (
	_ "embed"
	"net/http"

	"github.com/atknatk/tappa/internal/handler/api"
)

// THE SANDBOX'S OPENAPI DOCUMENT (M11 API-11). It is a document of its OWN rather than a section of
// internal/handler/api's openapi.yaml, because that document is the production surface's and is
// held to it both ways (TestOpenAPI_PathsAreTheRoutersAndTheErrorIsSection8: every path is a route
// of the router, every route a path): a path the production binary does not serve cannot be in it.
// This one describes what only the sandbox build adds -- POST /v1/sandbox/taps -- and refers to the
// shipped document for everything else; its error codes are the shipped set
// (TestSandboxOpenAPI_IsTheSimulationsRoutesAndTheSharedErrorSet). JSON rather than YAML: the
// standard library reads it, so its test needs no parser of its own.

//go:embed openapi.json
var openAPIDocument []byte

// OpenAPI returns a copy of the embedded document.
func OpenAPI() []byte { return append([]byte(nil), openAPIDocument...) }

func serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		api.WriteProblem(w, api.NoQuery())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPIDocument)
}
