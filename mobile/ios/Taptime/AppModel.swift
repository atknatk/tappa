import SwiftUI

// WHERE A PLAQUE URL GOES, AND WHAT THE APP SHOWS WHEN IT COMES BACK (M16-17). The two halves of
// the app -- activation (ActivationModel) and the tap (TapModel) -- are routed here and nowhere
// else:
//
//   launch        the Keychain record is read first; a plaque URL that arrives before that (a
//                 Universal Link that LAUNCHED the app) waits for it and is then routed
//   plaque URL    (or a venue screen's live QR URL, /q -- M16-22: the same routes)
//                 activated phone   → the tap screen of THIS read (TapModel.plaque)
//                 wizard in progress → the activation (its tap step completes it)
//                 anything else     → "This app isn't set up yet", with Safari (M16-16 hand-off 2)
//   back in front a result, a problem, "Activation complete" -- none of them has a way forward on
//                 an iPhone 7-X, which reads a plaque only from the app's own button -- so the app
//                 opens on its tap screen again (GET /app/v1/me), unless a plaque URL brought it
//                 to the front, which wins, or a result arrived while the app was away, which is
//                 shown once before the next return goes home.
@MainActor
final class AppModel: ObservableObject {
    let activation: ActivationModel
    let tap: TapModel
    /// The staff screens (the native shell, 2026-11-24): Today, My hours, Settings.
    let staff: StaffModel
    private let browser: BrowserOpening
    private let presence: PresenceSync
    private let reminder: ReminderScheduling
    private var launched = false
    private var queued: URL?
    private var returnHome = false
    /// Counts plaque links as they ARRIVE (linkArrived, synchronous in onOpenURL) -- not when their
    /// Task runs. The way back home checks it when its own Task runs, so a link that came with the
    /// return to the front wins whichever of the two Tasks the main actor runs first.
    private var links = 0

    init(activation: ActivationModel, tap: TapModel, browser: BrowserOpening,
         presence: PresenceSync, reminder: ReminderScheduling) {
        self.activation = activation
        self.tap = tap
        self.staff = StaffModel(tap: tap)
        self.browser = browser
        self.presence = presence
        self.reminder = reminder
        staff.onForgotten = { [weak self, weak activation, weak tap] in
            tap?.leave()
            self?.forgetPresence()
            activation?.forgetDevice()
        }
        tap.onMe = { [weak self, weak tap] me in
            let offset = tap?.api.clock.currentOffset ?? 0
            self?.reminder.apply(me, offset: offset)
            // The widget and the lock-screen counter (M16-47) follow the same answer.
            self?.presence.apply(me, offset: offset)
        }
        // EVERY signed-out path clears the reminder, the widget's share and the counter (M16-46
        // trigger 3, M16-47): the home screen's 401, a tap's 401 (its problem screen and its
        // "Start again"), and the quiet /me after a tap.
        tap.onSignedOut = { [weak self, weak activation] in
            self?.forgetPresence()
            self?.staff.forgetPerson() // M16-38: the next activation may be another person
            activation?.signedOut()
        }
        tap.onPresenceGone = { [weak self] in self?.forgetPresence() }
        tap.onStartAgain = { [weak self, weak activation] in
            self?.forgetPresence()
            activation?.startAgain()
        }
        activation.onRelaunch = { [weak self] in
            Task { await self?.boot() }
        }
        activation.onQueuedURL = { [weak self] url in
            Task { await self?.open(url) }
        }
    }

    static func live(baseURL: URL) -> AppModel {
        let client = AppClient(baseURL: baseURL)
        let reader = CoreNFCPlaqueReader()
        let browser = SystemBrowser()
        return AppModel(
            activation: ActivationModel(client: client, keys: SecureEnclaveKeyFactory(), store: KeychainActivationStore(),
                                        reader: reader),
            tap: TapModel(client: client, reader: reader, location: OneShotLocation(), browser: browser),
            browser: browser, presence: .shared, reminder: StaffNotifier.shared)
    }

    private func forgetPresence() {
        reminder.cancel()
        presence.clear()
    }

    func launch() async {
        guard !launched else {
            return
        }
        await boot()
    }

    /// Reads the record and lands on the right screen; a URL that waited for it goes first.
    private func boot() async {
        await activation.start()
        launched = true
        if activation.device == nil {
            // No activated phone (the Android twin's NotActivated): nothing may say "at work".
            forgetPresence()
        }
        if let d = activation.device, activation.screen == .activated {
            tap.enter(d)
        }
        if let u = queued {
            queued = nil
            await open(u)
        } else if activation.screen == .activated {
            await tap.home()
        }
    }

    /// Called by onOpenURL the moment the link arrives, before any Task: this return to the front is
    /// the link's (M16-17 review -- iOS may deliver .active first, and a Task only marks it late).
    func linkArrived() {
        links += 1
        returnHome = false
    }

    func open(_ url: URL) async {
        guard launched else {
            queued = url
            return
        }
        returnHome = false
        if let d = activation.device, !activation.inWizard {
            tap.enter(d)
            activation.showActivated()
            await tap.plaque(url)
        } else {
            await activation.plaqueURLOpened(url)
        }
    }

    func openInBrowser(_ url: URL) {
        browser.open(url)
    }

    func scenePhase(_ phase: ScenePhase) {
        switch phase {
        case .background:
            returnHome = launched
            tap.inForeground = false
        case .active:
            tap.inForeground = true
            guard returnHome else {
                return
            }
            returnHome = false
            // A result that came back while the app was away is shown first; the NEXT return goes
            // home. (A plaque link never reaches here: linkArrived cleared returnHome.)
            if activation.screen == .activated, tap.showUnseenResult() {
                // The result stays; the reminder, the widget and the counter catch up with it.
                Task { [weak self] in await self?.tap.syncPresence() }
                return
            }
            guard let d = activation.device else {
                return
            }
            switch activation.screen {
            case .done, .activated:
                let seen = links
                Task { [weak self] in
                    // A link arrived after .active but before this ran: the link's screen stands.
                    guard let self = self, self.links == seen else {
                        return
                    }
                    self.tap.enter(d)
                    self.activation.showActivated()
                    await self.tap.home()
                }
            default:
                break
            }
        default:
            break
        }
    }
}

/// What AppModel needs of the reminder (consumer-side interface, §7; the tests pass a fake).
protocol ReminderScheduling: AnyObject {
    /// Follows one /me answer.
    func apply(_ me: MeResponse?, offset: TimeInterval)
    /// Takes the pending and the delivered reminder away.
    func cancel()
}
