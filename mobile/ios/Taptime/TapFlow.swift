import CoreLocation
import Foundation
import UIKit

// THE TAP, ON EVERY NFC IPHONE (M16-17; ADR 0045 §2.4; Q41 (a)). An activated phone has ONE screen,
// the browser's tap screen: "Hello Maria", the venue, one button -- "Clock in", "Clock out" or
// "Tap" -- and, under a predicted direction only, the small "Not right? …" (CLAUDE.md §9). It is
// reached two ways, and the button does what the way needs:
//
//   from a plaque   iPhone XS and later read the plaque in the background; the notification opens
//                   the plaque URL as a Universal Link and the app shows tap/start's screen (GET
//                   /t's twin: the TAPPED venue, nothing recorded, no counter spent). The button
//                   is the press.
//   from the app    iPhone 7-X read a plaque only inside an app, in iOS's own scan sheet. The app
//                   opens on GET /app/v1/me's screen (the predicted direction without a plaque,
//                   M16-07's Q41 measurement); the button opens the sheet, and the read goes to
//                   tap/start and then straight to the press, with the direction the button said.
//                   One screen, one press. Any iPhone opened from its home screen lands here too.
//
// THE PRESS is POST /api/checkin's twin: the signed context, the direction pressed (`want`, a
// preference the engine may overrule), at most ONE location fix and its mock report. Nothing in
// it names a plaque, a counter, a verdict or a person; the server decides (ADR 0004 §8).
//
// 🔴 CLAUDE.md §4.2 -- LOCATION ONLY AT THE PRESS. The one location call of the app is in THIS file
// (OneShotLocation below; TapSourceGuardTests pins the call site): When-In-Use, one reading,
// started by the press and by nothing else, given up after eight seconds (tap.js's
// FIX_TIMEOUT_MS). No timer repeats it -- there is no repeating timer in this file, and redline R2
// fails one placed beside the call. A press without a fix still records: GPS is backup evidence
// (§5 rows 6-7), never a requirement.
//
// 🔴 §4.6 -- A TAP IS NEVER LOST AND NEVER DOUBLED. The press is sent once. An answer that never
// came back may belong to a recorded tap, so it is NOT retried with the same context (a second
// send of a spent counter would record a refusal and start the person's 60-second debounce); the
// screen says the tap may not have been recorded and the next step is a new physical read. "Open
// in browser" (ADR 0045 Bedel 8) is offered only while the plaque URL is UNSPENT -- nothing that
// could advance its counter has been sent -- so Safari's own tap of it is the first.
//
// NOTHING HERE LOGS: the press carries a context, a nonce, a signature and a coordinate, and the
// simplest way to keep them out of a log is a file with no log call (CLAUDE.md §7).

// MARK: - Location, read once

/// One reading's outcome. `lat`/`lng` are nil when there is none worth sending; `mock` is true
/// ONLY when the platform reported the sent reading as simulated, and nil otherwise -- never false
/// (see OneShotLocation.report).
struct TapLocationReport: Equatable {
    var lat: Double?
    var lng: Double?
    var mock: Bool?

    static let none = TapLocationReport(lat: nil, lng: nil, mock: nil)
}

protocol TapLocating: AnyObject {
    /// Starts ONE reading now, for the press made at `pressedAt`, and calls `completion` exactly
    /// once on the main queue -- with what came, or with .none when nothing came within `timeout`.
    func readOnce(pressedAt: Date, timeout: TimeInterval, completion: @escaping (TapLocationReport) -> Void)
}

/// Core Location, When-In-Use, one `requestLocation` per press. Used on the main thread only.
final class OneShotLocation: NSObject, TapLocating, CLLocationManagerDelegate {
    private lazy var manager: CLLocationManager = {
        let m = CLLocationManager()
        m.delegate = self
        m.desiredAccuracy = kCLLocationAccuracyBest
        return m
    }()

    private var completion: ((TapLocationReport) -> Void)?
    private var pressedAt = Date.distantPast
    private var awaitingPermission = false

