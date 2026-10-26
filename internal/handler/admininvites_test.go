package handler

import (
	"testing"

	"github.com/atknatk/tappa/internal/adminauth"
)

// TestAdminInvite_ThePathIsTheLinksPath: the route the invitee's page is mounted on is the path the
// link adminauth mints opens (IssuedInvite.Link), and the templates' two forms post there.
func TestAdminInvite_ThePathIsTheLinksPath(t *testing.T) {
	if adminInvitePath != adminauth.InvitePath {
		t.Fatalf("the route is %q, the link opens %q", adminInvitePath, adminauth.InvitePath)
	}
}
