package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAppClaims_SubmitHasOneCallerInTheHandlers ([G43-j]): the browser's card and the app's send
// reach claim.Service.Submit through claimSend alone -- no third write path.
func TestAppClaims_SubmitHasOneCallerInTheHandlers(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var callers []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(b), "claims.Submit("); n > 0 {
			callers = append(callers, f)
			if f != "appclaims.go" || n != 1 {
				t.Errorf("%s calls claims.Submit %d time(s); only claimSend in appclaims.go may", f, n)
			}
		}
	}
	if len(callers) != 1 {
		t.Fatalf("Submit callers %v, want exactly appclaims.go", callers)
	}
}
