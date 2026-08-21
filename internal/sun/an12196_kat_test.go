package sun

import (
	"bytes"
	"testing"
)

// EXTERNALLY SOURCED KNOWN-ANSWER TESTS FOR THE SDM CHAIN (M2-08).
//
// 🔴 WHY THIS FILE EXISTS. Every other SUN vector in this repository was minted by
// the very code it checks: verify_mac_test.go's referenceMAC calls the production
// sv2() and truncateSDMMAC(), and test/fixtures/sun_vectors.json was generated the
// same way and says so in its own _warning. That arrangement can prove internal
// consistency and nothing else — and it did exactly that for months while sv2()
// appended the read counter in the WRONG BYTE ORDER. The suite was green the whole
// time, because the defect was present on both sides of every comparison.
//
// The expected values below come from NXP's application note AN12196, which
// publishes complete worked examples including the intermediate values. They are
// therefore the ONLY vectors here that could have failed on the day the defect was
// written — and they do fail against the old code, which is the point.
//
// 🔴 NOTHING IN THIS FILE MAY CALL referenceMAC, OR ANY OTHER HELPER THAT ROUTES
// THROUGH THE CODE UNDER TEST TO PRODUCE AN EXPECTATION. Every constant below is
// transcribed from the document. If a future change makes these tests fail, the
// change is wrong until the document says otherwise; do not "update the expected
// values" the way one would for a self-consistent golden.
//
// SECRETS (CLAUDE.md §4.7). The key material here is NOT secret and is not a Tappa
// key: it is printed in a public NXP application note as example data (one is
// all-zero, the factory default). §4.7 protects real per-tag K_sdmfileread values,
// which are random per plaque (ADR 0003 madde 3) and live only KEK-wrapped in the
// database. Publishing-a-published-document-vector is what CLAUDE.md §8 asks for
// when it requires "bilinen cevap vektörleri".
//
// SOURCE, with page numbers so the next reader can check rather than trust:
//
//	AN12196, "NTAG 424 DNA and NTAG 424 DNA TagTamper features and hints"
//	  rev. 1.8, 17 November 2020 (59 pp.) -- section numbering used below
//	  rev. 2.0,  4 March    2025 (59 pp.) -- same values, sections RENUMBERED
//
// 🔴 READ THIS BEFORE TRUSTING THE TABLE BELOW: IT IS A CONVENIENCE LIST, NOT A
// COMPLETE ONE, AND IT IS KNOWN TO BE INCOMPLETE. It used to claim "every pair this
// repository cites, measured". That claim was falsified in FOUR CONSECUTIVE ROUNDS
// (each one named below), so the sentence was DELETED from the table's own heading
// rather than repaired for a fifth time.
//
// ⚠️ AND THE DELETION ITSELF TOOK TWO ROUNDS, WHICH IS THE POINT. Round five wrote
// this withdrawal note and left the claim standing thirty-six lines below it, where
// the reader actually looks; an audit found the file contradicting itself and the
// wrong half was the visible one. A retraction that lives far from the retracted
// sentence is not a retraction. That is why the short version now sits ON the
// table's heading and this long one only explains it.
//
// 🔴 THE RENUMBERING RULE, IN FULL — AND THE PARTIAL VERSION WAS THE BUG. This
// note used to say only "sections renumbered 4.x -> 3.x". That is TRUE and
// INCOMPLETE, and the incompleteness propagated: six AN12196 citations elsewhere
// in this repo shipped with no revision at all, three of them §6.x — about which
// "4.x -> 3.x" says exactly nothing. Measured on both PDFs' tables of contents:
// rev 2.0 moved §1 "Abbreviations" to the BACK of the document (it is §12 there),
// so EVERY top-level section from §2 to §10 shifts DOWN BY ONE.
//
//	rev 1.8                              rev 2.0
//	 1 Abbreviations                     12  (moved to the back)
//	 2 Introduction                       1
//	 3 Definition of variables            2
//	 4 Secure Dynamic Messaging (SDM)     3
//	 5 Standard Secure Messaging (SSM)    4
//	 6 Personalization example            5
//	 7 Special functionalities            6
//	 8 Originality Signature Verif.       7
//	 9 System implementation concepts     8
//	10 Supporting tools                   9
//
// The numbering then RE-CONVERGES rather than staying offset: rev 2.0 inserts a
// NEW §10 ("Note about the source code in the document"), so §11 References keeps
// its number in both. An offset applied blindly past §10 is wrong.
//
// ⚠️ SUBSECTION DEPTH SURVIVES, TABLE NUMBERS DO NOT — the two must be looked up
// separately. §4.4.4.2.1 becomes §3.4.4.2.1 with the same title, but its table
// goes 5 -> 4; §6.6's table is 14 in BOTH, so the table number cannot be derived
// from the section shift either way.
//
// 🔴 WHAT THE TABLE BELOW IS: A PARTIAL LOOKUP AID, KNOWN INCOMPLETE. It is NOT a
// list of every pair this repository cites — it said that for four rounds and was
// falsified in every one of them (dated list further down). Its silence about a
// citation is not evidence. Add a row when you cite a new section; expect the next
// reader to find one you forgot.
//
//	rev 1.8                     rev 2.0                what it is
//	§4.3   Table 2  p.10        §3.3   Table 1  p.9    SDM Session Key Generation
//	§4.4.1          p.11        §3.4.1          p.10   PICCData mirror (plain SUN URL)
//	§4.4.2.1        p.11        §3.4.2.1        p.10   Encryption of PICCData
//	§4.4.4.2.1 Table 5 p.15     §3.4.4.2.1 Table 4     CMACInputOffset == CMACOffset
//	§6 (chapter) p.24           §5 (chapter) p.22      "Personalization example"
//	§6.4   Table 12 p.26        §5.4   Table 10 p.24   Get File Settings (NDEF file)
//	§6.5   Table 13 p.27        §5.5   Table 12 p.25   Get Version (UID, three frames)
//	§6.6   Table 14             §5.6   Table 14        AuthenticateEV2First, key 0x00
//	§6.8.2 Table 18 p.32        §5.8.2 Table 17 p.29   Write NDEF File, CommMode.FULL
//	§6.9   Table 19             §5.9   Table 18        Change NDEF File Settings
//	§6.10  Table 20 p.35        §5.10  Table 19 p.32   AuthenticateEV2First, key 0x03
//	§6.14  Table 24 p.39        §5.14  Table 23 p.36   AuthenticateEV2NonFirst, key 0x00
//	§6.16  Table 26/27          §5.16  Table 25/26     Changing the Key
//	§7 (chapter) p.43           §6 (chapter) p.40      "Special functionalities"
//	§7.2   Table 28 p.43        §6.2   Table 27 p.40    Enabling Random ID - RID
//	§7.3   Table 29 p.45        §6.3   Table 28 p.42   Get UID (CommMode.FULL response)
//	§10.1           p.54        §9.1            p.51   Supporting tools list
//	§3 "Definition of variables" p.6   §2 p.4          MACt() truncation notation
//
// ⚠️ SUBSECTION DEPTH IS NOT RE-LISTED. ADR 0017 cites §6.16.1 and §6.16.2, which
// the §6.16 row already covers: the depth survives the renumbering (§6.16.1 ->
// §5.16.1), only the TABLE numbers move. That is the rule stated above, applied.
//
// 🔴 THE FOUR FALSIFICATIONS, EACH DATED, BECAUSE THE PATTERN IS THE POINT:
//
//	round 1 (2026-08-20)  ADR 0017 cited §6.4 and §6.10 -- both absent
//	round 2 (2026-08-20)  ADR 0017's LOAD-BEARING measurement cites §6.14 Table 24
//	                      (the session keys behind the "one session survives a
//	                      non-zero ChangeKey" finding) -- absent. And §10.1, cited
//	                      by deploy/README.md, had been absent since before either
//	                      round.
//	round 3 (2026-08-20)  ADR 0017's correction to that same finding cites the
//	                      "Special functionalities" chapter and rev 2.0's Table 27
//	                      (the FRESH session that shows the old one did not carry
//	                      on) -- both absent. Damage was limited because both
//	                      citations carry their revision; what was stale was the
//	                      COMPLETENESS CLAIM, for the third time in one task.
//	round 4 (2026-08-20)  ADR 0017 cites §6.5 Table 13 -- the three-frame GetVersion
//	                      that its "at least ten APDU exchanges" derivation RESTS ON
//	                      -- and it too was absent. The round before had just written
//	                      "falsified twice"; by the next audit it was four.
//
//	round 5 (2026-08-21)  NOT a falsification, recorded so the maintenance history is
//	                      complete: M8-05 FAZ B2a (ev2_kat_test.go) cited §6.8.2/§5.8.2
//	                      and §7.3/§6.3 for the first time and added both rows BEFORE
//	                      the next audit could find them missing. It also measured that
//	                      section, table AND page all move independently — §6.8.2
//	                      Table 18 p.32 becomes §5.8.2 Table 17 p.29 — so the "page
//	                      does not move" shortcut that keeps suggesting itself is false.
//
// FOUR ROUNDS, FOUR MISSES, AND EACH ROUND PATCHED THE PREVIOUS ROUND'S GAP. That is
// the shape this repository names outright: a detector written to the shape of the
// last defect documents nothing about the next. So the claim is gone (see the top of
// this comment); what remains is a lookup aid that is honest about being partial.
//
// THE MECHANICAL VERSION, NAMED -- AND WHY IT IS STILL NOT WRITTEN. The fix that would
// actually hold is a test that scans every text file for AN12196 section citations and
// requires each one to appear in this table: structurally the same walk cmd/tappa's
// test-name citation check already performs, with the same ratchet shape. It is not
// written here for one reason and it is worth stating so the next reader does not
// assume neglect: the four rounds above were DOCUMENT rounds (an ADR and a runbook),
// under an explicit no-new-code constraint, and writing a scanner is a code task with
// its own design questions -- what counts as a citation, how revisions are spelled,
// what the ratchet's starting budget is. It is on the backlog. Until it lands, the
// table is maintained by hand and the honest expectation is that it will drift again.
//
// So a citation without its revision points at a DIFFERENT place — and for §6.x it
// lands in a different CHAPTER: rev 2.0 §6 is "Special functionalities", not
// "Personalization example".
//
// The AES-CMAC primitive underneath is separately anchored to the official RFC 4493
// test vectors in cmac_test.go. These tests anchor the LAYER ABOVE it: SV2's byte
// layout, the session-key derivation, the empty MAC input (ADR 0003 madde 2) and
// the odd-indexed truncation.

