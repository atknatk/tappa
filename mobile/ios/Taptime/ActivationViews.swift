import SwiftUI

// The activation screens (M16-16), drawn after the browser wizard (web/templates/pages/
// activate.templ): the same order, the same words (ActivationCopy.swift), the kitchen-docket
// panels and the palette of skill tappa-brand. Which look each screen's primary button takes is
// ONE rule, ActivationModel.Screen.primaryLook (CLAUDE.md §9) -- the views only draw it. The waiting step has no primary button in the
// browser; iOS reads a plaque only while its own sheet is open, so the tap step opens the sheet by
// itself and offers "Scan the plaque" again only when the sheet has closed.
//
// Fonts: Space Grotesk and IBM Plex Mono (Typeface.swift, M16-17), as on every browser screen.

// MARK: - Building blocks

struct Wordmark: View {
    let look: BrandLook

    var body: some View {
        VStack(spacing: 2) {
            Text("taptime")
                .font(Typeface.display(22, .bold))
                .foregroundColor(look.mark)
            Text("PUNCHLESS")
                .font(Typeface.mono(10))
                .kerning(2)
                .foregroundColor(Palette.ink.opacity(0.7))
        }
        .accessibilityElement(children: .combine)
    }
}

struct ScreenFrame<Content: View>: View {
    let look: BrandLook
    @ViewBuilder let content: () -> Content

    var body: some View {
        ZStack {
            Palette.porcelain.ignoresSafeArea()
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    Wordmark(look: look).frame(maxWidth: .infinity)
                    content()
                }
                .padding(16)
                .frame(maxWidth: 480)
                .frame(maxWidth: .infinity)
            }
        }
        .foregroundColor(Palette.ink)
    }
}

struct DocketLabel: View {
    let text: String

    var body: some View {
        Text(text.uppercased())
            .font(Typeface.mono(11, bold: true))
            .kerning(1.5)
            .foregroundColor(Palette.ink.opacity(0.7))
    }
}

struct Docket<Content: View>: View {
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 8, content: content)
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Palette.paper)
            .overlay(Rectangle().stroke(Palette.line, lineWidth: 1))
    }
}

struct Heading: View {
    let lines: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(lines.enumerated()), id: \.offset) { i, l in
                Text(l)
                    .font(Typeface.display(28, .bold))
                    .foregroundColor(i == 0 ? Palette.ink : Palette.ink.opacity(0.7))
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

enum NoticeTone {
    case ok, warn, alert

    var color: Color {
        switch self {
        case .ok: return Palette.green
        case .warn: return Palette.saffron
        case .alert: return Palette.tomato
        }
    }
}

struct Notice: View {
    let tone: NoticeTone
    var title: String?
    let text: String
    var hint: String?

    var body: some View {
        HStack(spacing: 0) {
            Rectangle().fill(tone.color).frame(width: 4)
            VStack(alignment: .leading, spacing: 6) {
                if let t = title {
                    Text(t).font(Typeface.display(17, .bold))
                }
                Text(text).font(Typeface.display(15))
                if let h = hint {
                    Text(h).font(Typeface.display(15)).foregroundColor(Palette.ink.opacity(0.7))
                }
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .background(Palette.paper)
        .fixedSize(horizontal: false, vertical: true)
    }
}

struct PrimaryButton: View {
    let title: String
    let look: BrandLook
    var busy = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                Text(title).opacity(busy ? 0 : 1)
                if busy {
                    ProgressView().progressViewStyle(CircularProgressViewStyle(tint: look.buttonText))
                }
            }
            .font(Typeface.display(20, .bold))
            .foregroundColor(look.buttonText)
            .frame(maxWidth: .infinity, minHeight: 64)
            .background(look.buttonFill)
            .overlay(Rectangle().strokeBorder(Palette.ink, lineWidth: look.buttonEdge ? 2 : 0))
        }
        .disabled(busy)
    }
}

struct BackButton: View {
    let title: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(Typeface.display(17))
                .underline()
                .foregroundColor(Palette.ink.opacity(0.85))
                .frame(maxWidth: .infinity, minHeight: 44)
        }
    }
}

struct StepProgress: View {
    let step: Int
    let look: BrandLook

    private var name: String {
        [Copy.Progress.welcome, Copy.Progress.ready, Copy.Progress.tap][step - 1]
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            DocketLabel(text: Copy.Progress.header(step: step, name: name))
            HStack(spacing: 6) {
                ForEach(1...3, id: \.self) { n in
                    Rectangle()
                        .fill(n <= step ? look.mark : Palette.line)
                        .frame(height: 4)
                }
            }
            .accessibilityHidden(true)
        }
    }
}

struct ActivatedStamp: View {
    let text: String

