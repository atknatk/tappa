package operatorauth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// The operator's password (ADR 0020 §1): bcrypt at cost 12, at least 14 runes, at
// most 72 bytes, no composition rule (ADR 0019's reasoning applies unchanged; the
// TENANT floor stays 8 -- this is a separate identity with a separate rule).

const (
	// Cost is the bcrypt work factor of every digest this package makes -- the same 12
	// as internal/adminauth.Cost and the FLOOR of 00026's CHECK
	// (`\$2[aby]\$1[2-4]\$...`): a weaker digest cannot even be stored. The CEILING of
	// that CHECK is 14, 00018's denial-of-service bound.
	//
	// 🔴 THE DUMMY IS MADE AT THIS SAME COST, and that equality is the whole of ADR 0020
	// §3's "same time": an unknown address and a wrong password each pay exactly one
	// comparison at this cost (TestPassword_EveryArmPaysOneComparisonAtTheSameCost).
	// Raising it means raising 00026's CHECK in the same change, or every enrollment
	// is refused (the digest would not fit the column).
	Cost = 12

	// MinPasswordRunes is ADR 0020 §1's floor (K10): 14 characters, counted as runes
	// so a passphrase in any script is measured the way a person counts it.
	MinPasswordRunes = 14

	// MaxPasswordBytes is bcrypt's own input limit. internal/adminauth measured why it
	// is enforced rather than trusted: the GENERATOR refuses a longer input but the
	// COMPARER silently truncates, so two passwords sharing a 72-byte prefix
	// authenticate each other. Refused when SET (checkPasswordPolicy) and never a
	// match when PRESENTED (compare).
	MaxPasswordBytes = 72
)

// checkPasswordPolicy is the rule a NEW password must meet. It is applied when a
// password is set (enrollment) and never at sign-in: telling a sign-in form "that is
// too short" would answer a question ADR 0020 §3 says every failure answers the same
// way.
//
// Invalid UTF-8 is refused because the rune count of such a string is not a count
// anybody typed.
func checkPasswordPolicy(password string) error {
	if !utf8.ValidString(password) ||
		utf8.RuneCountInString(password) < MinPasswordRunes ||
		len(password) > MaxPasswordBytes {
		return ErrWeakPassword
	}
	return nil
}

// hashPassword is the ONE place an operator digest is made. It re-applies the policy
// so no caller can store a digest of a password the policy refuses.
func hashPassword(password string) (string, error) {
	if err := checkPasswordPolicy(password); err != nil {
		return "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), Cost)
	if err != nil {
		// bcrypt's error names a length at most (internal/adminauth measured it); it is
		// replaced rather than wrapped so no future x/crypto message can carry more.
		return "", errors.New("operatorauth: the digest could not be made")
	}
	return string(h), nil
}

// newDummyDigest makes the digest an unknown address is compared against: a real
// bcrypt at Cost over 32 random bytes nobody keeps.
//
// WHY AT RUN TIME AND NOT A LITERAL (internal/adminauth keeps a literal). Two reasons,
// both about the thing the dummy exists for: the cost is then Cost BY CONSTRUCTION
// rather than by a pinned string somebody has to regenerate when Cost moves, and no
// digest-shaped string enters a public repository to be classified by the secret scan.
// The price is one bcrypt when the Authenticator is built.
func newDummyDigest() ([]byte, error) {
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, errors.New("operatorauth: read randomness for the dummy digest failed")
	}
	pw := []byte(base64.RawURLEncoding.EncodeToString(seed[:]))
	clear(seed[:])
	d, err := bcrypt.GenerateFromPassword(pw, Cost)
	clear(pw)
	if err != nil {
		return nil, errors.New("operatorauth: the dummy digest could not be made")
	}
	return d, nil
}

// compare reports whether password matches the stored digest. ONE bcrypt comparison
// on every path -- the property every arm of the password step shares.
//
// An over-long password pays a full comparison against THIS digest (so the time is
// the time of this row) and is then refused whatever that comparison said: bcrypt's
// comparer truncates at 72 bytes, and a match there is the silent truncation
// internal/adminauth measured and refuses (Q03).
func (a *Authenticator) compare(digest, password string) bool {
	if len(password) > MaxPasswordBytes {
		_ = a.compareFn([]byte(digest), []byte(password[:MaxPasswordBytes]))
		return false
	}
	return a.compareFn([]byte(digest), []byte(password)) == nil
}

// compareDummy is the unknown-address arm: a real comparison against the dummy
// digest, result discarded, always false.
//
// The input is clamped to MaxPasswordBytes as compare clamps it, so an over-long
// password makes both arms hash EXACTLY the same input. That is a defence, not today's
// necessity (OP-6 verification, 10th round, the 9th auditor read it and it was
// re-measured): golang.org/x/crypto v0.54.0's CompareHashAndPassword has NO length
// exit -- only GenerateFromPassword refuses more than 72 bytes (ErrPasswordTooLong) --
// and the key schedule's cost does not depend on the key's length (cost 12: 196-212 ms
// for 8, 72, 73, 200 and 4096 bytes alike). The first version of this comment named an
// "early length exit" that does not exist; the clamp stays so that a future comparer
// that did shortcut long input could not open a difference between the arms. A mutant
// deleting it is therefore EQUIVALENT today, by name (internal/adminauth's
// clampForDummy carries the same old premise and is not this package's to change).
func (a *Authenticator) compareDummy(password string) {
	if len(password) > MaxPasswordBytes {
		password = password[:MaxPasswordBytes]
	}
	_ = a.compareFn(a.keys.dummyDigest.bytes(), []byte(password))
}
