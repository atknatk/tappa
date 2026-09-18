package mt.taptime.relay

import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.io.IOException

/**
 * The relay loop, driven end to end: the real HttpRelayServer against an in-process
 * panel, and a scripted chip. Each test names, in its own comment, what mutation of
 * the code under test turns it red — a test that nothing can fail is not a test.
 */
class RelayLoopTest {
    private lateinit var panel: FakePanel
    private val cookie = "tappa_admin_session=TESTCOOKIE-not-a-real-token"

    @Before
    fun up() {
        panel = FakePanel()
    }

    @After
    fun down() {
        panel.close()
    }

    private fun http(): HttpRelayServer = HttpRelayServer(panel.base, cookie = { cookie }, allowHttp = true)

    private fun run(chip: Chip = ScriptedChip(), listener: RelayLoop.Listener = RelayLoop.Listener.NONE): Outcome =
        RelayLoop(http(), chip, listener).run()

    // --- the happy path ---------------------------------------------------------------

    /**
     * Ten commands out, ten responses back, done. RED IF: the loop stops early, skips
     * a command, posts a wrong R-APDU, or forgets the handle on a step.
     */
    @Test
    fun fullRound_tenCommandsTenResponses_isCompleted() {
        val chip = ScriptedChip()
        val progress = mutableListOf<Pair<Int, String>>()
        val outcome = run(chip) { n, step -> progress += n to step }

        assertEquals(Outcome.Completed(10, null), outcome)
        assertEquals(10, chip.received.size)
        for (i in 0 until 10) assertArrayEquals("command $i", panel.commands[i], chip.received[i])

        assertEquals(1, panel.begins().size)
        assertEquals(10, panel.steps().size)
        assertEquals(0, panel.aborts().size)
        for (s in panel.steps()) {
            assertEquals(panel.handle, s.form["session"])
            assertEquals("9100", s.form["rapdu"])
        }
        // The screen saw every exchange, with the server's step word for the one before.
        assertEquals((1..10).toList(), progress.map { it.first })
        assertEquals(listOf("begin") + FakePanel.STEPS.dropLast(1), progress.map { it.second })
    }

    // --- the done rule ----------------------------------------------------------------

    /**
     * encode.Progress.Done: "READ Done BEFORE READING THE ERROR." A body that says
     * done AND carries a fault is a completion. RED IF: the parser reads `fault`
     * before `done` (Faulted instead of Completed), or the loop re-runs (a second
     * begin), or aborts.
     */
    @Test
    fun doneWithAFaultInTheSameBody_isCompletedAndNeverRerun() {
        val base = panel.script
        panel.script = { s ->
            val a = base(s)
            if (s.path == HttpRelayServer.STEP_PATH && a.body.contains("\"done\":true")) {
                panel.json(200, a.body.dropLast(1) + ""","fault":"server-error"}""")
            } else a
        }
        val outcome = run()
        assertEquals(Outcome.Completed(10, "server-error"), outcome)
        assertEquals(1, panel.begins().size)
        assertEquals(0, panel.aborts().size)
        assertTrue(Wording.forOutcome(outcome).contains("Do NOT encode this plaque again"))
    }

    /** The rule holds even against the status code: done with a 500 is still done. RED IF: status is read first. */
    @Test
    fun doneWithANon200Status_isStillCompleted() {
        val base = panel.script
        panel.script = { s ->
            val a = base(s)
            if (s.path == HttpRelayServer.STEP_PATH && a.body.contains("\"done\":true")) panel.json(500, a.body) else a
        }
        assertEquals(Outcome.Completed(10, null), run())
        assertEquals(0, panel.aborts().size)
    }

    // --- every fault word -------------------------------------------------------------

