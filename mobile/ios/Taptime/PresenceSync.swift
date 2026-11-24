import Foundation
import WidgetKit
#if canImport(ActivityKit)
import ActivityKit
#endif

// THE LOCK SCREEN AND THE HOME SCREEN FOLLOW /me (M16-47, the app's half). Every /me answer -- the
// same hook that drives the clock-out reminder (TapModel.onMe) -- is turned into a PresenceShare,
// written to the App Group for the widget, and reconciled against the running Live Activity. Signing
// out, deleting the account and a 401 clear both. Nothing else starts, updates or ends either: the
// app runs nothing in the background, asks for no push token and reads no location (§4.2).
//
// THE COUNTER COMES ONLY FROM THE SERVER'S INSTANT. presence.inSince (moved onto this phone's clock
// by the measured offset) is the start; the app predicts no direction and counts nothing itself --
// the system draws the running number (Shared/LiveViews.swift).
//
// A failed /me (no network) calls nothing here: the last server answer stands, as it does for the
// reminder (M16-46).

extension PresenceShare {
    /// /me's presence on this phone's clock: the server's instant minus ServerClock's offset (local +
    /// offset = server, so server - offset = local). The "HH:MM" strings travel as the server wrote
    /// them -- the app converts no time zone (§6).
    init(me: MeResponse, offset: TimeInterval) {
        guard let p = me.presence, let since = Presence.instant(p.inSince) else {
            self.init(isIn: false)
            return
        }
        func local(_ s: String?) -> Date? { Presence.instant(s)?.addingTimeInterval(-offset) }
        let end = local(p.shiftEndsAt)
        self.init(isIn: true, inSince: since.addingTimeInterval(-offset), inSinceLocal: p.inSinceLocal,
                  shiftEndsAt: end, shiftEndsAtLocal: end == nil ? nil : p.shiftEndsAtLocal,
                  showUntil: local(p.showUntil))
    }
}

// MARK: - The counter's plan (pure, so the tests hold it without ActivityKit)

/// What one Live Activity shows. The instants are this phone's; the strings are the server's.
struct CounterState: Equatable {
    var inSince: Date
    var inSinceLocal: String
    var shiftEndsAt: Date?
    var shiftEndsAtLocal: String?
}

/// A Live Activity the system holds for this app.
struct RunningCounter: Equatable {
    let id: String
    let state: CounterState
    let staleAt: Date?
}

enum CounterOp: Equatable {
    /// Request a new activity, stale at showUntil.
    case start(CounterState, staleAt: Date)
    case update(id: String, CounterState, staleAt: Date)
    /// End with the immediate dismissal: an "in" the server no longer reports must not stay up.
    case end(id: String)
}

enum LiveCounter {
    /// The Settings switch. OFF until the person turns it on: Q53 (1) (a), answered by the user on
    /// 2026-11-19, and the Android twin's `timer` preference (StaffNotifications.kt) -- the lock
    /// screen is seen by whoever is next to the phone.
    static let defaultOn = false
    private static let key = "staff.liveCounter"

    static var enabled: Bool {
        get { (UserDefaults.standard.object(forKey: key) as? Bool) ?? defaultOn }
        set { UserDefaults.standard.set(newValue, forKey: key) }
    }

    /// Whether this phone can show one at all (ActivityContent and staleDate are iOS 16.2).
    static var supported: Bool {
        if #available(iOS 16.2, *) { return true }
        return false
    }

    /// At most ONE activity, and only while the switch is on and the share says "at work" now
    /// (before showUntil). Anything else the system holds is a stray and is ended.
    static func plan(share: PresenceShare, enabled: Bool, now: Date, running: [RunningCounter]) -> [CounterOp] {
        guard enabled, share.face(at: now) == .atWork, let since = share.inSince, let local = share.inSinceLocal,
              let until = share.showUntil else {
            return running.map { .end(id: $0.id) }
        }
        let want = CounterState(inSince: since, inSinceLocal: local, shiftEndsAt: share.shiftEndsAt,
                                shiftEndsAtLocal: share.shiftEndsAtLocal)
        guard let keep = running.first else {
            return [.start(want, staleAt: until)]
        }
        var ops: [CounterOp] = []
        if keep.state != want || keep.staleAt != until {
            ops.append(.update(id: keep.id, want, staleAt: until))
        }
        ops += running.dropFirst().map { .end(id: $0.id) }
        return ops
    }
}

