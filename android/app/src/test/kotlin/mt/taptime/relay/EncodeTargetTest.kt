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
    private val factory = "tappa_admin_session=SESSION-IN-KEBAB-FACTORY"
    private val manufacturing = "tappa_admin_session=SESSION-IN-KEBAB-MANUFACTURING"

    @Before
    fun up() {
        panel = FakePanel()
        panel.businesses["SESSION-IN-KEBAB-FACTORY"] = panel.business("Kebab Factory Ltd.")
        panel.businesses["SESSION-IN-KEBAB-MANUFACTURING"] = panel.business("Kebab Manufacturing Co Ltd.")
    }

    @After
    fun down() {
        panel.close()
    }

    private fun http(cookie: String = factory): HttpRelayServer = HttpRelayServer(panel.base, cookie = { cookie }, allowHttp = true)

    // --- the wire -------------------------------------------------------------------

    /**
     * A GET, with Origin and exactly the cookie it was handed, and no Sec-Fetch-Site (GATE-1:
     * the server refuses same-site / cross-site even beside a matching Origin). RED IF: the
     * read drops Origin, sends another cookie, follows a redirect, or uses POST.
     */
    @Test
    fun businessRead_isAGetWithOriginAndTheExactCookie() {
        val r = http(cookie = "ignored=1").business(factory)
        assertEquals(BusinessReply.Known("Kebab Factory Ltd.", canSwitch = true, suspended = false), r)
        val seen = panel.businessReads().single()
        assertEquals("GET", seen.method)
        assertEquals(listOf(panel.base), seen.headers["origin"])
        assertEquals(listOf(factory), seen.headers["cookie"])
        assertNull(seen.headers["sec-fetch-site"])
    }

    /** RED IF: a 303 is followed (it would parse a sign-in page) or read as anything but a refusal. */
    @Test
    fun businessRead_aDeadSessionIsA303Refusal() {
        assertEquals(BusinessReply.Refused(303), http().business("tappa_admin_session=REVOKED"))
    }

    @Test
    fun businessRead_faultWordsAndBadShapesAreNamed() {
        panel.businesses["SESSION-IN-KEBAB-FACTORY"] = panel.fault(503, "encode-unavailable")
        assertEquals(BusinessReply.Fault(503, "encode-unavailable"), http().business(factory))

        panel.businesses["SESSION-IN-KEBAB-FACTORY"] = panel.json(200, """{"business":"Kebab Factory Ltd.","switch":true}""")
        assertTrue(http().business(factory) is BusinessReply.Malformed)

        val closed = ServerSocket(0).use { it.localPort }
        val r = HttpRelayServer("http://127.0.0.1:$closed", cookie = { null }, allowHttp = true).business(factory)
        assertTrue("$r", r is BusinessReply.Unreachable)
    }

    /** The parser accepts the three fields with their types and nothing less. RED IF: a missing or mistyped field is accepted. */
    @Test
    fun parse_requiresTheThreeFieldsWithTheirTypes() {
        val ct = "application/json; charset=utf-8"
        assertEquals(
            BusinessReply.Known("Ħaġar <Qim>", canSwitch = false, suspended = true),
            BusinessReply.parse(200, ct, """{"business":"Ħaġar <Qim>","switch":false,"suspended":true}"""),
        )
        for (bad in listOf(
            """{"business":"","switch":true,"suspended":false}""",
            """{"business":"X","switch":"true","suspended":false}""",
            """{"business":"X","suspended":false}""",
            """{"business":7,"switch":true,"suspended":false}""",
            """{"switch":true,"suspended":false}""",
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
        assertNull(target.arm(factory))
        assertNull(target.take(factory))
        assertTrue(panel.businessReads().isEmpty())

        assertEquals("Kebab Factory Ltd.", (target.read(factory) as BusinessReply.Known).name)
        assertEquals("Kebab Factory Ltd.", target.arm(factory)?.name)
        assertEquals("Kebab Factory Ltd.", target.take(factory)?.name)
    }

    /**
     * A SWITCH ISSUES A NEW COOKIE, AND THE OLD NAME MUST NOT ARM UNDER IT — the defect this
     * change closes (prod: two businesses, last-used = Kebab Factory). RED IF: arm / take stop
     * comparing the session the name was read under.
     */
    @Test
    fun afterASwitch_theOldNameNeverArms_untilTheNewOneIsRead() {
        val target = EncodeTarget(http())
        target.read(factory)
        assertEquals("Kebab Factory Ltd.", target.arm(factory)?.name)

        assertNull("the factory's name armed under the manufacturing session", target.arm(manufacturing))
        assertNull(target.take(manufacturing))

        target.read(manufacturing)
        assertEquals("Kebab Manufacturing Co Ltd.", target.arm(manufacturing)?.name)
        assertNull("the manufacturing name answered for the old session", target.confirmedFor(factory))
    }

    /** RED IF: take re-checks only `armed` and not the session — a cookie change between arm and tap. */
    @Test
    fun aCookieChangeBetweenArmAndTap_startsNoRound() {
        val target = EncodeTarget(http())
        target.read(factory)
        target.arm(factory)
        assertNull(target.take(manufacturing))
    }

    /** RED IF: take does not disarm — the same plaque left on the phone would start a second round. */
    @Test
    fun takeDisarms_soTheSamePlaqueCannotStartASecondRound() {
        val target = EncodeTarget(http())
        target.read(factory)
        target.arm(factory)
        assertEquals("Kebab Factory Ltd.", target.take(factory)?.name)
        assertNull(target.take(factory))
        target.disarm()
        assertNull(target.take(factory))
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
        assertTrue(target.read(factory) is BusinessReply.Known)
        assertNull(target.confirmedFor(factory))
        assertNull(target.arm(factory))
    }

    /** RED IF: forget leaves the name or the arm in place. */
    @Test
    fun forget_dropsTheNameAndTheArm() {
        val target = EncodeTarget(http())
        target.read(factory)
        target.arm(factory)
        target.forget()
        assertNull(target.take(factory))
        assertNull(target.arm(factory))
    }

    /** RED IF: a suspended business is armed — every round of it is refused by the server (ADR 0025 §3). */
    @Test
    fun aSuspendedBusiness_isNamedButNeverArmed() {
        panel.businesses["SESSION-IN-KEBAB-FACTORY"] = panel.business("Kebab Factory Ltd.", suspended = true)
        val target = EncodeTarget(http())
        val r = target.read(factory) as BusinessReply.Known
        assertEquals("Kebab Factory Ltd.", r.name)
        assertTrue(r.suspended)
        assertEquals(r, target.confirmedFor(factory))
        assertNull(target.arm(factory))
        assertNull(target.take(factory))
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
     * The order on the wire: the business read, THEN the round — and the round carries the
     * session the name was read under. RED IF: the round can start without the read (the
     * activity's onTag asks take(), which answers only after read + arm).
     */
    @Test
    fun theBusinessIsReadBeforeTheRoundBegins_andTheRoundCarriesThatSession() {
        val relay = http(cookie = factory)
        val target = EncodeTarget(relay)
        target.read(factory)
        target.arm(factory)
        val business = target.take(factory)
        assertEquals("Kebab Factory Ltd.", business?.name)

        assertEquals(Outcome.Completed(11, null), RelayLoop(relay, ScriptedChip(), RelayLoop.Listener.NONE).run())
        assertEquals(HttpRelayServer.BUSINESS_PATH, panel.seen.first().path)
        assertEquals(HttpRelayServer.ENCODE_PATH, panel.seen[1].path)
        for (s in panel.seen) assertEquals(s.path, listOf(factory), s.headers["cookie"])
    }
}
