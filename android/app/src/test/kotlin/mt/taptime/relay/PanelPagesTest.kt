package mt.taptime.relay

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * When the panel's WebView closes (2026-11-10). Two halves, the CleartextGateTest shape:
 *   1. the rule, PanelPages.endsVisit — the switch screen is a multi-page flow like the
 *      sign-in, and closing on it ends a switch before the person picked a business;
 *   2. MainActivity.kt's source — the rule is what onPageFinished asks, the reader is armed
 *      only through EncodeTarget, and opening the WebView forgets the business.
 * MUTATIONS: drop the switch clause in endsVisit → 1 red; put the old login-only check back
 * in onPageFinished → 2 red; arm the reader outside EncodeTarget → 2 red.
 */
class PanelPagesTest {
    private val base = "https://taptime.mt"

    @Test
    fun theSwitchScreenAndTheSignInKeepTheWebViewOpen() {
        for (path in listOf(
            "/admin/switch",
            "/admin/switch?tb=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
            "/admin/switch#top",
            "/admin/login",
            "/admin/login/choose",
            "/admin/login?next=1",
        )) {
            assertFalse("$path ended the visit", PanelPages.endsVisit(base, base + path, hasSession = true))
        }
    }

    @Test
    fun anyOtherPanelPageEndsTheVisitOnceASessionExists() {
        for (path in listOf("/admin", "/admin/", "/admin/transactions", "/admin/switchboard", "/admin/account")) {
            assertTrue("$path did not end the visit", PanelPages.endsVisit(base, base + path, hasSession = true))
            assertFalse("$path ended the visit without a session", PanelPages.endsVisit(base, base + path, hasSession = false))
        }
    }

    @Test
    fun aPageOffThePanelsOriginNeverEndsTheVisit() {
        for (url in listOf("about:blank", "https://evil.example/admin", "https://taptime.mt.evil.example/admin", "http://taptime.mt/admin")) {
            assertFalse(url, PanelPages.endsVisit(base, url, hasSession = true))
        }
    }

    // --- MainActivity's wiring, read from its source --------------------------------

    private val activity: String by lazy {
        val f = File("src/main/kotlin/mt/taptime/relay/MainActivity.kt")
        assertTrue("expected ${f.absolutePath}", f.isFile)
        f.readText()
    }

    /** The body of `private fun name(` up to the next member declaration. */
    private fun body(name: String): String {
        val start = activity.indexOf("fun $name(")
        assertTrue("no fun $name in MainActivity", start >= 0)
        val rest = activity.substring(start)
        val end = Regex("""\n    (private |override |@|/\*\*|companion)""").find(rest, 1)?.range?.first ?: rest.length
        return rest.substring(0, end)
    }

    @Test
    fun onPageFinished_closesOnlyByTheRule() {
        val finished = body("onPageFinished")
        assertTrue("onPageFinished must decide through PanelPages.endsVisit", finished.contains("PanelPages.endsVisit("))
        assertEquals(1, Regex("""PanelPages\.endsVisit\(""").findAll(activity).count())
        assertFalse("a hand-written login-only check is back in onPageFinished", finished.contains("LOGIN_PATH"))
        assertTrue("the end of a visit must re-read the business", finished.contains("loadBusiness()"))
    }

    @Test
    fun theReaderIsArmedOnlyThroughEncodeTarget() {
        assertFalse("MainActivity keeps an arming flag of its own", Regex("""\bvar armed\b""").containsMatchIn(activity))
        assertTrue("becomeReady must arm through EncodeTarget.arm", body("becomeReady").contains("target.arm("))
        assertTrue("onTag must ask EncodeTarget.take before a round", body("onTag").contains("target.take("))
        val onTag = body("onTag")
        assertTrue("onTag opens the chip before asking take()", onTag.indexOf("target.take(") < onTag.indexOf("IsoDepChip.open("))
        // enableReader is reached only from becomeReady (after arm) and from onResume.
        val callers = Regex("""\n    (?:private |override )?fun (\w+)\(""").findAll(activity).map { it.groupValues[1] }
            .filter { it != "enableReader" && body(it).contains("enableReader()") }.toSet()
        assertEquals(setOf("onResume", "becomeReady"), callers)
    }

    /**
     * The round is pinned (both audits, 2026-11-10): its client carries the cookie take()
     * checked and the business ref the screen showed, never a fresh read of the cookie jar.
     * A changed session is said, not ignored. RED IF: onTag uses server(), drops the ref, or
     * swallows SessionChanged.
     */
    @Test
    fun theRoundIsPinnedToTheTakenCookieAndTheShownBusiness() {
        val onTag = body("onTag")
        assertFalse("onTag must not build its client from the cookie jar", onTag.contains("server()"))
        assertTrue(onTag.contains("cookie = { go.cookie }"))
        assertTrue(onTag.contains("business = business.ref"))
        assertTrue("RelayLoop must run on the pinned client", onTag.contains("RelayLoop(relay,"))
        val changed = onTag.substringAfter("EncodeTarget.Take.SessionChanged ->").substringBefore("EncodeTarget.Take.NotArmed")
        assertTrue("SessionChanged must update the screen", changed.contains("onSessionChanged()"))
    }

    /**
     * `business-changed` (the server refused a begin naming another business): nothing was
     * written, retrying the same screen is wrong, and the button reads the business again.
     * RED IF: the word gets "Try again", or the FAULT button stops re-reading on it.
     */
    @Test
    fun aChangedBusiness_isReadAgainNotRetried() {
        val o = Outcome.Faulted(0, 409, "business-changed")
        assertTrue(Wording.rechecksTheBusiness(o))
        assertFalse(Wording.retryIsSafe(o))
        assertEquals("Check the business", Wording.primaryLabel(o))
        assertTrue(Wording.forOutcome(o).contains("Nothing was written to the plaque"))
        assertTrue(body("onPrimary").contains("faultRechecks -> loadBusiness()"))
        assertTrue(body("onTag").contains("faultRechecks = Wording.rechecksTheBusiness(outcome)"))
    }

    @Test
    fun openingThePanelForgetsTheBusinessFirst() {
        val open = body("openPanelPage")
        assertTrue("no panel page while a round is busy", open.contains("if (busy.get()) return"))
        assertTrue("the busy check comes first", open.indexOf("busy.get()") < open.indexOf("target.forget()"))
        assertTrue("a fresh visit clears the history on its first page", open.contains("freshVisit = true"))
        assertTrue(body("onPageFinished").contains("clearHistory()"))
        assertTrue(open.contains("target.forget()"))
        assertTrue("forget must come before the page loads", open.indexOf("target.forget()") < open.indexOf("loadUrl("))
        assertTrue(body("openSwitch").contains("HttpRelayServer.SWITCH_PATH"))
        assertEquals("only openPanelPage loads a panel page", 1, Regex("""web\.loadUrl\("\${'$'}base""").findAll(activity).count())
    }
}