    /**
     * Each word the endpoint can utter ends the round, gets its own sentence, and
     * sends NO abort (RelayLoop's class comment gives the reason per word). RED IF: the
     * loop aborts on a fault, keeps stepping, or a word falls through to the
     * "does not know" sentence.
     */
    @Test
    fun everyFaultWord_stopsTheRound_withoutAnAbort_andHasItsOwnSentence() {
        val statuses = mapOf(
            "encode-unavailable" to 503, "bad-request" to 400, "unknown-session" to 404,
            "busy" to 409, "refused" to 422, "too-many-rounds" to 429, "server-error" to 500,
        )
        assertEquals(Wording.FAULT_WORDS.toSet(), statuses.map { (word, _) -> word }.toSet())
        for (word in Wording.FAULT_WORDS) {
            val local = FakePanel()
            try {
                val base = local.script
                var steps = 0
                local.script = { s ->
                    if (s.path == HttpRelayServer.STEP_PATH && ++steps == 3) local.fault(statuses.getValue(word), word) else base(s)
                }
                val chip = ScriptedChip()
                val outcome = RelayLoop(HttpRelayServer(local.base, cookie = { cookie }, allowHttp = true), chip).run()
                assertEquals(word, Outcome.Faulted(3, statuses.getValue(word), word), outcome)
                assertEquals(word, 3, chip.received.size)
                assertEquals(word, 0, local.aborts().size)
                val text = Wording.forOutcome(outcome)
                assertFalse("$word fell through to the unknown-word sentence", text.contains("does not know"))
                assertTrue("$word has an empty sentence", text.length > 20)
            } finally {
                local.close()
            }
        }
        // And an unknown word is still reported, not swallowed.
        assertTrue(Wording.forFault("something-new").contains("something-new"))
    }

    // --- the chip leaves the field ----------------------------------------------------

    /**
     * TagLost mid-round: the round ends, ONE abort goes out carrying the handle. RED
     * IF: no abort is sent, two are sent, the abort lacks the handle, or the loop
     * keeps stepping.
     */
    @Test
    fun chipLostMidRound_abortsExactlyOnce() {
        val chip = object : ScriptedChip() {
            override fun transceive(command: ByteArray): ByteArray {
                if (received.size == 4) throw ChipLostException("tag lost")
                return super.transceive(command)
            }
        }
        val outcome = run(chip)
        assertEquals(Outcome.ChipLost(4, abortSent = true), outcome)
        assertEquals(4, panel.steps().size)
        assertEquals(1, panel.aborts().size)
        assertEquals(panel.handle, panel.aborts()[0].form["session"])
        assertTrue(Wording.forOutcome(outcome).endsWith("The round was cancelled on the server."))
    }

