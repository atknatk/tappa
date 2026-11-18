package mt.taptime.app

import mt.taptime.app.activation.AccentFill
import mt.taptime.app.activation.InviteCode
import mt.taptime.app.activation.PlaqueRead
import mt.taptime.app.ui.endSentence
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** What the person pastes, what the plaque writes, and how the accent is painted. */
class InputsTest {
    private val code = "q9bX3yJ0r1sT2uV3wX4yZ5a6b7c8d9e0f1g2h3i4j5k"
    private val base = "https://taptime.mt"

    @Test
    fun theCodeIsReadFromThisServersActivationLinkOrOnItsOwn() {
        assertEquals(43, code.length)
        assertEquals(code, InviteCode.from(code, base))
        assertEquals(code, InviteCode.from("  $code\n", base))
        assertEquals(code, InviteCode.from("https://taptime.mt/activate?code=$code", base))
        assertEquals(code, InviteCode.from("https://TapTime.MT/activate?x=1&code=$code", base))
        for (junk in listOf("", "hello", code.dropLast(1), "$code=", "https://taptime.mt/activate", "https://taptime.mt/activate?code=short",
            "https://taptime.mt/activate?code=$code%20", "https://taptime.mt/activate?xcode=$code")) {
            assertNull(junk, InviteCode.from(junk, base))
        }
    }

    @Test
    fun aLinkOfAnotherHostOrPathNeverYieldsItsCode() {
        for (other in listOf(
            "https://taptime.mt.evil.example/activate?code=$code", "https://evil.example/activate?code=$code",
            "http://taptime.mt/activate?code=$code", "https://taptime.mt/activate/?code=$code",
            "https://taptime.mt/app/activate?code=$code", "https://taptime.mt/t?code=$code",
            "https://user@taptime.mt/activate?code=$code", "taptime.mt/activate?code=$code",
        )) {
            assertNull(other, InviteCode.from(other, base))
        }
    }

    @Test
    fun onlyThisServersPlaqueRouteYieldsASunQuery() {
        val q = "tag=04A1B2C3D4E5F6&ctr=000011&cmac=0011223344556677"
        assertEquals(q, PlaqueRead.sunQuery("https://taptime.mt/t?$q", base))
        assertEquals("host case and an explicit default port are the same server", q, PlaqueRead.sunQuery("https://TAPTIME.mt:443/t?$q", base))
        // A static QR plaque URL (no ctr/cmac) is still the plaque route: the server decides
        // (activation_needs_touch), not the app.
        assertEquals("tag=04A1B2C3D4E5F6", PlaqueRead.sunQuery("https://taptime.mt/t?tag=04A1B2C3D4E5F6", base))
        for (other in listOf(
            "http://taptime.mt/t?$q", "https://taptime.mt.evil.example/t?$q", "https://evil.example/t?$q",
            "https://taptime.mt:8443/t?$q", "https://taptime.mt/t/?$q", "https://taptime.mt/tap?$q", "https://taptime.mt/t",
            "https://taptime.mt/t?$q#x", "https://user@taptime.mt/t?$q", "/t?$q", "", "not a url",
        )) {
            assertNull(other, PlaqueRead.sunQuery(other, base))
        }
    }

    @Test
    fun theAccentIsPaintedAsTheBrowsersThemeWouldPaintIt() {
        // ADR 0023 §3's table: the label and the edge for the documented colours.
        val green = AccentFill.of("1F5C41")!!
        assertEquals(AccentFill.PAPER, green.label)
        assertFalse(green.edge)
        assertEquals(AccentFill.PAPER, AccentFill.of("DA291C")!!.label)
        assertEquals(AccentFill.PAPER, AccentFill.of("be3d2a")!!.label)
        val yellow = AccentFill.of("FFC72C")!!
        assertEquals(AccentFill.INK, yellow.label)
        assertTrue(yellow.edge)
        val saffron = AccentFill.of("D98E2B")!!
        assertEquals(AccentFill.INK, saffron.label)
        assertTrue(saffron.edge)
        // The table's ratios, to two decimals.
        assertEquals(7.73, AccentFill.contrast(AccentFill.PAPER, 0x1F5C41), 0.005)
        assertEquals(10.56, AccentFill.contrast(AccentFill.INK, 0xFFC72C), 0.005)
        assertEquals(14.32, AccentFill.contrast(AccentFill.INK, AccentFill.PORCELAIN), 0.005)
        for (none in listOf(null, "", "#1F5C41", "1F5C4", "1F5C411", "GGGGGG")) assertNull(none, AccentFill.of(none))
    }

    @Test
    fun theDebugBuildWithoutTheOverrideTalksToProduction() {
        // The release value is pinned in build.gradle.kts and its override refusal measured by a
        // release build (README, "The signed requests and the clock"); this pins the default.
        assertEquals(base, BuildConfig.BASE_URL)
    }

    @Test
    fun aNameEndingInAFullStopGetsNoSecond() {
        assertEquals("To record your working hours for KF.", endSentence("To record your working hours for KF"))
        assertEquals("To record your working hours for Kebab Ltd.", endSentence("To record your working hours for Kebab Ltd."))
    }

    // Parity with iOS (2026-11-18): "Open in browser" rebuilds <base>/t + the verified query; the
    // URL as it arrived (host case, an explicit default port) is not passed on.
    @Test
    fun theBrowserGetsThePlaqueUrlRebuiltFromTheBase() {
        val q = "tag=04A1B2C3D4E5F6&ctr=000011&cmac=0011223344556677"
        assertEquals("https://taptime.mt/t?$q", PlaqueRead.browserUrl("https://TAPTIME.mt:443/t?$q", base))
        assertEquals("https://taptime.mt/t?$q", PlaqueRead.browserUrl("https://taptime.mt/t?$q", "https://taptime.mt/"))
        for (other in listOf("https://evil.example/t?$q", "https://taptime.mt/t?$q#x", "https://taptime.mt/t", "")) {
            assertNull(other, PlaqueRead.browserUrl(other, base))
        }
    }

    // M16-12 hand-over (2): InviteCode compares the port, as PlaqueRead always did.
    @Test
    fun theCodesLinkMustBeOnThisServersPort() {
        assertEquals(code, InviteCode.from("https://taptime.mt:443/activate?code=$code", base))
        assertNull(InviteCode.from("https://taptime.mt:8443/activate?code=$code", base))
        assertNull(InviteCode.from("https://taptime.mt:443/activate?code=$code", "https://taptime.mt:8443"))
        assertEquals(code, InviteCode.from("https://taptime.mt:8443/activate?code=$code", "https://taptime.mt:8443"))
    }
}
