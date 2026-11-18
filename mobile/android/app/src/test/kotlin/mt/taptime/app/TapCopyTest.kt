package mt.taptime.app

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import mt.taptime.app.tap.ResultCopy
import mt.taptime.app.tap.ResultCopy.Line
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element

/**
 * THE TAP SCREENS SPEAK THE BROWSER'S WORDS (card M16-13; ADR 0045 §2.1: a difference is a §9
 * question). Every tap, result and tap-problem string in strings.xml is one of three kinds, and
 * every one is classified here:
 *
 *	verbatim   found, word for word, in the browser's source: internal/handler/tap.go's problem
 *	           texts, web/templates/pages/tap.templ, result.templ, view.go, web/static/js/tap.js
 *	           (whitespace collapsed -- templ text runs over lines). A format string's literal parts
 *	           are found one by one.
 *	adapted    the browser's sentence with "page" changed to the app's word; the original is found
 *	           in the browser's source and the app's text is pinned here.
 *	app-only   no browser twin (no answer from the network; the app's own exit), pinned here.
 *
 * A browser edit to one of these sentences turns this red until the app follows (or the line is
 * reclassified, which is a §9 question).
 */
class TapCopyTest {
    private val strings: Map<String, String> by lazy {
        val f = File(System.getProperty("taptime.mainSrc") ?: throw AssertionError("taptime.mainSrc is not set -- run through Gradle"),
            "res/values/strings.xml")
        val doc = DocumentBuilderFactory.newInstance().apply {
            setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
        }.newDocumentBuilder().parse(f)
        val nodes = doc.getElementsByTagName("string")
        (0 until nodes.length).map { nodes.item(it) as Element }
            .associate { it.getAttribute("name") to it.textContent.replace("\\'", "'").replace("\\\"", "\"") }
    }

    private val browser: String by lazy {
        listOf("taptime.go.tap", "taptime.templ.tap", "taptime.templ.result", "taptime.go.view", "taptime.js.tap")
            .joinToString("\n") { serverFile(it).readText() }
            .replace(Regex("\\s+"), " ")
    }

    private val verbatim = listOf(
        "tap_tapping", "tap_hello", "tap_clock_in", "tap_clock_out", "tap_tap", "tap_not_right", "tap_pending",
        "result_clocked_in", "result_clocked_out", "result_tapped", "result_already", "result_not_counted", "result_practice",
        "brand_restaurant_in", "brand_restaurant_out", "brand_production_in", "brand_production_out",
        "brand_other_in", "brand_other_out", "brand_no_direction",
        "result_ignored", "result_rejected",
        "tp_bad_link_title", "tp_bad_link_hint",
        "tp_unknown_title", "tp_unknown_message", "tp_unknown_hint",
        "tp_server_title", "tp_server_message", "tp_server_hint",
        "tp_too_many_title", "tp_too_many_message", "tp_too_many_hint",
        "tp_stale_title", "tp_stale_hint",
        "tp_foreign_title", "tp_foreign_message", "tp_foreign_hint",
        "try_again",
    )

    /** key -> (the browser's original, the app's text). */
    private val adapted = mapOf(
        "result_all_done" to ("All done — you can close this page." to "All done — you can close the app."),
        "result_close" to ("You can close this page." to "You can close the app."),
        "tp_bad_link_message" to ("This page opens when you hold your phone to the plaque at the door." to
            "Taptime opens when you hold your phone to the plaque at the door."),
        "tp_stale_message" to ("Nothing was recorded. Tap pages are only good for a few minutes." to
            "Nothing was recorded. Tap screens are only good for a few minutes."),
    )

    private val appOnly = mapOf(
        "tp_unreachable_title" to "Can't reach Taptime",
        "tp_unreachable_message" to "Nothing was recorded.",
        "tp_unreachable_hint" to "Check your connection and tap the plaque again.",
        "tp_lost_title" to "Can't reach Taptime",
        "tp_lost_message" to "We couldn't confirm this tap, so it may not have been recorded.",
        "tp_lost_hint" to "Check your connection and tap the plaque again. Tell your manager if that looks wrong.",
        "open_in_browser" to "Open in browser",
        "p_not_set_up_title" to "This app isn't set up yet",
        "p_not_set_up_message" to "This app hasn't been set up for Taptime yet, so it can't record this tap.",
        "p_not_set_up_hint" to "Open the tap in your browser instead, or set up the app with the link your workplace sent you.",
    )

