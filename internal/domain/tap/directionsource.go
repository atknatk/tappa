package tap

// DirectionSource is who chose a recorded direction -- transactions.direction_source
// (migration 00065, ADR 0032 §B.4). The vocabulary exists in two places: 00065's CHECK
// transactions_direction_source_is_known (the source of truth) and the constants below;
// internal/db's TestDirectionSource_TheCheckAdmitsTheTwoSourcesAndRefusesEveryOtherValue binds
// them.
//
// There is no "manager" value on purpose: a manual row's direction is the manager's, and the
// channel ('manual') and entered_by already say so. A third value would be a second spelling of
// one fact, and the CHECK refuses it.
type DirectionSource string

const (
	// DirectionAuto: the engine's direction D was recorded -- no request, or the request
	// equalled D (ADR 0032 §B.3).
	DirectionAuto DirectionSource = "auto"
	// DirectionEmployee: the person asked for the other direction and it was recorded. The
	// invariant is "recorded direction != D", never "a request arrived" (M15-10's trap).
	DirectionEmployee DirectionSource = "employee"
)

// DirectionSources is every value the column admits, in the CHECK's order.
var DirectionSources = []DirectionSource{DirectionAuto, DirectionEmployee}

// RecordedDirectionSource is THE reader rule for transactions.direction_source (ADR 0032 §B.4):
// the one place that turns a stored row into who chose its direction, so no reader spells the
// NULL rule twice. ok is false when the row has no direction source to have:
//
//	manual channel        the direction is the manager's; the column is NULL and means nothing
//	                      (a value there could only be a code slip -- it is not believed)
//	undirected (type NULL) reject / ignored: no direction, so nobody chose one
//	an unknown value      one the CHECK refuses; not guessed at
//
// On a DIRECTED nfc or qr row a NULL means "before M15, the engine's choice" -- every such row
// written before the column existed, or before M15-11's binary writes it -- and reads as
// DirectionAuto. The column is never back-filled (CLAUDE.md §4.3), so this reading is made here,
// at the reader, every time.
//
// The arguments are the row's own columns: channel, type and direction_source, as the store
// model carries them ((*Type)(row.Type) and (*DirectionSource)(row.DirectionSource) convert).
func RecordedDirectionSource(channel Channel, direction *Type, stored *DirectionSource) (DirectionSource, bool) {
	if channel != ChannelNFC && channel != ChannelQR {
		return "", false
	}
	if direction == nil {
		return "", false
	}
	if stored == nil {
		return DirectionAuto, true
	}
	switch *stored {
	case DirectionAuto, DirectionEmployee:
		return *stored, true
	}
	return "", false
}
