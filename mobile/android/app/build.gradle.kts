import com.android.build.api.artifact.SingleArtifact
import java.net.URI

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

val PRODUCTION_BASE_URL = "https://taptime.mt"
val baseUrlOverride: String? = providers.gradleProperty("taptime.baseUrl").orNull
val debugBaseUrl: String = (baseUrlOverride ?: PRODUCTION_BASE_URL).also {
    if (!it.startsWith("https://")) throw GradleException("taptime.baseUrl must be https://, got $it")
}

/** [s] as a Java string literal: backslash and quote escaped, control characters refused. */
fun javaString(s: String): String {
    if (s.any { it < ' ' }) throw GradleException("BASE_URL must not contain control characters")
    return "\"" + s.replace("\\", "\\\\").replace("\"", "\\\"") + "\""
}

/** The host of [url], refused when there is none. */
fun hostOf(url: String): String =
    URI(url).host ?: throw GradleException("BASE_URL has no host: $url")

// A release build with the debug-only override set stops before anything runs: the release
// BASE_URL is the production one, and a release that silently ignored the property would be the
// worse surprise.
gradle.taskGraph.whenReady {
    val release = allTasks.filter { it.project == project && it.name.contains("Release") }
    if (baseUrlOverride != null && release.isNotEmpty()) {
        throw GradleException(
            "taptime.baseUrl is set ($baseUrlOverride) and a release task is scheduled (${release.first().name}): " +
                "the release BASE_URL is fixed to $PRODUCTION_BASE_URL",
        )
    }
}

// Release signing with the UPLOAD key (M16-28). Play App Signing holds the app signing key; the
// upload key lives outside this public repository, and its path and password reach the build only
// from the environment of the machine that uploads. With either missing, the release build is
// unsigned and Play refuses it -- nothing here falls back to a key in the tree.
val uploadKeystore: String? = providers.environmentVariable("TAPTIME_UPLOAD_KEYSTORE").orNull
val uploadPassword: String? = providers.environmentVariable("TAPTIME_UPLOAD_PASSWORD").orNull

// A relative path would resolve under app/, inside the repository, where a key must never live;
// an absolute one inside the repository is refused for the same reason.
uploadKeystore?.let { path ->
    val f = File(path)
    if (!f.isAbsolute) throw GradleException("TAPTIME_UPLOAD_KEYSTORE must be an absolute path outside the repository")
    val repo = rootDir.parentFile.parentFile.canonicalFile
    if (f.canonicalFile.startsWith(repo)) {
        throw GradleException("TAPTIME_UPLOAD_KEYSTORE points inside the repository; keep the upload key outside it")
    }
}