// --- AN12196 §4.3 Table 2 (p.10): SDM session key generation ------------------
//
// The document gives, for one read of one tag, both the counter's SV2 spelling and
// the resulting session keys. Transcribed verbatim:
//
//	step 3  UID                = 04C767F2066180
//	step 4  SDMReadCtr         = 010000   "(LSB first as per [Section 3.1])"
//	step 5  KSDMFileRead       = 5ACE7E50AB65D5D51FD5BF5A16B8205B
//	step 7  SV2                = 3CC30001008004C767F2066180010000
//	step 9  KSesSDMFileReadMAC = MAC(KSDMFileRead; SV2)
//	                           = 3A3E8110E05311F7A3FCF0D969BF2B48
const (
	katT2Key   = "5ACE7E50AB65D5D51FD5BF5A16B8205B"
	katT2UID   = "04C767F2066180"
	katT2SV2   = "3CC30001008004C767F2066180010000"
	katT2SesKe = "3A3E8110E05311F7A3FCF0D969BF2B48"
	// katT2URLCtr is the SAME read as seen from the URL side. §4.4.1 (p.11)
	// publishes the plain SUN URL for this very UID as
	//   https://ntag.nxp.com/424?uid=04C767F2066180&ctr=000001&c=54A45B2C3A558765
	// so the counter is 000001 in the URL and 010000 in SV2: one value, MSB-first
	// on the wire and LSB-first in the crypto. This pair is the entire basis for
	// the reversal in sv2() and for reading Params.Ctr big-endian.
	katT2URLCtr = "000001"
)

