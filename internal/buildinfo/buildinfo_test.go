package buildinfo

import (
	"runtime/debug"
	"testing"
)

// The reader, driven over every shape debug.ReadBuildInfo can return.
//
// 🔴 THE INPUTS ARE SYNTHETIC BECAUSE THE TEST BINARY IS UNSTAMPED, and that is a
// measurement rather than a convenience: `go test` does not record vcs settings, so
// a test that asserted on its own process could only ever see the fallback branch —
// it would pass unchanged against a Read that returned Unknown for everything and
// never parsed a revision at all. TestRead_TheTestBinaryItselfIsUnstamped below
// records that fact so this comment cannot go stale silently.
func TestRead_ParsesWhatTheToolchainRecords(t *testing.T) {
	t.Parallel()

	// The settings below are the REAL ones, copied from `go version -m` on a binary
	// built by the Makefile's build target (2026-08-15).
	stamped := &debug.BuildInfo{
		GoVersion: "go1.26.5",
		Main:      debug.Module{Path: "github.com/atknatk/tappa", Version: "v0.0.0-20260815011242-5a86f9b9bb53"},
		Settings: []debug.BuildSetting{
			{Key: "-trimpath", Value: "true"},
			{Key: "CGO_ENABLED", Value: "0"},
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: "5a86f9b9bb5301ed0a5d5ed79cb533317c81785a"},
			{Key: "vcs.time", Value: "2026-08-15T01:12:42Z"},
			{Key: "vcs.modified", Value: "false"},
		},
	}
	dirty := &debug.BuildInfo{
		GoVersion: "go1.26.5",
		Main:      debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "5a86f9b9bb5301ed0a5d5ed79cb533317c81785a"},
			{Key: "vcs.modified", Value: "true"},
		},
	}

	tests := []struct {
		name string
		bi   *debug.BuildInfo
		ok   bool
		want Build
	}{
		{
			name: "a release build of a clean tree",
			bi:   stamped, ok: true,
			want: Build{
				Version:  "v0.0.0-20260815011242-5a86f9b9bb53",
				Revision: "5a86f9b9bb5301ed0a5d5ed79cb533317c81785a",
				Time:     "2026-08-15T01:12:42Z",
				Go:       "go1.26.5",
				Modified: false,
				Stamped:  true,
			},
		},
		{
			// The developer-machine shape: a revision, no version, and the flag that
			// says the revision does not describe the binary.
			name: "a build of a modified tree",
			bi:   dirty, ok: true,
			want: Build{
				Version:  Unknown,
				Revision: "5a86f9b9bb5301ed0a5d5ed79cb533317c81785a",
				Time:     Unknown,
				Go:       "go1.26.5",
				Modified: true,
				Stamped:  true,
			},
		},
		{
			// -buildvcs=false, or a build from outside a checkout: everything the
			// deployment would want is missing, and it says so rather than printing
			// five empty fields.
			name: "no vcs information at all",
			bi:   &debug.BuildInfo{GoVersion: "go1.26.5"}, ok: true,
			want: Build{Version: Unknown, Revision: Unknown, Time: Unknown, Go: "go1.26.5"},
		},
		{
			name: "no build info at all",
			bi:   nil, ok: false,
			want: Build{Version: Unknown, Revision: Unknown, Time: Unknown, Go: Unknown},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := read(tc.bi, tc.ok); got != tc.want {
				t.Errorf("read() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestRead_TheTestBinaryItselfIsUnstamped is the measurement the comment above
// rests on. It asserts the WEAK half (Read does not panic and names the toolchain)
// and RECORDS the strong half, so that if a future Go release starts stamping test
// binaries this test's log says so on the next run.
func TestRead_TheTestBinaryItselfIsUnstamped(t *testing.T) {
	t.Parallel()
	b := Read()
	t.Logf("this test binary reports %+v", b)
	if b.Go == Unknown {
		t.Error("Read() could not even name the Go toolchain, which every binary carries")
	}
}

// LogArgs must stay pairs, because slog silently degrades an odd list into a
// "!BADKEY" entry rather than failing.
func TestLogArgs_IsPairsAndCarriesTheRevision(t *testing.T) {
	t.Parallel()
	b := Build{Version: "v1", Revision: "abc", Time: "t", Go: "go1.26.5", Modified: true}
	args := b.LogArgs()
	if len(args)%2 != 0 {
		t.Fatalf("LogArgs returned %d values; slog needs pairs", len(args))
	}
	found := map[string]any{}
	for i := 0; i < len(args); i += 2 {
		k, ok := args[i].(string)
		if !ok {
			t.Fatalf("key %d is %T, not a string", i, args[i])
		}
		found[k] = args[i+1]
	}
	for _, k := range []string{"version", "revision", "committed", "go", "modified"} {
		if _, ok := found[k]; !ok {
			t.Errorf("LogArgs has no %q key: %v", k, args)
		}
	}
	if found["revision"] != "abc" {
		t.Errorf("revision = %v, want abc — the one field an operator greps for", found["revision"])
	}
	if found["modified"] != true {
		t.Errorf("modified = %v, want true", found["modified"])
	}
}
