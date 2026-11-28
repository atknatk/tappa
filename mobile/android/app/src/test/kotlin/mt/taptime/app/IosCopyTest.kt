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
 *
 * M16-47: the staff screens (StaffCopy.swift) and the lock screen and widget (Shared/LiveCopy.swift)
 * are held the same way and in BOTH languages -- values-tr's line must be the Turkish half of the
 * same Swift pair (`%1$d` and `%2$02d` are holes too). LiveCopy is held both ways: each of
 * its literals is one of Android's lines or is named in [liveIosOnly].
 */
class IosCopyTest {
    private fun file(property: String): File? = System.getProperty(property)?.let(::File)?.takeIf { it.isFile }

    private val strings: Map<String, String> by lazy { read("res/values/strings.xml") }
    private val turkish: Map<String, String> by lazy { read("res/values-tr/strings.xml") }

    private fun read(path: String): Map<String, String> {
        val f = File(System.getProperty("taptime.mainSrc")!!, path)
        val doc = DocumentBuilderFactory.newInstance().apply {
            setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
        }.newDocumentBuilder().parse(f)
        val nodes = doc.getElementsByTagName("string")
        return (0 until nodes.length).map { nodes.item(it) as Element }
            .associate { it.getAttribute("name") to it.textContent.replace("\\'", "'").replace("\\\"", "\"") }
    }

    /** A format string's holes (`%1$s`, `%1$d`, `%2$02d`) as Swift's "{}". */
    private fun holes(s: String): String = s.replace(Regex("""%\d\$0?\d*[sd]"""), "{}")

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
            holes(mine) !in ios
        }.map { "$it = \"${strings[it]}\"" }
        assertEquals("tap lines with no character-for-character iOS twin", emptyList<String>(), missing)
        for (key in composed) {
            for (part in strings.getValue(key).split("%1\$s").filter { it.isNotEmpty() }) {
                assertEquals("$key: \"$part\"", true, ios.any { it.contains(part) })
            }
        }
    }

    /** The staff screens' lines with an iOS twin in StaffCopy.swift (the native shell, 2026-11-24). */
    private val staff = listOf(
        "tab_today", "tab_hours", "tab_settings",
        "today_hello", "today_at_work", "today_not_in", "today_in_at", "today_not_in_body", "today_shift_until",
        "today_left", "today_over", "today_tap_out", "today_tap_in", "today_missing_one", "today_missing_many",
        "today_missing_sub", "today_week",
        "hours_counted_label", "hours_duration", "hours_awaiting", "hours_truncated", "hours_day_empty",
        "hours_dir_in", "hours_dir_out", "hours_state_approved", "hours_state_waiting", "hours_state_rejected",
        "hours_state_repeat", "hours_channel_manual", "hours_channel_qr", "hours_channel_qr_live",
        "hours_claim_pending", "hours_claim_approved", "hours_claim_rejected", "hours_prev", "hours_next",
        "hours_more", "hours_off_title", "hours_off_body", "hours_chip_open",
        "claim_lead_out_today", "claim_lead_out_yesterday", "claim_lead_out_earlier",
        "claim_lead_in_today", "claim_lead_in_yesterday", "claim_lead_in_earlier",
        "claim_question_out", "claim_question_in", "claim_send", "claim_sent", "claim_window_hint", "claim_outside",
        "claim_window_after_before", "claim_window_from_before", "claim_window_after_upto", "claim_window_from_upto",
        "claim_expired_title", "claim_tell_manager", "claim_resolved_title", "claim_resolved_message",
        "claim_not_active_title", "claim_still_open_title", "claim_still_open_message",
        "sheet_sending",
        // M16-44: the card's skip line, the outcomes' full lines, and the person's own claims.
        "claim_skip", "claim_expired_message", "claim_resolved_hint", "claim_not_active_message",
        "missing_sent_label", "missing_sent_row", "missing_kind_in", "missing_kind_out",
        "claim_state_pending", "claim_state_approved", "claim_state_approved_at", "claim_state_rejected",
        "settings_connected", "settings_notifications", "settings_remind", "settings_remind_hint",
        "settings_notifications_off", "settings_timer", "settings_shift_label", "settings_usual",
        "settings_account", "settings_language", "settings_language_sub", "settings_signout",
        "signout_title", "signout_body", "signout_confirm", "cancel", "settings_privacy", "settings_version",
        "settings_delete", "settings_delete_sub", "delete_title", "delete_body", "delete_confirm",
        "delete_your_employer",
        // M16-38: the privacy notice's row and the deactivated person's deletion words.
        "settings_privacy_policy", "delete_body_deactivated",
        "remind_title", "remind_body", "remind_body_shift", "remind_snooze",
    )

    /** Built by `+` on iOS ("Waiting for your manager: " + parts + "."): held by its literal parts. */
    private val staffComposed = listOf("hours_waiting_line")

    /** The lock screen's and the widget's lines (Android's timer notification and PresenceWidget). */
    private val live = listOf(
        "timer_title", "timer_shift_ends", "today_at_work", "today_not_in", "today_in_at", "today_tap_in",
        "widget_description", "wordmark", "app_name",
    )

    /**
     * LiveCopy's literals Android has no line for: the stale Live Activity's "Open Taptime to
     * check" (Android's timer notification is simply taken away at showUntil, M16-47 O3), and the
     * language codes the file reads the phone's language and capitalises with.
     */
    private val liveIosOnly = setOf("Open Taptime to check", "Kontrol için Taptime'ı aç", "tr", "en")

    /** Each key's English line and, where values-tr has one, its Turkish line, missing from [ios]. */
    private fun missingIn(ios: Set<String>, keys: List<String>): List<String> = keys.flatMap { key ->
        val en = strings[key] ?: throw AssertionError("strings.xml has no $key")
        listOfNotNull("en" to en, turkish[key]?.let { "tr" to it })
            .filter { (_, line) -> holes(line) !in ios }
            .map { (lang, line) -> "$key [$lang] = \"$line\"" }
    }

    @Test
    fun everyStaffLineIsTheIosAppsLineInBothLanguages() {
        val f = file("taptime.ios.staffCopy")
        assumeTrue("the iOS app's staff copy is not in this checkout", f != null)
        val ios = literals(f!!)
        assertEquals("staff lines with no character-for-character iOS twin", emptyList<String>(), missingIn(ios, staff))
        for (key in staffComposed) {
            for (line in listOfNotNull(strings.getValue(key), turkish[key])) {
                for (part in line.split("%1\$s").filter { it.isNotEmpty() }) {
                    assertEquals("$key: \"$part\"", true, ios.any { it.contains(part) })
                }
            }
        }
    }

    @Test
    fun theLockScreenAndTheWidgetSayWhatAndroidSays() {
        val f = file("taptime.ios.liveCopy")
        assumeTrue("the iOS app's lock-screen copy is not in this checkout", f != null)
        val ios = literals(f!!)
        assertEquals("lock-screen lines with no character-for-character iOS twin", emptyList<String>(), missingIn(ios, live))
        val android = live.flatMap { listOfNotNull(strings[it], turkish[it]) }.map(::holes).toSet()
        assertEquals("LiveCopy lines Android has not got (name them in liveIosOnly)", emptySet<String>(),
            ios - android - liveIosOnly)
    }
}