// TestSDMKAT_SessionKeyMatchesAN12196Table2 drives sv2() with the counter in URL
// order and requires it to reproduce the SV2 byte string NXP published, then
// requires our CMAC over it to reproduce NXP's published session key.
//
// This is the assertion the old verbatim sv2() could not satisfy: feeding 00 00 01
// straight through yields a different SV2 and therefore a different session key.
func TestSDMKAT_SessionKeyMatchesAN12196Table2(t *testing.T) {
	uid := hexBytes(t, katT2UID)
	urlCtr := hexBytes(t, katT2URLCtr)

	// Step 7 — SV2 layout, including the counter's byte order.
	gotSV2 := sv2(uid, urlCtr)
	if !bytes.Equal(gotSV2, hexBytes(t, katT2SV2)) {
		t.Fatalf("SV2 does not match AN12196 §4.3 Table 2 step 7\n got %X\nwant %s",
			gotSV2, katT2SV2)
	}

	// Step 9 — the session key derived from it.
	sk, err := cmac(hexBytes(t, katT2Key), gotSV2)
	if err != nil {
		t.Fatalf("session-key cmac: %v", err)
	}
	if !bytes.Equal(sk[:], hexBytes(t, katT2SesKe)) {
		t.Fatal("derived session key does not match AN12196 §4.3 Table 2 step 9")
	}
}

