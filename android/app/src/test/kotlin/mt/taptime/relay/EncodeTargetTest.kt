package mt.taptime.relay

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.net.ServerSocket

/**
 * Which business a plaque is encoded for (2026-11-10): the business read over the REAL
 * HttpRelayServer against the in-process panel, and EncodeTarget — the only way the reader
 * is armed. Each test names the mutation that turns it red.
 */
class EncodeTargetTest {
    private lateinit var panel: FakePanel

    /** Two panel sessions of one person: before and after a switch (a switch issues a NEW cookie). */
    private val one = "tappa_admin_session=SESSION-IN-BUSINESS-ONE"
    private val two = "tappa_admin_session=SESSION-IN-BUSINESS-TWO"
    private val refOne = "11111111-1111-4111-8111-111111111111"
    private val refTwo = "22222222-2222-4222-8222-222222222222"
    private val nameOne = "Business One Ltd."
    private val nameTwo = "Business Two Ltd."

    @Before
    fun up() {
        panel = FakePanel()
        panel.businesses["SESSION-IN-BUSINESS-ONE"] = panel.business(nameOne, refOne)
        panel.businesses["SESSION-IN-BUSINESS-TWO"] = panel.business(nameTwo, refTwo)
    }

    private fun go(t: EncodeTarget.Take): EncodeTarget.Take.Go =
        t as? EncodeTarget.Take.Go ?: throw AssertionError("take answered $t, want Go")

    @After
    fun down() {
        panel.close()
    }

    private fun http(cookie: String = one): HttpRelayServer = HttpRelayServer(panel.base, cookie = { cookie }, allowHttp = true)

    // --- the wire -------------------------------------------------------------------

    /**
     * A GET, with Origin and exactly the cookie it was handed, and no Sec-Fetch-Site (GATE-1:
     * the server refuses same-site / cross-site even beside a matching Origin). RED IF: the
     * read drops Origin, sends another cookie, follows a redirect, or uses POST.
     */
    @Test
    fun businessRead_isAGetWithOriginAndTheExactCookie() {
        val r = http(cookie = "ignored=1").business(one)
        assertEquals(BusinessReply.Known(nameOne, canSwitch = true, suspended = false, ref = refOne), r)
        val seen = panel.businessReads().single()
        assertEquals("GET", seen.method)
        assertEquals(listOf(panel.base), seen.headers["origin"])
        assertEquals(listOf(one), seen.headers["cookie"])
        assertNull(seen.headers["sec-fetch-site"])
    }

    /** RED IF: a 303 is followed (it would parse a sign-in page) or read as anything but a refusal. */
    @Test
    fun businessRead_aDeadSessionIsA303Refusal() {
        assertEquals(BusinessReply.Refused(303), http().business("tappa_admin_session=REVOKED"))
    }

    @Test
    fun businessRead_faultWordsAndBadShapesAreNamed() {
        panel.businesses["SESSION-IN-BUSINESS-ONE"] = panel.fault(503, "encode-unavailable")
        assertEquals(BusinessReply.Fault(503, "encode-unavailable"), http().business(one))

        panel.businesses["SESSION-IN-BUSINESS-ONE"] = panel.json(200, """{"business":"$nameOne","switch":true,"suspended":false}""")
        assertTrue("a body without the ref", http().business(one) is BusinessReply.Malformed)

        val closed = ServerSocket(0).use { it.localPort }
        val r = HttpRelayServer("http://127.0.0.1:$closed", cookie = { null }, allowHttp = true).business(one)
        assertTrue("$r", r is BusinessReply.Unreachable)
    }