    func readOnce(pressedAt: Date, timeout: TimeInterval, completion: @escaping (TapLocationReport) -> Void) {
        finish(.none) // a press still waiting gives way to this one
        self.completion = completion
        self.pressedAt = pressedAt
        // One deadline per press: a single-shot dispatch, never a repeating timer.
        DispatchQueue.main.asyncAfter(deadline: .now() + timeout) { [weak self] in
            self?.finishIfStill(pressedAt)
        }
        switch manager.authorizationStatus {
        case .notDetermined:
            // The question is asked at the press, as the browser asks at its first fix; the reading
            // starts only if the answer comes while THIS press still waits.
            awaitingPermission = true
            manager.requestWhenInUseAuthorization()
        case .authorizedWhenInUse, .authorizedAlways:
            manager.requestLocation()
        default:
            finish(.none)
        }
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        guard awaitingPermission, completion != nil else {
            return // no press is waiting: a permission change never starts a reading by itself
        }
        switch manager.authorizationStatus {
        case .notDetermined:
            return
        case .authorizedWhenInUse, .authorizedAlways:
            awaitingPermission = false
            manager.requestLocation()
        default:
            finish(.none)
        }
    }

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        guard let reading = locations.last else {
            finish(.none)
            return
        }
        finish(OneShotLocation.report(reading, pressedAt: pressedAt,
                                      precise: manager.accuracyAuthorization == .fullAccuracy))
    }

    func locationManager(_ manager: CLLocationManager, didFailWithError error: Error) {
        // Denied, unavailable, no fix: all the same here -- no coordinates, and the tap goes on.
        // The error is not logged (it can carry nothing useful and the press must not wait).
        finish(.none)
    }

    /// What one reading is worth to the press.
    ///
    /// - A reading older than the press is refused (tap.js's maximumAge 0): a remembered position
    ///   is where the phone WAS, which is exactly what §4.2 keeps out of the product.
    /// - An approximate reading (Precise Location off) is not sent: a fix fuzzed by kilometres
    ///   inside a 150 m radius proves nothing, and as a coordinate it could only contradict the
    ///   venue's network. The server takes no accuracy from the app, so the app decides here.
    /// - The mock report (iOS 15+, sourceInformation.isSimulatedBySoftware) is sent ONLY as true,
    ///   and only WITH the reading it describes (M16-17 review; Android's rule too). A positive
    ///   report tightens (base:mock-location-review); a false one proves nothing, and every value
    ///   the app sends is written into the immutable transactions row (decide.go keeps any non-nil
    ///   report) -- so "not simulated" is never sent, and neither is a flag without coordinates.
    static func report(_ reading: CLLocation, pressedAt: Date, precise: Bool) -> TapLocationReport {
        let c = reading.coordinate
        guard precise, reading.horizontalAccuracy >= 0, reading.timestamp >= pressedAt.addingTimeInterval(-1),
              CLLocationCoordinate2DIsValid(c), c.latitude.isFinite, c.longitude.isFinite else {
            return .none
        }
        let simulated = reading.sourceInformation?.isSimulatedBySoftware == true
        return TapLocationReport(lat: c.latitude, lng: c.longitude, mock: simulated ? true : nil)
    }

    private func finishIfStill(_ press: Date) {
        if press == pressedAt {
            finish(.none)
        }
    }

    private func finish(_ r: TapLocationReport) {
        awaitingPermission = false
        guard let done = completion else {
            return
        }
        completion = nil
        done(r)
    }
}

// MARK: - Safari

protocol BrowserOpening: AnyObject {
    func open(_ url: URL)
}

/// Opens the plaque URL in the phone's browser. **doğrula (M16-18):** that iOS opens an app's own
/// Universal Link in Safari when the app itself asks, rather than handing it back to the app.
final class SystemBrowser: BrowserOpening {
    func open(_ url: URL) {
        UIApplication.shared.open(url, options: [:], completionHandler: nil)
    }
}

// MARK: - The model

/// The activated phone: what signs for it, and the name the Keychain record keeps for offline.
struct ActivatedDevice {
    let identity: DeviceIdentity
    let storedName: String
}

@MainActor
final class TapModel: ObservableObject {
    enum Source: Equatable {
        /// GET /app/v1/me's screen: the button opens the scan sheet.
        case home
        /// tap/start's screen: the button is the press. `sun` is the plaque URL's checked query.
        case plaque(sun: String, context: String)
    }

    struct View: Equatable {
        let employeeName: String
        /// "" draws no venue line (home, another business's plaque).
        let locationName: String
        let brand: AppBrand
        /// "in", "out" or "".
        let direction: String
        let source: Source
    }

    struct Result: Equatable {
        let verdict: String
        let direction: String
        let venue: String
        let at: String
        let businessType: String
        let practice: Bool
    }

    struct Problem: Equatable {
        let copy: Copy.Problem
        /// "Try again": back to the home screen, whose button reads the plaque anew.
        var retry = true
        /// "Open in browser": the plaque URL, only while it is unspent.
        var browserURL: URL?
        /// "Start again": back to activation (signed out).
        var startAgain = false
    }

    enum Screen: Equatable {
        case loading
        case tap(View, problem: Copy.Problem?)
        case result(Result)
        case problem(Problem)
    }

    @Published private(set) var screen: Screen = .loading
    /// True from the press until its answer: the buttons are disabled and the primary one says
    /// "Tapping…".
    @Published private(set) var pressing = false

