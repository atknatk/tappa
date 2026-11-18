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
        let look = BrandLook(model.brand)
        switch model.screen {
        case .checking:
            ScreenFrame(look: BrandLook(.taptime)) { EmptyView() }
        case .code(let notice):
            CodeScreen(model: model, notice: notice)
        case .welcome:
            if let invite = model.invite {
                WelcomeScreen(model: model, invite: invite, look: look, baseURL: baseURL)
            }
        case .ready:
            if let invite = model.invite {
                ReadyScreen(model: model, invite: invite, look: look)
            }
        case .tap(let problem):
            if let invite = model.invite {
                TapStepScreen(model: model, invite: invite, look: look, problem: problem)
            }
        case .done(let name, let second):
            DoneScreen(name: name, secondDevice: second, look: look)
        case .already(let name):
            AlreadyScreen(name: name, look: look)
        case .problem(let p, let retry):
            ProblemScreen(model: model, problem: p, retry: retry)
        }
    }
}
