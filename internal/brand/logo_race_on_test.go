//go:build race

package brand

// logoRaceBuild is whether this test binary was built with -race: the race detector's
// shadow memory would be measured as the decode's, so the RSS measurement skips.
const logoRaceBuild = true
