package mt.taptime.app

import java.io.File
import mt.taptime.app.activation.ActiveDevice
import mt.taptime.app.activation.ActivationFlow
import mt.taptime.app.activation.KeyBacking
import mt.taptime.app.activation.Problem
import mt.taptime.app.tap.TapFlow
import mt.taptime.app.wire.BindReply
import mt.taptime.app.wire.Brand
import mt.taptime.app.wire.NonceReply
import mt.taptime.app.wire.TapStartReply
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * NO SECRET IN A PRINTED VALUE (security review, 2026-11-18; CLAUDE.md §7). A Kotlin data class
 * prints every field in its generated toString(), so one carrying the plaque URL (the chip's CMAC and
 * counter), the SUN query, the signed tap context, a nonce, the activation challenge or the invite
 * code would print it into any log, crash report or debugger line it reached. The rule is held twice:
 *
 *  * over the source: every `data class` under src/main with a String field so named overrides
 *    toString in its own body;
 *  * over the values: the overrides print none of it.
 */
class RedactionTest {
    private val secretField = Regex("""\b(url|sun|context|nonce|challenge|code|signature)\s*:\s*String\b""")

    @Test
    fun everyDataClassCarryingASecretOverridesToString() {
        val main = File(System.getProperty("taptime.mainSrc") ?: throw AssertionError("taptime.mainSrc is not set"))
        val violations = mutableListOf<String>()
        var seen = 0
        for (f in main.walkTopDown().filter { it.isFile && it.extension == "kt" }) {
            val text = f.readText()
            for (m in Regex("""\bdata class (\w+)\s*(?:<[^>]*>)?\(""").findAll(text)) {
                val params = balanced(text, m.range.last, '(', ')')
                if (!secretField.containsMatchIn(params.second)) continue
                seen++
                // The class body, if any: the first '{' after the header and before the next declaration.
                val rest = text.substring(params.first + 1)
                val header = rest.substringBefore('\n')
                val body = if (header.contains('{')) balanced(text, params.first + 1 + header.indexOf('{'), '{', '}').second else ""
                if (!body.contains("override fun toString")) violations += "${f.name}: ${m.groupValues[1]}"
            }
        }
        assertTrue("CONTROL: data classes with such fields were found ($seen)", seen >= 7)
        assertEquals("data classes printing a secret", emptyList<String>(), violations)
    }

    /** The end index and the text between the bracket at [open] and its match. */
    private fun balanced(s: String, open: Int, o: Char, c: Char): Pair<Int, String> {
        var depth = 0
        for (i in open until s.length) {
            when (s[i]) {
                o -> depth++
                c -> if (--depth == 0) return i to s.substring(open + 1, i)
            }
        }
        throw AssertionError("unbalanced $o at $open")
    }

    @Test
    fun theOverridesPrintNoneOfIt() {
        val sun = "tag=04A1B2C3D4E5F6&ctr=000011&cmac=0011223344556677"
        val facts = TapStartReply("Maria Borg", "St Julians", Brand.TAPTIME, "in", false, "signed-context-XYZ")
        val ready = TapFlow.Start.Ready(ActiveDevice("00000000-0000-4000-8000-000000000001", "a", KeyBacking.TEE, "Maria Borg"),
            JdkSigner(), sun, facts)
        val printed = listOf(
            ready.toString(), facts.toString(),
            BindReply("00000000-0000-4000-8000-000000000002", "CHALLENGE-abc").toString(),
            NonceReply("NONCE-abc", "2026-11-18T12:00:00Z").toString(),
            ActivationFlow.CompleteResult.Rebind("INVITE-CODE-abc", Problem.SERVER).toString(),
        ).joinToString("\n")
        for (secret in listOf("0011223344556677", "cmac", "ctr=", "signed-context-XYZ", "CHALLENGE-abc", "NONCE-abc", "INVITE-CODE-abc")) {
            assertFalse("printed: $secret", printed.contains(secret))
        }
        assertTrue("CONTROL: the redaction marker is there", printed.contains("<redacted>"))
    }
}