    /**
     * The advice after a chip-side interruption depends on how far the round got —
     * three arms, three different sentences (second-round audit, N4). The arms are
     * driven through the REAL loop (chip lost at exchange 2, 5 and 9) so that the
     * count the wording sees is the count the loop produces. RED IF: the wording
     * ignores the count, the thresholds are off by one, or two arms share a sentence.
     */
    @Test
    fun chipLost_advice_dependsOnHowFarTheRoundGot() {
        fun lostAfter(n: Int): Outcome {
            val local = FakePanel()
            try {
                val chip = object : ScriptedChip() {
                    override fun transceive(command: ByteArray): ByteArray {
                        if (received.size == n) throw ChipLostException("tag lost")
                        return super.transceive(command)
                    }
                }
                return RelayLoop(HttpRelayServer(local.base, cookie = { cookie }, allowHttp = true), chip).run()
            } finally {
                local.close()
            }
        }
        val row = Wording.ROW_WRITTEN_AFTER_EXCHANGES
        val secret = Wording.SECRET_INSTALLED_AFTER_EXCHANGES

        // Below the row: retry is the right advice.
        val early = lostAfter(row - 2)
        assertEquals(Outcome.ChipLost(row - 2, abortSent = true), early)
        val earlyText = Wording.forOutcome(early)
        assertTrue(earlyText, earlyText.contains("Nothing was written yet") && earlyText.contains("try again"))

        // On the row boundary and below the secret: the row exists, a retry is refused.
        for (n in listOf(row, secret - 1)) {
            val mid = lostAfter(n)
            assertEquals(Outcome.ChipLost(n, abortSent = true), mid)
            val midText = Wording.forOutcome(mid)
            assertTrue(midText, midText.contains("inventory row") && midText.contains("will be refused"))
            assertFalse(midText, midText.contains("try again"))
        }

        // On the secret boundary and beyond: part-written, do not run again.
        val late = lostAfter(secret)
        assertEquals(Outcome.ChipLost(secret, abortSent = true), late)
        val lateText = Wording.forOutcome(late)
        assertTrue(lateText, lateText.contains("PART-WRITTEN") && lateText.contains("Do NOT run it again"))
        assertFalse(lateText, lateText.contains("try again"))

        // The same three arms for an I/O error, which leaves the chip in the same place.
        assertTrue(Wording.afterInterruption(0).contains("try again"))
        assertTrue(Wording.afterInterruption(row).contains("inventory row"))
        assertTrue(Wording.afterInterruption(secret).contains("PART-WRITTEN"))
        assertEquals(3, setOf(Wording.afterInterruption(0), Wording.afterInterruption(row), Wording.afterInterruption(secret)).size)

        // And the button under the message agrees with it.
        assertTrue(Wording.retryIsSafe(early))
        assertFalse(Wording.retryIsSafe(lostAfter(row)))
        assertFalse(Wording.retryIsSafe(late))
        assertFalse(Wording.retryIsSafe(Outcome.Faulted(3, 422, "refused")))
        assertFalse(Wording.retryIsSafe(Outcome.Faulted(3, 404, "unknown-session")))
        assertTrue(Wording.retryIsSafe(Outcome.Faulted(0, 409, "busy")))
        assertTrue(Wording.retryIsSafe(Outcome.Refused(0, 303)))
        assertFalse(Wording.retryIsSafe(Outcome.Unreachable(row, "timeout", abortSent = false)))
    }

    /** Any other chip I/O error is reported by name and also aborts once. RED IF: it is swallowed or mis-typed as ChipLost. */
    @Test
    fun chipIOError_isReportedAndAbortsOnce() {
        val chip = object : ScriptedChip() {
            override fun transceive(command: ByteArray): ByteArray {
                if (received.size == 1) throw IOException("transceive failed")
                return super.transceive(command)
            }
        }
        assertEquals(Outcome.ChipError(1, "transceive failed", abortSent = true), run(chip))
        assertEquals(1, panel.aborts().size)
    }

    // --- the Origin contract ----------------------------------------------------------

    /**
     * plaqueencode.go: "THE RELAY MUST SEND AN Origin HEADER." Every one of the three
     * routes must see Origin == the server's origin, plus the panel cookie and a form
     * body. The chip is lost at exchange 5 so that all three routes are exercised in
     * one round. RED IF: HttpRelayServer drops or misspells Origin on any route, sends
     * a path-bearing origin, forgets the cookie, or posts a different content type.
     */
    @Test
    fun originAndCookie_onAllThreeRoutes() {
        val chip = object : ScriptedChip() {
            override fun transceive(command: ByteArray): ByteArray {
                if (received.size == 5) throw ChipLostException("tag lost")
                return super.transceive(command)
            }
        }
        run(chip)
        val routes = listOf(HttpRelayServer.ENCODE_PATH, HttpRelayServer.STEP_PATH, HttpRelayServer.ABORT_PATH)
        assertEquals(routes.toSet(), panel.seen.map { it.path }.toSet())
        for (s in panel.seen) {
            assertEquals("Origin on ${s.path}", listOf(panel.base), s.headers["origin"])
            assertEquals("Cookie on ${s.path}", listOf(cookie), s.headers["cookie"])
            assertEquals(
                "Content-Type on ${s.path}",
                listOf("application/x-www-form-urlencoded; charset=utf-8"),
                s.headers["content-type"],
            )
        }
        // Begin carries an empty body: tenant and actor come from the session (K3, ADR 0017 §6 md. 10).
        assertEquals(emptyMap<String, String>(), panel.begins()[0].form)
    }

