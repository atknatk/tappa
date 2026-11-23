package mt.taptime.app.staff

import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException
import java.util.Locale
import mt.taptime.app.R

// The words of the "Missing times" screen (M16-44): the browser card's sentences
// (internal/handler/claim.go claimCardView) chosen from the server's data -- the kind, the day word
// and the record's clock -- and the window phrase (claimWindowPhrase). The app builds no clock of
// its own: every "HH:MM" is the server's.

object ClaimWords {
    /** The card's first sentence: a string id and its arguments (the record's clock, maybe a date). */
    data class Line(val id: Int, val args: List<String>)

    fun lead(kind: String, day: String, anchorDate: String, anchorAt: String, locale: Locale): Line = when (kind) {
        "in" -> when (day) {
            "today" -> Line(R.string.claim_lead_in_today, listOf(anchorAt))
            "yesterday" -> Line(R.string.claim_lead_in_yesterday, listOf(anchorAt))
            else -> Line(R.string.claim_lead_in_earlier, listOf(date(anchorDate, locale), anchorAt))
        }
        else -> when (day) {
            "today" -> Line(R.string.claim_lead_out_today, listOf(anchorAt))
            "yesterday" -> Line(R.string.claim_lead_out_yesterday, listOf(anchorAt))
            else -> Line(R.string.claim_lead_out_earlier, listOf(date(anchorDate, locale), anchorAt))
        }
    }

    fun question(kind: String): Int = if (kind == "in") R.string.claim_question_in else R.string.claim_question_out

    /** claimWindowPhrase: an excluded end "after" / "before", an included one "from" / "up to". */
    fun window(fromInclusive: Boolean, toInclusive: Boolean): Int = when {
        fromInclusive && toInclusive -> R.string.claim_window_from_upto
        fromInclusive -> R.string.claim_window_from_before
        toInclusive -> R.string.claim_window_after_upto
        else -> R.string.claim_window_after_before
    }

    /** Go's "Mon 2 Jan" in English; the phone's language otherwise (M16-49 (7)). */
    fun date(anchorDate: String, locale: Locale): String = try {
        LocalDate.parse(anchorDate).format(DateTimeFormatter.ofPattern("EEE d MMM", locale))
    } catch (_: DateTimeParseException) {
        anchorDate
    }

    /** A sent claim's state line. */
    fun state(state: String, approvedAt: String?): Line = when (state) {
        "approved" -> if (approvedAt != null) Line(R.string.claim_state_approved_at, listOf(approvedAt)) else Line(R.string.claim_state_approved, emptyList())
        "rejected" -> Line(R.string.claim_state_rejected, emptyList())
        else -> Line(R.string.claim_state_pending, emptyList())
    }
}
