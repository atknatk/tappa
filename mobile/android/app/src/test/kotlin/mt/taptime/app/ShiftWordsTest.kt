package mt.taptime.app

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import mt.taptime.app.staff.ShiftWords
import mt.taptime.app.wire.UsualShift
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element

/**
 * M16-48's usual shift: [G48-d] a night shift (`overnight: true`) says "(next day)" -- in Today's
 * sentence and in Settings' row -- and a day shift never does; [G48-c] Settings' row names the
 * venue when there is one. The words are read from strings.xml itself (no Android runtime here).
 * [G48-a], no log line, is NoLogTest's: src/main writes none at all.
 */
class ShiftWordsTest {
    private val strings: Map<Int, String> by lazy {
        val f = File(System.getProperty("taptime.mainSrc")!!, "res/values/strings.xml")
        val doc = DocumentBuilderFactory.newInstance().apply {
            setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
        }.newDocumentBuilder().parse(f)
        val nodes = doc.getElementsByTagName("string")
        val byName = (0 until nodes.length).map { nodes.item(it) as Element }.associate { it.getAttribute("name") to it.textContent }
        R.string::class.java.fields.filter { byName.containsKey(it.name) }.associate { it.getInt(null) to byName.getValue(it.name) }
    }

    private fun said(line: mt.taptime.app.staff.ClaimWords.Line): String =
        line.args.foldIndexed(strings.getValue(line.id)) { i, s, a -> s.replace("%${i + 1}\$s", a) }

    private val day = UsualShift("09:00", "17:00", false, "St Julians")
    private val night = UsualShift("18:00", "02:00", true, "St Julians")

    @Test
    fun aNightShiftSaysNextDayAndADayShiftDoesNot() {
        assertEquals("Usual shift: 18:00–02:00 (next day) · St Julians", said(ShiftWords.line(night)))
        assertEquals("Usual shift: 09:00–17:00 · St Julians", said(ShiftWords.line(day)))
        assertEquals("(next day)", ShiftWords.nextDay(night)?.let(strings::getValue))
        assertNull(ShiftWords.nextDay(day))
        assertFalse(said(ShiftWords.line(day)).contains("next day"))
    }

    @Test
    fun noHomeVenueNamesNoneAndLeavesNoSeparator() {
        assertEquals("Usual shift: 18:00–02:00 (next day)", said(ShiftWords.line(night.copy(venue = ""))))
        assertEquals("Usual shift: 09:00–17:00", said(ShiftWords.line(day.copy(venue = ""))))
        assertNull(ShiftWords.venue(day.copy(venue = "")))
    }

    @Test
    fun settingsRowHasTheVenueAndTheClocks() {
        assertEquals("St Julians", ShiftWords.venue(night))
        assertEquals("18:00–02:00", ShiftWords.clocks(night))
        assertTrue(ShiftWords.clocks(day).isNotEmpty())
    }
}
