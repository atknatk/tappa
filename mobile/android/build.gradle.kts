// Root build: plugin declarations only, applied by :app.
//
// AGP 9 carries Kotlin built in (KGP 2.2.10, android/README.md), so there is no
// org.jetbrains.kotlin.android plugin -- applying one is an error on AGP 9. The Compose
// compiler is a Kotlin compiler plugin and must carry the SAME version as that built-in KGP;
// it is a build-time plugin, nothing of it ships in the APK (README.md, "Dependencies").
plugins {
    id("com.android.application") version "9.4.1" apply false
    id("org.jetbrains.kotlin.plugin.compose") version "2.2.10" apply false
}
