package mt.taptime.app

import java.io.IOException
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.AppErrorCode
import mt.taptime.app.wire.Headers
import mt.taptime.app.wire.HttpReply
import mt.taptime.app.wire.Outcome
import mt.taptime.app.wire.Paths
import mt.taptime.app.wire.ServerClock
import mt.taptime.app.wire.Transport
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The clock-skew contract (card M16-12, carried from M16-07's review): (1) every reply's Date
 * re-measures the server's clock and reads are signed with the corrected second; (2) a 401 is
 * "signed out" only after ONE retry with a fresh measurement.
 */
class ClockSkewTest {
    private val deviceId = "7d1c2a4e-9b3f-4c5d-8e6f-0a1b2c3d4e5f"
    private val serverNow = Instant.parse("2026-11-18T12:00:00Z")

    private fun httpDate(i: Instant) = DateTimeFormatter.RFC_1123_DATE_TIME.withZone(ZoneOffset.UTC).format(i)

    /** A scripted server: answers in order, records each request's headers. */
    private class Script(vararg val replies: HttpReply) : Transport {
        val seen = mutableListOf<Pair<String, Map<String, String>>>()
        override fun exchange(method: String, path: String, headers: Map<String, String>, body: ByteArray?): HttpReply {
            seen += path to headers
            return replies.getOrNull(seen.size - 1) ?: throw IOException("script ran out")
        }
    }

    private fun me(name: String = "E") = """{"employeeName":"$name","brand":{},"direction":""}""".toByteArray()

    @Test
    fun theDateHeaderSetsTheOffsetAndAnUnreadableOneChangesNothing() {
        var local = serverNow.toEpochMilli() - 5 * 60_000 // the phone is five minutes slow
        val clock = ServerClock { local }
        assertEquals(serverNow.epochSecond - 300, clock.nowSeconds())
        clock.observe(httpDate(serverNow))
        assertTrue(clock.measured)
        assertEquals(300_000L, clock.offsetMillis())
        assertEquals(serverNow.epochSecond, clock.nowSeconds())
        local += 10_000
        assertEquals("the offset holds as time passes", serverNow.epochSecond + 10, clock.nowSeconds())
        for (junk in listOf(null, "", "yesterday", "2026-11-18T12:00:00Z")) clock.observe(junk)
        assertEquals(300_000L, clock.offsetMillis())
    }

    @Test
    fun aDateMoreThanADayAwayIsIgnoredAndThePreviousOffsetKept() {
        val local = serverNow.toEpochMilli()
        val clock = ServerClock { local }
        clock.observe(httpDate(serverNow.plusSeconds(90)))
        assertEquals(90_000L, clock.offsetMillis())
        for (far in listOf(serverNow.plusSeconds(86_401), serverNow.minusSeconds(86_401), serverNow.plusSeconds(400L * 86_400))) {
            clock.observe(httpDate(far))
            assertEquals("ignored: $far", 90_000L, clock.offsetMillis())
        }
        clock.observe(httpDate(serverNow.minusSeconds(86_400)))
        assertEquals("exactly a day is still taken", -86_400_000L, clock.offsetMillis())
        val fresh = ServerClock { local }
        fresh.observe(httpDate(serverNow.plusSeconds(2 * 86_400)))
        assertEquals(false, fresh.measured)
        assertEquals(0L, fresh.offsetMillis())
    }

    @Test
    fun goesHttpDateFormatIsRead() {
        // Go's http.TimeFormat: "Mon, 02 Jan 2006 15:04:05 GMT".
        assertEquals(Instant.parse("2026-11-18T12:00:00Z").toEpochMilli(), ServerClock.parseHttpDate("Wed, 18 Nov 2026 12:00:00 GMT"))
        assertNull(ServerClock.parseHttpDate("Wed, 18 Nov 2026 12:00:00"))
    }

    @Test
    fun aReadRefusedForASkewedClockIsRetriedOnceWithTheCorrectedSecond() {
        val local = serverNow.toEpochMilli() - 5 * 60_000
        val t = Script(
            HttpReply(401, httpDate(serverNow), ByteArray(0)),
            HttpReply(200, httpDate(serverNow), me()),
        )
        val r = AppClient(t, ServerClock { local }, MiniJson).me(deviceId, JdkSigner())
        assertEquals("E", (r as Outcome.Ok).value.employeeName)
        assertEquals(2, t.seen.size)
        assertEquals((serverNow.epochSecond - 300).toString(), t.seen[0].second[Headers.TIMESTAMP])
        assertEquals("the retry carries the server's second", serverNow.epochSecond.toString(), t.seen[1].second[Headers.TIMESTAMP])
    }

    @Test
    fun aSecond401IsSignedOutAndThereIsNoThirdTry() {
        val t = Script(
            HttpReply(401, httpDate(serverNow), ByteArray(0)),
            HttpReply(401, httpDate(serverNow), ByteArray(0)),
            HttpReply(200, httpDate(serverNow), me()),
        )
        val r = AppClient(t, ServerClock { serverNow.toEpochMilli() }, MiniJson).me(deviceId, JdkSigner())
        assertEquals(Outcome.Unauthenticated, r)
        assertEquals(2, t.seen.size)
    }

    @Test
    fun aRefusalOrNoAnswerIsNotRetried() {
        val t = Script(HttpReply(429, httpDate(serverNow), """{"error":"rate_limited"}""".toByteArray()))
        val r = AppClient(t, ServerClock(), MiniJson).me(deviceId, JdkSigner())
        assertEquals(Outcome.Refused(429, AppErrorCode.RATE_LIMITED), r)
        assertEquals(1, t.seen.size)
        val none = AppClient(Script(), ServerClock(), MiniJson).me(deviceId, JdkSigner())
        assertTrue(none is Outcome.Failed)
    }

    @Test
    fun aNonceSignedRequestRetriesOnceWithAFreshNonce() {
        val n1 = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
        val n2 = "__79_Pv6-fj39vX08_Lx8O_u7ezr6uno5-bl5OPi4eA"
        fun nonce(n: String) = HttpReply(200, httpDate(serverNow), """{"nonce":"$n","expiresAt":"2026-11-18T12:01:00Z"}""".toByteArray())
        val t = Script(nonce(n1), HttpReply(401, httpDate(serverNow), ByteArray(0)), nonce(n2), HttpReply(204, httpDate(serverNow), ByteArray(0)))
        val r = AppClient(t, ServerClock(), MiniJson).signedRequest("POST", Paths.REVOKE, ByteArray(0), deviceId, JdkSigner()) { it }
        assertEquals(Outcome.Ok(null), r)
        assertEquals(listOf(Paths.NONCE, Paths.REVOKE, Paths.NONCE, Paths.REVOKE), t.seen.map { it.first })
        assertEquals(n1, t.seen[1].second[Headers.NONCE])
        assertEquals(n2, t.seen[3].second[Headers.NONCE])
        assertNull("a nonce-signed request carries no timestamp", t.seen[1].second[Headers.TIMESTAMP])
        assertNull("a read carries no nonce", t.seen[0].second[Headers.NONCE])
    }
}
