package mt.taptime.app

import mt.taptime.app.activation.ActivationFlow
import mt.taptime.app.activation.ActivationFlow.CompleteResult
import mt.taptime.app.activation.KeyBacking
import mt.taptime.app.activation.KeyGate
import mt.taptime.app.activation.PendingActivation
import mt.taptime.app.activation.Problem
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.HttpUrlTransport
import mt.taptime.app.wire.ServerClock
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The activation end to end over a socket: ActivationFlow -> AppClient -> HttpUrlTransport ->
 * FakeAppServer, with JDK keys standing in for the Keystore's. What the server decides (the
 * plaque, the counter, the invitation) is the fake's simplified rule; what is measured here is
 * the APP: which requests it sends in which order, what it signs, what it keeps and drops.
 */
class ActivationFlowTest {
    private val code = "q9bX3yJ0r1sT2uV3wX4yZ5a6b7c8d9e0f1g2h3i4j5k"
    private val server = FakeAppServer(code)
    private val keys = JdkKeys()
    private val state = MemoryState()
    private var phoneOffsetMillis = 0L
    private val clock = ServerClock { System.currentTimeMillis() + phoneOffsetMillis }
    private val flow = ActivationFlow(AppClient(HttpUrlTransport(server.base), clock, MiniJson), keys, state)

    @After
    fun stop() = server.close()

    private fun sun(ctr: Int) = "tag=04A1B2C3D4E5F6&ctr=%06X&cmac=0011223344556677".format(ctr)

    private fun bound(): PendingActivation {
        val facts = (flow.lookup(code) as ActivationFlow.LookupResult.Facts).facts
        return (flow.bind(code, facts) as ActivationFlow.BindResult.Bound).pending
    }

    @Test
    fun lookupBindAndTheFirstReadActivateTheKeyThatWasBound() {
        val facts = (flow.lookup(code) as ActivationFlow.LookupResult.Facts).facts
        assertEquals("Maria Borg", facts.employeeName)
        assertEquals("KF St Julians", facts.businessName)
        assertEquals("KF-Staff", facts.wifiSsid)
        assertTrue("the clock was measured from the first, unsigned answer", clock.measured)

        val p = (flow.bind(code, facts) as ActivationFlow.BindResult.Bound).pending
        assertArrayEquals("the server holds THIS key's point", p.key.signer.publicPoint, server.bound!!.point)
        assertNull("nothing is stored before the tap", state.device)

        val r = flow.complete(p, sun(17)) as CompleteResult.Activated
        assertEquals(1, server.completed)
        assertEquals(p.key.alias, r.device.keyAlias)
        assertEquals("Maria Borg", r.device.employeeName)
        assertEquals(r.device, state.device)
        assertArrayEquals(p.key.signer.publicPoint, server.devices[r.device.deviceId])
        assertEquals(
            listOf("POST /app/v1/activate/lookup", "POST /app/v1/activate/bind", "POST /app/v1/activate/complete"),
            server.seen,
        )
        assertTrue("no key was dropped", keys.deleted.isEmpty())
    }

    @Test
    fun aReplayedReadIsRefusedAndTheNextReadActivates() {
        val p = bound()
        server.lastCtr = 17
        val again = flow.complete(p, sun(17)) as CompleteResult.TapAgain
        assertEquals(Problem.TAP_FAILED, again.problem)
        assertEquals("the same bind waits for the next read", p, again.pending)
        assertNull(state.device)
        assertTrue(flow.complete(again.pending, sun(18)) is CompleteResult.Activated)
    }

    @Test
    fun aReadWithoutACounterIsNotATouch() {
        val p = bound()
        val r = flow.complete(p, "tag=04A1B2C3D4E5F6") as CompleteResult.TapAgain
        assertEquals(Problem.NEEDS_TOUCH, r.problem)
    }

    @Test
    fun aTakenKeyIsReplacedByAFreshPairWithoutAskingForConsentAgain() {
        val p = bound()
        server.newKeyOnce = true
        val r = flow.complete(p, sun(1)) as CompleteResult.TapAgain
        assertNotEquals("a NEW key was generated and bound", p.key.alias, r.pending.key.alias)
        assertTrue("the taken key is dropped", p.key.alias in keys.deleted)
        assertArrayEquals(r.pending.key.signer.publicPoint, server.bound!!.point)
        val done = flow.complete(r.pending, sun(2)) as CompleteResult.Activated
        assertEquals(r.pending.key.alias, done.device.keyAlias)
    }

