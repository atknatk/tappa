package ledger

// reading.go -- the three states of a panel figure (M12 D3, README §3 constraint 8).
//
// 🔴 §4.6 -- NEVER 0 FOR SOMETHING NOT MEASURED. A figure the panel prints beside a list is a
// claim about the world, and "nobody asked", "the database did not answer" and "the database
// answered 0" are three different claims. NotQueried is the zero value, so a struct nobody filled
// claims nothing; ReadFailed renders as "could not be read" (or "?"), never as 0.
//
// ⚠️ THE SAME TYPE IS DECLARED BY THE PASS READS (M12-13, floor.go) ON A PARALLEL BRANCH, word for
// word. The two branches meet in one package, so when they are merged one declaration goes and
// the other stays; the names, the order and Known() are identical on purpose so nothing that uses
// either has to change.

// Reading is how far one figure's read got.
type Reading uint8

const (
	// NotQueried is the zero value: nobody asked, and the figure claims nothing.
	NotQueried Reading = iota
	// ReadFailed is a read that was asked and did not answer. The figure is unknown -- "?" or
	// "could not be read" on screen, never 0.
	ReadFailed
	// Measured is a figure the database answered.
	Measured
)

// Known reports whether the figure was measured. It is the only state in which a screen may print
// a number, zero included.
func (r Reading) Known() bool { return r == Measured }
