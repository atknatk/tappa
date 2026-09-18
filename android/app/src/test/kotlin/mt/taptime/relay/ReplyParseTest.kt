package mt.taptime.relay

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** Reply.parse against the endpoint's shapes, and against the shapes it must refuse to guess about. */
class ReplyParseTest {
    private val json = "application/json; charset=utf-8"

    @Test
    fun aProgressBody_parsesAllFourFields() {
        val r = Reply.parse(200, json, """{"session":"abc","command":"00A4040C07D2760000850101","step":"begin","done":false}""")
        assertTrue(r is Reply.Progress)
        r as Reply.Progress
        assertEquals("abc", r.session)
        assertEquals("begin", r.step)
        assertEquals(false, r.done)
        assertArrayEquals(Hex.decode("00a4040c07d2760000850101"), r.command)
        assertEquals(null, r.trailingFault)
    }

    /** RED IF: `fault` or the status is consulted before `done`. */
    @Test
    fun done_isReadBeforeFaultAndBeforeStatus() {
        val withFault = Reply.parse(200, json, """{"session":"abc","command":"","step":"changefilesettings","done":true,"fault":"server-error"}""")
        assertTrue(withFault is Reply.Progress)
        assertEquals(true, (withFault as Reply.Progress).done)
        assertEquals("server-error", withFault.trailingFault)

        val with500 = Reply.parse(500, json, """{"session":"abc","command":"","step":"changefilesettings","done":true}""")
        assertTrue(with500 is Reply.Progress)
        assertEquals(true, (with500 as Reply.Progress).done)
    }

    @Test
    fun aFaultBody_carriesTheWordAndTheStatus() {
        assertEquals(Reply.Fault(409, "busy"), Reply.parse(409, json, """{"fault":"busy"}"""))
        assertEquals(Reply.Fault(422, "refused"), Reply.parse(422, json, """{"fault":"refused"}"""))
    }

    /** Non-JSON answers are the chain's: the status is all that is read. RED IF: the body is parsed. */
    @Test
    fun nonJson_isRefusedByStatusOnly() {
        assertEquals(Reply.Refused(303), Reply.parse(303, "text/html; charset=utf-8", "<html>sign in</html>"))
        assertEquals(Reply.Refused(429), Reply.parse(429, "text/html", """{"fault":"looks like json but is not declared as such"}"""))
        assertEquals(Reply.Refused(200), Reply.parse(200, null, """{"session":"x","command":"","step":"","done":true}"""))
    }

    @Test
    fun shapesThisClientDoesNotUnderstand_areMalformed() {
        assertTrue(Reply.parse(200, json, "not json") is Reply.Malformed)
        assertTrue(Reply.parse(200, json, """{"hello":"world"}""") is Reply.Malformed)
        assertTrue(Reply.parse(200, json, """{"session":"x","command":"zz","step":"begin","done":false}""") is Reply.Malformed)
        assertTrue(Reply.parse(200, json, """{"session":"x","command":"abc","step":"begin","done":false}""") is Reply.Malformed)
        assertTrue(Reply.parse(500, json, """{"session":"x","command":"00","step":"begin","done":false}""") is Reply.Malformed)
        assertTrue(Reply.parse(400, json, """{"fault":""}""") is Reply.Malformed)
    }

    @Test
    fun hex_roundTripsAndRefusesGarbage() {
        val bytes = byteArrayOf(0x00, 0x7f, 0x80.toByte(), 0xff.toByte())
        assertEquals("007f80ff", Hex.encode(bytes))
        assertArrayEquals(bytes, Hex.decode("007F80FF"))
        // "\uFF10\uFF11" are FULLWIDTH DIGIT ZERO / ONE: Character.digit accepts them,
        // the ASCII-only decoder must not (second-round audit, N9).
        for (bad in listOf("0", "0g", "zz", "\uFF10\uFF11", "0\u0660")) {
            try {
                Hex.decode(bad)
                throw AssertionError("decoded '$bad'")
            } catch (e: IllegalArgumentException) {
                // refused
            }
        }
    }
}