    @Test
    fun aBindThatMovedSendsThePersonBackToConsent() {
        val p = bound()
        server.bindElsewhere()
        val r = flow.complete(p, sun(5)) as CompleteResult.Rebind
        assertEquals(Problem.NOT_READY, r.problem)
        assertEquals(code, r.code)
        assertTrue(p.key.alias in keys.deleted)
        assertEquals("the plaque's counter was not spent by an unbound key", -1, server.lastCtr)
    }

    @Test
    fun aSoftwareKeyIsDeletedAndNeverBound() {
        keys.backing = KeyBacking.SOFTWARE
        val facts = (flow.lookup(code) as ActivationFlow.LookupResult.Facts).facts
        assertEquals(ActivationFlow.BindResult.Unsupported, flow.bind(code, facts))
        assertEquals("the key was generated and deleted", listOf("test-key-1"), keys.deleted)
        assertTrue(keys.live.isEmpty())
        assertEquals("no bind was sent", listOf("POST /app/v1/activate/lookup"), server.seen)
        assertNull(server.bound)
    }

    @Test
    fun theGateRefusesOnlySoftware() {
        for (b in KeyBacking.entries) assertEquals(b.name, b != KeyBacking.SOFTWARE, KeyGate.accepts(b))
        for (b in KeyBacking.entries.filter { it != KeyBacking.SOFTWARE }) {
            keys.backing = b
            val facts = (flow.lookup(code) as ActivationFlow.LookupResult.Facts).facts
            assertTrue(b.name, flow.bind(code, facts) is ActivationFlow.BindResult.Bound)
        }
    }

    @Test
    fun anUnknownCodeIsOneAnswer() {
        val r = flow.lookup("A".repeat(43)) as ActivationFlow.LookupResult.Refused
        assertEquals(Problem.BAD_LINK, r.problem)
    }

    @Test
    fun aPhoneFiveMinutesOffIsStillSetUpAfterOneCorrectedRetry() {
        flow.complete(bound(), sun(1)) as CompleteResult.Activated
        // A fresh client on a phone whose clock is five minutes slow, as after a restart: the
        // first signed read is outside the server's 60 s window.
        phoneOffsetMillis = -5 * 60_000
        val cold = ActivationFlow(AppClient(HttpUrlTransport(server.base), ServerClock { System.currentTimeMillis() + phoneOffsetMillis }, MiniJson), keys, state)
        server.seen.clear()
        val home = cold.home()
        assertTrue(home is ActivationFlow.HomeResult.SetUp)
        assertEquals("one 401, one corrected retry", listOf("GET /app/v1/me", "GET /app/v1/me"), server.seen)
        assertNotNull(state.device)
    }

    @Test
    fun aSignedOutPhoneKeepsItsRecordAndKeyUntilANewActivation() {
        val done = flow.complete(bound(), sun(1)) as CompleteResult.Activated
        server.devices.clear() // the panel's "Sign out their phones" -- or a server fault answering 401
        server.seen.clear()
        assertEquals(ActivationFlow.HomeResult.SignedOut, flow.home())
        assertEquals("one 401, one retry", listOf("GET /app/v1/me", "GET /app/v1/me"), server.seen)
        assertEquals("the record is kept", done.device, state.device)
        assertTrue("the key is kept", keys.deleted.isEmpty())
        assertEquals("and the next launch asks again", ActivationFlow.HomeResult.SignedOut, flow.home())
        // Only a new successful activation replaces the record, and then the old key goes.
        val again = flow.complete(bound(), sun(2)) as CompleteResult.Activated
        assertEquals(again.device, state.device)
        assertEquals(listOf(done.device.keyAlias), keys.deleted)
        assertTrue(flow.home() is ActivationFlow.HomeResult.SetUp)
    }

    @Test
    fun aRecordWhoseKeyIsGoneIsDropped() {
        val done = flow.complete(bound(), sun(1)) as CompleteResult.Activated
        keys.live.remove(done.device.keyAlias)
        server.seen.clear()
        assertEquals(ActivationFlow.HomeResult.NotActivated, flow.home())
        assertNull(state.device)
        assertTrue("nothing was asked of the server", server.seen.isEmpty())
    }

    @Test
    fun aNewActivationOnThisPhoneDropsItsEarlierKey() {
        val first = flow.complete(bound(), sun(1)) as CompleteResult.Activated
        val second = flow.complete(bound(), sun(2)) as CompleteResult.Activated
        assertNotEquals(first.device.keyAlias, second.device.keyAlias)
        assertTrue(first.device.keyAlias in keys.deleted)
        assertFalse(second.device.keyAlias in keys.deleted)
        assertEquals(second.device, state.device)
    }
}
