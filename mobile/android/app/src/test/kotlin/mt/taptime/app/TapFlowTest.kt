package mt.taptime.app

import mt.taptime.app.activation.ActiveDevice
import mt.taptime.app.activation.KeyBacking
import mt.taptime.app.tap.Fix
import mt.taptime.app.tap.TapFlow
import mt.taptime.app.tap.TapFlow.Press
import mt.taptime.app.tap.TapFlow.Start
import mt.taptime.app.tap.Buttons
import mt.taptime.app.tap.Retry
import mt.taptime.app.tap.TapProblem
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.HttpUrlTransport
import mt.taptime.app.wire.ServerClock
import mt.taptime.app.wire.Signer
import java.util.UUID
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The tap end to end over a socket: TapFlow -> AppClient -> HttpUrlTransport -> FakeAppServer,
 * with a JDK key standing in for the Keystore's. What the SERVER decides is the fake's simplified
 * rule; what is measured is the APP: which requests go out in which order, what each body carries
 * (and does not), and where each answer leads -- the screen, "Try again" or not, the device kept.
 */
class TapFlowTest {
    private val server = FakeAppServer("unused-code")
    private val keys = JdkKeys()
    private val state = MemoryState()
    private val flow = TapFlow(AppClient(HttpUrlTransport(server.base), ServerClock(), MiniJson), keys, state, server.base)
    private val sun = "tag=04A1B2C3D4E5F6&ctr=000011&cmac=0011223344556677"
    private val url = "${server.base}/t?$sun"

    @After
    fun stop() = server.close()

    private fun activated(): ActiveDevice {
        val key = keys.generate()
        val id = UUID.randomUUID().toString()
        server.devices[id] = key.signer.publicPoint
        return ActiveDevice(id, key.alias, KeyBacking.TEE, "Maria Borg").also { state.device = it }
    }

    private fun ready(): Start.Ready = flow.start(url) as Start.Ready

    @Test
    fun startIsASignedReadOfThePlaquesQueryAndDrawsTheTapScreen() {
        activated()
        val r = ready()
        assertEquals("Maria Borg", r.facts.employeeName)
        assertEquals("St Julians", r.facts.locationName)
        assertEquals("in", r.facts.direction)
        assertEquals("the plaque's query, verbatim", listOf("""{"sun":"$sun"}"""), server.tapStartBodies)
        assertEquals("a signed read: no nonce is fetched for the screen", listOf("POST /app/v1/tap/start"), server.seen)
        assertEquals("tap/start writes nothing", 0, server.recorded)
    }

    @Test
    fun aPressSendsTheContextTheDirectionPressedOneFixAndTheMockFlagOnlyWhenTrue() {
        activated()
        val r = ready()
        val got = flow.press(r, "out", Fix(35.9183, 14.4890, mock = true)) as Press.Recorded
        assertEquals("ok", got.reply.verdict)
        assertEquals("out", got.reply.direction)
        assertEquals("14:03", got.reply.at)
        assertEquals("""{"context":"${r.facts.context}","want":"out","lat":35.9183,"lng":14.489,"mockLocation":true}""", server.tapBodies.last())
        assertEquals(
            "a fresh nonce, then the press",
            listOf("POST /app/v1/tap/start", "POST /app/v1/nonce", "POST /app/v1/tap"),
            server.seen,
        )

        flow.press(ready(), "in", Fix(35.9, 14.4, mock = false))
        assertEquals("mock false is never written", """{"context":"${r.facts.context}","want":"in","lat":35.9,"lng":14.4}""", server.tapBodies.last())
    }

    @Test
    fun aPressWithoutAFixOrADirectionSendsNeither() {
        activated()
        server.direction = ""
        val r = ready()
        assertEquals("", r.facts.direction)
        flow.press(r, null, null)
        assertEquals("""{"context":"${r.facts.context}"}""", server.tapBodies.last())
        // A want the wire does not know is not sent either (the server would ignore it anyway).
        flow.press(ready(), "sideways", null)
        assertEquals("""{"context":"${r.facts.context}"}""", server.tapBodies.last())
    }

    @Test
    fun aPhoneWithNoDeviceIsNotSetUpAndAsksNothing() {
        assertEquals(Start.Problem(TapProblem.NOT_SET_UP, Retry.NONE), flow.start(url))
        assertTrue(server.seen.isEmpty())
    }

    @Test
    fun aRecordWhoseKeyIsGoneIsDroppedAndTheAppIsNotSetUp() {
        val d = activated()
        keys.live.remove(d.keyAlias)
        assertEquals(Start.Problem(TapProblem.NOT_SET_UP, Retry.NONE), flow.start(url))
        assertNull("a record nothing can sign for is dropped (permanent)", state.device)
        assertTrue(server.seen.isEmpty())
    }

