import Foundation

// THE APP'S ACTIVATION, screen by screen (ADR 0026's two steps for a device key; ADR 0045 §2.3;
// M16-16, the twin of Android's M16-12). The browser wizard's order, with the code entry in front
// because the invite link opens the browser, not the app (Q40):
//
//   code     the person pastes the link; the app keeps only the code        → lookup
//   welcome  who this is for, what is recorded, "Agree and continue"        → a NEW key, bind
//   ready    the venue Wi-Fi
//   tap      "Now tap the plaque": the first genuine NFC read               → complete
//   done     "Activation complete"
//
// After it, the phone is ACTIVATED and the tap screens (TapFlow.swift, M16-17) take over: the
// launch hands the restored key to them instead of greeting the person here.
//
// NOTHING BEFORE THE TAP ACTIVATES ANYTHING. bind records consent bound to this key and spends
// nothing; the device is born on the server only when complete passes CompleteByTap's order --
// and that touch writes no transactions row (CLAUDE.md §5, "Aktivasyon dokunuşu").
//
// A NEW KEY FOR EVERY BIND. The pending key lives in memory only until complete succeeds; then
// the record (device id, name, the key's Secure Enclave handle) goes to the Keychain. A
// new_key_required answer (the key is somebody's already) makes a fresh key and binds again --
// the plaque read that got it is spent, so the person taps once more.
@MainActor
final class ActivationModel: ObservableObject {
    enum Retry: Equatable {
        case toCode, toWelcome, toTap
        /// Run the launch again (the stored key could not be opened just now).
        case relaunch
        /// Nothing the person can do on this phone (no reader, no secure chip).
        case none
    }

    enum Screen: Equatable {
        case checking
        case code(notice: Copy.Problem?)
        case welcome
        case ready
        case tap(problem: Copy.Problem?)
        case done(name: String, secondDevice: Bool)
        /// The phone is activated: the tap screens are showing (RootView).
        case activated
        /// A plaque URL reached a phone that is not set up (M16-16 hand-off 2): Safari can record it,
        /// or the person sets up the app.
        case notSetUp(URL)
        case problem(Copy.Problem, retry: Retry)
    }

    @Published private(set) var screen: Screen = .checking
    @Published var codeInput = ""
    @Published private(set) var codeError: String?
    @Published private(set) var busy = false
    @Published private(set) var scanning = false
    @Published private(set) var invite: LookupResponse?
    @Published private(set) var brand: AppBrand = .taptime
    /// The activated device: restored at launch, or made by a successful activation. nil until
    /// then. Only a new successful activation replaces it.
    @Published private(set) var device: ActivatedDevice?
    /// "Try again" on the unreadable-key screen runs the whole launch again (AppModel), so an
    /// activated phone lands on its tap screen, not on an empty one.
    var onRelaunch: (() -> Void)?
    /// Where a plaque link that waited (queuedURL) is sent when the work it waited for is done:
    /// AppModel.open, so it is routed as if it had just arrived. nil: straight back here.
    var onQueuedURL: ((URL) -> Void)?
    /// A plaque link that arrived while the model was busy or still reading the record. ONE slot:
    /// it is handled once, after the work, and a second arrival of the link replaces it rather than
    /// queueing it twice (M16-17 review: it used to be dropped).
    private var queuedURL: URL?

    private let client: AppClient
    /// The server's origin (the terms sheet links its legal pages).
    var baseURL: URL { client.baseURL }
    private let keys: DeviceKeyFactory
    private let store: ActivationStore
    private let reader: PlaqueReading
    private var code: String?
    private var pendingKey: DeviceSigner?
    private var binding: BindResponse?

    init(client: AppClient, keys: DeviceKeyFactory, store: ActivationStore, reader: PlaqueReading) {
        self.client = client
        self.keys = keys
        self.store = store
        self.reader = reader
    }

    // MARK: Launch

    /// An activated phone restores its key and goes to the tap screens, which ask the server who
    /// it is (TapModel.home: a signed read, with the 401 retry). A phone the server answers as
    /// signed out comes back here through signedOut(): "This phone was signed out" and the way to
    /// activate again; offline, the stored name stands -- being unreachable is not being signed out.
    ///
    /// THE RECORD IS NEVER DELETED (M16-16 review, 2026-11-18). A 401 is the answer to a
    /// revoked device, but also to a server-side fault -- an RLS or GRANT regression that makes
    /// the resolver miss every device answers 401 to every phone at once -- and the Secure Enclave
    /// key behind a deleted record is gone for good. Keeping it costs nothing: a revoked key is
    /// refused by the server whatever the phone keeps. The browser does the same (a signed-out
    /// tap does not clear the cookie). Only a NEW successful activation replaces the record
    /// (ActivationStore.save).
    func start() async {
        restore()
        drainQueued()
    }