    /** The parser accepts the four fields with their types and nothing less. RED IF: a missing or mistyped field is accepted. */
    @Test
    fun parse_requiresTheFourFieldsWithTheirTypes() {
        val ct = "application/json; charset=utf-8"
        assertEquals(
            BusinessReply.Known("Ħaġar <Qim>", canSwitch = false, suspended = true, ref = refOne),
            BusinessReply.parse(200, ct, """{"business":"Ħaġar <Qim>","switch":false,"suspended":true,"ref":"$refOne"}"""),
        )
        for (bad in listOf(
            """{"business":"","switch":true,"suspended":false,"ref":"r"}""",
            """{"business":"X","switch":"true","suspended":false,"ref":"r"}""",
            """{"business":"X","suspended":false,"ref":"r"}""",
            """{"business":7,"switch":true,"suspended":false,"ref":"r"}""",
            """{"switch":true,"suspended":false,"ref":"r"}""",
            """{"business":"X","switch":true,"suspended":false}""",
            """{"business":"X","switch":true,"suspended":false,"ref":""}""",
            """{"business":"X","switch":true,"suspended":false,"ref":7}""",
            """[]""",
        )) {
            assertTrue(bad, BusinessReply.parse(200, ct, bad) is BusinessReply.Malformed)
        }
        assertTrue(BusinessReply.parse(403, ct, """{"fault":""}""") is BusinessReply.Malformed)
        assertEquals(BusinessReply.Refused(429), BusinessReply.parse(429, "text/html", "<html>"))
    }

    // --- arming -----------------------------------------------------------------------

    /**
     * THE NAME IS READ BEFORE THE READER CAN BE ARMED. RED IF: arm or take answers without a
     * read (e.g. arm returns a placeholder business, or take ignores `known`).
     */
    @Test
    fun theReaderCannotBeArmedBeforeTheBusinessIsRead() {
        val target = EncodeTarget(http())
        assertNull(target.arm(one))
        assertEquals(EncodeTarget.Take.NotArmed, target.take(one))
        assertTrue(panel.businessReads().isEmpty())

        assertEquals(nameOne, (target.read(one) as BusinessReply.Known).name)
        assertEquals(nameOne, target.arm(one)?.name)
        val g = go(target.take(one))
        assertEquals(nameOne, g.business.name)
        assertEquals(refOne, g.business.ref)
        assertEquals("the round's cookie is the one take() checked", one, g.cookie)
    }

    /**
     * A SWITCH ISSUES A NEW COOKIE, AND THE OLD NAME MUST NOT ARM UNDER IT — the defect this
     * change closes (a two-business identity whose last-used business is the other one). RED
     * IF: arm / take stop comparing the session the name was read under.
     */
    @Test
    fun afterASwitch_theOldNameNeverArms_untilTheNewOneIsRead() {
        val target = EncodeTarget(http())
        target.read(one)
        assertEquals(nameOne, target.arm(one)?.name)

        assertNull("business one's name armed under business two's session", target.arm(two))
        assertEquals(EncodeTarget.Take.NotArmed, target.take(two))

        target.read(two)
        assertEquals(nameTwo, target.arm(two)?.name)
        assertNull("business two's name answered for the old session", target.confirmedFor(one))
    }

    /**
     * Armed, then the cookie changed or vanished before the plaque arrived (the 12 h cookie
     * expired on the phone, a sign-in elsewhere): SessionChanged — not the silent NotArmed —
     * and the name is dropped. RED IF: take re-checks only `armed`, or answers NotArmed here.
     */
    @Test
    fun aCookieChangeBetweenArmAndTap_isSaidAndDropsTheName() {
        for (later in listOf(two, null, "other=1")) {
            val target = EncodeTarget(http())
            target.read(one)
            target.arm(one)
            assertEquals("cookie now $later", EncodeTarget.Take.SessionChanged, target.take(later))
            assertNull(target.confirmedFor(one))
            assertNull(target.arm(one))
        }
    }

    /** RED IF: take does not disarm — the same plaque left on the phone would start a second round. */
    @Test
    fun takeDisarms_soTheSamePlaqueCannotStartASecondRound() {
        val target = EncodeTarget(http())
        target.read(one)
        target.arm(one)
        go(target.take(one))
        assertEquals(EncodeTarget.Take.NotArmed, target.take(one))
        target.disarm()
        assertEquals(EncodeTarget.Take.NotArmed, target.take(one))
    }