/// The system's Live Activities, as PresenceSync needs them (consumer-side interface, §7; the tests
/// pass a fake).
protocol CounterDriving: AnyObject {
    /// The system's Settings → Taptime → Live Activities switch.
    var allowed: Bool { get }
    func running() -> [RunningCounter]
    func perform(_ ops: [CounterOp])
}

// MARK: - The sync

@MainActor
final class PresenceSync {
    static let shared = PresenceSync(store: .shared, reload: { WidgetCenter.shared.reloadAllTimelines() },
                                     counter: PresenceSync.systemCounter(), enabled: { LiveCounter.enabled })

    private let store: PresenceShareStore
    private let reload: () -> Void
    private let counter: CounterDriving?
    private let enabled: () -> Bool
    private let now: () -> Date

    init(store: PresenceShareStore, reload: @escaping () -> Void, counter: CounterDriving?,
         enabled: @escaping () -> Bool, now: @escaping () -> Date = Date.init) {
        self.store = store
        self.reload = reload
        self.counter = counter
        self.enabled = enabled
        self.now = now
    }

    static func systemCounter() -> CounterDriving? {
        #if canImport(ActivityKit)
        if #available(iOS 16.2, *) { return SystemCounter() }
        #endif
        return nil
    }

    /// Every /me answer.
    func apply(_ me: MeResponse, offset: TimeInterval) {
        let share = PresenceShare(me: me, offset: offset)
        if store.read() != share {
            store.write(share)
            reload()
        }
        reconcile(share)
    }

    /// Sign-out, account deletion, a 401: the widget knows nothing and the counter ends.
    func clear() {
        store.clear()
        reload()
        reconcile(.none)
    }

    /// The Settings switch moved.
    func counterSwitched() {
        reconcile(store.read())
    }

    var counterAllowed: Bool { counter?.allowed ?? false }

    private func reconcile(_ share: PresenceShare) {
        guard let c = counter else { return }
        c.perform(LiveCounter.plan(share: share, enabled: enabled() && c.allowed, now: now(), running: c.running()))
    }
}

// MARK: - ActivityKit

#if canImport(ActivityKit)
@available(iOS 16.2, *)
final class SystemCounter: CounterDriving {
    var allowed: Bool { ActivityAuthorizationInfo().areActivitiesEnabled }

    private var live: [Activity<TimerAttributes>] {
        Activity<TimerAttributes>.activities.filter { $0.activityState == .active || $0.activityState == .stale }
    }

    func running() -> [RunningCounter] {
        live.map { a in
            let s = a.content.state
            return RunningCounter(id: a.id, state: CounterState(inSince: s.inSince, inSinceLocal: s.inSinceLocal,
                                                                shiftEndsAt: s.shiftEndsAt, shiftEndsAtLocal: s.shiftEndsAtLocal),
                                  staleAt: a.content.staleDate)
        }
    }

    func perform(_ ops: [CounterOp]) {
        let byID = Dictionary(live.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        for op in ops {
            switch op {
            case let .start(s, staleAt):
                // No push type: started and ended here, never by a server (Q53). A refusal (the app
                // not in front, the system's limit) shows nothing to the person; the next /me retries.
                _ = try? Activity.request(attributes: TimerAttributes(), content: ActivityContent(state: SystemCounter.state(s), staleDate: staleAt),
                                          pushType: nil)
            case let .update(id, s, staleAt):
                guard let a = byID[id] else { continue }
                Task { await a.update(ActivityContent(state: SystemCounter.state(s), staleDate: staleAt)) }
            case let .end(id):
                guard let a = byID[id] else { continue }
                Task { await a.end(nil, dismissalPolicy: .immediate) }
            }
        }
    }

    private static func state(_ s: CounterState) -> TimerAttributes.ContentState {
        TimerAttributes.ContentState(inSince: s.inSince, inSinceLocal: s.inSinceLocal, shiftEndsAt: s.shiftEndsAt,
                                     shiftEndsAtLocal: s.shiftEndsAtLocal)
    }
}
#endif
