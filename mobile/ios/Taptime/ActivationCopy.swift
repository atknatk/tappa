import Foundation

// EVERY WORD THE ACTIVATION SCREENS SAY, in one file, so the iOS and Android apps (M16-12) can be
// held to the same list and so the browser wizard's words are visibly the source
// (web/templates/pages/activate.templ, internal/handler/activate.go's problem screens).
//
// Taken word for word from the wizard unless the line is marked:
//   [app]     the browser has no such screen or line (the code entry; the scan button iOS needs
//             to open its reader; the iPhone-specific refusals; "can't reach Taptime")
//   [adapted] the wizard's sentence names a browser thing the app does not have (a link to open
//             again, a cookie, a page to close) and says the app's equivalent instead
//   [M16-23]  the phone that cannot tap: the browser wizard takes the same sentences (the same card)
//   [R1]      the wizard's "no ..., no face, no voice" opens with a word the red-line scan
//             (scripts/redline-check.sh R1) refuses anywhere in the apps' tree, comments and tests
//             included, with no waiver, by design (M16-02). "No finger scans" says the same thing.
enum Copy {
    enum Code {
        static let label = "Setup" // [app]
        static let title = "Set up this phone" // [app]
        static let body = "Paste the activation link your workplace sent you." // [app]
        static let field = "Activation link" // [app]
        static let button = "Continue" // [app]
        static let notALink = "That isn't an activation link. Copy the whole link from the message your workplace sent and paste it here." // [app]
    }

    enum Progress {
        static let welcome = "Welcome"
        static let ready = "Get ready"
        static let tap = "Tap"
        static func header(step: Int, name: String) -> String { "Step \(step) of 3 · \(name)" }
    }

    enum Welcome {
        static func hi(_ name: String) -> String { "Hi \(name)" }
        static let sub = "Let's set up your phone."
        static func panel(_ employer: String) -> String {
            "\(employer) uses Taptime to record your shifts. Each tap saves the time, your phone's IP address and, if your phone shares it, your location at that moment. Your location is never tracked between taps. No finger scans, no face, no voice." // [R1]
        }
        static let newPhoneTitle = "This is a new phone"
        static let newPhoneBody = "You are already set up on another phone. When you finish here, that one is signed out — only the phone you are holding will work. If you did not expect this, stop and tell your manager."
        static let termsLead = "By continuing, you agree to the"
        static let termsLink = "terms and privacy details."
        static let agree = "Agree and continue"
    }

    enum Terms {
        static let title = "Terms and privacy"
        static let whoHeading = "Who is responsible"
        static func who(_ employer: String) -> String {
            "\(employer) is responsible for your attendance records. Taptime processes them on its behalf."
        }
        static let whatHeading = "What is recorded"
        static let what = "The time of each tap, the venue, your phone's IP address and, if your phone shares it, your location at that moment. A key kept on this phone keeps it signed in, so you don't need a password. Your location is never tracked between taps. No finger scans, no face, no voice." // [adapted: a key, not a cookie] [R1]
        static let whyHeading = "Why"
        static func why(_ employer: String) -> String { endSentence("To record your working hours for \(employer)") }
        static let more = "For more information, visit:"
        static let privacy = "Privacy policy"
        static let terms = "Terms of service"
        static let close = "Close"
    }

    enum Ready {
        static let label = "Get ready"
        static let title = "One quick thing before you tap." // [adapted: the browser's second thing, "Stay in this browser", has no app twin]
        static let wifiTitle = "Join the Wi-Fi"
        static let wifiConnect = "Connect to"
        static let wifiBody = "when you are at work. Being on the venue's network is how Taptime knows you are really there."
        static let wifiSkip = "You can skip this. Your taps then use your location instead, and your manager may need to confirm them."
        static let noWifi = "This workplace has no staff network to join, so Taptime checks your location at the moment you tap instead."
        static let ready = "I'm ready"
        static let back = "Back"
    }

    enum Tap {
        static let label = "Last step"
        static let title = "Now tap the plaque."
        static func body(_ employer: String) -> String {
            "Hold the top of your phone against any Taptime plaque at your workplace, \(employer)"
        }
        static let firstTap = "That first tap finishes setting up this phone — it doesn't clock you in."
        static let waiting = "Waiting for your tap…"
        static let signsOtherOut = "Your tap signs your other phone out."
        static let whenDone = "When the tap goes through, your phone shows “Activation complete”."
        static let scan = "Scan the plaque" // [app: iOS reads only while its own sheet is open]
        static let sheet = "Hold the top of your phone against the plaque." // [app: iOS's scan sheet]
        static let back = "Back"
        /// The secondary way when the manager can approve (lookup's managerApproval). Never the accent's slot.
        static let cantTap = "My phone can't tap" // [M16-23]
        /// In its place when the manager cannot (a screen runs there, or the server offers nothing).
        static let scanScreen = "Phone can't tap? Open your camera and scan the code on the Taptime screen at your workplace." // [M16-23]
    }

    /// Waiting for the manager's approval (M16-23). The pairing code sits between body and status.
    enum Approval {
        static let label = "Last step" // [M16-23]
        static let title = "Ask your manager to approve this phone" // [M16-23]
        static let body = "Show this code to your manager. They approve it in Taptime, under People." // [M16-23]
        static let status = "Waiting for your manager…" // [M16-23]
        static let footnote = "The code works for 15 minutes." // [M16-23]
        static let back = "Back"
        static let askAgain = "Ask again" // [M16-23] -- after a rejection
        static let newCode = "Get a new code" // [M16-23] -- after the code ran out
    }