    private func restore() {
        if let record = store.load() {
            // ONE retry, then a "try again" screen -- never a deletion (the same rule as the 401
            // above): a Secure Enclave that fails once (a locked moment, a busy chip) usually
            // answers the next time, and a deleted handle never would.
            guard let signer = (try? keys.restore(record.keyHandle)) ?? (try? keys.restore(record.keyHandle)) else {
                screen = .problem(Copy.Problems.keyUnreadable, retry: .relaunch)
                return
            }
            device = ActivatedDevice(identity: DeviceIdentity(deviceID: record.deviceId, signer: signer),
                                     storedName: record.employeeName)
            screen = .activated
            return
        }
        enterActivation(notice: nil)
    }

    /// The server answered the activated phone as signed out: the code screen with the notice.
    /// The record and the device stay (see start()); a new activation replaces them.
    func signedOut() {
        enterActivation(notice: Copy.Problems.signedOut)
    }

    /// "Start again" from a tap problem screen: the code screen, nothing deleted.
    func startAgain() {
        reset()
        enterActivation(notice: nil)
    }

    /// The tap screens take over (a plaque URL, or the app back in front after "Activation
    /// complete"). Only for an activated phone.
    func showActivated() {
        guard device != nil else {
            return
        }
        screen = .activated
    }

    /// True while the wizard holds an invitation the person is working through: a plaque URL then
    /// belongs to the activation, whatever device this phone already has.
    var inWizard: Bool {
        switch screen {
        case .welcome, .ready, .tap:
            return true
        default:
            return false
        }
    }

    /// The code screen, unless this phone cannot activate at all.
    private func enterActivation(notice: Copy.Problem?) {
        guard keys.isAvailable else {
            screen = .problem(Copy.Problems.noSecureChip, retry: .none)
            return
        }
        guard reader.isAvailable else {
            screen = .problem(Copy.Problems.noReader, retry: .none)
            return
        }
        screen = .code(notice: notice)
    }

    // MARK: Steps

    func submitCode() async {
        guard let host = client.baseURL.host, let c = InviteCode.parse(codeInput, host: host) else {
            codeError = Copy.Code.notALink
            return
        }
        codeError = nil
        busy = true
        defer { endBusy() }
        do {
            let found = try await client.lookup(code: c)
            code = c
            invite = found
            brand = found.brand
            screen = .welcome
        } catch {
            screen = .problem(problem(for: error), retry: .toCode)
        }
    }

    /// "Agree and continue": a fresh key, then bind.
    func agree() async {
        busy = true
        defer { endBusy() }
        if await bindNewKey() {
            screen = .ready
        }
    }

    func ready() {
        screen = .tap(problem: nil)
        scan()
    }

    func back() {
        switch screen {
        case .welcome:
            screen = .code(notice: nil)
        case .ready:
            screen = .welcome
        case .tap:
            screen = .ready
        default:
            break
        }
    }

    func retry(_ r: Retry) {
        switch r {
        case .toCode:
            reset()
            screen = .code(notice: nil)
        case .toWelcome:
            screen = invite == nil ? .code(notice: nil) : .welcome
        case .toTap:
            screen = binding == nil ? .code(notice: nil) : .tap(problem: nil)
        case .relaunch:
            screen = .checking
            if let again = onRelaunch {
                again()
            } else {
                Task { await start() }
            }
        case .none:
            break
        }
    }

    /// Opens iOS's scan sheet. The read goes to the same place a plaque link does.
    func scan() {
        guard case .tap = screen, !scanning, !busy else {
            return
        }
        scanning = true
        reader.read(alert: Copy.Tap.sheet) { [weak self] result in
            guard let self = self else {
                return
            }
            self.scanning = false
            switch result {
            case .url(let url):
                Task { await self.plaqueURLOpened(url) }
            case .failed:
                self.screen = .tap(problem: Copy.Problems.tapFailed)
            case .cancelled:
                break
            }
        }
    }

    /// A plaque URL -- read by the sheet, or opened as a link -- on a phone that is NOT activated
    /// (an activated one's go to the tap screens, AppModel). Where it lands (M16-16 hand-off 2):
    ///   tap step, or "I'm ready" with the consent bound   completes the activation (the browser's
    ///                                                      pending activation does the same)
    ///   welcome (no consent yet)                           "Finish the setup steps first"
    ///   anywhere else                                      "This app isn't set up yet", with Safari
    func plaqueURLOpened(_ url: URL) async {
        if busy || screen == .checking {
            queuedURL = url
            return
        }
        switch screen {
        case .tap:
            break
        case .ready where binding != nil:
            screen = .tap(problem: nil)
        case .welcome:
            screen = .problem(Copy.Problems.notReady, retry: .toWelcome)
            return
        default:
            // Only a plaque URL of this server, rebuilt from its checked query, is kept -- it is
            // the URL "Open in browser" will hand to Safari.
            guard let host = client.baseURL.host, let sun = PlaqueURL.sunQuery(of: url, host: host),
                  let safe = PlaqueURL.url(base: client.baseURL, sun: sun) else {
                return
            }
            screen = .notSetUp(safe)
            return
        }
        guard let c = code, let b = binding, let key = pendingKey else {
            return
        }
        guard let host = client.baseURL.host, let sun = PlaqueURL.sunQuery(of: url, host: host) else {
            screen = .tap(problem: Copy.Problems.tapFailed)
            return
        }
        busy = true
        defer { endBusy() }
        do {
            let done = try await client.complete(code: c, signer: key, binding: b, sun: sun)
            do {
                try store.save(ActivationRecord(deviceId: done.deviceId, employeeName: done.employeeName,
                                                keyHandle: key.persistentHandle))
            } catch {
                // The server holds an active device whose key this phone could not keep: the
                // activation is spent, so only a new link helps.
                reset()
                screen = .problem(Copy.Problems.server, retry: .toCode)
                return
            }
            let second = invite?.secondDevice ?? false
            reset()
            device = ActivatedDevice(identity: DeviceIdentity(deviceID: done.deviceId, signer: key),
                                     storedName: done.employeeName)
            screen = .done(name: done.employeeName, secondDevice: second)
        } catch AppAPIError.refused(let code, _) {
            await completeRefused(code)
        } catch AppAPIError.transport {
            screen = .problem(Copy.Problems.offline, retry: .toTap)
        } catch {
            screen = .problem(Copy.Problems.server, retry: .toWelcome)
        }
    }

