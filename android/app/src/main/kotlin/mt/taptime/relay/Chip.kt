package mt.taptime.relay

import java.io.IOException

/**
 * The chip, as the relay loop sees it: one call sends a C-APDU and returns the whole
 * R-APDU (data followed by SW1 SW2). The loop never looks inside either — the bytes
 * are the server's to build and to read (ADR 0017 §2.1).
 *
 * Declared here, at the consumer (CLAUDE.md §7). The Android implementation wraps
 * android.nfc.tech.IsoDep; the tests drive the loop with a scripted chip.
 */
interface Chip {
    @Throws(IOException::class)
    fun transceive(command: ByteArray): ByteArray
}

/**
 * The chip left the field mid-round. Mirrors android.nfc.TagLostException without
 * importing it, so the loop and its tests stay off the Android class path.
 */
class ChipLostException(message: String) : IOException(message)
