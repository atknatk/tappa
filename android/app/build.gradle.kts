plugins {
    id("com.android.application")
}

android {
    namespace = "mt.taptime.relay"
    compileSdk = 36

    defaultConfig {
        // K6: user-facing name is Taptime, package mt.taptime.relay.
        applicationId = "mt.taptime.relay"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }

    buildFeatures {
        // BuildConfig.DEBUG is the cleartext gate (HttpRelayServer.normaliseBase,
        // audit F4); AGP 8+ does not generate BuildConfig unless asked.
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    testOptions {
        // Every android.* method on the unit-test classpath throws unless mocked.
        // The relay loop, the reply parser and the HTTP client are written against
        // java.* only so that this stays FALSE and a test can never pass on a stub.
        unitTests.isReturnDefaultValues = false
        unitTests.all { test ->
            // MEASURED, NOT ASSUMED: without this the Origin test goes red on the JVM.
            // The desktop JDK's HttpURLConnection keeps an applet-era list of
            // "restricted" request headers — Origin among them — and drops them at
            // the socket unless this property is set. Android's HttpURLConnection is
            // OkHttp-backed and has no such list, so the phone sends the header; the
            // on-device evidence is the "Check server" probe on the signed-out screen
            // (README.md, "Origin kanıtı"). The property only lets the JVM test see
            // what the client sets.
            test.systemProperty("sun.net.http.allowRestrictedHeaders", "true")

            // 🔴 THE SERVER FILES ContractPinTest READS ARE TASK INPUTS, AND WITHOUT
            // THIS LINE THE PIN WAS BLIND ON A WARM TREE (second-round audit, B1).
            // Gradle decides whether a test task is UP-TO-DATE from its declared
            // inputs; a file the test opens with File("../..") is not one. Measured:
            // six drifts in the Go source — a route, a fault word, a step name, an
            // eleventh step, a form field, the cookie name — all left
            // `testDebugUnitTest UP-TO-DATE` and the suite 28/0 green, while a forced
            // clean run turned all six red. Declaring the files is the right fix;
            // `upToDateWhen { false }` would re-run every test on every build.
            //
            // The list is mirrored by ContractPinTest.PINNED_SOURCES, and a test
            // there requires every path it reads to appear in THIS file verbatim, so
            // a Go file added to the test without being added here turns red.
            test.inputs.files(
                rootProject.file("../internal/handler/plaqueencode.go"),
                rootProject.file("../internal/encode/driver.go"),
                rootProject.file("../internal/adminauth/cookie.go"),
                rootProject.file("../internal/handler/adminlogin.go"),
                rootProject.file("../deploy/k8s/05-config.yaml"),
            ).withPathSensitivity(PathSensitivity.RELATIVE)
        }
    }
}

dependencies {
    // Runtime: NONE. android.app.Activity, android.webkit.WebView/CookieManager,
    // android.nfc.tech.IsoDep, java.net.HttpURLConnection and org.json are all in
    // the platform (K7: stdlib > small library > framework; appcompat/webkit were
    // permitted and turned out not to be needed).
    //
    // Test only: JUnit 4, and the real org.json so the reply parser — which runs
    // against the platform's org.json on the phone — can be driven on the JVM.
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20260814")
}