    enum Done {
        static let label = "Setup"
        static let stamp = "Activated"
        static let title = "Activation complete"
        static func setFor(_ name: String) -> String { "This phone is set up for \(name)" }
        static let clockTitle = "This phone is now your time clock."
        static let clockBody = "To check in, tap the plaque again — that next tap counts."
        static let otherOut = "Your other phone has been signed out. Only this one works now."
        static let close = "You can close the app." // [adapted: "You can close this page."]
    }

    struct Problem: Equatable {
        let title: String
        let message: String
        let hint: String
    }

    enum Problems {
        static let badLink = Problem(
            title: "This link can't be used",
            message: "It may already have been used, or it may have run out of time.",
            hint: "Ask your manager for a new one — it only takes a moment.")
        static let tooMany = Problem(
            title: "Too many attempts",
            message: "We have stopped accepting activations from here for a few minutes.",
            hint: "Wait a little and try again.") // [adapted: "...and open your link again."]
        static let server = Problem(
            title: "Something went wrong on our side",
            message: "Nothing was activated and nothing was recorded against you.",
            hint: "Try again in a minute.") // [adapted: "Try your link again in a minute."]
        static let offline = Problem( // [app]
            title: "Can't reach Taptime",
            message: "Nothing was activated and nothing was recorded against you.",
            hint: "Check your connection and try again.")
        static let signedOut = Problem(
            title: "This phone was signed out",
            message: "This phone is no longer signed in to Taptime, so it can't record time.",
            hint: "Ask your manager for a new activation link.")
        static let tapFailed = Problem(
            title: "That tap didn't finish setup",
            message: "Your phone isn't activated yet, and nothing was recorded.",
            hint: "Hold the top of your phone against a Taptime plaque at your workplace and try again.")
        static let needsTouch = Problem(
            title: "Hold your phone against the plaque",
            message: "Activation needs a real tap on the plaque, so this didn't finish it.",
            hint: "Touch the top of your phone to the plaque to finish.")
        static let notReady = Problem(
            title: "Finish the setup steps first",
            message: "This phone hasn't agreed to the privacy notice yet, so the tap didn't activate it.",
            hint: "Go through the steps again, then tap the plaque.") // [adapted: "Open your activation link again, ..."]
        static let keyUnreadable = Problem( // [app]
            title: "Taptime couldn't open this phone's key",
            message: "Nothing was changed on this phone.",
            hint: "Try again in a moment. If it keeps happening, you can set this phone up again.")
        /// [app] M16-23: no longer a refusal -- the notice on the tap step in place of the scan button.
        /// The hint is shown only when the manager can approve (TapStepScreen).
        static let noReader = Problem(
            title: "This iPhone can't read the plaque",
            message: "Finish setup by scanning the code on the Taptime screen at your workplace with your camera.",
            hint: "Or ask your manager to approve this phone.")
        static let liveQRFailed = Problem( // [M16-23] -- every refusal of a live QR read is one code
            title: "That scan didn't finish setup",
            message: "Your phone isn't activated yet, and nothing was recorded.",
            hint: "Scan the code on the Taptime screen at your workplace again — it changes every few seconds.")
        static let approvalRejected = Problem( // [M16-23]
            title: "Your manager didn't approve this phone",
            message: "Nothing was set up and nothing was recorded.",
            hint: "Talk to your manager. If they ask you to, try again.")
        static let approvalExpired = Problem( // [M16-23]
            title: "That code ran out of time",
            message: "Your manager didn't approve it in time, so nothing was set up.",
            hint: "Get a new code and show it to your manager.")
        static let screenAvailable = Problem( // [M16-23] -- screen_available
            title: "Scan the screen's code instead",
            message: "There's a Taptime screen at your workplace, so your manager can't approve this phone.",
            hint: "Open your camera and scan the code on the screen to finish setup.")
        static let noSecureChip = Problem( // [app]
            title: "This iPhone can't be set up",
            message: "The app keeps its key in this iPhone's secure chip, and the chip isn't available.",
            hint: "Ask your manager — you can still use Taptime in your browser.")
    }

    static let tryAgain = "Try again"
    static let startAgain = "Start again"

    /// Closes a sentence that ends in a business's name without doubling a full stop the name may
    /// already carry ("Ltd.") -- the wizard's endSentence.
    static func endSentence(_ s: String) -> String { s.hasSuffix(".") ? s : s + "." }
}

enum InviteCode {
    /// The path of the activation link (internal/invite activationURL: <base>/activate?code=..).
    static let path = "/activate"

    /// The invite code inside what the person pasted: the activation link or the bare code. A link
    /// counts only if it is THIS server's -- https, the app's own host, the /activate path -- so a
    /// look-alike link of another host never has its code sent here (M16-16 review). A code is 43
    /// characters of unpadded base64url, 32 bytes (internal/invite codeLen); anything else is nil
    /// and never sent.
    static func parse(_ input: String, host: String) -> String? {
        let s = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let comps = URLComponents(string: s), comps.scheme != nil else {
            return valid(s) ? s : nil
        }
        guard comps.scheme?.lowercased() == "https", comps.host?.lowercased() == host.lowercased(),
              comps.percentEncodedPath == path,
              let code = comps.queryItems?.first(where: { $0.name == "code" })?.value, valid(code) else {
            return nil
        }
        return code
    }

    private static func valid(_ c: String) -> Bool {
        c.utf8.count == 43 && Base64URL.decode(c)?.count == 32
    }
}
