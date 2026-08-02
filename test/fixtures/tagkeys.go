package fixtures

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// The DEMO plaques' per-tag AES keys, and why they are DERIVED rather than
// written down.
//
// tags.aes_key_ref must hold a KEK-WRAPPED blob (ADR 0003 madde 4, CLAUDE.md
// §4.7): nonce(12) || ciphertext(16) || gcm_tag(16) = 44 bytes, sealed under the
// operator's TAPPA_TAG_KEK with the raw 7-byte UID as GCM AAD. That value CANNOT
// be a literal in seed.sql for two independent reasons:
//
//   - it depends on the operator's KEK, which lives in the environment and never
//     in the repo (§4.7), and
//   - sun.Wrap draws a fresh random nonce on every call, so there is no single
//     "the" wrapped value even for one KEK.
//
// So the seed carries the RECIPE, not the secret: the plaintext per-tag key is
// derived from a LOUD, PUBLIC label plus the tag's own UID, and the wrapping
// happens at seed time in test/fixtures/seedkeys (driven by scripts/seed.sh).
// Anybody who reads this file can recompute every demo key, which is exactly the
// point — a value everyone can recompute is self-evidently NOT a secret, and no
// real NTAG key ever enters the repository. Tests recompute the same key to
// assert that a seeded plaque really does open under the configured KEK.
//
// ⚠️ NEVER use this for a real plaque. A real per-tag K_sdmfileread is 16 random
// bytes generated at encoding time (ADR 0003 madde 3) and is known only to the
// chip and the KEK-wrapped column.

// SeedTagKeyLabel is the domain-separation prefix of the derivation below. It is
// deliberately a sentence rather than an opaque constant: a hex dump of a derived
// key tells you nothing, but the label in the source does, and the whole design
// depends on a reader being able to see at a glance that these keys are fake.
const SeedTagKeyLabel = "tappa-fake-seed-tag-key-do-not-use|"

// SeedTagKey returns the 16-byte AES-128 FAKE per-tag key for one demo plaque:
//
//	SHA-256(SeedTagKeyLabel || <uppercase 14-hex uid>)[:16]
//
// Per-tag distinct (the UID is in the input), deterministic (a test can
// recompute it), and truncated to 16 bytes because that is the AES-128
// SDMFileRead key size. The uid is upper-cased first so a caller that passes the
// lowercase form still derives the key the seed wrote.
func SeedTagKey(uid string) []byte {
	sum := sha256.Sum256([]byte(SeedTagKeyLabel + strings.ToUpper(uid)))
	return sum[:16]
}

// SeedTagUIDBytes decodes a 14-hex tag UID into the RAW 7 bytes that sun.Wrap and
// sun.Unwrap authenticate as GCM AAD. It errors rather than panicking so the seed
// tool can report which fixture is malformed (§7).
func SeedTagUIDBytes(uid string) ([]byte, error) {
	b, err := hex.DecodeString(uid)
	if err != nil {
		return nil, fmt.Errorf("fixtures: tag uid %q is not hex: %w", uid, err)
	}
	if len(b) != 7 {
		return nil, fmt.Errorf("fixtures: tag uid %q decodes to %d bytes, want 7", uid, len(b))
	}
	return b, nil
}

// SeedTag pairs a demo plaque with the tenant that owns it. The tenant travels
// with the uid so the seed's UPDATE can carry an explicit tenant_id filter
// alongside the primary key — §4.5's belt-and-braces, applied to fixture tooling
// too rather than only to production queries.
type SeedTag struct {
	UID      string
	TenantID uuid.UUID
}

// SeedTags is every plaque test/fixtures/seed.sql inserts, in that file's order.
// It MUST stay in sync with the tags section of seed.sql; the seed step verifies
// mechanically that nothing was missed (it fails if any row is left without a
// 44-byte wrapped key), so drift here becomes a loud seed failure rather than a
// plaque that 500s on the first tap.
var SeedTags = []SeedTag{
	{TagKFHamrun, TenantKF},
	{TagKFHamrunLost, TenantKF},
	{TagKFMellieha, TenantKF},
	{TagKFMsida, TenantKF},
	{TagKFValletta, TenantKF},
	{TagKFSanGwann, TenantKF},
	{TagKFStJulians, TenantKF},
	{TagKFStJuliansOld, TenantKF},
	{TagKFPaceville, TenantKF},
	{TagKFRustyBar, TenantKF},
	{TagKFHeadquarter, TenantKF},
	{TagKMPlant, TenantKM},
}
