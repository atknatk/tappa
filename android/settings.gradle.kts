// The Android relay is a separate Gradle build at the repository root, OUTSIDE the
// Go module: `go build ./...` never sees it and scripts/redline-check.sh's fixed SRC
// list does not scan it (CLAUDE.md §1: the Kotlin/Gradle chain was opened for this
// one directory by user decision, M8-05 FAZ B1).
//
// No Gradle wrapper is checked in — this repository carries no binaries. Build
// with the system Gradle named in README.md.
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

rootProject.name = "taptime-relay"
include(":app")