    @Test
    fun aKeystoreThatFailsTwiceIsKeyUnreadableAndKeepsTheRecord() {
        val d = activated()
        val failing = FailingKeys(keys, loadFailures = 2)
        val f = TapFlow(AppClient(HttpUrlTransport(server.base), ServerClock(), MiniJson), failing, state, server.base)
        assertEquals(Start.Problem(TapProblem.KEY_UNREADABLE, Retry.RESTART), f.start(url))
        assertEquals("never deleted on a Keystore failure", d, state.device)
        assertTrue(server.seen.isEmpty())
        // One failure is retried once and goes through.
        val once = TapFlow(AppClient(HttpUrlTransport(server.base), ServerClock(), MiniJson), FailingKeys(keys, loadFailures = 1), state, server.base)
        assertTrue(once.start(url) is Start.Ready)
    }

    @Test
    fun aSignerThatThrowsIsKeyUnreadableNotACrash() {
        activated()
        val f = TapFlow(AppClient(HttpUrlTransport(server.base), ServerClock(), MiniJson), FailingKeys(keys, signFails = true), state, server.base)
        assertEquals(Start.Problem(TapProblem.KEY_UNREADABLE, Retry.RESTART), f.start(url))
        // The press signs with the signer its screen was drawn with: one that throws by then (the
        // Keystore refusing mid-shift) is the same screen, and nothing reaches the record.
        val r = ready()
        val throwing = object : Signer {
            override val publicPoint = r.signer.publicPoint
            override fun sign(message: ByteArray): ByteArray = throw java.security.ProviderException("keystore refused")
        }
        assertEquals(Press.Problem(TapProblem.KEY_UNREADABLE, Retry.RESTART), flow.press(r.copy(signer = throwing), "in", null))
        assertEquals("nothing reached the record", 0, server.recorded)
        assertTrue("the press never went out", "POST /app/v1/tap" !in server.seen)
    }

    @Test
    fun anotherHostsUrlIsABadLinkAndIsSentNowhere() {
        activated()
        for (other in listOf("https://evil.example/t?$sun", "${server.base}/activate?code=x", "${server.base}/t")) {
            assertEquals(other, Start.Problem(TapProblem.BAD_LINK, Retry.NONE), flow.start(other))
        }
        assertTrue(server.seen.isEmpty())
    }

    @Test
    fun aSignedOutPhoneIsToldSoAfterOneRetryAndKeepsItsRecord() {
        val d = activated()
        server.devices.clear()
        assertEquals(Start.Problem(TapProblem.SIGNED_OUT, Retry.NONE), flow.start(url))
        assertEquals("one clock-corrected retry, no third", 2, server.seen.size)
        assertEquals(d, state.device)

        // Signed out between the screen and the press: the press is the 401's too.
        val d2 = activated()
        val r = ready()
        server.devices.remove(d2.deviceId)
        assertEquals(Press.Problem(TapProblem.SIGNED_OUT, Retry.NONE), flow.press(r, "in", null))
        assertEquals(0, server.recorded)
    }

    @Test
    fun theStartRefusalsAreGetTsAnswers() {
        activated()
        val cases = listOf(
            (400 to "bad_request") to Start.Problem(TapProblem.BAD_LINK, Retry.NONE),
            (404 to "unknown_plaque") to Start.Problem(TapProblem.UNKNOWN_PLAQUE, Retry.NONE),
            (429 to "rate_limited") to Start.Problem(TapProblem.TOO_MANY, Retry.NONE),
            (500 to "server_error") to Start.Problem(TapProblem.SERVER, Retry.RESTART),
            (409 to "something_new") to Start.Problem(TapProblem.SERVER, Retry.RESTART),
        )
        for ((answer, want) in cases) {
            server.tapStartFailsOnce = answer
            assertEquals(answer.second, want, flow.start(url))
        }
    }

    @Test
    fun thePressRefusalsOfferNoRetry() {
        activated()
        val cases = listOf(
            (400 to "stale_context") to TapProblem.STALE,
            (403 to "foreign_tenant") to TapProblem.FOREIGN,
            (404 to "unknown_plaque") to TapProblem.UNKNOWN_PLAQUE,
            (429 to "rate_limited") to TapProblem.TOO_MANY,
            (409 to "try_again") to TapProblem.SERVER,
            (500 to "server_error") to TapProblem.SERVER,
        )
        for ((answer, want) in cases) {
            val r = ready()
            server.tapFailsOnce = answer
            assertEquals(answer.second, Press.Problem(want, Retry.NONE), flow.press(r, "in", null))
        }
    }

