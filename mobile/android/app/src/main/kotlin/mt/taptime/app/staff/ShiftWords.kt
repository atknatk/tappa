package mt.taptime.app.staff

import mt.taptime.app.R
import mt.taptime.app.wire.UsualShift

// The usual shift's words (M16-48): the server's own "HH:MM" clocks and venue, put in a sentence.
// The app computes no clock and no zone (§6); a night shift is the server's `overnight`.

object ShiftWords {
    /**
     * "Usual shift: 09:00–17:00 · <venue>"; a night shift says "(next day)" after its end. A person
     * with no home venue (K-14) gets no venue and no separator.
     */
    fun line(s: UsualShift): ClaimWords.Line = when {
        s.venue.isEmpty() && s.overnight -> ClaimWords.Line(R.string.shift_usual_overnight_no_venue, listOf(s.start, s.end))
        s.venue.isEmpty() -> ClaimWords.Line(R.string.shift_usual_no_venue, listOf(s.start, s.end))
        s.overnight -> ClaimWords.Line(R.string.shift_usual_overnight, listOf(s.start, s.end, s.venue))
        else -> ClaimWords.Line(R.string.shift_usual, listOf(s.start, s.end, s.venue))
    }

    /** Settings' row: the two clocks, as the row's end. */
    fun clocks(s: UsualShift): String = "${s.start}–${s.end}"

    /** Settings' row: "(next day)" under the clocks, for a night shift only. */
    fun nextDay(s: UsualShift): Int? = if (s.overnight) R.string.shift_next_day else null

    /** Settings' row: the venue under its title, when the person has a home venue. */
    fun venue(s: UsualShift): String? = s.venue.takeIf { it.isNotEmpty() }
}
