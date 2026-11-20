package mt.taptime.app

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Document
import org.w3c.dom.Element

// M16-11: the permissions the APK really asks for are those of the MERGED manifest
// (processDebugManifest's output, every library's manifest folded in), not of
// src/main/AndroidManifest.xml. A library can add a permission the source never names; this
// test reads what the build produced. build.gradle.kts hands the file over as a declared task
// input, so a changed manifest re-runs the test instead of leaving it UP-TO-DATE.
class MergedManifestTest {

    private val androidNs = "http://schemas.android.com/apk/res/android"

    private fun file(property: String): File {
        val path = System.getProperty(property)
            ?: throw AssertionError("system property $property is not set -- run through Gradle (make mobile-android-test)")
        val f = File(path)
        assertTrue("$property points at a missing file: $path", f.isFile)
        return f
    }

    private fun parse(f: File): Document =
        DocumentBuilderFactory.newInstance().apply {
            isNamespaceAware = true
            // The input is our own build output, but an XML parser is configured closed anyway.
            setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
        }.newDocumentBuilder().parse(f)

    private fun Document.elements(tag: String): List<Element> {
        val nodes = getElementsByTagName(tag)
        return (0 until nodes.length).map { nodes.item(it) as Element }
    }

    private fun Document.names(tag: String): List<String> =
        elements(tag).map { it.getAttributeNS(androidNs, "name") }

    private val merged by lazy { parse(file("taptime.mergedManifest")) }

    @Test
    fun theMergedManifestAsksForTheClosedListOnly() {
        val allowed = setOf(
            "android.permission.NFC",
            "android.permission.INTERNET",
            "android.permission.ACCESS_FINE_LOCATION",
            "android.permission.ACCESS_COARSE_LOCATION",
        )
        val asked = merged.names("uses-permission")
        assertEquals("a permission is asked for twice: $asked", asked.size, asked.toSet().size)
        assertEquals("uses-permission in the merged manifest", allowed, asked.toSet())
        // The API-23+ spelling is a permission too; none is allowed.
        assertEquals("uses-permission-sdk-23", emptyList<String>(), merged.names("uses-permission-sdk-23"))
    }

    @Test
    fun theMergedManifestDeclaresNoPermissionOfItsOwn() {
        // androidx.core's DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION lands here unless removed.
        for (tag in listOf("permission", "permission-tree", "permission-group")) {
            assertEquals(tag, emptyList<String>(), merged.names(tag))
        }
    }

    @Test
    fun theAppDataDoesNotTravel() {
        val app = merged.elements("application").single()
        assertEquals("android:allowBackup", "false", app.getAttributeNS(androidNs, "allowBackup"))
        assertTrue(
            "android:dataExtractionRules is missing: allowBackup=false alone does not stop a " +
                "device-to-device transfer on Android 12+",
            app.getAttributeNS(androidNs, "dataExtractionRules").isNotEmpty(),
        )
    }

    // M16-12: the app speaks HTTPS only. targetSdk 28+ already defaults cleartext to off, but a
    // library's manifest or a networkSecurityConfig could turn it back on; the merged manifest
    // must say false explicitly and point at no network security config that could relax it.
    @Test
    fun cleartextTrafficIsOffExplicitly() {
        val app = merged.elements("application").single()
        assertEquals("android:usesCleartextTraffic", "false", app.getAttributeNS(androidNs, "usesCleartextTraffic"))
        assertEquals("android:networkSecurityConfig", "", app.getAttributeNS(androidNs, "networkSecurityConfig"))
    }

    @Test
    fun theExtractionRulesExcludeEveryDomainFromBothPaths() {
        val rules = parse(file("taptime.dataExtractionRules"))
        val domains = setOf(
            "root", "file", "database", "sharedpref", "external",
            "device_root", "device_file", "device_database", "device_sharedpref",
        )
        for (section in listOf("cloud-backup", "device-transfer")) {
            val el = rules.elements(section).single()
            val nodes = el.getElementsByTagName("include")
            assertEquals("$section: an <include> copies data out", 0, nodes.length)
            val excludes = el.getElementsByTagName("exclude")
            val excluded = (0 until excludes.length)
                .map { excludes.item(it) as Element }
                .filter { it.getAttribute("path") == "." }
                .map { it.getAttribute("domain") }
                .toSet()
            assertEquals("$section: whole-domain excludes", domains, excluded)
        }
    }

