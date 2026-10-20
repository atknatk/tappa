package components

import "testing"

func TestInitials_FirstAndLastWordRuneSafe(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"Maria Borg", "MB"},
		{"Julia Ann Sciberras", "JS"},
		{"ċensu żammit", "ĊŻ"},
		{"Ħarry", "Ħ"},
		{"  Ana   Silva  ", "AS"},
		{"<script> & 'co'", "SC"},
		{"", ""},
	} {
		if got := Initials(tc.name); got != tc.want {
			t.Errorf("Initials(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
