package mt.taptime.app

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * WHAT THE APP LOGS: NOTHING (card M16-13; CLAUDE.md §7 -- no coordinate, signature, nonce, token,
 * SUN query or invite code in a log line). The simplest line that cannot leak is the one that is not
 * written, so the inventory of log calls under src/main is pinned at zero; adding one is a deliberate
 * act that turns this red, and the second test then holds the new line's arguments.
 *
 * The callee list is the red-line scan's R7M (scripts/redline-check.sh) for Kotlin and Java, plus
 * printStackTrace. Comments are dropped first.
 */
class NoLogTest {
    private val sources: Map<String, String> by lazy {
        val main = File(System.getProperty("taptime.mainSrc") ?: throw AssertionError("taptime.mainSrc is not set -- run through Gradle"))
        main.walkTopDown().filter { it.isFile && (it.extension == "kt" || it.extension == "java") }
            .associate { f ->
                f.relativeTo(main).path to f.readText().replace(Regex("/\\*.*?\\*/", RegexOption.DOT_MATCHES_ALL), " ")
                    .lines().joinToString("\n") { it.substringBefore("//") }
            }
    }

    private val call = Regex(
        """(?:^|[^.\w])(?:print|println)\s*\(|\bSystem\.(?:out|err)\.\w+|\b(?:Log|Timber|Napier)\.\w+\s*\(|""" +
            """\b\w*(?:log|Log|logger|Logger)\.(?:trace|debug|info|warn|warning|error|v|d|i|w|e|wtf|println)\s*\(|\.printStackTrace\s*\(""",
    )

    private fun calls(): List<String> = sources.flatMap { (path, text) ->
        text.lines().mapIndexedNotNull { i, line -> if (call.containsMatchIn(line)) "$path:${i + 1}: ${line.trim()}" else null }
    }

    @Test
    fun theAppWritesNoLogLine() {
        assertTrue("CONTROL: the source tree was found", sources.size >= 15)
        assertTrue("CONTROL: the pattern sees a log call", call.containsMatchIn("""android.util.Log.d("x", "y")"""))
        assertEquals("log calls under src/main", emptyList<String>(), calls())
        assertEquals("android.util.Log is not even imported", emptyList<String>(),
            sources.filterValues { it.contains("import android.util.Log") }.keys.toList())
    }

    @Test
    fun aLogLineWouldCarryNoCoordinateSignatureOrNonce() {
        // Holds a future log line (the inventory above must be raised first): none of these names in
        // the call's line.
        val forbidden = Regex("""(?i)\b(lat|lng|latitude|longitude|fix|location|signature|sig|sign|nonce|challenge|context|sun|cmac|code|token)\b""")
        val leaking = calls().filter { forbidden.containsMatchIn(it.substringAfter(": ")) }
        assertEquals(emptyList<String>(), leaking)
    }
}
