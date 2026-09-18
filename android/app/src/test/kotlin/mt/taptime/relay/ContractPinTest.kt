package mt.taptime.relay

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * The relay's constants are pinned to the SERVER'S SOURCE, read straight from the Go
 * files two directories up. The contract is the server's (task brief: "uymuyorsa
 * uygulamayı düzelt, sunucuyu değil"); a route, a fault word, a step name or the
 * cookie name that changes there turns these red here, in this language.
 *
 * 🔴 UNDER ONE CONDITION, AND IT WAS MISSING IN THE FIRST ROUND: the files read here
 * are declared as INPUTS of the unit-test task in app/build.gradle.kts. Without that
 * declaration Gradle considered the task UP-TO-DATE whatever the Go source said —
 * measured, six drifts, all green on a warm tree. [everyFileThisTestReadsIsATaskInput]
 * keeps the two lists from drifting apart.
 *
 * ⚠️ THE READING IS TEXT, NOT AST, and comment lines are dropped before matching
 * ([goSource]). Named fragility that survives the filter: a code line that carries a
 * `faultX = "..."`-shaped TRAILING comment, or a block comment, would still match.
 * The safe direction — a false red, never a false green.
 *
 * Gradle runs module tests with android/app as the working directory.
 */
class ContractPinTest {
    private val repo = File("../..").canonicalFile

    private fun goSource(path: String): String {
        assertTrue("$path is read but not in PINNED_SOURCES", path in PINNED_SOURCES)
        val f = File(repo, path)
        assertTrue("expected ${f.path} — run these tests from the repository checkout", f.isFile)
        return stripComments(f.readText())
    }