    @Test
    fun theVerbatimLinesAreTheBrowsersWords() {
        for (key in verbatim) {
            val text = strings[key] ?: throw AssertionError("strings.xml has no $key")
            val parts = text.split("%1\$s").map { it }.filter { it.isNotBlank() }
            assertTrue("$key has words", parts.isNotEmpty())
            for (p in parts) assertTrue("$key: \"$p\" is not in the browser's source", browser.contains(p))
        }
    }

    @Test
    fun theAdaptedLinesChangeOnlyThePageWord() {
        for ((key, pair) in adapted) {
            val (original, app) = pair
            assertTrue("$key: the browser's original \"$original\" is gone -- the app must follow", browser.contains(original))
            assertEquals(key, app, strings[key])
        }
    }

    @Test
    fun theAppOnlyLinesArePinned() {
        for ((key, text) in appOnly) assertEquals(key, text, strings[key])
    }

    @Test
    fun everyTapLineIsClassified() {
        val tapKeys = strings.keys.filter {
            it.startsWith("tap_tapping") || it.startsWith("tap_hello") || it.startsWith("tap_clock") || it == "tap_tap" ||
                it.startsWith("tap_not_right") || it.startsWith("tap_pending") ||
                it.startsWith("result_") || it.startsWith("brand_") || it.startsWith("tp_") || it == "try_again" ||
                it == "open_in_browser" || it.startsWith("p_not_set_up")
        }.toSet()
        assertEquals(tapKeys, (verbatim + adapted.keys + appOnly.keys).toSet())
    }

    @Test
    fun everyResultLineHasItsString() {
        for (l in Line.entries) assertTrue(l.key, strings.containsKey(l.key))
    }

    // The choice of line is view.go's Headline and result.templ's closing, verdict by verdict.
    @Test
    fun theResultSaysWhatTheBrowserSaysForEveryVerdict() {
        fun headline(v: String, d: String) = strings.getValue(ResultCopy.headline(v, d).key)
        fun closing(v: String, d: String, t: String, practice: Boolean = false) =
            ResultCopy.closing(v, d, t, practice).map { strings.getValue(it.key) }

        assertEquals("Clocked in", headline("ok", "in"))
        assertEquals("Clocked out", headline("flag", "out"))
        assertEquals("Tapped", headline("ok", ""))
        assertEquals("Already tapped", headline("ignored", "in"))
        assertEquals("Not counted", headline("reject", ""))
        assertEquals("Tapped", headline("something-new", "in"))

        assertEquals(listOf("Have a great shift — keep those kebabs rolling! 🌯", "All done — you can close the app."), closing("ok", "in", "restaurant"))
        assertEquals(listOf("Shift complete. Thank you for your work today! 👋", "All done — you can close the app."), closing("ok", "out", "production"))
        assertEquals(listOf("Thanks for today. See you next time! 👋", "All done — you can close the app."), closing("ok", "out", "hotel"))
        assertEquals(listOf("Thanks — have a good one!", "All done — you can close the app."), closing("ok", "", "restaurant"))
        assertEquals("no send-off on a practice tap", listOf("All done — you can close the app."), closing("ok", "in", "restaurant", practice = true))
        assertEquals("flag says nothing about approval", listOf("You can close the app."), closing("flag", "in", "restaurant"))
        assertEquals(listOf(strings.getValue("result_ignored"), "You can close the app."), closing("ignored", "in", "restaurant"))
        assertEquals(listOf(strings.getValue("result_rejected"), "You can close the app."), closing("reject", "", "restaurant"))
        assertEquals("an unknown verdict is reject's words", closing("reject", "", ""), closing("unheard-of", "", ""))
    }
}