    var body: some View {
        Text(text.uppercased())
            .font(Typeface.display(11, .bold))
            .kerning(2)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(Palette.green.opacity(0.1))
            .overlay(Rectangle().stroke(Palette.green, lineWidth: 2))
            .overlay(Rectangle().inset(by: 3).stroke(Palette.green, lineWidth: 1))
            .rotationEffect(.degrees(-8))
    }
}

// MARK: - Screens

struct CodeScreen: View {
    @ObservedObject var model: ActivationModel
    let notice: Copy.Problem?
    let look: BrandLook

    var body: some View {
        ScreenFrame(look: look) {
            if let n = notice {
                Notice(tone: .alert, title: n.title, text: n.message, hint: n.hint)
            }
            Docket {
                DocketLabel(text: Copy.Code.label)
                Heading(lines: [Copy.Code.title])
                Text(Copy.Code.body).font(Typeface.display(17))
            }
            TextField(Copy.Code.field, text: $model.codeInput)
                .textInputAutocapitalization(.never)
                .disableAutocorrection(true)
                .keyboardType(.URL)
                .textContentType(.URL)
                .font(Typeface.mono(15))
                .padding(12)
                .frame(minHeight: 44)
                .background(Palette.paper)
                .overlay(Rectangle().stroke(Palette.line, lineWidth: 1))
                .onSubmit { Task { await model.submitCode() } }
            if let e = model.codeError {
                Notice(tone: .alert, text: e)
            }
            PrimaryButton(title: Copy.Code.button, look: look, busy: model.busy) {
                Task { await model.submitCode() }
            }
        }
    }
}

struct WelcomeScreen: View {
    @ObservedObject var model: ActivationModel
    let invite: LookupResponse
    let look: BrandLook
    let baseURL: URL
    @State private var showTerms = false

    var body: some View {
        ScreenFrame(look: look) {
            StepProgress(step: 1, look: look)
            VStack(alignment: .leading, spacing: 4) {
                DocketLabel(text: invite.businessName)
                Heading(lines: [Copy.Welcome.hi(invite.employeeName), Copy.Welcome.sub])
            }
            Docket {
                Text(Copy.Welcome.panel(invite.businessName)).font(Typeface.display(16))
            }
            if invite.secondDevice {
                Notice(tone: .warn, title: Copy.Welcome.newPhoneTitle, text: Copy.Welcome.newPhoneBody)
            }
            VStack(spacing: 4) {
                Text(Copy.Welcome.termsLead).font(Typeface.display(15))
                Button(Copy.Welcome.termsLink) { showTerms = true }
                    .font(Typeface.display(15, .bold))
                    .foregroundColor(Palette.ink)
                    .frame(minHeight: 44)
            }
            .frame(maxWidth: .infinity)
            PrimaryButton(title: Copy.Welcome.agree, look: look, busy: model.busy) {
                Task { await model.agree() }
            }
            BackButton(title: Copy.Ready.back) { model.back() }
        }
        .sheet(isPresented: $showTerms) {
            TermsSheet(employer: invite.businessName, baseURL: baseURL) { showTerms = false }
        }
    }
}

struct TermsSheet: View {
    let employer: String
    let baseURL: URL
    let close: () -> Void

    var body: some View {
        ZStack {
            Palette.paper.ignoresSafeArea()
            VStack(spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 10) {
                        Text(Copy.Terms.title).font(Typeface.display(22, .bold))
                        section(Copy.Terms.whoHeading, Copy.Terms.who(employer))
                        section(Copy.Terms.whatHeading, Copy.Terms.what)
                        section(Copy.Terms.whyHeading, Copy.Terms.why(employer))
                        Text(Copy.Terms.more).font(Typeface.display(15)).padding(.top, 8)
                        Link(Copy.Terms.privacy, destination: baseURL.appendingPathComponent("legal/privacy"))
                            .frame(minHeight: 44)
                        Link(Copy.Terms.terms, destination: baseURL.appendingPathComponent("legal/terms"))
                            .frame(minHeight: 44)
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                Button(Copy.Terms.close, action: close)
                    .font(Typeface.display(17, .bold))
                    .frame(maxWidth: .infinity, minHeight: 52)
                    .overlay(Rectangle().frame(height: 1).foregroundColor(Palette.line), alignment: .top)
            }
            .foregroundColor(Palette.ink)
        }
    }

    private func section(_ heading: String, _ body: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(heading).font(Typeface.display(15, .bold))
            Text(body).font(Typeface.display(15))
        }
        .padding(.top, 6)
    }
}

struct ReadyScreen: View {
    @ObservedObject var model: ActivationModel
    let invite: LookupResponse
    let look: BrandLook

