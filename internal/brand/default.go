package brand

// DefaultAccent is the tap button's colour for a business that has saved no accent:
// tappa-green, the fill app.css's :root defaults give the button (input.css's base
// layer; ADR 0023 §3's table, "the default of a tenant that set no brand"). It is read
// from the same palette constant the gate is computed from, which
// TestPalette_TheGoCopyEqualsTailwindConfig holds equal to tailwind.config.js. The
// Account editor (M10 WL-7) starts its colour picker on it.
func DefaultAccent() Color { return paletteColor(paletteTappaGreenHex) }
