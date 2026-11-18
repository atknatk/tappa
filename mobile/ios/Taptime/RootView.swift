import SwiftUI

// The placeholder the scaffold opens on: the wordmark the browser's tap screen draws when the
// business has no brand (lowercase green "taptime", "punchless" under it), and nothing to press.
// The real screens replace it; until then the app has no action an employee could take.
struct RootView: View {
    var body: some View {
        ZStack {
            Palette.porcelain.ignoresSafeArea()
            VStack(spacing: 4) {
                Text("taptime")
                    .font(.system(size: 28, weight: .bold))
                    .foregroundColor(Palette.green)
                Text("PUNCHLESS")
                    .font(.system(size: 10, design: .monospaced))
                    .kerning(2)
                    .foregroundColor(Palette.ink.opacity(0.7))
            }
            .accessibilityElement(children: .combine)
        }
    }
}
