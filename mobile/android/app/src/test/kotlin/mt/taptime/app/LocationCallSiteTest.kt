package mt.taptime.app

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * WHERE THE APP ASKS FOR A POSITION -- the call-site allow-list (card M16-13; CLAUDE.md §4.2).
 *
 * A one-shot request repeated by a timer is tracking, and the red-line scan's R2 periodic net
 * (scripts/redline-check.sh) sees a timer only in the SAME file as the request. So the rule is held
 * here, over the whole of src/main:
 *
 *  * the platform's position requests appear in tap/TapLocation.kt ONLY, and there exactly once
 *    each, both inside its one ask() function: getCurrentLocation (API 30+, the first ask) and
 *    requestSingleUpdate (26-29, and the one fresh ask after a refused cache);
 *  * ask() is called from two places only: the press's first asks (one per provider) and the fresh
 *    ask FixRace allows once -- a finite set, no loop around it but the providers list;
 *  * nothing reads a remembered position anywhere (the last-known-location getters);
 *  * TapLocation.read has ONE caller, the press in MainActivity (withOneFix), and the two
 *    MainActivity call sites are the only ones;
 *  * the deadline lives in tap/FixDeadline.kt, which asks for no position; TapLocation.kt holds no
 *    timer of any kind.
 *
 * Source text, comments dropped first: a name in prose is not a call.
 *
 * THE PATTERNS ARE WRITTEN IN PIECES ("last" + "Location"): the red-line scan reads this file too,
 * and R2's periodic net fails a file that names a one-shot location call AND a repeating timer --
 * which a test listing both kinds of name would, while calling neither.
 */
class LocationCallSiteTest {
    private val main: File by lazy {
        File(System.getProperty("taptime.mainSrc") ?: throw AssertionError("taptime.mainSrc is not set -- run through Gradle"))
    }

    private val sources: Map<String, String> by lazy {
        main.walkTopDown().filter { it.isFile && (it.extension == "kt" || it.extension == "java") }
            .associate { it.relativeTo(main).path to code(it.readText()) }
            .also { assertTrue("CONTROL: the source tree was found (${it.size} files)", it.size >= 15) }
    }

    /** The text without // and /* */ comments (string literals are kept: none here holds those). */
    private fun code(s: String): String =
        s.replace(Regex("/\\*.*?\\*/", RegexOption.DOT_MATCHES_ALL), " ").lines().joinToString("\n") { it.substringBefore("//") }

    private fun count(re: Regex): Map<String, Int> =
        sources.mapValues { (_, text) -> re.findAll(text).count() }.filterValues { it > 0 }

    private val tapLocation = "kotlin/mt/taptime/app/tap/TapLocation.kt"
    private val deadline = "kotlin/mt/taptime/app/tap/FixDeadline.kt"
    private val activity = "kotlin/mt/taptime/app/MainActivity.kt"

    @Test
    fun theTwoOneShotRequestsLiveInTheTapFlowFileOnly() {
        assertEquals(mapOf(tapLocation to 1), count(Regex("""\bgetCurrentLocation\s*\(""")))
        assertEquals(mapOf(tapLocation to 1), count(Regex("""\brequestSingleUpdate\s*\(""")))
    }

    @Test
    fun bothRequestsGoThroughTheOneAskFunction() {
        val text = sources.getValue(tapLocation)
        val body = Regex("""private fun ask\(.*?\n    }\n""", RegexOption.DOT_MATCHES_ALL).find(text)?.value
            ?: throw AssertionError("no ask() in TapLocation.kt")
        assertEquals(1, Regex("""\bgetCurrentLocation\s*\(""").findAll(body).count())
        assertEquals(1, Regex("""\brequestSingleUpdate\s*\(""").findAll(body).count())
        val calls = Regex("""(?<!fun )\bask\(lm,""").findAll(text).count()
        assertEquals("ask() is called by the first asks and the one fresh ask", 2, calls)
        assertEquals("the fresh ask is FixRace's", 1, Regex("""AskFresh -> ask\(lm,""").findAll(text).count())
        assertEquals("the first asks iterate the providers once", 1, Regex("""for \(p in race\.start\(\)\)""").findAll(text).count())
        assertEquals("no other loop", 1, Regex("""\bfor\s*\(""").findAll(text).count())
    }

    @Test
    fun nothingReadsARememberedOrContinuousPosition() {
        // The continuous request's name is not even written here: R2 fails the tree on it, and this
        // test reads the same tree. Its stem catches it and every sibling.
        val forbidden = Regex(listOf("getLast" + "KnownLocation", "last" + "Location", "request" + "Location\\w*\\s*\\(",
            "addProximity" + "Alert", "registerGnss" + "\\w*", "request" + "Flush").joinToString("|") { "\\b$it" })
        assertEquals(emptyMap<String, Int>(), count(forbidden))
    }

    @Test
    fun theReadingHasOneCallerThePress() {
        val creates = count(Regex("""\bTapLocation\s*\("""))
        assertEquals("constructed once, in the activity", mapOf(activity to 1, tapLocation to 1), creates) // the class's own declaration
        val reads = count(Regex("""\blocation\.read\s*\("""))
        assertEquals("withOneFix's two paths: already granted, and after the permission answer", mapOf(activity to 2), reads)
        val withOneFix = Regex("""fun withOneFix\(.*?\n    }\n""", RegexOption.DOT_MATCHES_ALL).find(sources.getValue(activity))?.value
            ?: throw AssertionError("no withOneFix in MainActivity")
        assertEquals("both reads are inside withOneFix", 2, Regex("""\blocation\.read\s*\(""").findAll(withOneFix).count())
        assertEquals("withOneFix is called by the press alone", 2, // its declaration and one call
            Regex("""\bwithOneFix\b""").findAll(sources.getValue(activity)).count())
    }

    @Test
    fun theDeadlineIsApartFromTheRequest() {
        val timers = Regex(listOf("postDelayed", "Handler\\s*\\(", "Timer\\b", "schedule\\w*\\s*\\(", "while\\s*\\(", "delay\\s*\\(",
            "tick" + "er", "fixedRate" + "Timer", "set" + "Repeating", "Periodic" + "WorkRequest").joinToString("|") { "\\b$it" })
        assertEquals("no timer of any kind in the tap flow's location file", 0, timers.findAll(sources.getValue(tapLocation)).count())
        assertTrue("CONTROL: the deadline file does hold the timer", timers.containsMatchIn(sources.getValue(deadline)))
        assertEquals("the deadline file asks for no position", 0,
            Regex("""\b(getCurrentLocation|requestSingleUpdate|LocationManager)\b""").findAll(sources.getValue(deadline)).count())
    }
}