    var body: some View {
        ScreenFrame(look: look) {
            StepProgress(step: 2, look: look)
            VStack(alignment: .leading, spacing: 4) {
                DocketLabel(text: Copy.Ready.label)
                Heading(lines: [Copy.Ready.title])
            }
            Docket {
                HStack(alignment: .top, spacing: 12) {
                    Text("01").font(Typeface.mono(15, bold: true)).foregroundColor(look.mark)
                    VStack(alignment: .leading, spacing: 4) {
                        Text(Copy.Ready.wifiTitle).font(Typeface.display(17, .bold))
                        if let ssid = invite.wifiSsid, !ssid.isEmpty {
                            (Text(Copy.Ready.wifiConnect + " ")
                                + Text(ssid).font(Typeface.mono(15, bold: true))
                                + Text(" " + Copy.Ready.wifiBody))
                                .font(Typeface.display(15))
                            Text(Copy.Ready.wifiSkip).font(Typeface.display(15)).foregroundColor(Palette.ink.opacity(0.7))
                        } else {
                            Text(Copy.Ready.noWifi).font(Typeface.display(15))
                        }
                    }
                }
            }
            PrimaryButton(title: Copy.Ready.ready, look: look) { model.ready() }
            BackButton(title: Copy.Ready.back) { model.back() }
        }
    }
}

struct TapStepScreen: View {
    @ObservedObject var model: ActivationModel
    let invite: LookupResponse
    let look: BrandLook
    /// The scan button's look -- primaryLook's, Taptime's green: an app-only button is no §9 slot.
    let buttonLook: BrandLook
    let problem: Copy.Problem?

    var body: some View {
        ScreenFrame(look: look) {
            StepProgress(step: 3, look: look)
            if let p = problem {
                Notice(tone: .alert, title: p.title, text: p.message, hint: p.hint)
            }
            VStack(alignment: .leading, spacing: 8) {
                DocketLabel(text: Copy.Tap.label)
                Heading(lines: [Copy.Tap.title])
                Text(Copy.Tap.body(invite.businessName)).font(Typeface.display(17))
                Text(Copy.Tap.firstTap).font(Typeface.display(17))
            }
            HStack(spacing: 8) {
                if model.scanning || model.busy {
                    ProgressView()
                }
                Text(Copy.Tap.waiting).font(Typeface.mono(15))
            }
            .frame(maxWidth: .infinity)
            .accessibilityElement(children: .combine)
            if invite.secondDevice {
                Notice(tone: .warn, text: Copy.Tap.signsOtherOut)
            }
            Text(Copy.Tap.whenDone)
                .font(Typeface.display(15))
                .foregroundColor(Palette.ink.opacity(0.7))
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
            if !model.scanning && !model.busy {
                PrimaryButton(title: Copy.Tap.scan, look: buttonLook) { model.scan() }
            }
            BackButton(title: Copy.Tap.back) { model.back() }
        }
    }
}

struct DoneScreen: View {
    let name: String
    let secondDevice: Bool
    let look: BrandLook

    var body: some View {
        ScreenFrame(look: look) {
            Docket {
                HStack(alignment: .top) {
                    DocketLabel(text: Copy.Done.label)
                    Spacer()
                    ActivatedStamp(text: Copy.Done.stamp)
                }
                Heading(lines: [Copy.Done.title])
                if !name.isEmpty {
                    Text(Copy.Done.setFor(name)).font(Typeface.display(16))
                }
            }
            Notice(tone: .ok, title: Copy.Done.clockTitle, text: Copy.Done.clockBody)
            if secondDevice {
                Notice(tone: .warn, text: Copy.Done.otherOut)
            }
            Text(Copy.Done.close)
                .font(Typeface.display(18))
                .foregroundColor(Palette.ink.opacity(0.7))
                .frame(maxWidth: .infinity)
        }
    }
}

/// A failure. Taptime's look whatever the invitation's brand: the browser's problem screens take
/// no brand (ADR 0023 WL-13), and neither does this one.
struct ProblemScreen: View {
    @ObservedObject var model: ActivationModel
    let problem: Copy.Problem
    let retry: ActivationModel.Retry
    let look: BrandLook

    var body: some View {
        ScreenFrame(look: look) {
            Notice(tone: .alert, title: problem.title, text: problem.message, hint: problem.hint)
            switch retry {
            case .none:
                EmptyView()
            case .toCode:
                PrimaryButton(title: Copy.startAgain, look: look) { model.retry(.toCode) }
            case .toWelcome, .toTap:
                PrimaryButton(title: Copy.tryAgain, look: look) { model.retry(retry) }
            case .relaunch:
                PrimaryButton(title: Copy.tryAgain, look: look) { model.retry(.relaunch) }
                // Not a dead end when the handle never opens again: activating anew replaces the
                // record; nothing is deleted before that succeeds.
                BackButton(title: Copy.startAgain) { model.retry(.toCode) }
            }
        }
    }
}
