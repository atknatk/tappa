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
    /// untouched), or nil for any other URL -- a URL of another host is never sent anywhere.
    static func sunQuery(of url: URL, host: String) -> String? {
        guard let comps = URLComponents(url: url, resolvingAgainstBaseURL: false),
              comps.scheme?.lowercased() == "https",
              comps.host?.lowercased() == host.lowercased(),
              comps.percentEncodedPath == path,
              let query = comps.percentEncodedQuery, !query.isEmpty else {
            return nil
        }
        return query
    }
}