    /// Called when the home screen's read is answered as signed out (two 401s after the clock
    /// retry): the activation screens take over, with "This phone was signed out".
    var onSignedOut: (() -> Void)?
    /// "Start again" on a problem screen: back to the activation code screen.
    var onStartAgain: (() -> Void)?
    /// Whether the app is in front (AppModel keeps it). A result that lands while it is not has
    /// not been seen yet.
    var inForeground = true
    /// The result showing arrived while the app was in the background: the way back to the front
    /// must not replace it with the home screen before the person has seen it (main-session
    /// decision, M16-17 review).
    private(set) var resultUnseen = false

    static let fixTimeout: TimeInterval = 8

    private let client: AppClient
    private let reader: PlaqueReading
    private let location: TapLocating
    private let browser: BrowserOpening
    private let now: () -> Date
    private var device: ActivatedDevice?
    /// Bumped by every new entry (home, a plaque, a press): an answer for an older one is dropped.
    private var generation = 0
    /// The press's POST is on the wire: nothing may replace the screen until it answers.
    private var sending = false

    init(client: AppClient, reader: PlaqueReading, location: TapLocating, browser: BrowserOpening,
         now: @escaping () -> Date = Date.init) {
        self.client = client
        self.reader = reader
        self.location = location
        self.browser = browser
        self.now = now
    }

    func enter(_ device: ActivatedDevice) {
        self.device = device
    }

    // MARK: Entries

    /// The app's own screen: GET /app/v1/me. Two 401s (after the clock retry) are the signed-out
    /// answer -- unless the answers' clock was days from the phone's, which is the phone's clock.
    /// Offline, or any other failure, the stored name and the direction-neutral "Tap" stand:
    /// being unreachable is not being signed out, and the press itself says what happened.
    func home() async {
        guard let d = device, !sending else {
            return
        }
        generation += 1
        let gen = generation
        pressing = false
        guard reader.isAvailable else {
            screen = .problem(Problem(copy: TapCopy.Problems.noReader, retry: false))
            return
        }
        screen = .loading
        do {
            let me = try await client.me(d.identity)
            guard gen == generation else { return }
            screen = .tap(View(employeeName: me.employeeName, locationName: "", brand: me.brand,
                               direction: me.direction, source: .home), problem: nil)
        } catch AppAPIError.unauthenticated {
            guard gen == generation else { return }
            if client.clock.lastAnswerWasFarOff {
                screen = .problem(Problem(copy: TapCopy.Problems.clock))
            } else {
                onSignedOut?()
            }
        } catch {
            guard gen == generation else { return }
            screen = .tap(View(employeeName: d.storedName, locationName: "", brand: .taptime,
                               direction: "", source: .home), problem: nil)
        }
    }

    /// A plaque URL opened as a Universal Link (or handed over at launch). Ignored while a press
    /// is on the wire -- its answer must not be hidden.
    func plaque(_ url: URL) async {
        guard let d = device, !sending else {
            return
        }
        generation += 1
        let gen = generation
        pressing = false
        guard let host = client.baseURL.host, let sun = PlaqueURL.sunQuery(of: url, host: host) else {
            screen = .problem(Problem(copy: TapCopy.Problems.badURL))
            return
        }
        screen = .loading
        do {
            let s = try await client.tapStart(d.identity, sun: sun)
            guard gen == generation else { return }
            screen = .tap(View(employeeName: s.employeeName, locationName: s.locationName, brand: s.brand,
                               direction: s.direction, source: .plaque(sun: sun, context: s.context)), problem: nil)
        } catch {
            guard gen == generation else { return }
            screen = .problem(problem(for: error, sun: sun, sent: false))
        }
    }

    // MARK: The press

