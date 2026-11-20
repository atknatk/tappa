package mt.taptime.app

import mt.taptime.app.activation.LiveQrRead
import mt.taptime.app.wire.AppErrorCode
import mt.taptime.app.wire.Bodies
import mt.taptime.app.wire.Canonical
import mt.taptime.app.wire.Headers
import mt.taptime.app.wire.Paths
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The app's wire constants are pinned to the SERVER'S SOURCE: route paths, signature header
 * names, canonical prefixes, the error vocabulary with each code's status, the platform name and
 * the request bodies' field names, read from the Go files. The contract is the server's; a change
 * there turns this red here, in this language (the relay's ContractPinTest pattern).
 *
 * Every file is reached through a system property the test task sets from build.gradle.kts's
 * serverFiles map, which also declares it a task INPUT -- so there is no path this class can open
 * that Gradle does not watch.
 *
 * The reading is text, not an AST; comment lines are dropped first. A trailing comment shaped
 * like a constant could still match: the safe direction (a false red).
 */
class ContractPinTest {
    private fun go(property: String): String =
        serverFile(property).readLines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")

    private fun goStringConst(src: String, name: String): String =
        Regex("""\b$name\s*=\s*"([^"]*)"""").find(src)?.groupValues?.get(1)
            ?: throw AssertionError("no string constant $name in the Go source")

    @Test
    fun theRoutesAreTheServersPaths() {
        val app = go("taptime.go.app")
        val pairs = mapOf(
            "AppActivateLookupPath" to Paths.ACTIVATE_LOOKUP,
            "AppActivateBindPath" to Paths.ACTIVATE_BIND,
            "AppActivateCompletePath" to Paths.ACTIVATE_COMPLETE,
            "AppNoncePath" to Paths.NONCE,
            "AppTapStartPath" to Paths.TAP_START,
            "AppTapPath" to Paths.TAP,
            "AppMePath" to Paths.ME,
            "AppRevokePath" to Paths.REVOKE,
        )
        for ((goName, kt) in pairs) assertEquals(goName, goStringConst(app, goName), kt)
        // M16-23: the two approval routes, pinned by VALUE (the Go names are the server's to choose).
        val m1623 = setOf(Paths.ACTIVATE_APPROVAL, Paths.ACTIVATE_APPROVAL_STATUS)
        // CONTROL: the server declares exactly these App*Path constants -- the eight above and the two.
        val declared = Regex("""\b(App\w+Path)\s*=\s*"([^"]*)"""").findAll(app).associate { it.groupValues[1] to it.groupValues[2] }
        assertEquals(pairs.keys, declared.keys - declared.filterValues { it in m1623 }.keys)
        assertEquals("the approval routes are the server's", m1623, declared.values.toSet() - pairs.values.toSet())
    }

    @Test
    fun theHeadersAndPrefixesAreTheVerifiers() {
        val request = go("taptime.go.request")
        val read = go("taptime.go.read")
        val canonical = go("taptime.go.canonical")
        assertEquals(goStringConst(request, "HeaderDeviceID"), Headers.DEVICE_ID)
        assertEquals(goStringConst(request, "HeaderNonce"), Headers.NONCE)
        assertEquals(goStringConst(request, "HeaderSignature"), Headers.SIGNATURE)
        assertEquals(goStringConst(read, "HeaderTimestamp"), Headers.TIMESTAMP)
        assertEquals(goStringConst(canonical, "CanonicalPrefix"), Canonical.REQUEST_PREFIX)
        assertEquals(goStringConst(canonical, "ReadPrefix"), Canonical.READ_PREFIX)
    }

    @Test
    fun theErrorVocabularyIsTheServersClosedSetWithItsStatuses() {
        val app = go("taptime.go.app")
        val codes = Regex("""\b(appErr\w+)\s+appError\s*=\s*"([^"]+)"""").findAll(app)
            .associate { it.groupValues[1] to it.groupValues[2] }
        val statusNames = mapOf(
            "StatusBadRequest" to 400, "StatusForbidden" to 403, "StatusNotFound" to 404,
            "StatusMethodNotAllowed" to 405, "StatusConflict" to 409, "StatusTooManyRequests" to 429,
            "StatusInternalServerError" to 500,
        )
        val statuses = Regex("""\b(appErr\w+)\s*:\s*http\.(Status\w+)""").findAll(app).associate {
            it.groupValues[1] to (statusNames[it.groupValues[2]] ?: throw AssertionError("unmapped status ${it.groupValues[2]}"))
        }
        assertTrue("CONTROL: the Go vocabulary was found (${codes.size} codes)", codes.size >= 16)
        assertEquals("every Go code has a status", codes.keys, statuses.keys)
        val server = codes.entries.associate { (ident, wire) -> wire to statuses.getValue(ident) }
        val mine = AppErrorCode.entries.associate { it.wire to it.status }
        assertEquals("the app's vocabulary is the server's, code by code and status by status", server, mine)
    }

    @Test
    fun androidIsAPlatformTheServerAccepts() {
        val activate = go("taptime.go.appactivate")
        val platforms = Regex("""appPlatforms\s*=\s*map\[string]string\{([^}]*)}""").find(activate)?.groupValues?.get(1)
            ?: throw AssertionError("no appPlatforms map in appactivate.go")
        assertTrue(platforms, platforms.contains("\"${Bodies.PLATFORM}\":"))
    }

    @Test
    fun theRequestBodiesUseTheServersFieldNames() {
        val activate = go("taptime.go.appactivate")
        fun tags(struct: String): Set<String> {
            val body = Regex("""type\s+$struct\s+struct\s*\{([^}]*)}""").find(activate)?.groupValues?.get(1)
                ?: throw AssertionError("no struct $struct in appactivate.go")
            return Regex("""json:"([^",]+)""").findAll(body).map { it.groupValues[1] }.toSet()
        }
        fun keys(b: ByteArray): Set<String> = MiniJson.decodeObject(String(b, Charsets.UTF_8)).keys
        val point = ByteArray(65).also { it[0] = 4 }
        assertEquals(tags("appLookupRequest"), keys(Bodies.lookup("c")))
        assertEquals(tags("appBindRequest"), keys(Bodies.bind("c", point)))
        // M16-23: complete takes ONE of `sun` and `liveQR`; the two bodies together are the struct.
        assertEquals(setOf("sun"), keys(Bodies.complete("c", point, "tag=1")) - keys(Bodies.approval("c", point)))
        assertEquals(setOf("liveQR"), keys(Bodies.completeLive("c", point, "t=1")) - keys(Bodies.approval("c", point)))
        assertEquals(tags("appCompleteRequest"), keys(Bodies.complete("c", point, "tag=1")) + keys(Bodies.completeLive("c", point, "t=1")))
        // The approval routes' body is complete's without the read.
        assertTrue(tags("appCompleteRequest").containsAll(keys(Bodies.approval("c", point))))
    }

    // M16-23: lookup's answer carries managerApproval (the app reads it; absent is false).
    @Test
    fun theLookupAnswerCarriesTheApprovalFlag() {
        val activate = go("taptime.go.appactivate")
        val body = Regex("""type\s+appLookupResponse\s+struct\s*\{(.*?)\n}""", RegexOption.DOT_MATCHES_ALL).find(activate)?.groupValues?.get(1)
            ?: throw AssertionError("no struct appLookupResponse in appactivate.go")
        assertTrue(body, Regex("""json:"managerApproval[",]""").containsMatchIn(body))
    }

    // M16-13: the tap's two request structs (apptap.go). Every key the app can send is one the
    // server decodes (it decodes strictly: an unknown field is 400), and occurredAt -- the queued
    // tap's field -- is the one the app never sends (no queue, ADR 0045 §2.8).
    @Test
    fun theTapBodiesUseTheServersFieldNames() {
        val tap = go("taptime.go.apptap")
        fun tags(struct: String): Set<String> {
            val body = Regex("""type\s+$struct\s+struct\s*\{(.*?)\n}""", RegexOption.DOT_MATCHES_ALL).find(tap)?.groupValues?.get(1)
                ?: throw AssertionError("no struct $struct in apptap.go")
            return Regex("""json:"([^",]+)""").findAll(body).map { it.groupValues[1] }.toSet()
        }
        fun keys(b: ByteArray): Set<String> = MiniJson.decodeObject(String(b, Charsets.UTF_8)).keys
        // M16-22: tap/start takes ONE of two fields -- the plaque's `sun` or the live QR's `liveQR`;
        // each body sends exactly one, and the two together are the server's whole struct.
        assertEquals(setOf("sun"), keys(Bodies.tapStart("tag=1")))
        assertEquals(setOf("liveQR"), keys(Bodies.tapStartLive("t=1")))
        assertEquals(tags("appTapStartRequest"), keys(Bodies.tapStart("tag=1")) + keys(Bodies.tapStartLive("t=1")))
        assertEquals(tags("appTapRequest") - "occurredAt", keys(Bodies.tap("c", "in", 1.0, 2.0, mockLocation = true)))
        assertTrue("CONTROL: the server still has the queued field", "occurredAt" in tags("appTapRequest"))
    }

    // M16-22: the live QR URL the screens show (liveqr.TapURL: <base>/q?t=<token>) is the route the
    // app's App Link takes; the path and the token's parameter are the server's constants, and GET /q
    // is mounted at the same literal (handler.LiveQRPath).
    @Test
    fun theLiveQrRouteIsTheServersPath() {
        val token = go("taptime.go.liveqrToken")
        assertEquals(goStringConst(token, "LiveQRTapPath"), LiveQrRead.PATH)
        assertEquals(goStringConst(go("taptime.go.livetap"), "LiveQRPath"), LiveQrRead.PATH)
        assertEquals("the token's parameter", "t", goStringConst(token, "TokenParam"))
    }
}
