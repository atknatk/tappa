import Foundation

// EVERY WORD THE TAP SCREENS SAY (M16-17; Android's twin is M16-13), in one file, so the two apps
// can be held to one list and the browser's words are visibly the source:
//   web/templates/pages/tap.templ        the tap screen ("Tapping", "Hello …", the button)
//   web/static/js/tap.js                  "Tapping…" while the press is under way
//   web/templates/pages/view.go           the button's words and the result's headline
//   web/templates/pages/result.templ      the result's closing sentences and the brand line
//   internal/handler/tap.go               the tap surface's problem screens
//   internal/handler/activate.go          "This phone was signed out"
//
// Taken word for word unless the line is marked:
//   [app]     the browser has no such line (iOS's scan sheet, no NFC reader, can't reach Taptime,
//             a phone clock the server cannot accept, "Open in browser", an app not set up yet)
//   [adapted] the browser's sentence names a browser thing (a page, a page to close) and says the
//             app's equivalent instead
enum TapCopy {
    enum Screen {
        static let label = "Tapping"
        static func hello(_ name: String) -> String { "Hello \(name)" }
        /// The button while the press is under way (tap.js markPending).
        static let pending = "Tapping…"
        /// iOS reads a plaque only inside its own scan sheet; this is the sheet's line. [app]
        static let sheet = Copy.Tap.sheet
    }

    /// The tap button's words for a direction ("in" / "out" / "" -- the wire's spelling).
    static func button(_ direction: String) -> String {
        switch direction {
        case "in": return "Clock in"
        case "out": return "Clock out"
        default: return "Tap"
        }
    }

    /// The direction the "Not right? …" button offers, or "" when there is nothing to correct.
    static func other(_ direction: String) -> String {
        switch direction {
        case "in": return "out"
        case "out": return "in"
        default: return ""
        }
    }

    /// "Not right? Clock out" under a predicted "Clock in", and the other way round; "" without one.
    static func correction(_ direction: String) -> String {
        let o = other(direction)
        return o.isEmpty ? "" : "Not right? " + button(o)
    }

    enum Result {
        /// ResultView.Headline: the verdict first, a direction only for the two that have one.
        static func headline(verdict: String, direction: String) -> String {
            switch verdict {
            case "ok", "flag":
                switch direction {
                case "in": return "Clocked in"
                case "out": return "Clocked out"
                default: return "Tapped"
                }
            case "ignored":
                return "Already tapped"
            case "reject":
                return "Not counted"
            default:
                return "Tapped"
            }
        }

        static let practice = "This practice tap does not count toward your hours."
        static let allDone = "All done — you can close the app." // [adapted: "… close this page."]
        static let close = "You can close the app." // [adapted: "You can close this page."]
        static let ignored = "You tapped a moment ago, so this one was not counted. Tell your manager if that looks wrong."
        static let rejected = "This tap was recorded but not counted. Tell your manager if that looks wrong."

        /// The closing sentences under the docket, one list per verdict (result.templ closing).
        static func closing(verdict: String) -> [String] {
            switch verdict {
            case "ok": return [allDone]
            case "flag": return [close]
            case "ignored": return [ignored, close]
            default: return [rejected, close]
            }
        }

        /// The warm line of a counted tap (result.templ brandMessage): keyed on the business TYPE,
        /// never on the business; nil on a practice tap and on every verdict but ok.
        static func brandLine(verdict: String, direction: String, businessType: String, practice: Bool) -> String? {
            guard verdict == "ok", !practice else {
                return nil
            }
            switch direction {
            case "in":
                switch businessType {
                case "restaurant": return "Have a great shift — keep those kebabs rolling! 🌯"
                case "production": return "Have a productive shift — stay safe on the floor! 🏭"
                default: return "Have a great shift!"
                }
            case "out":
                switch businessType {
                case "restaurant": return "Great work today. See you next shift! 👋"
                case "production": return "Shift complete. Thank you for your work today! 👋"
                default: return "Thanks for today. See you next time! 👋"
                }
            default:
                return "Thanks — have a good one!"
            }
        }
    }

    enum Problems {
        static let badURL = Copy.Problem(
            title: "That link didn't work",
            message: "Taptime opens when you hold your phone to the plaque at the door.", // [adapted: "This page opens …"]
            hint: "Try tapping the plaque again.")
        static let unknownPlaque = Copy.Problem(
            title: "We don't know that plaque",
            message: "This plaque isn't set up with Taptime, or it has been replaced.",
            hint: "Tell your manager which door you tapped.")
        static let server = Copy.Problem(
            title: "Something went wrong on our side",
            message: "Nothing was recorded. This is not your fault and it is not your time.",
            hint: "Tap the plaque again in a moment.")
        static let tooMany = Copy.Problem(
            title: "Too many taps from here",
            message: "We have paused requests from this network for a few minutes.",
            hint: "Wait a moment and tap the plaque again. Tell your manager if it keeps happening.")
        static let stale = Copy.Problem(
            title: "That tap has expired",
            message: "Nothing was recorded. Tap screens are only good for a few minutes.", // [adapted: "Tap pages …"]
            hint: "Hold your phone to the plaque again.")
        static let foreignTenant = Copy.Problem(
            title: "That plaque isn't yours",
            message: "This plaque belongs to a different employer, so nothing was recorded.",
            hint: "Use your own workplace's plaque, or tell your manager.")
        static let signedOut = Copy.Problems.signedOut
        static let offline = Copy.Problem( // [app] -- nothing was sent that could record a tap
            title: "Can't reach Taptime",
            message: "Nothing was recorded.",
            hint: "Check your connection and tap the plaque again.")
        static let offlineUnknown = Copy.Problem( // [app] -- the press was sent; its answer never came
            title: "Can't reach Taptime",
            message: "We couldn't confirm this tap, so it may not have been recorded.",
            hint: "Check your connection and tap the plaque again. Tell your manager if that looks wrong.")
        static let notRead = Copy.Problem( // [app] -- the scan sheet closed on an error
            title: "The plaque wasn't read",
            message: "Nothing was recorded.",
            hint: "Hold the top of your phone against the plaque and try again.")
        static let noReader = Copy.Problem( // [app] -- NFCNDEFReaderSession.readingAvailable is false
            title: "This iPhone can't read the plaque",
            message: "This iPhone has no NFC reader, so the app can't record a tap from here.",
            hint: "Ask your manager.")
        static let clock = Copy.Problem( // [app] -- M16-16 hand-off 4
            title: "Check your phone's clock",
            message: "Your phone's date and time are too far off for Taptime to accept them. Nothing was recorded.",
            hint: "Set the date and time to automatic in Settings, then try again.")
        static let notSetUp = Copy.Problem( // [adapted: the browser's "Finish setup in this browser"]
            title: "This app isn't set up yet",
            message: "This app hasn't been set up for Taptime yet, so it can't record this tap.",
            hint: "Open the tap in your browser instead, or set up the app with the link your workplace sent you.")
    }

    static let openInBrowser = "Open in browser" // [app] -- ADR 0045 Bedel 8
    static let tryAgain = Copy.tryAgain
    static let startAgain = Copy.startAgain
    static let setUp = Copy.Code.title // "Set up this phone"
}
