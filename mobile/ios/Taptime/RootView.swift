import SwiftUI

// The app's one window: the activation screens until this phone is activated (M16-16), then the
// "already set up" screen that M16-17's tap screen replaces.
struct RootView: View {
    @StateObject var model: ActivationModel
    let baseURL: URL

    var body: some View {
        content
            .task { await model.start() }
            // A plaque link opened while the tap step is showing completes the activation like a
            // read from the scan sheet; anywhere else the model ignores it (M16-17 routes the rest).
            .onOpenURL { url in Task { await model.plaqueURLOpened(url) } }
    }

    @ViewBuilder
    private var content: some View {
        // The header and marks follow the brand on every branded screen; the primary button
        // follows primaryLook (accent only on the wizard's steps 1-3).
        let look = BrandLook(model.brand)
        let button = model.screen.primaryLook(model.brand)
        switch model.screen {
        case .checking:
            ScreenFrame(look: BrandLook(.taptime)) { EmptyView() }
        case .code(let notice):
            CodeScreen(model: model, notice: notice, look: button)
        case .welcome:
            if let invite = model.invite {
                WelcomeScreen(model: model, invite: invite, look: button, baseURL: baseURL)
            }
        case .ready:
            if let invite = model.invite {
                ReadyScreen(model: model, invite: invite, look: button)
            }
        case .tap(let problem):
            if let invite = model.invite {
                TapStepScreen(model: model, invite: invite, look: button, problem: problem)
            }
        case .done(let name, let second):
            DoneScreen(name: name, secondDevice: second, look: look)
        case .already(let name):
            AlreadyScreen(name: name, look: look)
        case .problem(let p, let retry):
            ProblemScreen(model: model, problem: p, retry: retry, look: button)
        }
    }
}
