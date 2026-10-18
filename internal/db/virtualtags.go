package db

import (
	"github.com/jackc/pgx/v5"
)

// THE SANDBOX'S PLAQUE RANGE (M11 API-11, ADR 0028 §11: "sandbox minter'ı ve çözümleyicisi yalnız
// ayrılmış, NXP olmayan bir sanal UID aralığını kabul eder ... kontrol her çözümlemede ve her
// eklemede koşar"). A sandbox has no wall and no chip: its plaques are VIRTUAL, minted by
// internal/sandbox, and their UIDs come from a range no NXP chip carries -- a real NTAG 424 DNA UID
// starts with the manufacturer byte 0x04 (ISO/IEC 7816-6), a virtual one with VirtualTagUIDPrefix.
//
// The rule is an ALLOW-LIST (the prefix), not "not 0x04": a UID of any other manufacturer is refused
// as well, and the day a sandbox meets a real chip of any make it answers "unknown plaque".
//
// WHERE IT RUNS: on a pool whose configuration is TAPPA_ENV=sandbox (New reads it -- nothing to
// wire, nothing to forget), at the two doors a plaque has --
//
//	resolve  GetTagByUID, the context-less resolver EVERY tap path takes first (the tap page's SUN
//	         pre-check, the tap's re-resolve in checkin.Record, the simulation): a UID outside the
//	         range is refused BEFORE the query, as ErrRealPlaqueRefused, which reads as pgx.ErrNoRows
//	         -- an unknown plaque, the answer each caller already has (§5: no tenant, nothing to
//	         record against). It does not rely on encoded_at: a real plaque without the stamp is a
//	         legitimate state, so the stamp proves nothing.
//	insert   AdmitTagUID, which internal/encode's InsertUnassigned and internal/sandbox's minter call
//	         before their INSERT -- the only two writers of a tags row in this repository
//	         (cmd/tappa's TestSandbox_RealNXPUIDRefusedOnEveryResolveAndInsert counts them).
//
// Every other environment is untouched: production plaques ARE NXP chips.

// VirtualTagUIDPrefix is the first two bytes of every virtual plaque's 7-byte UID, in the column's
// canonical upper-case hex (00013's tags_uid_canonical_hex): 0x00 is no NXP byte (0x04), and 0x53
// is 'S'.
const VirtualTagUIDPrefix = "0053"

// IsVirtualTagUID reports whether uid is a virtual plaque's: 14 upper-case hex digits starting with
// VirtualTagUIDPrefix. Anything else -- a real NXP UID (04...), another spelling, another length --
// is not.
func IsVirtualTagUID(uid string) bool {
	if len(uid) != 14 || uid[:len(VirtualTagUIDPrefix)] != VirtualTagUIDPrefix {
		return false
	}
	for i := 0; i < len(uid); i++ {
		if (uid[i] < '0' || uid[i] > '9') && (uid[i] < 'A' || uid[i] > 'F') {
			return false
		}
	}
	return true
}

// realPlaqueRefused is ErrRealPlaqueRefused's type: it IS pgx.ErrNoRows for errors.Is, so every
// caller that maps "no such plaque" maps this too, and it stays distinguishable for a test or a log.
type realPlaqueRefused struct{}

func (realPlaqueRefused) Error() string {
	return "db: this sandbox accepts only virtual plaques; the uid is outside the virtual range (ADR 0028 §11)"
}

func (realPlaqueRefused) Is(target error) bool { return target == pgx.ErrNoRows }

// ErrRealPlaqueRefused: a sandbox pool was asked to resolve or admit a UID outside the virtual range.
// errors.Is(err, pgx.ErrNoRows) holds for it.
var ErrRealPlaqueRefused error = realPlaqueRefused{}

// AdmitTagUID is the insert door's check: nil on every pool but a sandbox's, and there nil only for a
// virtual UID (IsVirtualTagUID). A writer of a tags row calls it before its INSERT.
func (d *DB) AdmitTagUID(uid string) error {
	if d.virtualTagsOnly && !IsVirtualTagUID(uid) {
		return ErrRealPlaqueRefused
	}
	return nil
}
