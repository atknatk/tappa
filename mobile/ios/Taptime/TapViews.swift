import SwiftUI

// The tap screens (M16-17), drawn after the browser's (web/templates/pages/tap.templ, result.templ,
// the tap surface's problem pages): the same words (TapCopy.swift), the same order, the docket,
// the palette and the two typefaces of skill tappa-brand. CLAUDE.md §9 holds here as on the web:
//
//   the tap screen   a docket ("Tapping", "Hello Maria", the venue) and ONE button; under a
//                    predicted direction only, the small "Not right? …". The person's name is the
//                    largest text on it: on a phone that two people share, the app says whose tap
//                    this is before anything is recorded (ADR 0045, the accepted shared-phone risk).
//   the result       NO button. "All done — you can close the app." The next record needs a new
//                    touch of the plaque.
//   a problem        "Try again" where a retry can change something; "Open in browser" only where
//                    the plaque URL is unspent (TapFlow.swift says when).
//
// The business's brand fills ONE slot here: the tap button's fill (BrandLook). With an accent the
// wordmark is ink (K-2a). The result and the problems are Taptime's. There is no logo: the app has
// no route to the logo's bytes yet (M16-07 hand-off 2), so every screen draws the logo-less header.
//
// No timer lives in this file or in TapFlow.swift: nothing on these screens moves by itself
// (redline R2's periodic net is per file, M16-17 card's trap).

struct TapRootView: View {
    @ObservedObject var model: TapModel
    let staff: StaffModel

    var body: some View {
        switch model.screen {
        case .loading:
            ScreenFrame(look: BrandLook(.taptime)) {
                ProgressView().frame(maxWidth: .infinity)
            }
        case .tap(let view, let problem) where view.source == .home:
            // The app's own home (/me): the staff shell, whose Today tab holds the plaque button.
            StaffRootView(staff: staff, tap: model, view: view, problem: problem)
                .task { await staff.refreshToday() }
        case .tap(let view, let problem):
            TapScreen(model: model, view: view, problem: problem)
        case .result(let r):
            TapResultScreen(result: r)
        case .problem(let p):
            TapProblemScreen(model: model, problem: p)
        }
    }
}

/// The one button: 64 px, the accent's fill (or Taptime's green), Space Grotesk 20 / 400 -- the
/// `.tap-button` of input.css.
struct TapButton: View {
    let title: String
    let look: BrandLook
    let disabled: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(Typeface.display(20))
                .tracking(-0.5)
                .foregroundColor(look.buttonText)
                .frame(maxWidth: .infinity, minHeight: 64)
                .background(look.buttonFill)
                .overlay(RoundedRectangle(cornerRadius: 2).strokeBorder(Palette.ink, lineWidth: look.buttonEdge ? 2 : 0))
                .clipShape(RoundedRectangle(cornerRadius: 2))
        }
        .disabled(disabled)
    }
}

struct TapScreen: View {
    @ObservedObject var model: TapModel
    let view: TapModel.View
    let problem: Copy.Problem?

    var body: some View {
        let look = BrandLook(view.brand)
        ScreenFrame(look: look) {
            if let p = problem {
                Notice(tone: .alert, title: p.title, text: p.message, hint: p.hint)
            }
            Docket {
                DocketLabel(text: TapCopy.Screen.label)
                Text(TapCopy.Screen.hello(view.employeeName))
                    .font(Typeface.display(30, .bold))
                    .tracking(-0.75)
                    .accessibilityAddTraits(.isHeader)
                if !view.locationName.isEmpty {
                    Text(view.locationName)
                        .font(Typeface.display(16))
                        .foregroundColor(Palette.ink.opacity(0.7))
                }
            }
            TapButton(title: model.pressing ? TapCopy.Screen.pending : TapCopy.button(view.direction),
                      look: look, disabled: model.pressing) {
                Task { await model.press(want: view.direction.isEmpty ? nil : view.direction) }
            }
            let fix = TapCopy.correction(view.direction)
            if !fix.isEmpty {
                // Ink on porcelain, underlined, 44 px tall, 24 px under the button; never the accent.
                Button {
                    Task { await model.press(want: TapCopy.other(view.direction)) }
                } label: {
                    Text(fix)
                        .font(Typeface.display(16))
                        .underline()
                        .foregroundColor(Palette.ink)
                        .padding(.horizontal, 12)
                        .frame(minHeight: 44)
                }
                .disabled(model.pressing)
                .padding(.top, 4)
                .frame(maxWidth: .infinity)
            }
        }
    }
}

struct TapResultScreen: View {
    let result: TapModel.Result

    var body: some View {
        ScreenFrame(look: BrandLook(.taptime)) {
            Docket {
                if !result.venue.isEmpty {
                    DocketLabel(text: result.venue)
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text(TapCopy.Result.headline(verdict: result.verdict, direction: result.direction))
                        .font(Typeface.display(30, .bold))
                        .tracking(-0.75)
                    if !result.at.isEmpty {
                        Text(result.at)
                            .font(Typeface.mono(20))
                            .foregroundColor(Palette.ink.opacity(0.7))
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isHeader)
                if result.practice {
                    Text(TapCopy.Result.practice)
                        .font(Typeface.display(16))
                        .foregroundColor(Palette.ink.opacity(0.7))
                        .padding(.top, 4)
                }
            }
            if let line = TapCopy.Result.brandLine(verdict: result.verdict, direction: result.direction,
                                                   businessType: result.businessType, practice: result.practice) {
                Text(line).font(Typeface.display(18)).tracking(-0.45)
            }
            ForEach(TapCopy.Result.closing(verdict: result.verdict), id: \.self) { line in
                Text(line)
                    .font(Typeface.display(16))
                    .foregroundColor(Palette.ink.opacity(0.7))
            }
        }
    }
}

/// A failure. Taptime's look whatever the plaque's business (the browser's problem pages take no
/// brand). Its buttons are the app's: the browser's back and reload buttons do not exist here.
struct TapProblemScreen: View {
    @ObservedObject var model: TapModel
    let problem: TapModel.Problem

    var body: some View {
        let look = BrandLook(.taptime)
        ScreenFrame(look: look) {
            Notice(tone: .alert, title: problem.copy.title, text: problem.copy.message, hint: problem.copy.hint)
            if problem.retry {
                PrimaryButton(title: TapCopy.tryAgain, look: look) { model.retry() }
                if let url = problem.browserURL {
                    BackButton(title: TapCopy.openInBrowser) { model.openInBrowser(url) }
                }
            } else if let url = problem.browserURL {
                PrimaryButton(title: TapCopy.openInBrowser, look: look) { model.openInBrowser(url) }
            }
            if problem.startAgain {
                BackButton(title: TapCopy.startAgain) { model.startAgain() }
            }
        }
    }
}

/// A plaque URL on a phone that is not set up (M16-16 hand-off 2): the tap can still be recorded in
/// Safari -- nothing was sent, so the URL is unspent -- or the person sets up the app.
struct NotSetUpScreen: View {
    @ObservedObject var model: ActivationModel
    let url: URL
    let open: (URL) -> Void

    var body: some View {
        let look = BrandLook(.taptime)
        ScreenFrame(look: look) {
            let p = TapCopy.Problems.notSetUp
            Notice(tone: .alert, title: p.title, text: p.message, hint: p.hint)
            PrimaryButton(title: TapCopy.openInBrowser, look: look) { open(url) }
            BackButton(title: TapCopy.setUp) { model.retry(.toCode) }
        }
    }
}
