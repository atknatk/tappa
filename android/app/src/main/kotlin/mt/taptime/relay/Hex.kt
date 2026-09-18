package mt.taptime.relay

/**
 * Hex is the ONE encoding on this wire: the server hands out C-APDUs as hex and takes
 * R-APDUs back as hex (internal/handler/plaqueencode.go, writeEncodeReply). Both
 * directions live here so a second spelling cannot drift.
 */
object Hex {
    private const val DIGITS = "0123456789abcdef"

    fun encode(bytes: ByteArray): String {
        val out = StringBuilder(bytes.size * 2)
        for (b in bytes) {
            val v = b.toInt() and 0xff
            out.append(DIGITS[v ushr 4]).append(DIGITS[v and 0x0f])
        }
        return out.toString()
    }

    /**
     * Decodes ASCII hex, either case; throws on an odd length or any other character.
     *
     * ASCII ONLY, deliberately: the first version used Character.digit(c, 16), which
     * also accepts Unicode fullwidth digits (U+FF10..) and turns them into bytes
     * (second-round audit, N9). The server's encoding/hex emits ASCII and nothing
     * else, so anything wider is a body this client should refuse, not repair.
     */
    fun decode(text: String): ByteArray {
        require(text.length % 2 == 0) { "hex text has an odd length (${text.length})" }
        val out = ByteArray(text.length / 2)
        var i = 0
        while (i < text.length) {
            val hi = nibble(text[i])
            val lo = nibble(text[i + 1])
            require(hi >= 0 && lo >= 0) { "not a hex character at offset $i" }
            out[i / 2] = ((hi shl 4) or lo).toByte()
            i += 2
        }
        return out
    }

    private fun nibble(c: Char): Int = when (c) {
        in '0'..'9' -> c - '0'
        in 'a'..'f' -> c - 'a' + 10
        in 'A'..'F' -> c - 'A' + 10
        else -> -1
    }
}
