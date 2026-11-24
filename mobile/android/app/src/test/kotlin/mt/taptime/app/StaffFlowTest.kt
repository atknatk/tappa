package mt.taptime.app

import java.util.UUID
import mt.taptime.app.activation.ActiveDevice
import mt.taptime.app.activation.KeyBacking
import mt.taptime.app.staff.StaffFlow
import mt.taptime.app.staff.StaffFlow.Answer
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.HoursReply
import mt.taptime.app.wire.HttpUrlTransport
import mt.taptime.app.wire.ServerClock
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * My hours over a socket: StaffFlow -> AppClient -> HttpUrlTransport -> FakeAppServer. The server
 * binds a cursor to the person and the week (apphours.go ownCursorBound) and answers 400 for any
 * other; what is measured is the APP: a refused "Show more" reads page one of the same week again,
 * once, and says it was a restart, so the screen replaces its list instead of showing "can't reach".
 */
class StaffFlowTest {
    private val server = FakeAppServer("unused-code")
    private val keys = JdkKeys()
    private val state = MemoryState()
    private val flow = StaffFlow(AppClient(HttpUrlTransport(server.base), ServerClock(), MiniJson), keys, state)
    private val week = "2026-11-16"

    @After
    fun stop() = server.close()

    private fun activated() {
        val key = keys.generate()
        val id = UUID.randomUUID().toString()
        server.devices[id] = key.signer.publicPoint
        state.device = ActiveDevice(id, key.alias, KeyBacking.TEE, "Maria Borg")
    }

    private fun page(date: String, next: String?): String {
        val cursor = next?.let { "\"$it\"" } ?: "null"
        val days = (16..22).joinToString(",") { d -> """{"date":"2026-11-$d","workedMinutes":${if (d <= 17) 480 else 0}}""" }
        return """{"week":"$week","weeksVisible":4,"hasPrev":true,"hasNext":false,""" +
            """"totals":{"workedMinutes":960,"awaitingMinutes":0,"openEntries":0,"checkInMissing":0,"truncated":false,"complete":true},""" +
            """"days":[$days],"records":[""" +
            """{"date":"$date","at":"09:00","venue":"V","direction":"in","channel":"nfc","state":"approved","claim":null},""" +
            """{"date":"$date","at":"17:00","venue":"V","direction":"out","channel":"nfc","state":"approved","claim":null}],""" +
            """"nextCursor":$cursor,"waiting":{"records":0,"claims":0}}"""
    }

    private fun twoPages() {
        server.hoursPages[""] = page("2026-11-16", "own_c1")
        server.hoursPages["own_c1"] = page("2026-11-17", null)
    }

    @Test
    fun aRefusedShowMoreReadsPageOneOfTheSameWeekAgain() {
        activated()
        twoPages()
        val r = flow.hours(week, "own_old_unbound")
        assertTrue("a restart, not a network problem: $r", r is Answer.Restarted)
        val first = (r as Answer.Restarted<HoursReply>).value
        assertEquals("page one: its cursor stands", "own_c1", first.nextCursor)
        assertEquals(listOf("2026-11-16", "2026-11-16"), first.records.map { it.date })
        assertEquals(
            "the refused cursor, then the same week with none -- once",
            listOf("""{"week":"$week","after":"own_old_unbound"}""", """{"week":"$week"}"""),
            server.hoursBodies,
        )
    }

    @Test
    fun aGoodShowMoreIsTheNextPage() {
        activated()
        twoPages()
        val r = flow.hours(week, "own_c1")
        assertTrue("$r", r is Answer.Ok)
        assertEquals(null, (r as Answer.Ok<HoursReply>).value.nextCursor)
        assertEquals(1, server.hoursBodies.size)
    }

    @Test
    fun aRefusedFirstPageIsNotReadAgain() {
        activated()
        twoPages()
        assertEquals(Answer.Server, flow.hours("2026-11-09", null))
        assertEquals("no retry without a cursor", 1, server.hoursBodies.size)
    }

    /** The browser's WholeWeek: "No records" only when no page is left to load. */
    @Test
    fun noRecordsIsSaidOnlyWhenTheLoadedPagesHoldTheWholeWeek() {
        activated()
        twoPages()
        val first = (flow.hours(week, null) as Answer.Ok<HoursReply>).value
        var loaded = first.records
        fun on(date: String) = loaded.count { it.date == date }
        assertFalse("Tuesday's records are on page two", StaffFlow.saysNoRecords(on("2026-11-17"), first))
        assertFalse("a later page may hold Wednesday's", StaffFlow.saysNoRecords(on("2026-11-18"), first))
        val second = (flow.hours(week, first.nextCursor) as Answer.Ok<HoursReply>).value
        loaded = loaded + second.records
        assertTrue(StaffFlow.saysNoRecords(on("2026-11-18"), second))
        assertFalse(StaffFlow.saysNoRecords(on("2026-11-17"), second))
        assertFalse(StaffFlow.saysNoRecords(on("2026-11-16"), second))
    }

    @Test
    fun aRestartRefusedInTurnIsThatAnswer() {
        activated()
        twoPages()
        // A cursor and a week the server refuses both: page one of that week is refused as well.
        assertEquals(Answer.Server, flow.hours("2026-11-09", "own_c1"))
        assertEquals(2, server.hoursBodies.size)
    }
}