    @Test
    fun noAnswerToTheStartIsUnreachableAndSafeToRetry() {
        activated()
        server.close()
        assertEquals(Start.Problem(TapProblem.UNREACHABLE, Retry.RESTART), flow.start(url))
    }

    @Test
    fun aLostAnswerToThePressIsNeverRetriedAndSaysTapAgain() {
        activated()
        val r = ready()
        server.tapAnswerLostOnce = true
        assertEquals("Try again goes home and resends nothing", Press.Problem(TapProblem.LOST, Retry.HOME), flow.press(r, "in", null))
        assertEquals("the record was written; the app did not send it twice", 1, server.recorded)
        assertEquals(1, server.tapBodies.size)
    }

    @Test
    fun theNameTheServerAnswersIsKept() {
        val d = activated()
        state.device = d.copy(employeeName = "Old Name")
        ready()
        assertEquals("Maria Borg", state.device?.employeeName)
    }

    @Test
    fun anotherBusinesssPlaqueHasNoVenueAndNoBrand() {
        activated()
        server.locationName = null
        val r = ready()
        assertNull(r.facts.locationName)
        assertNull(r.facts.brand.accent)
        assertNotNull(r.facts.context)
        assertFalse(r.facts.qr)
    }

    // M16-16 hand-over (4), the iOS twin's clock screen: a 401 whose answers carried a Date more than
    // a day off this phone's clock is the phone's clock, not a signed-out device -- at the start and
    // at the press. An in-range Date afterwards clears it.
    @Test
    fun aPhoneADayOffIsToldToCheckItsClockNotThatItWasSignedOut() {
        activated()
        var offsetMillis = 0L
        val clock = ServerClock { System.currentTimeMillis() + offsetMillis }
        val f = TapFlow(AppClient(HttpUrlTransport(server.base), clock, MiniJson), keys, state, server.base)
        val r = f.start(url) as Start.Ready
        offsetMillis = 3L * 24 * 60 * 60 * 1000 // the phone jumps three days ahead
        assertEquals(Press.Problem(TapProblem.CLOCK, Retry.RESTART), f.press(r, "in", null))
        assertEquals(Start.Problem(TapProblem.CLOCK, Retry.RESTART), f.start(url))
        assertEquals(0, server.recorded)
        offsetMillis = 0
        assertTrue("an in-range answer clears it", f.start(url) is Start.Ready)
        assertFalse(clock.lastAnswerWasFarOff)
    }

    // The coordinator's decisions (2026-11-18), the iOS twin's buttons: "Open in browser" only while
    // the URL is certainly unspent and the app cannot go on; never after a lost press.
    @Test
    fun theProblemScreensButtonsAreTheIosTwins() {
        assertEquals(Buttons(Retry.HOME, browser = false, startAgain = false, setUp = false), Buttons.of(TapProblem.LOST, Retry.HOME))
        assertEquals(Buttons(Retry.RESTART, browser = true, startAgain = false, setUp = false), Buttons.of(TapProblem.UNREACHABLE, Retry.RESTART))
        assertEquals(Buttons(Retry.NONE, browser = true, startAgain = false, setUp = true), Buttons.of(TapProblem.NOT_SET_UP, Retry.NONE))
        assertEquals(Buttons(Retry.NONE, browser = true, startAgain = true, setUp = false), Buttons.of(TapProblem.SIGNED_OUT, Retry.NONE))
        for (p in TapProblem.entries) {
            val b = Buttons.of(p, Retry.RESTART)
            assertEquals("$p: Open in browser only where the app cannot go on and the URL is unspent",
                p in setOf(TapProblem.NOT_SET_UP, TapProblem.SIGNED_OUT, TapProblem.UNREACHABLE), b.browser)
        }
    }

    /** A key store whose load fails [loadFailures] times, and whose signer throws when [signFails]. */
    private class FailingKeys(private val inner: JdkKeys, var loadFailures: Int = 0, val signFails: Boolean = false) :
        mt.taptime.app.activation.DeviceKeys by inner {
        override fun load(alias: String): Signer? {
            if (loadFailures > 0) {
                loadFailures--
                throw java.security.KeyStoreException("keystore busy")
            }
            val s = inner.load(alias) ?: return null
            if (!signFails) return s
            return object : Signer {
                override val publicPoint = s.publicPoint
                override fun sign(message: ByteArray): ByteArray = throw java.security.ProviderException("keystore refused")
            }
        }
    }
}
