// Package appdevice answers the "who" of a native-app request (ADR 0045 §2.3) without a bearer
// credential: the device signs each request with a key that never leaves the phone, over a
// canonical text that includes a single-use nonce this server issued. It is the app's twin of
// internal/session, and the second home of cryptography in this repository -- ECDSA P-256
// verification -- separate from internal/sun's plaque cryptography (CLAUDE.md §3).
//
// WHAT THIS PACKAGE DOES NOT DO -- the rule internal/session/manager.go writes, kept here:
//
//   - It does NOT decide. It reports facts: this request is (or is not) signed by a live device's
//     key over a nonce that was unspent, and whose device that is. internal/policy and
//     internal/domain/tap decide what the tap means. In particular:
//     a REVOKED device is its own refusal, ErrRevokedDevice, and the caller treats it as §5 row
//     3's twin (401 / re-activation, NO record); a DEACTIVATED employee's device is NOT revoked
//     (deactivation revokes nothing -- the user's decision of 2026-08-07), so it verifies like
//     any other and §5 row 4 comes from employees.status + sys:employee-deactivated (reject +
//     record + alert). Neither outcome is turned into the other here: one way a record is lost,
//     the other way a recordless path writes one (§4.6).
//
//   - It reads nothing from the phone but the request. No attestation (M16-25 adds it as
//     evidence, elsewhere), no gate on the key that reads the person (§4.1, ADR 0045 §3), no probing.
//
//   - It logs NOTHING. There is no logger in this package (TestPackage_HasNoLogger): the
//     signature, the nonce, the public key and the body are §7's never-logged values, and the
//     surest way not to log them is to have no log call to put them in. Every error carries a
//     fixed text; no value is interpolated.
//
// THE ORDER OF THE CHECKS (Verify), and why:
//
//  1. Shape (NewSignedRequest / FromHTTP) -- no database.
//  2. Resolve the device id (db.GetAppDeviceByID, the SECURITY DEFINER resolver; the tenant is
//     the RESULT). Unknown -> ErrUnknownDevice. Revoked -> ErrRevokedDevice, BEFORE the
//     signature is looked at. The two errors print the same text and map to the same status
//     (M16-03 review: no status oracle); only errors.Is tells them apart, inside the server,
//     to choose the recordless path.
//  3. The key is parsed on the curve, the signature verified. Bad -> ErrBadSignature (the same
//     text again: a junk signature must not tell a caller that a device id is live).
//  4. Under the resolved tenant, in ONE transaction: TouchAppDevice, then
//     ConsumeAppRequestNonce. Touch FIRST is the M16-03 lock rule (D2): it takes the device
//     row's update lock before the consumption's share lock, so two requests of one device
//     queue on that row instead of upgrading a share lock into a deadlock; and it is the
//     in-transaction liveness check (0 rows -> revoked in flight -> ErrRevokedDevice). The
//     consumption is §4.4's one statement (0 rows -> ErrNonceRejected: unknown, another
//     device's, replayed or expired) and carries the load-bearing EXISTS ... FOR SHARE belt.
//     A refused consumption rolls the touch back with it.
package appdevice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// NonceTTL is how long an issued nonce stays spendable. The device asks for a nonce, signs and
// sends at once; a minute covers a slow network without leaving a captured nonce useful for
// long. 00104's CHECK caps any lifetime at five minutes.
const NonceTTL = 60 * time.Second

// Database is the narrow slice of *db.DB this package needs, declared at the consumer
// (CLAUDE.md §7). WithTenant runs every tenant-scoped statement (each carries its own tenant_id
// filter, §4.5); GetAppDeviceByID is the one context-less lookup (ADR 0002 md.7) -- a signed
// request names a device and no tenant.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	GetAppDeviceByID(ctx context.Context, deviceID uuid.UUID) (db.ResolvedAppDevice, error)
}

// AuthError is a refusal of a signed request. Its text is FIXED and shared: every refusal after
// the shape check prints notAuthenticated, so neither an error string a handler echoes nor a
// status code tells a caller whether a device id exists, was revoked, or holds a different key.
// errors.Is against the sentinels below is how the SERVER tells them apart.
type AuthError struct {
	text   string
	reason string
}

func (e *AuthError) Error() string { return e.text }

// Reason is a fixed, value-free label for the server's own use (an audit kind, a metric): it
// distinguishes the refusals that Error() deliberately does not. Never send it to the client.
func (e *AuthError) Reason() string { return e.reason }

const notAuthenticated = "appdevice: request not authenticated"