    /// The button: `want` is the direction it said ("in" / "out"), nil for "Tap".
    func press(want: String?) async {
        guard case .tap(let view, _) = screen, !pressing, let d = device else {
            return
        }
        generation += 1
        let gen = generation
        pressing = true
        let pressedAt = now()
        let fix = FixWait()
        location.readOnce(pressedAt: pressedAt, timeout: TapModel.fixTimeout) { fix.arrive($0) }

        let context: String
        let plaqueSun: String
        switch view.source {
        case .plaque(let sun, let ctx):
            context = ctx
            plaqueSun = sun
        case .home:
            // M16-15 hand-off 1: the reader is asked BEFORE a session is opened (iPhone 6s/SE run
            // iOS 15 and have none).
            guard reader.isAvailable else {
                pressing = false
                screen = .problem(Problem(copy: TapCopy.Problems.noReader, retry: false))
                return
            }
            let read = await readPlaque()
            guard gen == generation else { return }
            let url: URL
            switch read {
            case .cancelled:
                pressing = false
                return
            case .failed:
                pressing = false
                screen = .tap(view, problem: TapCopy.Problems.notRead)
                return
            case .url(let u):
                url = u
            }
            guard let host = client.baseURL.host, let sun = PlaqueURL.sunQuery(of: url, host: host) else {
                // Not this server's plaque: nothing is sent anywhere.
                pressing = false
                screen = .tap(view, problem: TapCopy.Problems.notRead)
                return
            }
            do {
                context = try await client.tapStart(d.identity, sun: sun).context
            } catch {
                guard gen == generation else { return }
                pressing = false
                screen = .problem(problem(for: error, sun: sun, sent: false))
                return
            }
            guard gen == generation else { return }
            plaqueSun = sun
        }

        let report = await fix.value()
        guard gen == generation else { return }
        // The flag rides only with coordinates, and only as true (OneShotLocation.report) -- held here
        // too, whatever a TapLocating implementation hands back.
        let hasFix = report.lat != nil && report.lng != nil
        let request = TapRequest(context: context, want: want, lat: hasFix ? report.lat : nil, lng: hasFix ? report.lng : nil,
                                 mockLocation: hasFix && report.mock == true ? true : nil)
        sending = true
        defer {
            sending = false
            pressing = false
        }
        do {
            let r = try await client.tap(d.identity, request)
            resultUnseen = !inForeground
            screen = .result(Result(verdict: r.verdict, direction: r.direction, venue: r.venue, at: r.at,
                                    businessType: r.businessType, practice: r.practice ?? false))
        } catch {
            screen = .problem(problem(for: error, sun: plaqueSun, sent: true))
        }
    }

    /// The app came to the front over a result it had not shown yet: it is shown now, so the next
    /// return goes home as usual. True when there was such a result.
    func showUnseenResult() -> Bool {
        guard resultUnseen, case .result = screen else {
            resultUnseen = false
            return false
        }
        resultUnseen = false
        return true
    }

    // MARK: Problem screens

    func retry() {
        Task { await home() }
    }

    func openInBrowser(_ url: URL) {
        browser.open(url)
    }

    func startAgain() {
        onStartAgain?()
    }

    // MARK: Internals

    private func readPlaque() async -> PlaqueReadResult {
        await withCheckedContinuation { (c: CheckedContinuation<PlaqueReadResult, Never>) in
            reader.read(alert: TapCopy.Screen.sheet) { c.resume(returning: $0) }
        }
    }

    /// Which screen a failure is. `sent` is true once the press itself went out: from then on the
    /// plaque URL may be spent, so Safari is not offered for it.
    private func problem(for error: Error, sun: String, sent: Bool) -> Problem {
        // "Open in browser" opens the plaque URL REBUILT from this server's origin and the checked
        // query -- never the URL as it arrived (M16-17 review).
        let plaqueURL = PlaqueURL.url(base: client.baseURL, sun: sun)
        switch error as? AppAPIError {
        case .unauthenticated?:
            if client.clock.lastAnswerWasFarOff {
                return Problem(copy: TapCopy.Problems.clock)
            }
            // A refused signature records nothing, so the URL is unspent even after the press.
            return Problem(copy: TapCopy.Problems.signedOut, retry: false, browserURL: plaqueURL, startAgain: true)
        case .refused(.staleContext, _)?:
            return Problem(copy: TapCopy.Problems.stale)
        case .refused(.foreignTenant, _)?:
            return Problem(copy: TapCopy.Problems.foreignTenant)
        case .refused(.unknownPlaque, _)?:
            return Problem(copy: TapCopy.Problems.unknownPlaque)
        case .refused(.rateLimited, _)?:
            return Problem(copy: TapCopy.Problems.tooMany)
        case .refused(.badRequest, _)? where !sent:
            return Problem(copy: TapCopy.Problems.badURL)
        case .transport?:
            return sent ? Problem(copy: TapCopy.Problems.offlineUnknown)
                : Problem(copy: TapCopy.Problems.offline, browserURL: plaqueURL)
        case .unexpected? where sent:
            // After the press, an answer outside the contract -- a 200 that does not decode, a
            // proxy's 502/504 -- says nothing about whether the server recorded the tap, so the
            // screen must not say "Nothing was recorded". (server_error and try_again are the
            // surface's OWN answers that nothing was written: they keep the server screen.)
            return Problem(copy: TapCopy.Problems.offlineUnknown)
        default:
            return Problem(copy: TapCopy.Problems.server)
        }
    }
}

/// The press's one location reading, awaited after the plaque is read: whichever comes first, the
/// reading or the wait for it, the other finds it ready.
@MainActor
private final class FixWait {
    private var report: TapLocationReport?
    private var waiter: CheckedContinuation<TapLocationReport, Never>?

    func arrive(_ r: TapLocationReport) {
        guard report == nil else { return }
        report = r
        waiter?.resume(returning: r)
        waiter = nil
    }

    func value() async -> TapLocationReport {
        if let r = report {
            return r
        }
        return await withCheckedContinuation { waiter = $0 }
    }
}