    @Test
    fun theEmojiFontProviderIsNotAskedAtStart() {
        // Review finding B-2 (ADR 0045 §2.2): emoji2's startup initialiser asks the system font
        // provider (Google Play services on most phones) for a downloadable font. The source
        // manifest removes it from androidx.startup's provider; a library update or a dropped
        // tools:node line would bring it back here.
        val emoji = merged.names("meta-data").filter { it.startsWith("androidx.emoji2") }
        assertEquals("emoji2 meta-data in the merged manifest", emptyList<String>(), emoji)
        // The control: the provider itself is still merged with its other initialisers, so the
        // assertion above is not passing on a manifest with no startup entries at all.
        assertTrue(
            "androidx.startup's ProcessLifecycleInitializer is missing from the merged manifest",
            "androidx.lifecycle.ProcessLifecycleInitializer" in merged.names("meta-data"),
        )
    }

    @Test
    fun thePackageIsTheQ35One() {
        assertEquals("mt.taptime.app", merged.documentElement.getAttribute("package"))
    }

    // M16-13: the plaque opens the app -- an NFC read (NDEF_DISCOVERED) and an App Link (VIEW,
    // autoVerify) -- for this build's BASE_URL host and the path /t. M16-22: a venue screen's live QR
    // opens it too -- an App Link (VIEW, autoVerify) to /q, and never an NFC read. Nothing else: the
    // activation link (/activate) stays the browser's (Q40).
    @Test
    fun thePlaqueRouteOpensTheAppAndNothingElseDoes() {
        val host = java.net.URI(BuildConfig.BASE_URL).host
        val activity = merged.elements("activity").single { it.getAttributeNS(androidNs, "name").endsWith(".MainActivity") }
        val filters = activity.getElementsByTagName("intent-filter").let { n -> (0 until n.length).map { n.item(it) as Element } }
        fun Element.children(tag: String) = getElementsByTagName(tag).let { n -> (0 until n.length).map { n.item(it) as Element } }
        fun actions(f: Element) = f.children("action").map { it.getAttributeNS(androidNs, "name") }.toSet()
        fun path(f: Element) = f.children("data").single().getAttributeNS(androidNs, "path")
        val withData = filters.filter { it.children("data").isNotEmpty() }
        assertEquals("three filters carry a URL", 3, withData.size)
        for (f in withData) {
            val data = f.children("data").single()
            assertEquals("https", data.getAttributeNS(androidNs, "scheme"))
            assertEquals(host, data.getAttributeNS(androidNs, "host"))
            assertTrue(path(f), path(f) == "/t" || path(f) == "/q")
            assertEquals("no prefix or pattern can widen the path", "", data.getAttributeNS(androidNs, "pathPrefix") + data.getAttributeNS(androidNs, "pathPattern"))
        }
        val nfc = withData.single { actions(it) == setOf("android.nfc.action.NDEF_DISCOVERED") }
        assertEquals("an NFC read is the plaque's only", "/t", path(nfc))
        assertTrue("android.intent.category.DEFAULT" in nfc.children("category").map { it.getAttributeNS(androidNs, "name") })
        val links = withData.filter { actions(it) == setOf("android.intent.action.VIEW") }
        assertEquals("the App Links: the plaque and the live QR", setOf("/t", "/q"), links.map(::path).toSet())
        for (link in links) {
            assertEquals("true", link.getAttributeNS(androidNs, "autoVerify"))
            assertEquals(setOf("android.intent.category.DEFAULT", "android.intent.category.BROWSABLE"),
                link.children("category").map { it.getAttributeNS(androidNs, "name") }.toSet())
        }
        // No other component of the merged manifest takes a URL.
        assertEquals("data elements in the merged manifest", 3, merged.elements("data").size)
    }

    // M16-22 (Q36 (c)): the live QR is read by the SYSTEM camera; the app has no QR reader, so it
    // asks for no camera and needs none. The closed permission list above already refuses CAMERA;
    // this names it, and the camera features too (a required one would hide the app from a phone).
    @Test
    fun noCameraIsAskedForOrDeclared() {
        assertTrue("CAMERA asked for", merged.names("uses-permission").none { it.contains("CAMERA") })
        assertEquals("camera features declared", emptyList<String>(),
            merged.names("uses-feature").filter { it.startsWith("android.hardware.camera") })
    }

    // M16-11 review B-3, decided by M16-13: location hardware is optional (a fix is backup evidence,
    // CLAUDE.md §5 rows 6-7); no feature is required at all.
    @Test
    fun noHardwareFeatureIsRequired() {
        val features = merged.elements("uses-feature")
        val location = features.single { it.getAttributeNS(androidNs, "name") == "android.hardware.location" }
        assertEquals("false", location.getAttributeNS(androidNs, "required"))
        val required = features.filter { it.getAttributeNS(androidNs, "required") != "false" }.map { it.getAttributeNS(androidNs, "name") }
        assertEquals("features declared required", emptyList<String>(), required)
    }
}
