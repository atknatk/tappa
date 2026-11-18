import SwiftUI

// The staff app (ADR 0045). One scene, no third-party package (Q36). M16-15 scaffolded it,
// M16-16 activates it with a Secure Enclave key, M16-17 adds the tap screen.
@main
struct TaptimeApp: App {
    /// The one server the app talks to; the plaque host and the associated domain are the same.
    static let baseURL = URL(string: "https://taptime.mt")!

    var body: some Scene {
        WindowGroup {
            RootView(model: ActivationModel(
                client: AppClient(baseURL: TaptimeApp.baseURL),
                keys: SecureEnclaveKeyFactory(),
                store: KeychainActivationStore(),
                reader: CoreNFCPlaqueReader()),
                     baseURL: TaptimeApp.baseURL)
        }
    }
}