var (
	// ErrMalformed: the request cannot be a signed app request -- a missing, repeated or
	// mis-shaped header, a query string, a method or path outside the canonical character set.
	// Decided before the database, from the caller's own input alone, so its distinct text
	// reveals nothing about any device.
	ErrMalformed = &AuthError{text: "appdevice: malformed signed request", reason: "malformed"}
	// ErrUnknownDevice: no device has this id.
	ErrUnknownDevice = &AuthError{text: notAuthenticated, reason: "unknown device"}
	// ErrRevokedDevice: the device exists and was revoked (or was revoked while this request was
	// in flight). §5 row 3's twin: 401 / re-activation, no record. NOT the deactivated case.
	ErrRevokedDevice = &AuthError{text: notAuthenticated, reason: "revoked device"}
	// ErrBadSignature: the signature does not verify under the device's key over this request's
	// canonical text (wrong key, any tampered part, a malformed DER).
	ErrBadSignature = &AuthError{text: notAuthenticated, reason: "bad signature"}
	// ErrNonceRejected: the nonce is not an unspent, unexpired nonce issued to THIS device in its
	// tenant -- never issued, another device's, replayed, expired, or deleted by a revocation.
	ErrNonceRejected = &AuthError{text: notAuthenticated, reason: "nonce rejected"}
)

// StatusOf is the HTTP status a refusal is answered with: 401 for every AuthError, 500 for
// anything else (a database failure is not the caller's fault and is not a 401 -- answering it
// with one would send every app user to re-activation during an outage).
func StatusOf(err error) int {
	var ae *AuthError
	if errors.As(err, &ae) {
		return http.StatusUnauthorized
	}
	return http.StatusInternalServerError
}

// Device is what a verified request resolves to: the facts, not a decision. EmployeeID's status
// is NOT here -- the caller reads it under the tenant context and the policy engine decides.
type Device struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
}

// IssuedNonce is a nonce handed to a device: Nonce.Wire() goes into the response body,
// ExpiresAt tells the device how long it has (the database's clock).
type IssuedNonce struct {
	Nonce     Nonce
	ExpiresAt time.Time
}

// Verifier issues nonces and verifies signed requests. Dependencies are struct fields (§7).
type Verifier struct {
	data Database
	// ttl is NonceTTL; a field so an in-package test can shorten it to measure expiry.
	ttl time.Duration
}

// New builds a Verifier.
func New(data Database) (*Verifier, error) {
	if data == nil {
		return nil, errors.New("appdevice: nil database")
	}
	return &Verifier{data: data, ttl: NonceTTL}, nil
}

// IssueNonce draws a fresh nonce for a LIVE device and records its hash. An unknown and a
// revoked device get the same refusal as in Verify (ErrUnknownDevice / ErrRevokedDevice, one
// text); a device revoked while this runs inserts nothing (IssueAppRequestNonce reads the device
// row FOR SHARE in the same statement) and is ErrRevokedDevice.
//
// ⚠️ COUNTED LIMIT, handed to M16-07: this answers "is this device id live" to anyone who sends
// the id, signature or not -- the nonce request cannot be signed (it is what the signature
// needs). A device id is a random UUID the device sends only over TLS, so knowing one is close
// to holding a captured request; if that is not enough, the handler can answer a non-live id
// with a decoy nonce it does not store, and the signed request then fails like any other.
//
// LOCK ORDER (M16-03 security re-check, D1): the insert takes the device row (FOR SHARE) and then
// its own nonce row; the dead-nonce sweep runs AFTER it in the same transaction, so this path
// takes the device row before any existing nonce row -- the revocation's order. Sweeping first
// deadlocked against a concurrent revoke and the measured victim was the REVOKE.
func (v *Verifier) IssueNonce(ctx context.Context, deviceID uuid.UUID) (IssuedNonce, error) {
	if deviceID == uuid.Nil {
		return IssuedNonce{}, ErrMalformed
	}
	dev, err := v.resolve(ctx, deviceID)
	if err != nil {
		return IssuedNonce{}, err
	}
	n, err := newNonce()
	if err != nil {
		return IssuedNonce{}, err
	}
	var expires time.Time
	err = v.data.WithTenant(ctx, dev.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, e := q.IssueAppRequestNonce(ctx, store.IssueAppRequestNonceParams{
			NonceHash:  n.hash(),
			TtlSeconds: int32(v.ttl / time.Second),
			DeviceID:   dev.ID,
			TenantID:   dev.TenantID,
		})
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrRevokedDevice
		}
		if e != nil {
			return e
		}
		expires = row.ExpiresAt
		if _, e := q.DeleteDeadAppRequestNonces(ctx, store.DeleteDeadAppRequestNoncesParams{
			TenantID: dev.TenantID, DeviceID: dev.ID,
		}); e != nil {
			return e
		}
		return nil
	})
	if errors.Is(err, ErrRevokedDevice) {
		return IssuedNonce{}, ErrRevokedDevice
	}
	if err != nil {
		return IssuedNonce{}, fmt.Errorf("appdevice: issue: %w", err)
	}
	return IssuedNonce{Nonce: n, ExpiresAt: expires}, nil
}