// TestSDMKAT_VerbatimCounterFailsAN12196Table2 is the negative half of the vector
// above, and it is what makes the positive half meaningful: it shows the document
// DISCRIMINATES between the two byte orders rather than being satisfied by either.
// Without this, a reader could not tell whether Table 2 actually pins the order.
func TestSDMKAT_VerbatimCounterFailsAN12196Table2(t *testing.T) {
	uid := hexBytes(t, katT2UID)
	urlCtr := hexBytes(t, katT2URLCtr)

	// The OLD behaviour, reconstructed locally: counter appended in URL order.
	verbatim := append([]byte{0x3C, 0xC3, 0x00, 0x01, 0x00, 0x80}, uid...)
	verbatim = append(verbatim, urlCtr...)

	if bytes.Equal(verbatim, hexBytes(t, katT2SV2)) {
		t.Fatal("verbatim SV2 equals the published SV2; this vector cannot " +
			"discriminate byte order and proves nothing")
	}
	sk, err := cmac(hexBytes(t, katT2Key), verbatim)
	if err != nil {
		t.Fatalf("session-key cmac: %v", err)
	}
	if bytes.Equal(sk[:], hexBytes(t, katT2SesKe)) {
		t.Fatal("verbatim counter reproduced the published session key; " +
			"the byte orders are indistinguishable here")
	}
}

