// The staff app (ADR 0045, M16 Faz B) is its own Gradle build under mobile/android/, outside
// the Go module and separate from the plaque relay in android/ (ADR 0017): two apps, two
// builds, no shared module. scripts/redline-check.sh scans this tree (mobile/ is in its SRC
// once the directory exists).
//
// No Gradle wrapper is checked in -- this repository carries no binaries. Build with the
// system Gradle named in README.md (`make mobile-android`).
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "taptime-staff"
include(":app")
