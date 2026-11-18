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

    enum Already {
        static let label = "Setup"
        static let stamp = "Activated"
        static let title = "This phone is already set up"
        static func signedInAs(_ name: String) -> String { "Signed in as \(name)" }
        static let notice = "Tap a plaque to check in. There is nothing else to do here."
        static let close = "You can close the app." // [adapted]
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
        static let noReader = Problem( // [app]
            title: "This iPhone can't read the plaque",
            message: "Setting up the app needs a tap on the plaque, and this iPhone has no NFC reader.",
            hint: "Ask your manager — setup without a plaque tap isn't available yet.")
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
    /// The invite code inside what the person pasted: the activation link
    /// (<base>/activate?code=<code>) or the bare code. A code is 43 characters of unpadded
    /// base64url, 32 bytes (internal/invite codeLen); anything else is nil and never sent.
    static func parse(_ input: String) -> String? {
        let s = input.trimmingCharacters(in: .whitespacesAndNewlines)
        if let comps = URLComponents(string: s), comps.scheme != nil,
           let code = comps.queryItems?.first(where: { $0.name == "code" })?.value {
            return valid(code) ? code : nil
        }
        return valid(s) ? s : nil
    }

    private static func valid(_ c: String) -> Bool {
        c.utf8.count == 43 && Base64URL.decode(c)?.count == 32
    }
}