// --- AN12196 §4.4.4.2.1 Table 5 (p.15): full SDMMAC, zero-length input ---------
//
// This is the vector that matters most to Tappa, because its MAC input is EMPTY —
// the "SDMMACInputOffset == SDMMACOffset" configuration, which is precisely the
// encode mode ADR 0003 madde 2 fixes for our plaques. It publishes every step:
//
//	step 1   KeySDMFileReadMAC  = 00000000000000000000000000000000
//	step 3   D(KSDMMetaRead, PICCENCData)
//	                            = C704DE5F1EACC0403D0000DA5CF60941
//	step 5   UID                = 04DE5F1EACC040   (read out of step 3's plaintext)
//	step 6   SDMReadCtr         = 3D0000           (idem; LSB first, so value 0x3D)
//	step 12  SV2                = 3CC30001008004DE5F1EACC0403D0000
//	step 13  KSesSDMFileReadMAC = 3FB5F6E3A807A03D5E3570ACE393776F
//	step 14  SDMMAC = MACt(KSesSDMFileReadMAC; zero length input)
//	                            = 94EED9EE65337086
//
// ⚠️ STEP 1's LABEL IS THE DOCUMENT'S, NOT A TYPO OF OURS — and it is confusing on
// purpose-free grounds, so read it carefully. This comment used to write it
// "KSDMFileRead"; both revisions actually print "KeySDMFileReadMAC" there (rev 1.8
// Table 5, rev 2.0 Table 4). The VALUE was and is right: it is the per-file read
// key, i.e. the same role step 13 names KSDMFileRead in "KSesSDMFileReadMAC =
// MAC(KSDMFileRead; SV2)". So the note prints two names for one value; the
// transcription follows the document, and katT5Key below feeds it in as the
// derivation key, which is what step 13 does with it.
//
// 🔴 WHAT TABLE 5 DOES **NOT** PIN: THE URL BYTE ORDER. This is the ENCRYPTED-
// PICCData example. Its UID and counter are never read off a URL — they fall out of
// DECRYPTING PICCENCData in step 3 — and the URL the note publishes for this very
// example (§4.4.2.1, p.11; rev 2.0 §3.4.2.1, p.10) is
//
//	https://ntag.nxp.com/424?e=EF963FF7828658A599F3041510671E88&c=94EED9EE65337086
//
// which carries NO plain ctr= parameter at all. Measured over pdftotext output: the
// string 00003D occurs ZERO times in rev 1.8 and ZERO times in rev 2.0. katT5URLCtr
// below is therefore OUR DERIVATION, not a transcription.
//
// (If you re-run that search with whitespace COLLAPSED you will get one apparent hit
// in rev 2.0. It is an artefact of joining two table cells: the all-zero key ends
// "…0000", the next row starts with the step number "3" and "D(KSDMMetaReadKey…".
// Not an occurrence. Search the text as laid out, not flattened.)
//
// So the URL-ORDER AXIS HAS ONE ANCHOR, NOT TWO: Table 2's SV2 counter 010000 read
// against §4.4.1's plain URL ctr=000001 for the SAME UID (third-party corroboration:
// icedevml/sdm-backend reverses the URL bytes on the plain path). An earlier draft of
// this comment claimed Table 5 nailed the byte order "a second time, independently of
// Table 2" — it does not, and the overclaim is recorded here rather than deleted so
// the next reader does not re-derive it.
//
// WHAT TABLE 5 DOES PIN, and Table 2 does not: the SV2 LAYOUT, the session-key
// derivation, the EMPTY MAC input (ADR 0003 madde 2), the odd-indexed truncation —
// and, because 3D0000 is non-palindromic, that sv2() genuinely REVERSES its input
// instead of passing it through. That last one is a property of our code, proven
// against a published SV2; it is not a second reading of the wire format.
const (
	katT5Key    = "00000000000000000000000000000000"
	katT5UID    = "04DE5F1EACC040"
	katT5SV2    = "3CC30001008004DE5F1EACC0403D0000"
	katT5SesKey = "3FB5F6E3A807A03D5E3570ACE393776F"
	katT5SDMMAC = "94EED9EE65337086"
	// katT5URLCtr is DERIVED, NOT TRANSCRIBED — the only value in this file that
	// is. The document never prints it (0 hits in both revisions, see above): it is
	// the byte-reverse of step 6's LSB-first 3D0000, i.e. the counter as a PLAIN-SUN
	// URL would carry it. The MSB-first spelling it assumes is anchored by Table 2 +
	// §4.4.1, not by Table 5. Feeding it to sv2() therefore tests that sv2 reverses;
	// it does not independently re-test what the wire format is.
	katT5URLCtr = "00003D"
)

