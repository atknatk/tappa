package mt.taptime.relay

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * The cleartext gate (security audit, F4). Before it, `http://` was accepted by
 * normaliseBase and refused only by the platform — targetSdk 36 defaults
 * usesCleartextTraffic to false and the debug manifest overlay turns it on — so a
 * release build was fail-closed for a reason this code did not own. Now the address
 * parser refuses it unless the caller says otherwise, and the ONLY caller that says
 * otherwise in the app passes BuildConfig.DEBUG.
 *
 * Two halves, because the second cannot be unit-tested through android.*:
 *   1. the parser, both settings;
 *   2. MainActivity.kt's source — every normaliseBase / HttpRelayServer call site
 *      passes `allowHttp = BuildConfig.DEBUG`, and none passes a literal true.
 * MUTATIONS: drop the `require(scheme == "https" || allowHttp)` line → 1 red;
 * write `allowHttp = true` in MainActivity → 2 red.
 */
class CleartextGateTest {
    @Test
    fun plainHttp_isRefusedUnlessTheBuildAllowsIt() {
        assertEquals("http://192.168.1.5:8080", HttpRelayServer.normaliseBase("http://192.168.1.5:8080", allowHttp = true))
        assertEquals("https://taptime.mt", HttpRelayServer.normaliseBase("https://taptime.mt", allowHttp = false))
        try {
            HttpRelayServer.normaliseBase("http://192.168.1.5:8080", allowHttp = false)
            throw AssertionError("a release-shaped caller accepted plain http")
        } catch (e: IllegalArgumentException) {
            assertTrue(e.message ?: "", (e.message ?: "").contains("debug build"))
        }
        // The constructor defaults to the strict side.
        try {
            HttpRelayServer("http://192.168.1.5:8080", cookie = { null })
            throw AssertionError("the constructor's default accepted plain http")
        } catch (e: IllegalArgumentException) {
            // refused
        }
    }

    @Test
    fun theAppPassesBuildConfigDebugAtEveryCallSite() {
        val src = File("src/main/kotlin/mt/taptime/relay/MainActivity.kt")
        assertTrue("expected ${src.absolutePath}", src.isFile)
        val text = src.readText()
        val calls = Regex("""(HttpRelayServer\(|normaliseBase\()""").findAll(text).count()
        val gated = Regex("""allowHttp = BuildConfig\.DEBUG""").findAll(text).count()
        assertTrue("MainActivity has no HttpRelayServer/normaliseBase call site", calls > 0)
        assertEquals("every call site must pass allowHttp = BuildConfig.DEBUG", calls, gated)
        assertEquals("no call site may hard-code the permissive side", 0, Regex("""allowHttp = true""").findAll(text).count())
    }
}
