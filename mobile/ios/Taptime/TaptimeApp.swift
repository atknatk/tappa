import SwiftUI

// The staff app (ADR 0045, M16-15). This is the empty shell: one scene, one screen, no
// third-party package (Q36). Activation (M16-16) and the tap screen (M16-17) grow from here.
@main
struct TaptimeApp: App {
    var body: some Scene {
        WindowGroup {
            RootView()
        }
    }
}