// TestSDMKAT_FullChainMatchesAN12196Table5 walks the whole algorithm against the
// document, asserting EACH published intermediate separately so a failure says
// which step broke rather than only that the end differs.
func TestSDMKAT_FullChainMatchesAN12196Table5(t *testing.T) {
	key := hexBytes(t, katT5Key)
	uid := hexBytes(t, katT5UID)
	urlCtr := hexBytes(t, katT5URLCtr)

	// Step 12 — SV2 bytes.
	gotSV2 := sv2(uid, urlCtr)
	if !bytes.Equal(gotSV2, hexBytes(t, katT5SV2)) {
		t.Fatalf("SV2 does not match AN12196 §4.4.4.2.1 step 12\n got %X\nwant %s",
			gotSV2, katT5SV2)
	}

	// Step 13 — session key.
	sk, err := cmac(key, gotSV2)
	if err != nil {
		t.Fatalf("session-key cmac: %v", err)
	}
	if !bytes.Equal(sk[:], hexBytes(t, katT5SesKey)) {
		t.Fatal("session key does not match AN12196 §4.4.4.2.1 step 13")
	}

	// Step 14a — the CMAC is taken over a ZERO-LENGTH message (ADR 0003 madde 2).
	full, err := cmac(sk[:], nil)
	if err != nil {
		t.Fatalf("sdm mac cmac: %v", err)
	}

	// Step 14b — truncation to the 8 odd-indexed bytes. AN12196's own notation for
	// this (rev 1.8 §3, "Definition of variables used in examples", p.6; rev 2.0
	// renumbers the same section to §2, p.4) is
	//   MACt(K,M) ... truncated to 8 bytes, using S14||S12||S10||S8||S6||S4||S2||S0
	// which is the same selection counted from the other end.
	got := truncateSDMMAC(full)
	if !bytes.Equal(got[:], hexBytes(t, katT5SDMMAC)) {
		t.Fatalf("truncated SDMMAC does not match AN12196 §4.4.4.2.1 step 14\n got %X\nwant %s",
			got, katT5SDMMAC)
	}
}

// TestSDMKAT_VerifyMACAcceptsAN12196Table5 is the end-to-end one: the PRODUCTION
// entry point is handed a key, a UID, a URL-order counter and a CMAC that all came
// out of NXP's document, and must accept them. Nothing in this call was computed by
// Tappa.
func TestSDMKAT_VerifyMACAcceptsAN12196Table5(t *testing.T) {
	ok, err := verifyMAC(
		hexBytes(t, katT5Key),
		hexBytes(t, katT5UID),
		hexBytes(t, katT5URLCtr),
		hexBytes(t, katT5SDMMAC),
	)
	if err != nil {
		t.Fatalf("verifyMAC error: %v", err)
	}
	if !ok {
		t.Fatal("verifyMAC rejected AN12196's own published SDMMAC — the chain " +
			"does not match the chip")
	}
}

// TestSDMKAT_VerifyMACRejectsWrongCounterOrder proves the end-to-end acceptance
// above is not accidental: the same document vector with the counter handed over in
// the document's LSB-first spelling (i.e. NOT reversed on the way in, the old
// behaviour) must be refused.
func TestSDMKAT_VerifyMACRejectsWrongCounterOrder(t *testing.T) {
	ok, err := verifyMAC(
		hexBytes(t, katT5Key),
		hexBytes(t, katT5UID),
		hexBytes(t, "3D0000"), // already LSB-first; sv2 will reverse it again
		hexBytes(t, katT5SDMMAC),
	)
	if err != nil {
		t.Fatalf("verifyMAC error: %v", err)
	}
	if ok {
		t.Fatal("verifyMAC accepted a double-reversed counter; the reversal is " +
			"not actually being applied")
	}
}