    // MARK: Internals

    private func endBusy() {
        busy = false
        drainQueued()
    }

    /// Hands the link that waited to its router, once.
    private func drainQueued() {
        guard let url = queuedURL, !busy, screen != .checking else {
            return
        }
        queuedURL = nil
        if let route = onQueuedURL {
            route(url)
        } else {
            Task { await self.plaqueURLOpened(url) }
        }
    }

    private func completeRefused(_ code: AppErrorCode) async {
        switch code {
        case .activationNeedsTouch:
            screen = .tap(problem: Copy.Problems.needsTouch)
        case .activationTapFailed, .unknownPlaque, .foreignTenant, .staleContext:
            screen = .tap(problem: Copy.Problems.tapFailed)
        case .newKeyRequired:
            // Nothing was consumed; the read was. A fresh key, a fresh bind, one more tap.
            if await bindNewKey() {
                screen = .tap(problem: Copy.Problems.tapFailed)
            }
        case .activationNotReady:
            screen = .problem(Copy.Problems.notReady, retry: .toWelcome)
        case .invalidInvite:
            screen = .problem(Copy.Problems.badLink, retry: .toCode)
        case .rateLimited:
            screen = .problem(Copy.Problems.tooMany, retry: .toTap)
        default:
            screen = .problem(Copy.Problems.server, retry: .toWelcome)
        }
    }

    /// Generates a key and binds it. On failure the screen already says why; false.
    private func bindNewKey() async -> Bool {
        guard let c = code else {
            screen = .code(notice: nil)
            return false
        }
        let key: DeviceSigner
        do {
            key = try keys.newKey()
        } catch DeviceKeyError.secureEnclaveUnavailable {
            screen = .problem(Copy.Problems.noSecureChip, retry: .none)
            return false
        } catch {
            screen = .problem(Copy.Problems.server, retry: .toWelcome)
            return false
        }
        do {
            binding = try await client.bind(code: c, publicPoint: key.publicPoint)
            pendingKey = key
            return true
        } catch {
            binding = nil
            pendingKey = nil
            screen = .problem(problem(for: error), retry: error.isInvalidInvite ? .toCode : .toWelcome)
            return false
        }
    }

    private func reset() {
        code = nil
        binding = nil
        pendingKey = nil
        codeInput = ""
    }

    private func problem(for error: Error) -> Copy.Problem {
        switch error as? AppAPIError {
        case .refused(.invalidInvite, _)?:
            return Copy.Problems.badLink
        case .refused(.rateLimited, _)?:
            return Copy.Problems.tooMany
        case .transport?:
            return Copy.Problems.offline
        default:
            return Copy.Problems.server
        }
    }
}

extension ActivationModel.Screen {
    /// The look of this screen's primary button (CLAUDE.md §9; ADR 0023 §2, WL-13): the business's
    /// accent on the wizard's steps 1 and 2 -- "Agree and continue" and "I'm ready" -- and Taptime's
    /// green everywhere else. Step 3 has no primary button in the browser; the app's "Scan the
    /// plaque" is an app-only button, so it is not one of §9's counted slots and stays green (its
    /// screen's marks still go ink beside an accent). The code screen comes before any invitation
    /// is known, and a problem screen must not tell a live invitation from a dead one by its
    /// colours (the browser's problem screens take no brand either).
    func primaryLook(_ brand: AppBrand) -> BrandLook {
        switch self {
        case .welcome, .ready:
            return BrandLook(brand)
        case .checking, .code, .tap, .done, .activated, .notSetUp, .problem:
            return BrandLook(.taptime)
        }
    }
}

private extension Error {
    var isInvalidInvite: Bool {
        if case AppAPIError.refused(.invalidInvite, _)? = self as? AppAPIError {
            return true
        }
        return false
    }
}
