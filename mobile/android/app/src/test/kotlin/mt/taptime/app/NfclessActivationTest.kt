package mt.taptime.app

import java.io.File
import mt.taptime.app.activation.ActivationFlow
import mt.taptime.app.activation.ActivationFlow.ApprovalResult
import mt.taptime.app.activation.ActivationFlow.CompleteResult
import mt.taptime.app.activation.ApprovalPoll
import mt.taptime.app.activation.PairingCode
import mt.taptime.app.activation.PendingActivation
import mt.taptime.app.activation.Problem
import mt.taptime.app.activation.TapLink
import mt.taptime.app.activation.TapStepParts
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.ApprovalReply
import mt.taptime.app.wire.HttpUrlTransport
import mt.taptime.app.wire.ServerClock
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * M16-23 over a socket: a phone that cannot tap activates by a venue screen's live QR (complete with
 * `liveQR`) or by the manager's approval (activate/approval, then approval/status). What is measured
 * is the APP -- what it sends, signed over which path, and where each answer leads; the server's
 * decisions are FakeAppServer's simplified rules.
 */
class NfclessActivationTest {
    private val code = "q9bX3yJ0r1sT2uV3wX4yZ5a6b7c8d9e0f1g2h3i4j5k"
    private val server = FakeAppServer(code)
    private val keys = JdkKeys()
    private val state = MemoryState()
    private val flow = ActivationFlow(AppClient(HttpUrlTransport(server.base), ServerClock(), MiniJson), keys, state)
    private val live = "t=AQ00000000000000000000000000000000000000000000000000000000000000000000000000000x"

    @After
    fun stop() = server.close()

    private fun bound(): PendingActivation {
        val facts = (flow.lookup(code) as ActivationFlow.LookupResult.Facts).facts
        return (flow.bind(code, facts) as ActivationFlow.BindResult.Bound).pending
    }

    private fun waiting(p: PendingActivation): ApprovalResult.Waiting = flow.requestApproval(p) as ApprovalResult.Waiting

    // --- A: no launch refusal ------------------------------------------------------------------

    @Test
    fun aPhoneWithoutAReaderIsNoLongerRefusedAtLaunchAndReachesTheTapStep() {
        // The refusal is gone from the flow and from the activity.
        assertFalse("ActivationFlow.refusedAtLaunch is gone",
            ActivationFlow.Companion::class.java.declaredMethods.any { it.name == "refusedAtLaunch" })
        val activity = File(System.getProperty("taptime.mainSrc")!!, "kotlin/mt/taptime/app/MainActivity.kt").readText()
        assertFalse(activity.contains("refusedAtLaunch"))
        assertFalse("no screen refuses for a missing reader", activity.contains("NO_READER"))
        // Nothing before step 3 asks about NFC: lookup and bind go through.
        val p = bound()
        assertEquals(listOf("POST /app/v1/activate/lookup", "POST /app/v1/activate/bind"), server.seen)
        // Step 3 on a phone without a reader: the notice in the plaque's place (copy D).
        val parts = TapStepParts.of(p.facts.managerApproval, hasReader = false)
        assertTrue(parts.noReader)
        assertFalse("no plaque to hold the phone to", parts.plaque)
    }

    // --- B: the tap step's secondary way -------------------------------------------------------

    @Test
    fun theCantTapButtonIsShownExactlyWhenTheManagerCanApprove() {
        server.managerApproval = true
        assertTrue("lookup reads managerApproval", bound().facts.managerApproval)
        server.managerApproval = false
        assertFalse(bound().facts.managerApproval)
        server.managerApproval = null
        assertFalse("absent is false", bound().facts.managerApproval)

        for (reader in listOf(true, false)) {
            val on = TapStepParts.of(managerApproval = true, hasReader = reader)
            assertTrue("button, reader=$reader", on.cantTap)
            assertFalse("no camera line beside the button, reader=$reader", on.scanScreen)
            assertEquals("the no-reader hint only with the button", !reader, on.noReaderHint)
            val off = TapStepParts.of(managerApproval = false, hasReader = reader)
            assertFalse("no button, reader=$reader", off.cantTap)
            assertTrue("the camera line (A2), reader=$reader", off.scanScreen)
            assertFalse("no hint without the button", off.noReaderHint)
        }
        assertEquals(TapStepParts(plaque = true, noReader = false, noReaderHint = false, cantTap = false, scanScreen = true),
            TapStepParts.of(managerApproval = false, hasReader = true))
    }

    // --- C: a live QR at the activation step ---------------------------------------------------

    @Test
    fun aLiveQrCompletesWithLiveQrAndNoSun() {
        val p = bound()
        val link = TapLink.of("${server.base}/q?$live", server.base)!!
        val r = flow.complete(p, link) as CompleteResult.Activated
        assertEquals(listOf(live), server.liveCompleted)
        val sent = MiniJson.decodeObject(server.completeBodies.single())
        assertEquals(setOf("code", "publicKey", "platform", "liveQR"), sent.keys)
        assertEquals("the query, verbatim", live, sent["liveQR"])
        assertEquals(r.device, state.device)
        assertArrayEquals(p.key.signer.publicPoint, server.devices[r.device.deviceId])
    }

