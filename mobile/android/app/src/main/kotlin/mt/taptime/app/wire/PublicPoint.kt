package mt.taptime.app.wire

// The device's public half on the wire: the RAW uncompressed P-256 point 04 || X || Y, 65
// bytes (00108's stored form; the body's "publicKey" is its unpadded base64url). Android's
// Keystore hands the public half over as X.509 SubjectPublicKeyInfo DER, which for a P-256
// point is a FIXED 26-byte header followed by the point -- so the conversion is a prefix
// check and a cut, not a DER parser (the vectors' "spki" entry is key A in that wrapper).
object PublicPoint {
    const val LEN = 65

    // SEQUENCE { SEQUENCE { OID id-ecPublicKey, OID prime256v1 }, BIT STRING (0 unused bits) }
    private val SPKI_HEADER = byteArrayOf(
        0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86.toByte(), 0x48, 0xce.toByte(), 0x3d, 0x02, 0x01,
        0x06, 0x08, 0x2a, 0x86.toByte(), 0x48, 0xce.toByte(), 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
    )

    /** The point inside a P-256 SubjectPublicKeyInfo, or null for anything else (another curve, a compressed point). */
    fun fromSpki(spki: ByteArray): ByteArray? {
        if (spki.size != SPKI_HEADER.size + LEN) return null
        for (i in SPKI_HEADER.indices) if (spki[i] != SPKI_HEADER[i]) return null
        val point = spki.copyOfRange(SPKI_HEADER.size, spki.size)
        return if (point[0] == 0x04.toByte()) point else null
    }

    /** The SubjectPublicKeyInfo of a raw point -- for a verifier on this side (tests); the app sends the point. */
    fun toSpki(point: ByteArray): ByteArray {
        require(point.size == LEN && point[0] == 0x04.toByte()) { "not an uncompressed P-256 point" }
        return SPKI_HEADER + point
    }
}
