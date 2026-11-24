import Foundation

// THE APP'S ACTIVATION, screen by screen (ADR 0026's two steps for a device key; ADR 0045 §2.3;
// M16-16, the twin of Android's M16-12). The browser wizard's order, with the code entry in front
// because the invite link opens the browser, not the app (Q40):
//
//   code     the person pastes the link; the app keeps only the code        → lookup
//   welcome  who this is for, what is recorded, "Agree and continue"        → a NEW key, bind
//   ready    the venue Wi-Fi
//   tap      "Now tap the plaque": the first genuine NFC read               → complete
//            or (M16-23, a phone that cannot tap) a venue screen's live QR   → complete (liveQR)
//            or "My phone can't tap"                                         → approval
//   waiting  the pairing code for the manager, polled every 5 s (ApprovalPoll) → approval/status
//   done     "Activation complete"
//
// After it, the phone is ACTIVATED and the tap screens (TapFlow.swift, M16-17) take over: the
// launch hands the restored key to them instead of greeting the person here.
//
// A PHONE WITHOUT AN NFC READER IS NOT REFUSED (M16-23). It walks the same steps; on the tap step
// the scan button gives way to a notice, and it finishes with the camera on the venue screen's live
// QR (the Universal Link brings the /q URL here) or with the manager's approval -- whose "approved"
// answer IS the activation: the server created the device in that call.
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
        /// M16-23: waiting for the manager's approval. The pairing code is `approval` (never in the
        /// screen's value); `ended` is the request's end that is not "approved" (rejected, expired),
        /// with a button for a new request; `notice` the last poll's passing trouble.
        case waiting(ended: Copy.Problem?, notice: Copy.Problem?)
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
    /// M16-23: the manager-approval path is offered for this invitation (lookup's managerApproval;
    /// a screen_available answer turns it off). Drives the tap step's "My phone can't tap".
    @Published private(set) var approvalOffered = false
    /// M16-23: the open approval request the waiting screen shows. nil when there is none.
    @Published private(set) var approval: PendingApproval?
    /// The next poll's delay in seconds: ApprovalPoll's base, doubled on each 429 in a row.
    private(set) var pollDelay: TimeInterval = ApprovalPoll.base
    /// Multiplies every poll's sleep; 1 in the app, tests shorten it.
    var pollTimeScale: Double = 1
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

    /// This phone was signed out by the person (Settings: "Sign this phone out" or "Delete my
    /// account"): the record -- and with it the Secure Enclave key's only handle -- is cleared, and
    /// the app is back at its code screen.
    func forgetDevice() {
        store.clear()
        device = nil
        reset()
        enterActivation(notice: nil)
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
        case .welcome, .ready, .tap, .waiting:
            return true
        default:
            return false
        }
    }

    /// Whether this iPhone reads a plaque at all (the tap step's scan button, or its notice).
    var canRead: Bool { reader.isAvailable }

    /// The code screen, unless this phone cannot activate at all (no secure chip). A missing NFC
    /// reader no longer stops it (M16-23): the tap step offers the venue screen's code instead.
    private func enterActivation(notice: Copy.Problem?) {
        guard keys.isAvailable else {
            screen = .problem(Copy.Problems.noSecureChip, retry: .none)
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
            approvalOffered = found.managerApproval ?? false
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
        if reader.isAvailable {
            scan()
        }
    }

    func back() {
        switch screen {
        case .welcome:
            screen = .code(notice: nil)
        case .ready:
            screen = .welcome
        case .tap:
            screen = .ready
        case .waiting:
            // Back to the tap step; the waiting screen's .task stops polling as it goes.
            approval = nil
            screen = .tap(problem: nil)
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
        guard case .tap = screen, !scanning, !busy, reader.isAvailable else {
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
    ///   (or waiting for the manager -- M16-23)             pending activation does the same); a
    ///                                                      plaque's SUN query as `sun`, a venue
    ///                                                      screen's live QR as `liveQR`
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
        case .ready where binding != nil, .waiting where binding != nil:
            approval = nil
            screen = .tap(problem: nil)
        case .welcome:
            screen = .problem(Copy.Problems.notReady, retry: .toWelcome)
            return
        default:
            // Only a plaque or live QR URL of this server (M16-22), rebuilt from its checked query,
            // is kept -- it is the URL "Open in browser" will hand to Safari.
            guard let host = client.baseURL.host, let link = TapLink.of(url, host: host),
                  let safe = link.browserURL(base: client.baseURL) else {
                return
            }
            screen = .notSetUp(safe)
            return
        }
        guard let c = code, let b = binding, let key = pendingKey else {
            return
        }
        // A plaque's SUN query, or (M16-23) a venue screen's live QR. Anything else is sent nowhere.
        guard let host = client.baseURL.host, let link = TapLink.of(url, host: host) else {
            screen = .tap(problem: Copy.Problems.tapFailed)
            return
        }
        var live = false
        busy = true
        defer { endBusy() }
        do {
            let done: CompleteResponse
            switch link {
            case .plaque(let sun):
                done = try await client.complete(code: c, signer: key, binding: b, sun: sun)
            case .liveQR(let q):
                live = true
                done = try await client.complete(code: c, signer: key, binding: b, liveQR: q)
            }
            finishActivation(done, key: key)
        } catch AppAPIError.refused(let code, _) {
            await completeRefused(code, live: live)
        } catch AppAPIError.transport {
            screen = .problem(Copy.Problems.offline, retry: .toTap)
        } catch {
            screen = .problem(Copy.Problems.server, retry: .toWelcome)
        }
    }

    // MARK: The manager's approval (M16-23)

    /// "My phone can't tap" on the tap step, or "Ask again" / "Get a new code" on the waiting
    /// screen's end: a new request (the server withdraws an earlier open one of the invitation).
    func askApproval() async {
        guard let c = code, let b = binding, let key = pendingKey, !busy else {
            return
        }
        busy = true
        defer { endBusy() }
        pollDelay = ApprovalPoll.base // a new request starts at the base
        do {
            approvalAnswered(try await client.approval(code: c, signer: key, binding: b), key: key)
        } catch AppAPIError.refused(.screenAvailable, _) {
            screenAvailable()
        } catch AppAPIError.refused(.rateLimited, _) {
            screen = .problem(Copy.Problems.tooMany, retry: .toTap)
        } catch AppAPIError.refused(let code, _) {
            await approvalRefused(code)
        } catch AppAPIError.transport {
            screen = .problem(Copy.Problems.offline, retry: .toTap)
        } catch {
            screen = .problem(Copy.Problems.server, retry: .toWelcome)
        }
    }

    /// True while the waiting screen waits (a request is open and has not ended).
    var awaitingApproval: Bool {
        if case .waiting(nil, _) = screen, approval != nil {
            return true
        }
        return false
    }

    /// The waiting screen's poll, every `pollDelay`, for as long as the CALLER's task runs: the
    /// view's `.task` starts it when the screen appears and cancels it when it goes. One sleep, one
    /// status request, again -- never a repeating timer.
    func pollWhileWaiting() async {
        while !Task.isCancelled {
            try? await Task.sleep(nanoseconds: UInt64(pollDelay * pollTimeScale * 1_000_000_000))
            guard !Task.isCancelled, awaitingApproval else {
                return
            }
            await pollApproval()
        }
    }

    /// One status request. A passing trouble (no answer, too many, the server's own failure) keeps
    /// the code on screen with a notice; the request's end, or a bind that no longer holds, leaves.
    /// An APPROVED answer stands whatever is on screen: the device exists now.
    func pollApproval() async {
        guard awaitingApproval, !busy, let c = code, let b = binding, let key = pendingKey else {
            return
        }
        do {
            let state = try await client.approvalStatus(code: c, signer: key, binding: b)
            pollDelay = ApprovalPoll.nextDelay(pollDelay, .answered)
            if case .approved = state {
                approvalAnswered(state, key: key)
            } else if awaitingApproval {
                approvalAnswered(state, key: key)
            }
        } catch {
            pollDelay = ApprovalPoll.nextDelay(pollDelay, ApprovalPoll.outcome(of: error))
            guard awaitingApproval else {
                return
            }
            switch error as? AppAPIError {
            case .transport?:
                screen = .waiting(ended: nil, notice: Copy.Problems.offline)
            case .refused(.rateLimited, _)?:
                screen = .waiting(ended: nil, notice: Copy.Problems.tooMany)
            case .refused(.serverError, _)?, .unexpected?:
                screen = .waiting(ended: nil, notice: Copy.Problems.server)
            case .refused(.screenAvailable, _)?:
                screenAvailable()
            case .refused(let code, _)?:
                await approvalRefused(code)
            default:
                screen = .problem(Copy.Problems.server, retry: .toWelcome)
            }
        }
    }

    private func approvalAnswered(_ state: ApprovalState, key: DeviceSigner) {
        switch state {
        case .waiting(let p):
            approval = p
            screen = .waiting(ended: nil, notice: nil)
        case .approved(let done):
            approval = nil
            finishActivation(done, key: key)
        case .rejected:
            approval = nil
            screen = .waiting(ended: Copy.Problems.approvalRejected, notice: nil)
        case .expired:
            approval = nil
            screen = .waiting(ended: Copy.Problems.approvalExpired, notice: nil)
        }
    }

    /// screen_available: a live QR screen runs at the workplace -- the tap step, its button gone.
    private func screenAvailable() {
        approval = nil
        approvalOffered = false
        screen = .tap(problem: Copy.Problems.screenAvailable)
    }

    /// The answers the approval routes share with complete's.
    private func approvalRefused(_ code: AppErrorCode) async {
        approval = nil
        switch code {
        case .newKeyRequired:
            // The request belonged to the taken key: a fresh key, a fresh bind, ask again.
            if await bindNewKey() {
                screen = .tap(problem: Copy.Problems.server)
            }
        case .activationNotReady:
            screen = .problem(Copy.Problems.notReady, retry: .toWelcome)
        case .invalidInvite:
            screen = .problem(Copy.Problems.badLink, retry: .toCode)
        default:
            screen = .problem(Copy.Problems.server, retry: .toWelcome)
        }
    }

    /// A finished activation -- complete's answer, or an approved request's: the record is kept and
    /// the device made; "Activation complete".
    private func finishActivation(_ done: CompleteResponse, key: DeviceSigner) {
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

    /// `live`: the read was a venue screen's live QR (M16-23). Every refusal of one is
    /// activation_tap_failed (one code, §4.7); the app knows what it sent, so the text says to scan
    /// the screen again, not to tap the plaque.
    private func completeRefused(_ code: AppErrorCode, live: Bool) async {
        let readFailed = live ? Copy.Problems.liveQRFailed : Copy.Problems.tapFailed
        switch code {
        case .activationNeedsTouch:
            screen = .tap(problem: live ? readFailed : Copy.Problems.needsTouch)
        case .activationTapFailed, .unknownPlaque, .foreignTenant, .staleContext:
            screen = .tap(problem: readFailed)
        case .newKeyRequired:
            // Nothing was consumed; the read was. A fresh key, a fresh bind, one more tap.
            if await bindNewKey() {
                screen = .tap(problem: readFailed)
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
        approval = nil
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

/// THE WAITING SCREEN'S POLL INTERVAL (M16-23 review, ORTA-1). approval/status spends the
/// address's status budget, shared with the browser's polling: ~20 phones waiting behind one venue
/// NAT must fit it, so the base is 5 s (15 minutes = 180 polls a phone). A 429 doubles the interval
/// -- 5, 10, 20, 40, then 60 s at most -- and any answered poll goes back to the base. No answer at
/// all (the phone's network) keeps the base: it spent nothing of the budget. Pure, so the chain is a
/// unit test; the Android twin is ApprovalPoll.kt.
enum ApprovalPoll {
    static let base: TimeInterval = 5
    static let max: TimeInterval = 60

    enum Outcome: Equatable {
        /// The server answered (200, or any refusal but 429).
        case answered
        /// 429 rate_limited.
        case rateLimited
        /// No answer at all.
        case failed
    }

    static func nextDelay(_ previous: TimeInterval, _ outcome: Outcome) -> TimeInterval {
        switch outcome {
        case .rateLimited:
            return Swift.min(Swift.max(previous, base) * 2, max)
        case .answered, .failed:
            return base
        }
    }

    static func outcome(of error: Error) -> Outcome {
        switch error as? AppAPIError {
        case .refused(.rateLimited, _)?:
            return .rateLimited
        case .transport?:
            return .failed
        default:
            return .answered
        }
    }
}

extension ActivationModel.Screen {
    /// The look of this screen's primary button (CLAUDE.md §9; ADR 0023 §2, WL-13): the business's
    /// accent on the wizard's steps 1 and 2 -- "Agree and continue" and "I'm ready" -- and Taptime's
    /// green everywhere else (the waiting screen's "Ask again" included, M16-23). Step 3 has no primary button in the browser; the app's "Scan the
    /// plaque" is an app-only button, so it is not one of §9's counted slots and stays green (its
    /// screen's marks still go ink beside an accent). The code screen comes before any invitation
    /// is known, and a problem screen must not tell a live invitation from a dead one by its
    /// colours (the browser's problem screens take no brand either).
    func primaryLook(_ brand: AppBrand) -> BrandLook {
        switch self {
        case .welcome, .ready:
            return BrandLook(brand)
        case .checking, .code, .tap, .waiting, .done, .activated, .notSetUp, .problem:
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
