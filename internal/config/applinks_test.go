package config_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// Fake values of the right shapes -- the platforms' own documentation samples, never a real Team ID
// or certificate (Q35: the repository holds none).
const (
	fakeTeamID = "ABCDE12345"
	fakePrintA = "14:6D:E9:83:C5:73:06:50:D8:EE:B9:95:2F:34:FC:64:16:A0:83:42:E6:1D:BE:A8:8A:04:96:B2:3F:CF:44:E5"
	fakePrintB = "00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF"
)

// TestLoad_TheAppValuesAreOptional (M16-08): with neither variable set the server loads -- in dev and
// in production -- and both fields are empty, so neither association file is served. Not a startup
// failure: the app is optional (ADR 0045).
func TestLoad_TheAppValuesAreOptional(t *testing.T) {
	envs := []string{""}
	if !config.SandboxBuild() {
		envs = append(envs, config.EnvProd)
	}
	for _, env := range envs {
		t.Run("env="+env, func(t *testing.T) {
			setRequired(t)
			if env != "" {
				t.Setenv("TAPPA_ENV", env)
				t.Setenv("TAPPA_BASE_URL", "https://taptime.mt")
				t.Setenv("TAPPA_TRUSTED_PROXIES", "10.42.0.1/32")
			}
			c, err := config.Load()
			if err != nil {
				t.Fatalf("unset app values refused: %v", err)
			}
			if c.AppAppleTeamID != "" || c.AppAndroidSignerSHA256 != nil {
				t.Errorf("unset loaded as %q / %v", c.AppAppleTeamID, c.AppAndroidSignerSHA256)
			}
		})
	}
	t.Run("blank certificate list is unset", func(t *testing.T) {
		setRequired(t)
		t.Setenv(config.EnvAppAndroidSignerSHA256, "   ")
		c, err := config.Load()
		if err != nil || c.AppAndroidSignerSHA256 != nil {
			t.Fatalf("blanks: %v / %v", err, c)
		}
	})
}

// TestLoad_TheAppleTeamIDIsOneSpelling: ten upper-case letters or digits, kept as given; any other
// value is refused naming the variable and repeating nothing.
func TestLoad_TheAppleTeamIDIsOneSpelling(t *testing.T) {
	for _, v := range []string{fakeTeamID, "0000000000", "ZZZZZZZZZZ"} {
		setRequired(t)
		t.Setenv(config.EnvAppAppleTeamID, v)
		c, err := config.Load()
		if err != nil {
			t.Fatalf("%q refused: %v", v, err)
		}
		if c.AppAppleTeamID != v {
			t.Errorf("loaded %q, want %q", c.AppAppleTeamID, v)
		}
	}
	for i, v := range []string{
		"abcde12345", "ABCDE1234", "ABCDE123456", " ABCDE12345", "ABCDE12345 ", "ABCDE-1234", "ABCDE12345.mt.taptime.app",
		"\u00c4BCDE12345", "   ", "HUNTER2XYZ!",
	} {
		t.Run(fmt.Sprintf("bad/%d", i), func(t *testing.T) {
			setRequired(t)
			t.Setenv(config.EnvAppAppleTeamID, v)
			_, err := config.Load()
			if err == nil {
				t.Fatalf("%q loaded", v)
			}
			if !strings.Contains(err.Error(), config.EnvAppAppleTeamID) {
				t.Errorf("refused without naming the variable: %v", err)
			}
			if strings.TrimSpace(v) != "" && strings.Contains(err.Error(), strings.TrimSpace(v)) {
				t.Errorf("the refusal repeats the value: %v", err)
			}
		})
	}
}

// TestLoad_TheAndroidCertDigestsAreAList: one or more Play Console certificate digests, comma-separated,
// blanks around an entry trimmed, order kept; a malformed entry, an empty entry or a duplicate is
// refused by POSITION, never by value.
func TestLoad_TheAndroidCertDigestsAreAList(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want []string
	}{
		{fakePrintA, []string{fakePrintA}},
		{fakePrintA + "," + fakePrintB, []string{fakePrintA, fakePrintB}},
		{" " + fakePrintB + " , " + fakePrintA + " ", []string{fakePrintB, fakePrintA}},
	} {
		setRequired(t)
		t.Setenv(config.EnvAppAndroidSignerSHA256, tc.v)
		c, err := config.Load()
		if err != nil {
			t.Fatalf("%q refused: %v", tc.v, err)
		}
		if !slices.Equal(c.AppAndroidSignerSHA256, tc.want) {
			t.Errorf("loaded %v, want %v", c.AppAndroidSignerSHA256, tc.want)
		}
	}
	for _, tc := range []struct {
		v, entry string
	}{
		{strings.ToLower(fakePrintA), "entry 1"},
		{strings.ReplaceAll(fakePrintA, ":", ""), "entry 1"},
		{strings.ReplaceAll(fakePrintA, ":", "-"), "entry 1"},
		{fakePrintA[:len(fakePrintA)-3], "entry 1"},                   // 31 pairs
		{fakePrintA + ":00", "entry 1"},                               // 33 pairs
		{"G" + fakePrintA[1:], "entry 1"},                             // not hex
		{"SHA256:" + fakePrintA, "entry 1"},                           // a label
		{fakePrintA + ",", "entry 2"},                                 // an empty entry
		{fakePrintA + ",," + fakePrintB, "entry 2"},                   // an empty entry in the middle
		{fakePrintA + "," + fakePrintB + "," + fakePrintA, "entry 3"}, // a duplicate
		{fakePrintA + ";" + fakePrintB, "entry 1"},                    // the wrong separator
	} {
		t.Run(tc.v, func(t *testing.T) {
			setRequired(t)
			t.Setenv(config.EnvAppAndroidSignerSHA256, tc.v)
			_, err := config.Load()
			if err == nil {
				t.Fatalf("%q loaded", tc.v)
			}
			if !strings.Contains(err.Error(), config.EnvAppAndroidSignerSHA256) || !strings.Contains(err.Error(), tc.entry) {
				t.Errorf("refused without naming the variable and %s: %v", tc.entry, err)
			}
			for _, needle := range []string{"14:6D", "14:6d", "146D", "00:11:22"} {
				if strings.Contains(err.Error(), needle) {
					t.Errorf("the refusal repeats the value (%q): %v", needle, err)
				}
			}
		})
	}
}
