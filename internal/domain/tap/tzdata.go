package tap

// This blank import embeds the IANA timezone database into every binary that links
// the tap engine (M4-05). Lateness (decide.go: shiftStartInstant) resolves a shift's
// wall-clock start through time.LoadLocation(shift.Timezone) — e.g. "Europe/Malta"
// — and that fails if the OS has no /usr/share/zoneinfo. The production deploy is a
// SINGLE static binary (CLAUDE.md §1) that may run on a scratch/distroless image
// with no system tzdata, so without this embed LoadLocation would fail there and
// lateness would silently never be computed — a report-correctness failure that no
// test on a developer machine (which has system tzdata) would catch.
//
// WHY HERE, in the tap package, and not in cmd/tappa. The package that CALLS
// LoadLocation owns the guarantee that the data is present, so the engine is
// correct in EVERY environment it is compiled into — tests, tools and the deploy
// binary — without depending on a caller (or the deferred M8 deploy task,
// docs/plan/m8-deploy-pilot.md) remembering to embed it. It is stdlib
// (time/tzdata), so it adds NO new dependency and no clock/DB/HTTP — the tap
// package's purity proof (go list -deps has no DB/HTTP; no time.Now) is unchanged;
// this only makes time.LoadLocation's data self-contained. If cmd/tappa (or M8)
// also imports time/tzdata later, the embed is idempotent — the linker includes one
// copy. Cost: the embedded database adds ~450 KB to the binary, acceptable for a
// single-binary deploy and cheaper than a silently wrong attendance report.
import _ "time/tzdata"
