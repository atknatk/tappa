import Foundation

// THE CLOCK-SKEW CONTRACT (M16-07 review, 2026-11-14; card M16-16). A signed read carries the
// Unix second it was signed at, and the server accepts it only within 60 s of ITS clock. A phone
// whose clock is further off than that gets the same 401 as a signed-out phone -- it could not
// even fetch a nonce. So the app never signs with its own clock:
//
//   (1) every /app/v1 answer carries a Date header (200, 401, 404 and 405 alike -- pinned by
//       TestApp_UnknownPathsAndMethodsAnswerTheVocabulary and TestApp_TheSignedPathIsTheWirePath);
//       observe() turns it into an offset from the local clock, and now() is the local clock
//       plus that offset;
//   (2) a 401 is read as "signed out" only after ONE retry signed with a fresh offset (AppClient).
//
// The offset is measured against the MIDPOINT of the round trip: the server wrote the header
// somewhere between sending and receiving, and the midpoint halves the worst error. The header's
// own resolution is one second, far inside the 60 s window.
final class ServerClock {
    private let localNow: () -> Date
    private let lock = NSLock()
    private var offset: TimeInterval = 0
    private var measured = false

    init(localNow: @escaping () -> Date = Date.init) {
        self.localNow = localNow
    }

    /// The local clock, read through the injected source (tests move it).
    func local() -> Date { localNow() }

    /// The best estimate of the server's clock right now.
    func now() -> Date {
        lock.lock()
        defer { lock.unlock() }
        return localNow().addingTimeInterval(offset)
    }

    /// Whether any answer has been measured yet.
    var hasMeasurement: Bool {
        lock.lock()
        defer { lock.unlock() }
        return measured
    }

    /// The current offset (server minus local), seconds.
    var currentOffset: TimeInterval {
        lock.lock()
        defer { lock.unlock() }
        return offset
    }

    /// Records the server's clock from one answer's Date header. A missing or unreadable header
    /// leaves the previous measurement in place (false).
    @discardableResult
    func observe(dateHeader: String?, sentAt: Date, receivedAt: Date) -> Bool {
        guard let header = dateHeader, let server = ServerClock.parseHTTPDate(header) else {
            return false
        }
        let mid = sentAt.addingTimeInterval(receivedAt.timeIntervalSince(sentAt) / 2)
        lock.lock()
        offset = server.timeIntervalSince(mid)
        measured = true
        lock.unlock()
        return true
    }

    /// RFC 9110 §5.6.7 IMF-fixdate, the only form Go's net/http writes:
    /// "Sun, 06 Nov 1994 08:49:37 GMT".
    static func parseHTTPDate(_ s: String) -> Date? {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = TimeZone(identifier: "GMT")
        f.dateFormat = "EEE, dd MMM yyyy HH:mm:ss 'GMT'"
        return f.date(from: s)
    }
}