    /**
     * The signed-out probe sends Origin and NO cookie, and reads the chain's answer the
     * way the server shapes it: 303 to /admin/login = origin accepted, 303 to /admin =
     * refused. RED IF: the probe carries a cookie (it could then open a round), drops
     * Origin, or misreads the Location.
     */
    @Test
    fun probeOrigin_sendsOriginAndNoCookie_andReadsTheLocation() {
        panel.script = { s ->
            if (s.headers["origin"] == listOf(panel.base)) FakePanel.Answer(303, "text/html", "").also { }
            else FakePanel.Answer(303, "text/html", "")
        }
        // The fake answers Location=/admin/login for every 303; tell accepted from refused by what was sent.
        val withCookie = HttpRelayServer(panel.base, cookie = { cookie }, allowHttp = true)
        val p = withCookie.probeOrigin()
        assertEquals(303, p.status)
        assertTrue(p.originAccepted)
        assertFalse(p.originRefused)
        val seen = panel.seen.single()
        assertEquals(HttpRelayServer.ENCODE_PATH, seen.path)
        assertEquals(listOf(panel.base), seen.headers["origin"])
        assertEquals(null, seen.headers["cookie"])
        assertTrue(p.describe().contains("accepted"))

        assertTrue(HttpRelayServer.Probe(303, "/admin").originRefused)
        assertTrue(HttpRelayServer.Probe(303, "/admin").describe().contains("refused"))
        assertFalse(HttpRelayServer.Probe(200, null).originAccepted)
    }

    /**
     * The Origin is lower-case scheme + lower-case host [+ port] and nothing else,
     * WHATEVER CASE the operator typed. The input is passed RAW: the round-1 test
     * lower-cased it itself and so never saw that `https://TapTime.mt` broke sign-in
     * (audit F5 — Chromium reports hosts lower-case, MainActivity compares with
     * startsWith). RED IF: a path, slash, userinfo or upper-case letter survives.
     */
    @Test
    fun origin_isTheBareLowerCaseOrigin() {
        assertEquals("https://taptime.mt", HttpRelayServer("https://taptime.mt/", cookie = { null }).origin)
        assertEquals("https://taptime.mt", HttpRelayServer("  https://TapTime.mt  ", cookie = { null }).origin)
        assertEquals("https://taptime.mt", HttpRelayServer("HTTPS://TAPTIME.MT", cookie = { null }).origin)
        assertEquals("https://taptime.mt:8443", HttpRelayServer("https://TapTime.mt:8443/", cookie = { null }).origin)
        assertEquals("http://192.168.1.5:8080", HttpRelayServer("http://192.168.1.5:8080", cookie = { null }, allowHttp = true).origin)
        for (bad in listOf("taptime.mt", "https://taptime.mt/admin", "ftp://taptime.mt", "https://taptime.mt?x=1", "", "https://", "https://user:pw@taptime.mt")) {
            try {
                HttpRelayServer.normaliseBase(bad, allowHttp = true)
                throw AssertionError("accepted '$bad'")
            } catch (e: IllegalArgumentException) {
                // refused, as it should be
            }
        }
    }

    // --- shapes this client refuses to guess about ------------------------------------

    /**
     * command == "" with done == false is not a completion and not a quiet stop: it is
     * a fault, and the still-live session is aborted. RED IF: it is treated as done,
     * or the loop steps with an empty command, or no abort goes out.
     */
    @Test
    fun emptyCommandWithoutDone_isAFaultAndAborts() {
        val base = panel.script
        var steps = 0
        panel.script = { s ->
            if (s.path == HttpRelayServer.STEP_PATH && ++steps == 3) panel.progress(ByteArray(0), "getversion.2") else base(s)
        }
        val chip = ScriptedChip()
        val outcome = run(chip)
        assertTrue(outcome.toString(), outcome is Outcome.Protocol)
        assertEquals(3, outcome.exchanges)
        assertTrue((outcome as Outcome.Protocol).abortSent)
        assertEquals(3, chip.received.size)
        assertEquals(1, panel.aborts().size)
    }

