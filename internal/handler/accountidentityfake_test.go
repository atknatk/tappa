package handler

import (
	"context"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// fakeIdentityHooks are fakeAdmins' account-page identity acts (M14-18). A nil hook answers the
// harmless default: no window, every act refused, nothing written. calls records each act by name.
type fakeIdentityHooks struct {
	window       func() (adminauth.Window, error)
	keepAll      func() error
	request      func(s adminauth.Resolved) (adminauth.IssuedVerification, error)
	verify       func(raw string) (bool, error)
	unlink       func(s adminauth.Resolved, password string, target uuid.UUID) (adminauth.FreshAct, bool, error)
	relink       func(s adminauth.Resolved, password string, target uuid.UUID) (adminauth.FreshAct, error)
	linkBusiness func(s adminauth.Resolved, current, other string) (adminauth.LinkResult, error)
	bucket       func(s adminauth.Resolved) (string, error)
	calls        []string
	budgets      []adminauth.FailureBudget
	verified     []string
}

func (f *fakeAdmins) identCall(name string) {
	f.mu.Lock()
	f.ident.calls = append(f.ident.calls, name)
	f.mu.Unlock()
}

func (f *fakeAdmins) identCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ident.calls...)
}

func (f *fakeAdmins) ConfirmWindow(context.Context, adminauth.Token) (adminauth.Window, error) {
	f.identCall("window")
	if f.ident.window == nil {
		return adminauth.Window{}, nil
	}
	return f.ident.window()
}

func (f *fakeAdmins) KeepAll(context.Context, adminauth.Token) error {
	f.identCall("keep-all")
	if f.ident.keepAll == nil {
		return adminauth.ErrWindowRefused
	}
	return f.ident.keepAll()
}

func (f *fakeAdmins) RequestVerification(_ context.Context, s adminauth.Resolved, _ adminauth.Token) (adminauth.IssuedVerification, error) {
	f.identCall("request-verification")
	if f.ident.request == nil {
		return adminauth.IssuedVerification{}, adminauth.ErrNotLinked
	}
	return f.ident.request(s)
}

func (f *fakeAdmins) VerifyEmail(_ context.Context, raw string, _ adminauth.Token) (bool, error) {
	f.identCall("verify-email")
	f.mu.Lock()
	f.ident.verified = append(f.ident.verified, raw)
	f.mu.Unlock()
	if f.ident.verify == nil {
		return false, adminauth.ErrVerificationRefused
	}
	return f.ident.verify(raw)
}

func (f *fakeAdmins) Unlink(_ context.Context, s adminauth.Resolved, password string, target uuid.UUID, budget adminauth.FailureBudget) (adminauth.FreshAct, bool, error) {
	f.identCall("unlink")
	f.mu.Lock()
	f.ident.budgets = append(f.ident.budgets, budget)
	f.mu.Unlock()
	if f.ident.unlink == nil {
		return adminauth.FreshAct{}, false, adminauth.ErrWindowRefused
	}
	return f.ident.unlink(s, password, target)
}

func (f *fakeAdmins) Relink(_ context.Context, s adminauth.Resolved, password string, target uuid.UUID, budget adminauth.FailureBudget) (adminauth.FreshAct, error) {
	f.identCall("relink")
	f.mu.Lock()
	f.ident.budgets = append(f.ident.budgets, budget)
	f.mu.Unlock()
	if f.ident.relink == nil {
		return adminauth.FreshAct{}, adminauth.ErrWindowRefused
	}
	return f.ident.relink(s, password, target)
}

func (f *fakeAdmins) LinkBusiness(_ context.Context, s adminauth.Resolved, current, other string, budget adminauth.FailureBudget) (adminauth.LinkResult, error) {
	f.identCall("link")
	f.mu.Lock()
	f.ident.budgets = append(f.ident.budgets, budget)
	f.mu.Unlock()
	if f.ident.linkBusiness == nil {
		return adminauth.LinkResult{}, nil
	}
	return f.ident.linkBusiness(s, current, other)
}

func (f *fakeAdmins) AccountBucket(_ context.Context, s adminauth.Resolved) (string, error) {
	if f.ident.bucket == nil {
		return "row:" + s.AdminUserID.String(), nil
	}
	return f.ident.bucket(s)
}
