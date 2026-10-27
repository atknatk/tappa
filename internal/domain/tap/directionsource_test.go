package tap

import "testing"

// TestRecordedDirectionSource_NullOnADirectedTapIsTheEnginesChoice is ADR 0032 §B.4's reader rule,
// cell by cell: on a directed nfc/qr row NULL is "before M15, the engine's choice" (auto) and a
// stored value is itself; a manual row, an undirected row and a value the CHECK refuses have no
// source -- even when the column holds something.
func TestRecordedDirectionSource_NullOnADirectedTapIsTheEnginesChoice(t *testing.T) {
	t.Parallel()
	in, out := TypeIn, TypeOut
	auto, employee := DirectionAuto, DirectionEmployee
	manager, empty := DirectionSource("manager"), DirectionSource("")
	tests := []struct {
		name      string
		channel   Channel
		direction *Type
		stored    *DirectionSource
		want      DirectionSource
		wantOK    bool
	}{
		{"a directed nfc row before M15 (NULL) is the engine's", ChannelNFC, &in, nil, DirectionAuto, true},
		{"a directed qr row before M15 (NULL) is the engine's", ChannelQR, &out, nil, DirectionAuto, true},
		{"a recorded auto is auto", ChannelNFC, &out, &auto, DirectionAuto, true},
		{"a recorded employee is employee", ChannelNFC, &out, &employee, DirectionEmployee, true},
		{"a recorded employee on qr is employee", ChannelQR, &in, &employee, DirectionEmployee, true},
		{"a manual row has none", ChannelManual, &in, nil, "", false},
		{"a manual row with a value is not believed", ChannelManual, &out, &employee, "", false},
		{"an undirected nfc row has none", ChannelNFC, nil, nil, "", false},
		{"an undirected qr row with a value has none", ChannelQR, nil, &auto, "", false},
		{"an unknown channel has none", Channel("api"), &in, nil, "", false},
		{"manager is no value", ChannelNFC, &in, &manager, "", false},
		{"the empty string is no value", ChannelNFC, &in, &empty, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := RecordedDirectionSource(tc.channel, tc.direction, tc.stored)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("RecordedDirectionSource = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestDirectionSources_AreTheTwoConstants keeps the list the DB parity test walks equal to the
// constants: a third constant not in the list would escape that test.
func TestDirectionSources_AreTheTwoConstants(t *testing.T) {
	t.Parallel()
	if len(DirectionSources) != 2 || DirectionSources[0] != DirectionAuto || DirectionSources[1] != DirectionEmployee {
		t.Fatalf("DirectionSources = %v, want [auto employee]", DirectionSources)
	}
	in := TypeIn
	for _, s := range DirectionSources {
		if _, ok := RecordedDirectionSource(ChannelNFC, &in, &s); !ok {
			t.Errorf("%q is listed but not read", s)
		}
	}
}
