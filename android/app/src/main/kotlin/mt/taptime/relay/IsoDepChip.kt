package mt.taptime.relay

import android.nfc.Tag
import android.nfc.TagLostException
import android.nfc.tech.IsoDep
import java.io.IOException

/**
 * The chip over android.nfc.tech.IsoDep — "Send raw ISO-DEP data to the tag and
 * receive the response" (deploy/README.md, "B — kendi Android uygulamamız").
 *
 * The connection stays OPEN for the whole round. Android puts no lifetime on an
 * ISO-DEP session: it lives until the tag leaves the field or close() is called,
 * which is the property the README calls the decisive advantage over iOS. A round
 * is eleven exchanges with an HTTPS turn between each, and the chip's authentication
 * state must survive all of them.
 */
class IsoDepChip private constructor(private val dep: IsoDep) : Chip, AutoCloseable {

    val maxTransceiveLength: Int get() = dep.maxTransceiveLength

    override fun transceive(command: ByteArray): ByteArray {
        val limit = dep.maxTransceiveLength
        if (command.size > limit) {
            // Every NTAG 424 DNA command is under 255 bytes (README), so this is a
            // phone we did not expect rather than a plaque; say so instead of letting
            // the stack truncate.
            throw IOException("a ${command.size}-byte command exceeds this phone's ${limit}-byte transceive limit")
        }
        return try {
            dep.transceive(command)
        } catch (e: TagLostException) {
            throw ChipLostException(e.message ?: "tag lost")
        }
    }

    override fun close() {
        try {
            dep.close()
        } catch (e: IOException) {
            // Closing a connection the tag already dropped; nothing to do.
        }
    }

    companion object {
        /**
         * Per-transceive timeout. Not a measurement — deploy/README.md gives no number
         * for the chip's slow steps (the authentication handshake and the command that
         * installs the plaque's secret), only that they ask for frame waiting-time
         * extensions (WTX), which the NFC controller honours below this layer. 5000 ms is the server's own per-exchange budget
         * (internal/encode, exchangeBudget = 5 s) applied to the chip side, and far
         * above the default the stack derives from the tag's FWI. FAZ B3's hardware
         * half measures the real figure; until then this is generous on purpose.
         */
        const val TRANSCEIVE_TIMEOUT_MS = 5_000

        @Throws(IOException::class)
        fun open(tag: Tag): IsoDepChip {
            val dep = IsoDep.get(tag) ?: throw IOException("not an ISO-DEP tag")
            dep.connect()
            dep.timeout = TRANSCEIVE_TIMEOUT_MS
            return IsoDepChip(dep)
        }
    }
}