    @Test
    fun aRefusedLiveQrSaysScanTheScreenNotTapThePlaque() {
        val p = bound()
        server.liveRefused = live
        assertEquals(CompleteResult.TapAgain(p, Problem.LIVE_QR_FAILED), flow.completeLive(p, live))
        assertNull(state.device)
        // The same server code after a plaque read keeps the plaque's text.
        server.completeFailsOnce = 400 to "activation_tap_failed"
        assertEquals(CompleteResult.TapAgain(p, Problem.TAP_FAILED), flow.complete(p, "tag=04&ctr=000001&cmac=00"))
        // A static plaque QR (no SUN) still needs a touch: sent as `sun`, refused as before.
        val static = TapLink.of("${server.base}/t?tag=04A1B2C3D4E5F6", server.base)!!
        assertEquals(CompleteResult.TapAgain(p, Problem.NEEDS_TOUCH), flow.complete(p, static))
        assertTrue(MiniJson.decodeObject(server.completeBodies.last()).containsKey("sun"))
    }

    // --- D: the manager's approval -------------------------------------------------------------

    @Test
    fun anApprovalRequestIsSignedOverItsOwnPathAndShowsTheGroupedCode() {
        val p = bound()
        val w = waiting(p)
        assertEquals("482913", w.approval.pairingCode)
        assertEquals("482 913", PairingCode.grouped(w.approval.pairingCode))
        assertNull(w.notice)
        assertEquals("POST /app/v1/activate/approval", server.seen.last())
        assertEquals(setOf("code", "publicKey", "platform"), MiniJson.decodeObject(server.approvalBodies.single()).keys)
        assertEquals(1, server.approvals)
        // The fake verifies the signature over THIS path; a text over complete's path would be 401
        // and land on the consent screen instead.
        assertTrue(flow.pollApproval(p, w.approval) is ApprovalResult.Waiting)
        assertEquals("POST /app/v1/activate/approval/status", server.seen.last())
    }

    @Test
    fun screenAvailableSendsBackToTheTapStepWithoutTheButton() {
        server.managerApproval = true
        val p = bound()
        server.approvalFailsOnce = 409 to "screen_available"
        val r = flow.requestApproval(p) as ApprovalResult.TapStep
        assertEquals(Problem.SCREEN_AVAILABLE, r.problem)
        assertFalse("the step now offers the screen's code, not the button", r.pending.facts.managerApproval)
        assertEquals("the same bind and key", p.key.alias, r.pending.key.alias)
        assertTrue(keys.deleted.isEmpty())
    }

    @Test
    fun anApprovedStatusIsAFinishedActivation() {
        val first = flowActivatedByPlaque()
        val p = bound()
        val w = waiting(p)
        server.statusScript += listOf("waiting", "approved")
        assertTrue(flow.pollApproval(p, w.approval) is ApprovalResult.Waiting)
        val done = flow.pollApproval(p, w.approval) as ApprovalResult.Activated
        assertEquals("Maria Borg", done.device.employeeName)
        assertEquals(p.key.alias, done.device.keyAlias)
        assertEquals("the device is saved", done.device, state.device)
        assertArrayEquals(p.key.signer.publicPoint, server.devices[done.device.deviceId])
        assertEquals("this phone's earlier key is dropped", listOf(first), keys.deleted)
    }

    private fun flowActivatedByPlaque(): String =
        (flow.complete(bound(), "tag=04A1B2C3D4E5F6&ctr=000001&cmac=00") as CompleteResult.Activated).device.keyAlias

    @Test
    fun rejectedAndExpiredEndTheWaitAndAskingAgainOpensANewRequest() {
        val p = bound()
        val w = waiting(p)
        server.statusScript += listOf("rejected", "expired")
        assertEquals(ApprovalResult.Ended(p, Problem.APPROVAL_REJECTED), flow.pollApproval(p, w.approval))
        assertEquals(ApprovalResult.Ended(p, Problem.APPROVAL_EXPIRED), flow.pollApproval(p, w.approval))
        assertNull("nothing was set up", state.device)
        assertTrue("the bind is kept for the new request", keys.deleted.isEmpty())
        assertTrue("\"Ask again\" / \"Get a new code\"", flow.requestApproval(p) is ApprovalResult.Waiting)
        assertEquals(2, server.approvals)
    }

    @Test
    fun newKeyRequiredOnTheApprovedCollectBindsAFreshKey() {
        val p = bound()
        val w = waiting(p)
        server.statusScript += "409:new_key_required"
        val r = flow.pollApproval(p, w.approval) as ApprovalResult.TapStep
        assertNotEquals("a NEW key", p.key.alias, r.pending.key.alias)
        assertTrue(p.key.alias in keys.deleted)
        assertArrayEquals("bound with the consent already given", r.pending.key.signer.publicPoint, server.bound!!.point)
        assertNull(state.device)
    }

