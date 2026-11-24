import SwiftUI

// The app's one window: the activation screens until this phone is activated (M16-16), then the
// tap screens (M16-17). Where a plaque URL goes and what the app shows when it comes back to the
// front is AppModel's; this view only draws the screen each model says.
struct RootView: View {
    @ObservedObject var app: AppModel
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        RootContent(app: app, activation: app.activation, tap: app.tap)
            .task { await app.launch() }
            .onOpenURL { url in
                // Synchronously first: the link owns this return to the front (AppModel.linkArrived).
                app.linkArrived()
                Task { await app.open(url) }
            }
            .onChange(of: scenePhase) { phase in app.scenePhase(phase) }
    }
}

private struct RootContent: View {
    let app: AppModel
    @ObservedObject var activation: ActivationModel
    @ObservedObject var tap: TapModel

    var body: some View {
        // The header and marks follow the brand on every branded screen; the primary button
        // follows primaryLook (accent only on the wizard's steps 1 and 2).
        let look = BrandLook(activation.brand)
        let button = activation.screen.primaryLook(activation.brand)
        switch activation.screen {
        case .checking:
            ScreenFrame(look: BrandLook(.taptime)) { EmptyView() }
        case .activated:
            TapRootView(model: tap, staff: app.staff)
        case .notSetUp(let url):
            NotSetUpScreen(model: activation, url: url) { app.openInBrowser($0) }
        case .code(let notice):
            CodeScreen(model: activation, notice: notice, look: button)
        case .welcome:
            if let invite = activation.invite {
                WelcomeScreen(model: activation, invite: invite, look: button, baseURL: activation.baseURL)
            }
        case .ready:
            if let invite = activation.invite {
                ReadyScreen(model: activation, invite: invite, look: button)
            }
        case .tap(let problem):
            if let invite = activation.invite {
                TapStepScreen(model: activation, invite: invite, look: look, buttonLook: button, problem: problem)
            }
        case .waiting(let ended, let notice):
            WaitingScreen(model: activation, look: look, buttonLook: button, ended: ended, notice: notice)
        case .done(let name, let second):
            DoneScreen(name: name, secondDevice: second, look: look)
        case .problem(let p, let retry):
            ProblemScreen(model: activation, problem: p, retry: retry, look: button)
        }
    }
}
