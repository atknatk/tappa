// Root build: one plugin declaration, applied by :app. AGP 9 carries Kotlin support
// built in (a runtime dependency on KGP 2.2.10), so there is no
// org.jetbrains.kotlin.android plugin here — applying one is an error on AGP 9.
plugins {
    id("com.android.application") version "9.4.1" apply false
}