    /** A 303 from the chain (no session, or Origin refused) ends the round with no abort and a sign-in sentence. RED IF: the body is parsed, or an abort is sent for a round that never opened. */
    @Test
    fun refusal303_stopsWithoutAbort() {
        panel.script = { panel.html(303) }
        val outcome = run()
        assertEquals(Outcome.Refused(0, 303), outcome)
        assertEquals(0, panel.aborts().size)
        assertTrue(Wording.forOutcome(outcome).contains("Sign in"))
    }

    /** A 429 HTML page from floodGate/sessionGate mid-round: status only, no abort (the server lets the round die). */
    @Test
    fun refusal429MidRound_stopsWithoutAbort() {
        val base = panel.script
        var steps = 0
        panel.script = { s -> if (s.path == HttpRelayServer.STEP_PATH && ++steps == 7) panel.html(429) else base(s) }
        assertEquals(Outcome.Refused(7, 429), run())
        assertEquals(0, panel.aborts().size)
    }

    /** A server that never says done is stopped by the runaway guard, with an abort. RED IF: the loop has no ceiling (the timeout is what turns a hang into a failure). */
    @Test(timeout = 20_000)
    fun serverThatNeverSaysDone_isStoppedAndAborted() {
        panel.script = { s ->
            when (s.path) {
                HttpRelayServer.ABORT_PATH -> panel.json(200, """{"session":"","command":"","step":"abort","done":false}""")
                else -> panel.progress(panel.commands[0], "select")
            }
        }
        val outcome = run()
        assertTrue(outcome.toString(), outcome is Outcome.Protocol)
        assertEquals(RelayLoop.MAX_EXCHANGES, outcome.exchanges)
        assertEquals(1, panel.aborts().size)
    }

    /** A body that is JSON but not the contract is a Protocol outcome, aborted once. */
    @Test
    fun malformedJsonMidRound_isAFaultAndAborts() {
        val base = panel.script
        var steps = 0
        panel.script = { s -> if (s.path == HttpRelayServer.STEP_PATH && ++steps == 2) panel.json(200, """{"hello":"world"}""") else base(s) }
        val outcome = run()
        assertTrue(outcome.toString(), outcome is Outcome.Protocol)
        assertEquals(1, panel.aborts().size)
    }

    /** No server at all: Unreachable, and no abort can be sent for a round that never opened. */
    @Test
    fun unreachableServer_isReportedHonestly() {
        val dead = FakePanel().also { it.close() }
        val outcome = RelayLoop(HttpRelayServer(dead.base, cookie = { cookie }, allowHttp = true), ScriptedChip()).run()
        assertTrue(outcome.toString(), outcome is Outcome.Unreachable)
        assertFalse((outcome as Outcome.Unreachable).abortSent)
        assertTrue(Wording.forOutcome(outcome).contains("check the plaque list"))
    }

    // --- the handle never leaks -------------------------------------------------------

    /**
     * The session handle is a bearer credential. It must not be printable from any
     * Outcome, nor from Reply.Progress's toString, nor from any wording. RED IF: an
     * Outcome grows a session field, or Progress loses its redacting toString.
     */
    @Test
    fun theHandleIsNeverInAnOutcomeAStringOrASentence() {
        val chip = object : ScriptedChip() {
            override fun transceive(command: ByteArray): ByteArray {
                if (received.size == 2) throw ChipLostException("tag lost")
                return super.transceive(command)
            }
        }
        val outcomes = listOf(run(), run(chip))
        for (o in outcomes) {
            assertFalse(o.toString(), o.toString().contains(panel.handle))
            assertFalse(Wording.forOutcome(o).contains(panel.handle))
            for (f in o.javaClass.declaredFields) {
                f.isAccessible = true
                assertFalse("${o.javaClass.simpleName}.${f.name}", f.get(o)?.toString()?.contains(panel.handle) == true)
            }
        }
        val p = Reply.parse(200, "application/json", """{"session":"${panel.handle}","command":"00a4","step":"begin","done":false}""")
        assertTrue(p is Reply.Progress)
        assertFalse(p.toString(), p.toString().contains(panel.handle))
        assertFalse(p.toString(), p.toString().contains("00a4"))
    }
}