// Verify authenticates one signed request and returns its device. Outcomes:
//
//	(Device{...}, nil)              signed by the live device's key over an unspent nonce, which
//	                                is now spent; the employee may be deactivated (§5 row 4 is
//	                                the caller's, through the policy engine)
//	(Device{}, ErrMalformed)        the request has no valid shape (only from a zero request)
//	(Device{}, ErrUnknownDevice)    } one text, one status (401): §5 row 3's twin, NO record.
//	(Device{}, ErrRevokedDevice)    } Device is EMPTY for every refusal -- unlike session.Verify's
//	(Device{}, ErrBadSignature)     } ErrRevoked, nothing is attributable: the device id is a
//	(Device{}, ErrNonceRejected)    } claim, not a credential, until the signature verifies.
//	(Device{}, other error)         the database failed (500); never "not authenticated"
//
// WHY A REVOKED DEVICE CARRIES NOTHING (where session.ErrRevoked carries the employee): a cookie
// is itself the credential, so a revoked cookie is still proof of which browser sent it; here the
// id alone proves nothing, and the revoked branch is decided BEFORE the signature (no status
// oracle). And a revoked device never needs row 4: deactivation does not revoke.
func (v *Verifier) Verify(ctx context.Context, req SignedRequest) (Device, error) {
	r := req.get()
	if r == nil {
		return Device{}, ErrMalformed
	}
	dev, err := v.resolve(ctx, r.deviceID)
	if err != nil {
		return Device{}, err
	}
	if err := checkSignature(dev, req); err != nil {
		return Device{}, err
	}
	if err := v.spend(ctx, dev, r.nonce); err != nil {
		return Device{}, err
	}
	return Device{ID: dev.ID, TenantID: dev.TenantID, EmployeeID: dev.EmployeeID}, nil
}

// resolve is step 2: the context-less lookup, and the refusals that come before the signature.
func (v *Verifier) resolve(ctx context.Context, deviceID uuid.UUID) (db.ResolvedAppDevice, error) {
	dev, err := v.data.GetAppDeviceByID(ctx, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ResolvedAppDevice{}, ErrUnknownDevice
	}
	if err != nil {
		// Not "unknown": an outage answered as 401 would send every app user to re-activation.
		return db.ResolvedAppDevice{}, fmt.Errorf("appdevice: resolve: %w", err)
	}
	if dev.RevokedAt != nil {
		return db.ResolvedAppDevice{}, ErrRevokedDevice
	}
	return dev, nil
}

// checkSignature is step 3, pure: the stored key on the curve, the signature over this
// request's canonical text. A stored key that fails the curve check is a server-side integrity
// fault (activation checks it first), answered 500 -- fail closed, and not blamed on the caller.
func checkSignature(dev db.ResolvedAppDevice, req SignedRequest) error {
	r := req.get()
	if r == nil {
		return ErrMalformed
	}
	if dev.ID != r.deviceID {
		// The resolver is keyed by the primary key, so this cannot happen; if it ever does, the
		// canonical text would name one device and the key would be another's.
		return errors.New("appdevice: the resolver answered for another device")
	}
	pub, err := ParsePublicKey(dev.PublicKey)
	if err != nil {
		return errors.New("appdevice: a stored device key is not a P-256 point")
	}
	if !verifySignature(pub, req.canonicalText(), r.signature) {
		return ErrBadSignature
	}
	return nil
}

// spend is step 4: touch, then consume, in one transaction under the device's tenant.
func (v *Verifier) spend(ctx context.Context, dev db.ResolvedAppDevice, n Nonce) error {
	err := v.data.WithTenant(ctx, dev.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if _, e := q.TouchAppDevice(ctx, store.TouchAppDeviceParams{ID: dev.ID, TenantID: dev.TenantID}); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrRevokedDevice
			}
			return e
		}
		if _, e := q.ConsumeAppRequestNonce(ctx, store.ConsumeAppRequestNonceParams{
			TenantID: dev.TenantID, DeviceID: dev.ID, NonceHash: n.hash(),
		}); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrNonceRejected
			}
			return e
		}
		return nil
	})
	switch {
	case errors.Is(err, ErrRevokedDevice):
		return ErrRevokedDevice
	case errors.Is(err, ErrNonceRejected):
		return ErrNonceRejected
	case err != nil:
		return fmt.Errorf("appdevice: spend: %w", err)
	}
	return nil
}
