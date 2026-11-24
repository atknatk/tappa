import Foundation

// WHAT THE APP TELLS ITS WIDGET (M16-47; compiled into BOTH the app and the TaptimeWidgets
// extension). The two processes share one App Group, and through it one UserDefaults suite; this
// file is the whole of what crosses it. It is the lock screen's and the home screen's input, and
// both are seen by anyone standing next to the phone, so it carries instants and one flag -- and
// nothing else:
//
//   in                 whether /me answered with an open check-in
//   inSince            when that check-in began (on THIS phone's clock: the server's instant minus
//                      the measured offset, ServerClock -- the system draws the counter from it)
//   inSinceLocal       the same instant as the business's wall clock, "HH:MM", exactly as the
//                      server wrote it (Presence.inSinceLocal): the app converts no time zone (§6)
//   shiftEndsAt        the frozen shift's end, when the owner marked shifts real (M16-45 O1)
//   shiftEndsAtLocal   its "HH:MM", as the server wrote it
//   showUntil          when the widget stops saying "in" (Presence.showUntil: min(remindAt + 2 h,
//                      staleAt) -- never after the business day's end; Q53 (3) (a))
//
// NO name, NO business, NO venue, NO hour total, NO coordinate: SharedKeysTests pins the key set,
// so a new key is a decision, not a side effect. The values are never logged.
struct PresenceShare: Equatable {
    /// False only for `none`: "out" and "never synced" read differently.
    var synced: Bool
    var isIn: Bool
    var inSince: Date?
    var inSinceLocal: String?
    var shiftEndsAt: Date?
    var shiftEndsAtLocal: String?
    var showUntil: Date?

    /// Nothing known: a phone that was never synced, or was signed out.
    static let none = PresenceShare(synced: false, isIn: false)

    init(synced: Bool = true, isIn: Bool, inSince: Date? = nil, inSinceLocal: String? = nil, shiftEndsAt: Date? = nil,
         shiftEndsAtLocal: String? = nil, showUntil: Date? = nil) {
        self.synced = synced
        self.isIn = isIn
        self.inSince = inSince
        self.inSinceLocal = inSinceLocal
        self.shiftEndsAt = shiftEndsAt
        self.shiftEndsAtLocal = shiftEndsAtLocal
        self.showUntil = showUntil
    }

    /// What the screen says at a moment.
    enum Face: Equatable {
        /// Clocked in, counting from inSince.
        case atWork
        /// /me said there is no open check-in.
        case out
        /// Nothing to go on: never synced, signed out, or past showUntil (a clock-out in the
        /// browser cannot reach the phone, so after showUntil the honest answer is "open the app").
        case unknown
    }

    /// When the widget's face can change without the app: now, and showUntil while at work (the
    /// timeline's entries; the app reloads the timeline on every /me for anything else).
    func faceChanges(from now: Date) -> [Date] {
        guard face(at: now) == .atWork, let until = showUntil else { return [now] }
        return [now, until]
    }

    func face(at now: Date) -> Face {
        guard synced else { return .unknown }
        guard isIn else { return .out }
        guard inSince != nil, let until = showUntil, now < until else { return .unknown }
        return .atWork
    }
}

/// The App Group's suite: read by the widget, written only by the app.
struct PresenceShareStore {
    static let appGroup = "group.mt.taptime.app"

    /// Every key this store may write -- the test pins it as a set.
    enum Key {
        static let synced = "presence.synced"
        static let isIn = "presence.in"
        static let inSince = "presence.inSince"
        static let inSinceLocal = "presence.inSinceLocal"
        static let shiftEndsAt = "presence.shiftEndsAt"
        static let shiftEndsAtLocal = "presence.shiftEndsAtLocal"
        static let showUntil = "presence.showUntil"
        static let all: Set<String> = [synced, isIn, inSince, inSinceLocal, shiftEndsAt, shiftEndsAtLocal, showUntil]
    }

    let defaults: UserDefaults

    init(defaults: UserDefaults) {
        self.defaults = defaults
    }

    /// The shared suite. An unsigned build (CI, the simulator) has no group container; the suite
    /// then lives in the process's own container and the widget simply sees nothing.
    static var shared: PresenceShareStore {
        PresenceShareStore(defaults: UserDefaults(suiteName: appGroup) ?? .standard)
    }

    func read() -> PresenceShare {
        guard defaults.bool(forKey: Key.synced) else { return .none }
        return PresenceShare(isIn: defaults.bool(forKey: Key.isIn),
                             inSince: defaults.object(forKey: Key.inSince) as? Date,
                             inSinceLocal: defaults.string(forKey: Key.inSinceLocal),
                             shiftEndsAt: defaults.object(forKey: Key.shiftEndsAt) as? Date,
                             shiftEndsAtLocal: defaults.string(forKey: Key.shiftEndsAtLocal),
                             showUntil: defaults.object(forKey: Key.showUntil) as? Date)
    }

    func write(_ s: PresenceShare) {
        clear()
        guard s.synced else { return }
        defaults.set(true, forKey: Key.synced)
        defaults.set(s.isIn, forKey: Key.isIn)
        func put(_ v: Any?, _ k: String) {
            if let v = v { defaults.set(v, forKey: k) }
        }
        put(s.inSince, Key.inSince)
        put(s.inSinceLocal, Key.inSinceLocal)
        put(s.shiftEndsAt, Key.shiftEndsAt)
        put(s.shiftEndsAtLocal, Key.shiftEndsAtLocal)
        put(s.showUntil, Key.showUntil)
    }

    func clear() {
        for k in Key.all {
            defaults.removeObject(forKey: k)
        }
    }
}