    private fun goStringConst(source: String, name: String): String {
        val m = Regex("""\b$name\s*=\s*"([^"]*)"""").find(source)
            ?: throw AssertionError("no string constant $name in the Go source")
        return m.groupValues[1]
    }

    /** Every path [goSource] may open must be declared as a test-task input in the module build file. */
    @Test
    fun everyFileThisTestReadsIsATaskInput() {
        val build = File("build.gradle.kts")
        assertTrue("expected ${build.absolutePath}", build.isFile)
        val text = build.readText()
        for (path in PINNED_SOURCES) {
            assertTrue(
                "$path is read by ContractPinTest but not declared in app/build.gradle.kts as a test input — " +
                    "on a warm tree a change to it would leave the tests UP-TO-DATE and green",
                text.contains("rootProject.file(\"../$path\")"),
            )
        }
    }

    @Test
    fun theThreeRoutes_matchPlaqueencodeGo() {
        val src = goSource(PLAQUEENCODE_GO)
        assertEquals(goStringConst(src, "plaqueEncodeHref"), HttpRelayServer.ENCODE_PATH)
        assertEquals(goStringConst(src, "plaqueEncodeStepHref"), HttpRelayServer.STEP_PATH)
        assertEquals(goStringConst(src, "plaqueEncodeAbortHref"), HttpRelayServer.ABORT_PATH)
    }

    @Test
    fun theFaultVocabulary_matchesPlaqueencodeGo() {
        val src = goSource(PLAQUEENCODE_GO)
        val words = Regex("""\bfault[A-Z][A-Za-z]*\s*=\s*"([a-z-]+)"""").findAll(src).map { it.groupValues[1] }.toList()
        assertEquals("the server's fault constants", words.toSet(), Wording.FAULT_WORDS.toSet())
        assertEquals(words.size, Wording.FAULT_WORDS.size)
    }

    @Test
    fun theTenStepNames_matchDriverGo() {
        assertEquals(FakePanel.STEPS, stepNames())
        assertEquals("EXPECTED_EXCHANGES is cosmetic but must not lie", stepNames().size, MainActivity.EXPECTED_EXCHANGES)
    }

    /**
     * Wording's two milestones are POSITIONS IN THE SERVER'S TABLE, not numbers
     * somebody remembered: the inventory row is written by the accept of the step
     * whose adr field names "step 3" (getversion.3 today), and the plaque's secret is
     * installed by the accept of changekey.sdmfileread. An exchange counts as
     * completed once its response was posted and accepted, so "after N exchanges"
     * means index N-1 in the table.
     */
    @Test
    fun theTwoMilestones_matchDriverGo() {
        val defs = stepDefs()
        val rowStep = defs.indexOfFirst { it.contains("step 3") }
        val secretStep = defs.indexOfFirst { it.contains("\"changekey.sdmfileread\"") }
        assertTrue("no step whose adr names step 3", rowStep >= 0)
        assertTrue("no changekey.sdmfileread step", secretStep >= 0)
        assertEquals(rowStep + 1, Wording.ROW_WRITTEN_AFTER_EXCHANGES)
        assertEquals(secretStep + 1, Wording.SECRET_INSTALLED_AFTER_EXCHANGES)
    }

    @Test
    fun theTwoFormFields_matchWhatTheHandlersRead() {
        val src = goSource(PLAQUEENCODE_GO)
        val fields = Regex("""PostFormValue\("([a-z]+)"\)""").findAll(src).map { it.groupValues[1] }.toSet()
        assertEquals(setOf("session", "rapdu"), fields)
    }

    @Test
    fun thePanelCookieAndPaths_matchTheServer() {
        assertEquals(goStringConst(goSource(COOKIE_GO), "CookieName"), MainActivity.PANEL_COOKIE)
        assertEquals(goStringConst(goSource(COOKIE_GO), "CookiePath"), HttpRelayServer.PANEL_PATH)
        val login = goSource(ADMINLOGIN_GO)
        assertEquals(goStringConst(login, "adminLoginPath"), HttpRelayServer.LOGIN_PATH)
        assertTrue(login.contains("r.Post(\"${HttpRelayServer.LOGOUT_PATH}\""))
    }

    @Test
    fun theDefaultServer_isTheDeployedBaseURL() {
        val cfg = goSource(CONFIG_YAML)
        val m = Regex("""TAPPA_BASE_URL:\s*"([^"]+)"""").find(cfg) ?: throw AssertionError("no TAPPA_BASE_URL in 05-config.yaml")
        assertEquals(m.groupValues[1], MainActivity.DEFAULT_SERVER)
    }

    /** The roundSteps table, one entry per `{ ... }` literal, comments already stripped. */
    private fun stepDefs(): List<String> {
        val src = goSource(DRIVER_GO)
        val table = src.substringAfter("var roundSteps = []stepDef{").substringBefore("\n}\n")
        return Regex("""\{[^{}]*\}""").findAll(table).map { it.value }.toList()
    }

    private fun stepNames(): List<String> =
        stepDefs().map { def ->
            Regex("""\bname:\s*"([a-z0-9.]+)"""").find(def)?.groupValues?.get(1)
                ?: throw AssertionError("a roundSteps entry without a name: $def")
        }

    companion object {
        const val PLAQUEENCODE_GO = "internal/handler/plaqueencode.go"
        const val DRIVER_GO = "internal/encode/driver.go"
        const val COOKIE_GO = "internal/adminauth/cookie.go"
        const val ADMINLOGIN_GO = "internal/handler/adminlogin.go"
        const val CONFIG_YAML = "deploy/k8s/05-config.yaml"

        /** Every server file this test may read. Mirrored, path for path, in app/build.gradle.kts. */
        val PINNED_SOURCES = listOf(PLAQUEENCODE_GO, DRIVER_GO, COOKIE_GO, ADMINLOGIN_GO, CONFIG_YAML)

        /**
         * Drops whole-line `//` comments and `/* */` blocks so that prose in the Go
         * source cannot satisfy — or, the case that was measured, FAIL — a pin: a
         * comment reading `faultExample = "example-word"` above encodeFaults turned
         * the vocabulary test red (second-round audit, N5). YAML `#` comments are
         * dropped the same way.
         */
        fun stripComments(text: String): String {
            val noBlocks = Regex("""/\*.*?\*/""", RegexOption.DOT_MATCHES_ALL).replace(text, "")
            return noBlocks.lineSequence()
                .filterNot { it.trimStart().startsWith("//") || it.trimStart().startsWith("#") }
                .joinToString("\n")
        }
    }
}