    @Test
    fun aMovedBindingOnStatusGoesBackToConsentAsCompleteDoes() {
        val p = bound()
        val w = waiting(p)
        server.bindElsewhere()
        val r = flow.pollApproval(p, w.approval) as ApprovalResult.Rebind
        assertEquals(Problem.NOT_READY, r.problem)
        assertEquals(code, r.code)
        assertTrue(p.key.alias in keys.deleted)
        // And on the request itself.
        val q = bound()
        server.bindElsewhere()
        assertEquals(Problem.NOT_READY, (flow.requestApproval(q) as ApprovalResult.Rebind).problem)
    }

    @Test
    fun passingTroubleWhilePollingKeepsWaiting() {
        val p = bound()
        val w = waiting(p)
        server.statusScript += listOf("429:rate_limited", "500:server_error")
        assertEquals(ApprovalResult.Waiting(p, w.approval, Problem.TOO_MANY), flow.pollApproval(p, w.approval))
        assertEquals(ApprovalResult.Waiting(p, w.approval, Problem.SERVER), flow.pollApproval(p, w.approval))
        assertEquals("the next answer clears it", ApprovalResult.Waiting(p, w.approval), flow.pollApproval(p, w.approval))
        server.close()
        assertEquals(ApprovalResult.Waiting(p, w.approval, Problem.NETWORK), flow.pollApproval(p, w.approval))
        assertTrue(keys.deleted.isEmpty())
    }

    @Test
    fun aDeadInvitationOnStatusIsTheCodeScreen() {
        val p = bound()
        val w = waiting(p)
        server.statusScript += "400:invalid_invite"
        assertEquals(ApprovalResult.Restart(Problem.BAD_LINK), flow.pollApproval(p, w.approval))
    }

    // --- the poll's interval (review ORTA-1) ---------------------------------------------------

    @Test
    fun thePollBacksOffOnEach429AndAnAnswerOrNoAnswerIsTheBase() {
        val rl = ApprovalPoll.Outcome.RATE_LIMITED
        val chain = generateSequence(ApprovalPoll.BASE_MS) { ApprovalPoll.nextDelay(it, rl) }.take(6).toList()
        assertEquals(listOf(5_000L, 10_000L, 20_000L, 40_000L, 60_000L, 60_000L), chain)
        for (from in chain) {
            assertEquals("an answer is the base, from $from", 5_000L, ApprovalPoll.nextDelay(from, ApprovalPoll.Outcome.ANSWERED))
            assertEquals("no answer is the base, from $from", 5_000L, ApprovalPoll.nextDelay(from, ApprovalPoll.Outcome.FAILED))
        }
    }

    @Test
    fun aPollsResultNamesItsBudgetOutcome() {
        val p = bound()
        val w = waiting(p)
        server.statusScript += listOf("429:rate_limited", "500:server_error", "waiting")
        assertEquals(ApprovalPoll.Outcome.RATE_LIMITED, ApprovalPoll.outcomeOf(flow.pollApproval(p, w.approval)))
        assertEquals(ApprovalPoll.Outcome.ANSWERED, ApprovalPoll.outcomeOf(flow.pollApproval(p, w.approval)))
        assertEquals(ApprovalPoll.Outcome.ANSWERED, ApprovalPoll.outcomeOf(flow.pollApproval(p, w.approval)))
        server.close()
        assertEquals(ApprovalPoll.Outcome.FAILED, ApprovalPoll.outcomeOf(flow.pollApproval(p, w.approval)))
    }

    // --- the wire ------------------------------------------------------------------------------

    @Test
    fun theApprovalReplyIsReadStrictlyWhereItMatters() {
        fun read(s: String) = mt.taptime.app.wire.Replies.approval(MiniJson.decodeObject(s))
        assertEquals(ApprovalReply.Waiting("482913", "2026-11-20T10:15:00Z"),
            read("""{"state":"waiting","pairingCode":"482913","expiresAt":"2026-11-20T10:15:00Z"}"""))
        assertEquals(ApprovalReply.Rejected, read("""{"state":"rejected"}"""))
        assertEquals(ApprovalReply.Expired, read("""{"state":"expired"}"""))
        for (bad in listOf(
            """{"state":"waiting","pairingCode":"48291","expiresAt":"x"}""",
            """{"state":"waiting","pairingCode":"48291a","expiresAt":"x"}""",
            """{"state":"approved","deviceId":"not-a-uuid","employeeName":"M"}""",
            """{"state":"maybe"}""",
        )) {
            try {
                read(bad)
                throw AssertionError("read: $bad")
            } catch (_: mt.taptime.app.wire.JsonException) {
            }
        }
    }

    @Test
    fun theLiveCompleteAndApprovalBodiesAreTheContractsFields() {
        val point = ByteArray(65).also { it[0] = 4 }
        val key = mt.taptime.app.wire.B64Url.encode(point)
        assertEquals("""{"code":"c","publicKey":"$key","platform":"android","liveQR":"t=1"}""",
            String(mt.taptime.app.wire.Bodies.completeLive("c", point, "t=1")))
        assertEquals("""{"code":"c","publicKey":"$key","platform":"android"}""",
            String(mt.taptime.app.wire.Bodies.approval("c", point)))
    }
}
