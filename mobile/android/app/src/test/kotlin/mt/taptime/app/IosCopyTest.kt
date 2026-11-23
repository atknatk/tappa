package mt.taptime.app

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.w3c.dom.Element

/**
 * THE TWO APPS SAY THE SAME THING (coordinator decision, 2026-11-18): every tap-screen line of
 * strings.xml that has an iOS twin is, character for character, a string literal of the iOS app's
 * mobile/ios/Taptime/TapCopy.swift or ActivationCopy.swift -- "iPhone" read as "phone" (the one
 * allowed platform-word difference). A format string's `%1$s` and Swift's `\(name)` are the same
 * hole, and a line Swift builds by `+` is held by its literal parts.
 *
 * SKIPPED, NOT PASSED, when the Swift files are not in this checkout (build.gradle.kts hands them
 * over only when present): the two apps are built on separate branches until they merge.
 */
class IosCopyTest {
    private fun file(property: String): File? = System.getProperty(property)?.let(::File)?.takeIf { it.isFile }

    private val strings: Map<String, String> by lazy {
        val f = File(System.getProperty("taptime.mainSrc")!!, "res/values/strings.xml")
        val doc = DocumentBuilderFactory.newInstance().apply {
            setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
        }.newDocumentBuilder().parse(f)
        val nodes = doc.getElementsByTagName("string")
        (0 until nodes.length).map { nodes.item(it) as Element }
            .associate { it.getAttribute("name") to it.textContent.replace("\\'", "'").replace("\\\"", "\"") }
    }

    /** Every Swift string literal, interpolation as "{}", "iPhone" as "phone". */
    private fun literals(vararg files: File): Set<String> = files.flatMap { f ->
        Regex(""""((?:[^"\\\n]|\\.)*)"""").findAll(f.readText()).map {
            it.groupValues[1].replace(Regex("""\\\([^)]*\)"""), "{}").replace("\\\"", "\"").replace("iPhone", "phone")
        }
    }.toSet()

    /** The tap screens' lines that have an iOS twin (all of them but the Android-only one below). */
    private val shared = listOf(
        "tap_tapping", "tap_hello", "tap_clock_in", "tap_clock_out", "tap_tap", "tap_pending",
        "result_clocked_in", "result_clocked_out", "result_tapped", "result_already", "result_not_counted",
        "result_practice", "result_all_done", "result_close", "result_ignored", "result_rejected",
        "brand_restaurant_in", "brand_restaurant_out", "brand_production_in", "brand_production_out",
        "brand_other_in", "brand_other_out", "brand_no_direction",
        "tp_bad_link_title", "tp_bad_link_message", "tp_bad_link_hint",
        "tp_unknown_title", "tp_unknown_message", "tp_unknown_hint",
        "tp_server_title", "tp_server_message", "tp_server_hint",
        "tp_too_many_title", "tp_too_many_message", "tp_too_many_hint",
        "tp_stale_title", "tp_stale_message", "tp_stale_hint",
        "tp_foreign_title", "tp_foreign_message", "tp_foreign_hint",
        "tp_unreachable_title", "tp_unreachable_message", "tp_unreachable_hint",
        "tp_lost_title", "tp_lost_message", "tp_lost_hint",
        "p_not_set_up_title", "p_not_set_up_message", "p_not_set_up_hint",
        "p_clock_title", "p_clock_message", "p_clock_hint",
        "p_signed_out_title", "p_signed_out_message", "p_signed_out_hint",
        "p_key_unreadable_title", "p_key_unreadable_message", "p_key_unreadable_hint",
        "try_again", "start_again", "open_in_browser", "code_title",
        // M16-23: the phone that cannot tap -- step 3, the live QR refusal, the waiting screen and its
        // ends, the reader notices (activation and activated), the live QR tap problems.
        "cant_tap", "scan_screen_line", "back",
        "p_live_failed_title", "p_live_failed_message", "p_live_failed_hint",
        "approval_label", "approval_title", "approval_body", "approval_status", "approval_footnote",
        "approval_ask_again", "approval_new_code",
        "p_rejected_title", "p_rejected_message", "p_rejected_hint",
        "p_expired_title", "p_expired_message", "p_expired_hint",
        "p_screen_available_title", "p_screen_available_message", "p_screen_available_hint",
        "p_no_reader_title", "p_no_reader_message", "p_no_reader_hint",
        "p_no_nfc_title", "p_no_nfc_message", "p_no_nfc_hint",
        "tp_live_server_hint", "tp_live_too_many_hint", "tp_live_stale_hint",
        "tp_live_foreign_title", "tp_live_foreign_message", "tp_live_foreign_hint",
    )

    /** Built by `+` on iOS ("Not right? " + button(o)): held by its literal parts. */
    private val composed = listOf("tap_not_right")

    @Test
    fun everySharedTapLineIsTheIosAppsLine() {
        val tap = file("taptime.ios.tapCopy")
        val activation = file("taptime.ios.activationCopy")
        assumeTrue("the iOS app's copy is not in this checkout", tap != null && activation != null)
        val ios = literals(tap!!, activation!!)
        val missing = shared.filter { key ->
            val mine = strings[key] ?: throw AssertionError("strings.xml has no $key")
            mine.replace("%1\$s", "{}") !in ios
        }.map { "$it = \"${strings[it]}\"" }
        assertEquals("tap lines with no character-for-character iOS twin", emptyList<String>(), missing)
        for (key in composed) {
            for (part in strings.getValue(key).split("%1\$s").filter { it.isNotEmpty() }) {
                assertEquals("$key: \"$part\"", true, ios.any { it.contains(part) })
            }
        }
    }
}
