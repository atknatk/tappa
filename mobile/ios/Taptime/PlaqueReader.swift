import CoreNFC
import Foundation

// THE FIRST GENUINE NFC READ, for activation (ADR 0026's activating touch; M16-07's complete).
// The app reads the URL the plaque's chip writes -- NDEF only, the entitlement's one format -- and
// hands the server its QUERY (tag=..&ctr=..&cmac=..). The app verifies nothing about it: CMAC and
// the atomic counter are the server's (sun.Verify, CLAUDE.md §4.4), and the app holds no plaque
// key (§4.7).
//
// The reader is a foreground session the person starts (iOS shows its own scan sheet); M16-17
// grows the tap screen's reading from here.

enum PlaqueReadResult: Equatable {
    case url(URL)
    /// The person closed the sheet, or it timed out: nothing was read.
    case cancelled
    /// The sheet saw something it could not read as a URL, or the session failed.
    case failed
}

protocol PlaqueReading: AnyObject {
    /// False on an iPhone without an NFC reader (6s, SE 1st gen -- iOS 15 runs there, Core NFC
    /// does not) and in the simulator.
    var isAvailable: Bool { get }
    func read(alert: String, completion: @escaping (PlaqueReadResult) -> Void)
}

final class CoreNFCPlaqueReader: NSObject, PlaqueReading, NFCNDEFReaderSessionDelegate {
    private var session: NFCNDEFReaderSession?
    private var completion: ((PlaqueReadResult) -> Void)?

    var isAvailable: Bool { NFCNDEFReaderSession.readingAvailable }

    func read(alert: String, completion: @escaping (PlaqueReadResult) -> Void) {
        self.completion = completion
        let s = NFCNDEFReaderSession(delegate: self, queue: nil, invalidateAfterFirstRead: true)
        s.alertMessage = alert
        session = s
        s.begin()
    }

    func readerSession(_ session: NFCNDEFReaderSession, didDetectNDEFs messages: [NFCNDEFMessage]) {
        let url = messages.lazy.flatMap { $0.records }.compactMap { $0.wellKnownTypeURIPayload() }.first
        deliver(url.map(PlaqueReadResult.url) ?? .failed)
    }

    func readerSession(_ session: NFCNDEFReaderSession, didInvalidateWithError error: Error) {
        let code = (error as? NFCReaderError)?.code
        switch code {
        case .readerSessionInvalidationErrorUserCanceled, .readerSessionInvalidationErrorSessionTimeout:
            deliver(.cancelled)
        default:
            // After a successful first read the session always ends with "first NDEF tag read";
            // deliver() has already run then and ignores this second call.
            deliver(.failed)
        }
    }

    /// Exactly one result per read(), on the main queue.
    private func deliver(_ result: PlaqueReadResult) {
        DispatchQueue.main.async {
            guard let done = self.completion else {
                return
            }
            self.completion = nil
            self.session = nil
            done(result)
        }
    }
}

enum PlaqueURL {
    /// The plaque path the server serves (internal/handler TapPath) and the app links to.
    static let path = "/t"

    /// The SUN query of a plaque URL on this server, as written by the chip (percent-encoding
    /// untouched), or nil for any other URL -- a URL of another host is never sent anywhere. The
    /// chip writes https://<host>/t?<query> and nothing else, so a port, a user, a password or a
    /// fragment makes it not a plaque URL (M16-17 review).
    static func sunQuery(of url: URL, host: String) -> String? {
        routeQuery(of: url, host: host, path: path)
    }

    /// The plaque URL rebuilt from THIS server's origin and a query sunQuery let through -- what
    /// "Open in browser" opens. The URL that arrived is never handed to the system as it came.
    static func url(base: URL, sun: String) -> URL? {
        routeURL(base: base, path: path, query: sun)
    }
}

/// A VENUE SCREEN'S LIVE QR (M16-22, ADR 0045 §2.5; Q36 (c)): the system camera reads the screen's
/// rotating code, https://<host>/q?t=<token>, and the Universal Link opens the app with it (the
/// AASA lists /q, M16-20). The app has no QR reader and asks for no camera. The query goes to
/// tap/start as `liveQR`, exactly as written -- the server opens the token (GET /q's reader); the app
/// judges nothing about it. The token is a ~20-second credential: kept in memory, never logged.
enum LiveQRURL {
    /// The live QR path the server serves (internal/liveqr LiveQRTapPath, handler LiveQRPath).
    static let path = "/q"

    /// The query of a live QR URL on this server -- the plaque URL's rule (https, this host, no
    /// port, no user, no password, no fragment, the exact path) -- or nil: another host's code is
    /// never sent anywhere.
    static func query(of url: URL, host: String) -> String? {
        routeQuery(of: url, host: host, path: path)
    }

    /// The live QR URL rebuilt from this server's origin: what "Open in browser" opens for it.
    static func url(base: URL, query: String) -> URL? {
        routeURL(base: base, path: path, query: query)
    }
}

/// What opened the tap screen: a plaque (an NFC read, or its printed static QR -- the same /t
/// route, https://<host>/t?tag=<UID>; the server tells them apart) or a venue screen's live QR.
enum TapLink: Equatable {
    case plaque(sun: String)
    case liveQR(query: String)

    /// This server's plaque or live QR URL, or nil for anything else.
    static func of(_ url: URL, host: String) -> TapLink? {
        if let sun = PlaqueURL.sunQuery(of: url, host: host) {
            return .plaque(sun: sun)
        }
        if let q = LiveQRURL.query(of: url, host: host) {
            return .liveQR(query: q)
        }
        return nil
    }

    /// The URL "Open in browser" hands Safari, rebuilt from the server's origin.
    func browserURL(base: URL) -> URL? {
        switch self {
        case .plaque(let sun):
            return PlaqueURL.url(base: base, sun: sun)
        case .liveQR(let q):
            return LiveQRURL.url(base: base, query: q)
        }
    }
}

/// The one origin check of the two links: https, this host, no port, no user, no password, no
/// fragment, the path exactly `path`, a non-empty query (percent-encoding untouched).
private func routeQuery(of url: URL, host: String, path: String) -> String? {
    guard let comps = URLComponents(url: url, resolvingAgainstBaseURL: false),
          comps.scheme?.lowercased() == "https",
          comps.host?.lowercased() == host.lowercased(),
          comps.port == nil, comps.user == nil, comps.password == nil, comps.fragment == nil,
          comps.percentEncodedPath == path,
          let query = comps.percentEncodedQuery, !query.isEmpty else {
        return nil
    }
    return query
}

private func routeURL(base: URL, path: String, query: String) -> URL? {
    guard var comps = URLComponents(url: base, resolvingAgainstBaseURL: false) else {
        return nil
    }
    comps.percentEncodedPath = path
    comps.percentEncodedQuery = query
    comps.fragment = nil
    return comps.url
}
