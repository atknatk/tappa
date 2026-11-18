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
        case already(name: String)
        case problem(Copy.Problem, retry: Retry)
    }

    @Published private(set) var screen: Screen = .checking
    @Published var codeInput = ""
    @Published private(set) var codeError: String?
    @Published private(set) var busy = false
    @Published private(set) var scanning = false
    @Published private(set) var invite: LookupResponse?
    @Published private(set) var brand: AppBrand = .taptime

    private let client: AppClient
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

    /// An activated phone asks the server who it is (a signed read, with the 401 retry). A phone
    /// the server answers as signed out shows "This phone was signed out" and the way to activate
    /// again; offline, the stored name stands -- being unreachable is not being signed out.
    ///
    /// THE RECORD IS NEVER DELETED HERE (M16-16 review, 2026-11-18). A 401 is the answer to a
    /// revoked device, but also to a server-side fault -- an RLS or GRANT regression that makes
    /// the resolver miss every device answers 401 to every phone at once -- and the Secure Enclave
    /// key behind a deleted record is gone for good. Keeping it costs nothing: a revoked key is
    /// refused by the server whatever the phone keeps. The browser does the same (a signed-out
    /// tap does not clear the cookie). Only a NEW successful activation replaces the record
    /// (ActivationStore.save).
    func start() async {
        if let record = store.load() {
            // ONE retry, then a "try again" screen -- never a deletion (the same rule as the 401
            // above): a Secure Enclave that fails once (a locked moment, a busy chip) usually
            // answers the next time, and a deleted handle never would.
            guard let signer = (try? keys.restore(record.keyHandle)) ?? (try? keys.restore(record.keyHandle)) else {
                screen = .problem(Copy.Problems.keyUnreadable, retry: .relaunch)
                return
            }
            do {
                let me = try await client.me(DeviceIdentity(deviceID: record.deviceId, signer: signer))
                brand = me.brand
                screen = .already(name: me.employeeName)
            } catch AppAPIError.unauthenticated {
                enterActivation(notice: Copy.Problems.signedOut)
            } catch {
                screen = .already(name: record.employeeName)
            }
            return
        }
        enterActivation(notice: nil)
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
        defer { busy = false }
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
        defer { busy = false }
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
            Task { await start() }
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

    /// A plaque URL -- read by the sheet, or opened as a link while the tap step is showing.
    /// Anywhere else it is ignored: only the tap step completes an activation.
    func plaqueURLOpened(_ url: URL) async {
        guard case .tap = screen, !busy, let c = code, let b = binding, let key = pendingKey else {
            return
        }
        guard let host = client.baseURL.host, let sun = PlaqueURL.sunQuery(of: url, host: host) else {
            screen = .tap(problem: Copy.Problems.tapFailed)
            return
        }
        busy = true
        defer { busy = false }
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
    /// accent on the wizard's steps 1-3 -- "Agree and continue", "I'm ready" and step 3's "Scan the
    /// plaque" -- and Taptime's green everywhere else. The code screen comes before any invitation
    /// is known, and a problem screen must not tell a live invitation from a dead one by its
    /// colours (the browser's problem screens take no brand either).
    func primaryLook(_ brand: AppBrand) -> BrandLook {
        switch self {
        case .welcome, .ready, .tap:
            return BrandLook(brand)
        case .checking, .code, .done, .already, .problem:
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
