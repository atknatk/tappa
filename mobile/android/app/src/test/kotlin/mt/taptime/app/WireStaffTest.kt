package mt.taptime.app

import mt.taptime.app.wire.Replies
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

// M16-43/45: the /me additions and the claims list decode from the server's own shapes
// (internal/handler/app_test.go's "me (at work)" and "claims" cases), and an old server's /me
// without them still decodes.
class WireStaffTest {
    private fun obj(s: String) = MiniJson.decodeObject(s)

    @Test
    fun meCarriesPresenceShiftAndTheMissingCount() {
        val me = Replies.me(obj("""{"employeeName":"E","brand":{},"direction":"out","presence":{"inSince":"2026-11-23T08:00:00Z","inSinceLocal":"09:00",""" +
            """"shiftEndsAt":"2026-11-23T16:00:00Z","shiftEndsAtLocal":"17:00","remindAt":"2026-11-23T16:30:00Z",""" +
            """"showUntil":"2026-11-23T18:30:00Z","staleAt":"2026-11-24T02:00:00Z"},""" +
            """"shift":{"start":"09:00","end":"17:00","overnight":false,"venue":"V"},"missingTimes":2}"""))
        val p = me.presence!!
        assertEquals(java.time.Instant.parse("2026-11-23T08:00:00Z").toEpochMilli(), p.inSinceMs)
        assertEquals("17:00", p.shiftEndsAtLocal)
        assertEquals(java.time.Instant.parse("2026-11-23T16:30:00Z").toEpochMilli(), p.remindAtMs)
        assertEquals("V", me.shift!!.venue)
        assertEquals(2, me.missingTimes)
    }

    @Test
    fun anOldServersMeStillDecodes() {
        val me = Replies.me(obj("""{"employeeName":"E","brand":{},"direction":""}"""))
        assertNull(me.presence)
        assertNull(me.shift)
        assertEquals(0, me.missingTimes)
    }

    @Test
    fun theClaimsListDecodes() {
        val r = Replies.claims(obj("""{"open":[{"context":"C","kind":"out","anchorDate":"2026-11-22","anchorAt":"09:12","day":"yesterday",""" +
            """"windowFrom":{"at":"09:12","inclusive":false},"windowTo":{"at":"05:00","inclusive":false},"prefill":null}],""" +
            """"sent":[{"kind":"out","anchorDate":"2026-11-20","anchorAt":"10:00","claimedAt":"18:00","state":"pending","approvedAt":null}]}"""))
        assertEquals("C", r.open.single().context)
        assertEquals("yesterday", r.open.single().day)
        assertNull(r.open.single().prefill)
        assertEquals("pending", r.sent.single().state)
        assertEquals("still_open", Replies.claimSend(obj("""{"result":"still_open"}""")))
    }
}