// TestSDMKAT_TruncationSelectsOddIndices checks the truncation choice against the
// document ALONE, without the surrounding chain, so that a truncation regression
// is reported as a truncation regression. The wrong slices tried here are the three
// that AN12196 and the tappa-sun skill both warn about; each must differ from the
// published SDMMAC.
func TestSDMKAT_TruncationSelectsOddIndices(t *testing.T) {
	sk := hexBytes(t, katT5SesKey)
	full, err := cmac(sk, nil)
	if err != nil {
		t.Fatalf("cmac: %v", err)
	}
	want := hexBytes(t, katT5SDMMAC)

	odd := truncateSDMMAC(full)
	if !bytes.Equal(odd[:], want) {
		t.Fatalf("odd-indexed truncation != published SDMMAC\n got %X\nwant %s", odd, katT5SDMMAC)
	}

	even := make([]byte, 8)
	for i := range even {
		even[i] = full[2*i]
	}
	// The field is `sel` (a byte SELECTION), not `mac`: scripts/redline-check.sh
	// R7 warns on bytes.Equal over anything named like a MAC, and it is right to —
	// a constant-time compare belongs on any real MAC path. This is a test asserting
	// two slices DIFFER, so the warning would be noise, and a tolerated noisy warning
	// is how a net stops being read (the script's own R7 comment makes this argument).
	for _, wrong := range []struct {
		name string
		sel  []byte
	}{
		{"even_indexed", even},
		{"first_8", full[0:8]},
		{"last_8", full[8:16]},
	} {
		t.Run(wrong.name, func(t *testing.T) {
			if bytes.Equal(wrong.sel, want) {
				t.Fatalf("%s truncation also equals the published SDMMAC; this "+
					"vector cannot discriminate truncation", wrong.name)
			}
		})
	}
}

// --- What these vectors DO NOT close -----------------------------------------
//
// 🔴 §4.4.1's PLAIN-URL EXAMPLE IS NOT CLOSED, and saying so is the honest report.
// That example (p.11) publishes
//
//	https://ntag.nxp.com/424?uid=04C767F2066180&ctr=000001&c=54A45B2C3A558765
//
// and its c= value could NOT be reproduced here. The document does not state which
// key that URL was produced under — §4.3 Table 2 shares the URL's UID but is
// presented as a session-key example, not as the source of this URL — so the
// following were tried and all differed from 54A45B2C3A558765:
//
//	key = Table 2's KSDMFileRead, counter reversed into SV2 (010000)
//	key = Table 2's KSDMFileRead, counter verbatim in SV2   (000001)
//	key = all-zero factory default, counter reversed        (010000)
//	key = all-zero factory default, counter verbatim        (000001)
//	key = all-zero, reversed SV2, MAC input = UID || ctr    (non-empty input)
//	key = all-zero, reversed SV2, MAC input = UID           (non-empty input)
//
// Six hypotheses, six mismatches. The most likely explanation is simply that the
// URL was generated with a key the note never prints, but that is a HYPOTHESIS and
// is recorded as one. What §4.4.1 IS used for above is only its counter TEXT
// (ctr=000001 against Table 2's 010000), which needs no key to read.
//
// 🔴 AND NO REAL SILICON HAS BEEN SEEN. These vectors anchor Tappa's chain to NXP's
// published description of the chip. A physical NTAG 424 DNA plaque, encoded by our
// own M8-05 runbook and tapped, remains the only thing that can prove the ENCODE
// side agrees too — in particular that our encoder mirrors the counter into the URL
// MSB-first, which is a choice ADR 0003 §1 makes and which these vectors assume
// rather than verify. Do not read a green run here as "the chip works".