android {
    // Q35 (2026-11-14): store name Taptime, package mt.taptime.app. The package cannot change
    // after the first release and is written into /.well-known/assetlinks.json (M16-08).
    namespace = "mt.taptime.app"
    compileSdk = 36

    defaultConfig {
        applicationId = "mt.taptime.app"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "1.0.0"
    }

    signingConfigs {
        if (uploadKeystore != null && uploadPassword != null) {
            create("upload") {
                storeFile = file(uploadKeystore)
                storePassword = uploadPassword
                keyAlias = "upload"
                keyPassword = uploadPassword
            }
        }
    }

    buildTypes {
        // BASE_URL: the server the app talks to, whose activation links (<base>/activate) and
        // plaque URLs (<base>/t) it accepts (M16-12). RELEASE IS PINNED to production's
        // TAPPA_BASE_URL and nothing can change it: with taptime.baseUrl set, any release task
        // stops the build (the check below). A DEBUG build may point elsewhere with
        // -Ptaptime.baseUrl=https://... -- HTTPS only (the manifest forbids cleartext). The value
        // is written as a Java string literal, escaped (javaString).
        // plaqueHost: the host of BASE_URL, the only host whose /t the manifest's two plaque
        // filters (NFC dispatch, App Link) take (M16-13). PlaqueRead re-checks scheme, host and
        // port before anything is sent.
        debug {
            buildConfigField("String", "BASE_URL", javaString(debugBaseUrl))
            manifestPlaceholders["plaqueHost"] = hostOf(debugBaseUrl)
        }
        // The debug build signs with Gradle's default debug key (per machine,
        // ~/.android/debug.keystore); release signs with the upload key above when the
        // environment names it. This repository is public; no keystore and no signing secret
        // ever enters it.
        release {
            isMinifyEnabled = false
            signingConfigs.findByName("upload")?.let { signingConfig = it }
            buildConfigField("String", "BASE_URL", javaString(PRODUCTION_BASE_URL))
            manifestPlaceholders["plaqueHost"] = hostOf(PRODUCTION_BASE_URL)
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    testOptions {
        // Every android.* method on the unit-test classpath throws unless mocked, so a JVM test
        // can never pass on a stub (the relay's rule, android/app/build.gradle.kts).
        unitTests.isReturnDefaultValues = false
    }
}

// The merged manifest -- what the APK really carries, after every library's manifest is
// merged in -- is the input of MergedManifestTest. It is handed over as a declared, lazy task
// input (a provider of the variant's MERGED_MANIFEST artefact), so:
//   * testDebugUnitTest depends on processDebugManifest through the provider, with no task
//     name written here;
//   * a changed manifest (a new library, a new <uses-permission>) makes the test task out of
//     date. A file the test opened by a guessed path would not be an input, and Gradle would
//     report the test UP-TO-DATE on a warm tree (the relay measured this blindness, audit B1).
class MergedManifestArg(
    @get:InputFile
    @get:PathSensitive(PathSensitivity.NONE)
    val manifest: Provider<RegularFile>,
) : CommandLineArgumentProvider {
    override fun asArguments(): List<String> =
        listOf("-Dtaptime.mergedManifest=${manifest.get().asFile.absolutePath}")
}

// The repository files the JVM tests read, by system property (M16-12). ContractPinTest checks
// that it reads nothing outside this map.
val serverFiles = mapOf(
    "taptime.vectors" to "test/fixtures/appdevice_vectors.json",
    "taptime.go.app" to "internal/handler/app.go",
    "taptime.go.appactivate" to "internal/handler/appactivate.go",
    "taptime.go.request" to "internal/appdevice/request.go",
    "taptime.go.read" to "internal/appdevice/read.go",
    "taptime.go.canonical" to "internal/appdevice/canonical.go",
    // M16-13: the tap's request bodies (apptap.go) and the BROWSER's tap screen, whose words the
    // app's tap screens carry (TapCopyTest): the problem texts, the two templates, the headline.
    "taptime.go.apptap" to "internal/handler/apptap.go",
    "taptime.go.tap" to "internal/handler/tap.go",
    "taptime.templ.tap" to "web/templates/pages/tap.templ",
    "taptime.templ.result" to "web/templates/pages/result.templ",
    "taptime.go.view" to "web/templates/pages/view.go",
    "taptime.js.tap" to "web/static/js/tap.js",
    // M16-22: the live QR route the screens' codes point at (liveqr.LiveQRTapPath, TokenParam) and
    // the literal GET /q is mounted at (handler.LiveQRPath) -- the path the app's App Link takes.
    "taptime.go.liveqrToken" to "internal/liveqr/token.go",
    "taptime.go.livetap" to "internal/handler/livetap.go",
)

val mergedManifests = mutableMapOf<String, Provider<RegularFile>>()
androidComponents {
    onVariants { variant ->
        mergedManifests[variant.name] = variant.artifacts.get(SingleArtifact.MERGED_MANIFEST)
    }
}
tasks.withType<Test>().configureEach {
    // testDebugUnitTest -> debug, testReleaseUnitTest -> release.
    val variant = name.removePrefix("test").removeSuffix("UnitTest").replaceFirstChar { it.lowercase() }
    val manifest = mergedManifests[variant]
        ?: throw GradleException("$name: no merged manifest recorded for variant '$variant'")
    jvmArgumentProviders.add(MergedManifestArg(manifest))
    // The extraction rules the merged manifest points at, read from source by the same test.
    val rules = layout.projectDirectory.file("src/main/res/xml/data_extraction_rules.xml")
    inputs.file(rules).withPathSensitivity(PathSensitivity.RELATIVE)
    systemProperty("taptime.dataExtractionRules", rules.asFile.absolutePath)
    // M16-12: the server's own files the wire tests are held to -- the known-answer vectors
    // (M16-05, M16-07) and the Go source of the routes, headers and error vocabulary
    // (ContractPinTest). Declared INPUTS, so a change on the server side re-runs the tests
    // instead of leaving them UP-TO-DATE on a warm tree (the relay's audit B1 lesson).
    // M16-13: the app's own source and resources, read by LocationCallSiteTest (where the location
    // calls are), NoLogTest (what is logged) and TapCopyTest (strings.xml). A declared input
    // directory, so an edit re-runs those tests.
    val mainSrc = layout.projectDirectory.dir("src/main")
    inputs.dir(mainSrc).withPathSensitivity(PathSensitivity.RELATIVE)
    systemProperty("taptime.mainSrc", mainSrc.asFile.absolutePath)
    // M16-13: the iOS twin's tap copy (mobile/ios/Taptime/TapCopy.swift, ActivationCopy.swift), held
    // line for line by IosCopyTest; M16-47 adds the staff screens' (StaffCopy.swift) and the lock
    // screen's and widget's (Shared/LiveCopy.swift), in both languages. The files exist on a branch
    // that carries both apps; on a branch without them the test is SKIPPED, not passed.
    // -Ptaptime.iosRoot=<dir> reads another checkout's (a measurement, never a build input of release).
    val iosRoot = providers.gradleProperty("taptime.iosRoot").orNull?.let { file(it) } ?: rootProject.file("../ios")
    for ((property, name) in mapOf(
        "taptime.ios.tapCopy" to "Taptime/TapCopy.swift",
        "taptime.ios.activationCopy" to "Taptime/ActivationCopy.swift",
        "taptime.ios.staffCopy" to "Taptime/StaffCopy.swift",
        "taptime.ios.liveCopy" to "Shared/LiveCopy.swift",
    )) {
        val f = File(iosRoot, name)
        if (f.isFile) {
            inputs.file(f).withPathSensitivity(PathSensitivity.RELATIVE)
            systemProperty(property, f.absolutePath)
        }
    }
    for ((property, path) in serverFiles) {
        val f = rootProject.file("../../$path")
        inputs.file(f).withPathSensitivity(PathSensitivity.RELATIVE)
        systemProperty(property, f.absolutePath)
    }
}

dependencies {
    // Runtime: exactly the Q36 set (2026-11-14) -- the Compose BOM, ui, foundation,
    // activity-compose. No material3 (brand components are drawn by hand, skill tappa-brand),
    // no QR library, no Play Integrity (Faz E). Everything else on debugRuntimeClasspath is
    // what these four pull in; the full dump is in README.md.
    //
    // THE BOM IS NOT THE NEWEST, AND THAT WAS MEASURED (2026-11-18): 2026.09.00 (Compose
    // 1.12.1) fails checkDebugAarMetadata -- eight Compose artefacts require compileSdk 37,
    // and the measured toolchain is 36 (android/README.md); 2026.08.00's Compose 1.12.0 carries
    // the same minCompileSdk=37 in its AAR metadata. 2026.06.01 (Compose 1.11.4,
    // minCompileSdk=35) is the newest BOM that builds on 36. Moving to 37 is a toolchain change (a new platform in the SDK),
    // not a version bump here.
    val composeBom = platform("androidx.compose:compose-bom:2026.06.01")
    implementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.foundation:foundation")
    // Not in the BOM: activity-compose versions with androidx.activity.
    implementation("androidx.activity:activity-compose:1.13.0")

    // Test only: JUnit 4, the relay's measured version. The manifest is read with the JDK's
    // own XML parser.
    testImplementation("junit:junit:4.13.2")
}