    /**
     * Opening the WebView (forget) while a read is in flight: the late answer must not become
     * current. RED IF: read stores its answer without the generation check.
     */
    @Test
    fun aReadThatFinishesAfterForget_leavesNothingKnown() {
        lateinit var target: EncodeTarget
        val real = http()
        target = EncodeTarget(object : BusinessSource {
            override fun business(cookieHeader: String): BusinessReply {
                val r = real.business(cookieHeader)
                target.forget() // the person pressed "Change business" meanwhile
                return r
            }
        })
        assertTrue(target.read(one) is BusinessReply.Known)
        assertNull(target.confirmedFor(one))
        assertNull(target.arm(one))
    }

    /** RED IF: forget leaves the name or the arm in place. */
    @Test
    fun forget_dropsTheNameAndTheArm() {
        val target = EncodeTarget(http())
        target.read(one)
        target.arm(one)
        target.forget()
        assertEquals(EncodeTarget.Take.NotArmed, target.take(one))
        assertNull(target.arm(one))
    }

    /** RED IF: a suspended business is armed — every round of it is refused by the server (ADR 0025 §3). */
    @Test
    fun aSuspendedBusiness_isNamedButNeverArmed() {
        panel.businesses["SESSION-IN-BUSINESS-ONE"] = panel.business(nameOne, refOne, suspended = true)
        val target = EncodeTarget(http())
        val r = target.read(one) as BusinessReply.Known
        assertEquals(nameOne, r.name)
        assertTrue(r.suspended)
        assertEquals(r, target.confirmedFor(one))
        assertNull(target.arm(one))
        assertEquals(EncodeTarget.Take.NotArmed, target.take(one))
    }

    /** No session cookie: nothing is asked. RED IF: read sends a request without the panel cookie. */
    @Test
    fun withoutThePanelCookie_nothingIsAsked() {
        val target = EncodeTarget(http())
        assertEquals(BusinessReply.NoSession, target.read(null))
        assertEquals(BusinessReply.NoSession, target.read("other=1; xtappa_admin_session=X"))
        assertTrue(panel.businessReads().isEmpty())
    }

    @Test
    fun panelCookie_findsTheSessionAmongOtherCookies() {
        assertEquals("S1", PanelCookie.sessionOf("a=1; tappa_admin_session=S1; b=2"))
        assertEquals("S1", PanelCookie.sessionOf("tappa_admin_session=S1"))
        assertNull(PanelCookie.sessionOf("xtappa_admin_session=S1"))
        assertNull(PanelCookie.sessionOf("tappa_admin_session="))
        assertNull(PanelCookie.sessionOf(""))
        assertNull(PanelCookie.sessionOf(null))
    }

    // --- end to end -------------------------------------------------------------------

    /**
     * The order on the wire and what the round carries: the business read, THEN the round;
     * the begin names the business ref the read returned, and every request of the round
     * carries the cookie take() pinned — even after the cookie jar has moved on (a jar that
     * changes mid-round is read by nothing). RED IF: the round reads the jar again, or the
     * begin does not carry the ref.
     */
    @Test
    fun theRoundCarriesTheShownBusinessAndThePinnedCookie() {
        var jar: String? = one
        val target = EncodeTarget(HttpRelayServer(panel.base, cookie = { jar }, allowHttp = true))
        target.read(jar)
        target.arm(jar)
        val g = go(target.take(jar))
        jar = two // the jar moves on after the round began

        val relay = HttpRelayServer(panel.base, cookie = { g.cookie }, allowHttp = true, business = g.business.ref)
        assertEquals(Outcome.Completed(11, null), RelayLoop(relay, ScriptedChip(), RelayLoop.Listener.NONE).run())
        assertEquals(HttpRelayServer.BUSINESS_PATH, panel.seen.first().path)
        assertEquals(HttpRelayServer.ENCODE_PATH, panel.seen[1].path)
        assertEquals(refOne, panel.begins().single().form["business"])
        for (s in panel.seen) assertEquals(s.path, listOf(one), s.headers["cookie"])
    }

    /** A client that never begins a round sends no `business`. RED IF: a null ref is sent as text. */
    @Test
    fun aRelayWithoutARef_sendsNoBusinessField() {
        HttpRelayServer(panel.base, cookie = { one }, allowHttp = true).begin()
        assertNull(panel.begins().single().form["business"])
    }
}
