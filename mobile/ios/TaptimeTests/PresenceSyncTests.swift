import XCTest
@testable import Taptime

// M16-47: what crosses the App Group, how /me becomes it, and the lock-screen counter's plan.
// The ActivityKit driver is replaced by FakeCounter; the plan is the pure part the tests hold.

private func presence(inSince: String = "2026-11-24T15:54:00Z", local: String = "17:54",
                      shiftEndsAt: String? = nil, shiftLocal: String? = nil,
                      showUntil: String = "2026-11-25T03:00:00Z") -> Presence {
    Presence(inSince: inSince, inSinceLocal: local, shiftEndsAt: shiftEndsAt, shiftEndsAtLocal: shiftLocal,
             remindAt: "2026-11-25T01:00:00Z", showUntil: showUntil, staleAt: "2026-11-25T04:00:00Z")
}

private func me(_ p: Presence?) -> MeResponse {
    MeResponse(employeeName: "Maria Borg", brand: AppBrand(name: "Kebab Factory", logo: nil, accent: nil), direction: p == nil ? "in" : "out",
               presence: p, shift: UsualShift(start: "18:00", end: "02:00", overnight: true, venue: "Valletta"),
               missingTimes: 2, hoursWeeks: 4, businessName: "Kebab Factory Ltd")
}

private func iso(_ s: String) -> Date { Presence.instant(s)! }

final class PresenceSyncTests: XCTestCase {
    private var suite: String!
    private var defaults: UserDefaults!

    override func setUp() {
        super.setUp()
        suite = "taptime.tests.\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suite)
    }

    override func tearDown() {
        defaults.removePersistentDomain(forName: suite)
        super.tearDown()
    }

    private var stored: [String: Any] { defaults.persistentDomain(forName: suite) ?? [:] }

    // MARK: The App Group's keys

    /// The whole key set, written with every field filled: instants, their two "HH:MM" strings and
    /// the flags. A new key fails here until it is decided (Shared/PresenceShare.swift).
    func testSharedKeysAreExactlyTheInstantsAndTheFlags() {
        let store = PresenceShareStore(defaults: defaults)
        store.write(PresenceShare(me: me(presence(shiftEndsAt: "2026-11-25T00:00:00Z", shiftLocal: "02:00")), offset: 0))
        let want: Set<String> = ["presence.synced", "presence.in", "presence.inSince", "presence.inSinceLocal",
                                 "presence.shiftEndsAt", "presence.shiftEndsAtLocal", "presence.showUntil"]
        XCTAssertEqual(Set(stored.keys), want)
        XCTAssertEqual(PresenceShareStore.Key.all, want)
        for (k, v) in stored {
            XCTAssertTrue(v is Date || v is Bool || v is String, "\(k) holds a \(type(of: v))")
        }
    }

    /// The strings that do cross are wall clocks; no name, business, venue or total ever does.
    func testTheShareCarriesNoNameBusinessVenueOrTotal() {
        PresenceShareStore(defaults: defaults).write(PresenceShare(me: me(presence(shiftEndsAt: "2026-11-25T00:00:00Z", shiftLocal: "02:00")), offset: 0))
        let strings = stored.values.compactMap { $0 as? String }
        XCTAssertEqual(Set(strings), ["17:54", "02:00"])
        for s in strings {
            XCTAssertNotNil(s.range(of: #"^\d{2}:\d{2}$"#, options: .regularExpression), s)
        }
    }

    func testAnOutAnswerStoresOnlyTheFlags() {
        PresenceShareStore(defaults: defaults).write(PresenceShare(me: me(nil), offset: 0))
        XCTAssertEqual(Set(stored.keys), ["presence.synced", "presence.in"])
        XCTAssertEqual(stored["presence.in"] as? Bool, false)
    }

    func testClearLeavesNothing() {
        let store = PresenceShareStore(defaults: defaults)
        store.write(PresenceShare(me: me(presence()), offset: 0))
        store.clear()
        XCTAssertTrue(stored.isEmpty)
        XCTAssertEqual(store.read(), .none)
    }

    func testReadGivesBackWhatWasWritten() {
        let store = PresenceShareStore(defaults: defaults)
        let s = PresenceShare(me: me(presence(shiftEndsAt: "2026-11-25T00:00:00Z", shiftLocal: "02:00")), offset: 12)
        store.write(s)
        XCTAssertEqual(store.read(), s)
    }

    // MARK: /me -> the share

    /// The server's instants on this phone's clock: local + offset = server.
    func testTheInstantsMoveOntoThePhoneClock() {
        let s = PresenceShare(me: me(presence(shiftEndsAt: "2026-11-25T00:00:00Z", shiftLocal: "02:00")), offset: 30)
        XCTAssertTrue(s.isIn)
        XCTAssertEqual(s.inSince, iso("2026-11-24T15:54:00Z").addingTimeInterval(-30))
        XCTAssertEqual(s.shiftEndsAt, iso("2026-11-25T00:00:00Z").addingTimeInterval(-30))
        XCTAssertEqual(s.showUntil, iso("2026-11-25T03:00:00Z").addingTimeInterval(-30))
        XCTAssertEqual(s.inSinceLocal, "17:54")
        XCTAssertEqual(s.shiftEndsAtLocal, "02:00")
    }

    func testNoShiftMeansNoShiftLine() {
        let s = PresenceShare(me: me(presence(shiftEndsAt: nil, shiftLocal: "02:00")), offset: 0)
        XCTAssertNil(s.shiftEndsAt)
        XCTAssertNil(s.shiftEndsAtLocal)
    }

    func testNoPresenceIsOut() {
        let s = PresenceShare(me: me(nil), offset: 0)
        XCTAssertEqual(s, PresenceShare(isIn: false))
        XCTAssertEqual(s.face(at: Date()), .out)
    }

    // MARK: The faces

    func testFaces() {
        let s = PresenceShare(me: me(presence()), offset: 0)
        let tests: [(name: String, share: PresenceShare, at: String, want: PresenceShare.Face)] = [
            ("in before showUntil", s, "2026-11-24T20:00:00Z", .atWork),
            ("in at showUntil", s, "2026-11-25T03:00:00Z", .unknown),
            ("in after showUntil", s, "2026-11-25T05:00:00Z", .unknown),
            ("out", PresenceShare(isIn: false), "2026-11-24T20:00:00Z", .out),
            ("never synced", .none, "2026-11-24T20:00:00Z", .unknown),
        ]
        for t in tests {
            XCTAssertEqual(t.share.face(at: iso(t.at)), t.want, t.name)
        }
    }

    /// The widget's timeline: one entry now, and one at showUntil while at work.
    func testTheWidgetTimelineTurnsAtShowUntil() {
        let s = PresenceShare(me: me(presence()), offset: 0)
        let now = iso("2026-11-24T20:00:00Z")
        XCTAssertEqual(s.faceChanges(from: now), [now, iso("2026-11-25T03:00:00Z")])
        XCTAssertEqual(PresenceShare(isIn: false).faceChanges(from: now), [now])
        XCTAssertEqual(s.faceChanges(from: iso("2026-11-25T04:00:00Z")), [iso("2026-11-25T04:00:00Z")])
    }

    // MARK: The counter's plan

    func testCounterPlan() {
        let s = PresenceShare(me: me(presence()), offset: 0)
        let until = iso("2026-11-25T03:00:00Z")
        let want = CounterState(inSince: iso("2026-11-24T15:54:00Z"), inSinceLocal: "17:54", shiftEndsAt: nil, shiftEndsAtLocal: nil)
        let other = CounterState(inSince: iso("2026-11-24T09:00:00Z"), inSinceLocal: "11:00", shiftEndsAt: nil, shiftEndsAtLocal: nil)
        let now = iso("2026-11-24T20:00:00Z")
        let tests: [(name: String, share: PresenceShare, enabled: Bool, at: Date, running: [RunningCounter], want: [CounterOp])] = [
            ("in, switch on, none running: start, stale at showUntil", s, true, now, [], [.start(want, staleAt: until)]),
            ("in, the same one running: nothing", s, true, now, [RunningCounter(id: "a", state: want, staleAt: until)], []),
            ("in, an older check-in running: update", s, true, now, [RunningCounter(id: "a", state: other, staleAt: until)],
             [.update(id: "a", want, staleAt: until)]),
            ("in, a new showUntil: update", s, true, now, [RunningCounter(id: "a", state: want, staleAt: nil)],
             [.update(id: "a", want, staleAt: until)]),
            ("in, two running: keep one, end the stray", s, true, now,
             [RunningCounter(id: "a", state: want, staleAt: until), RunningCounter(id: "b", state: other, staleAt: until)], [.end(id: "b")]),
            ("out: end", PresenceShare(isIn: false), true, now, [RunningCounter(id: "a", state: want, staleAt: until)], [.end(id: "a")]),
            ("switch off: end", s, false, now, [RunningCounter(id: "a", state: want, staleAt: until)], [.end(id: "a")]),
            ("switch off, none running: nothing", s, false, now, [], []),
            ("signed out: end", .none, true, now, [RunningCounter(id: "a", state: want, staleAt: until)], [.end(id: "a")]),
            // M16-47 O3: still in after showUntil -- the stale view stays until the system's limit.
            ("past showUntil, still in: left stale, not ended", s, true, iso("2026-11-25T03:30:00Z"),
             [RunningCounter(id: "a", state: want, staleAt: until)], []),
            ("past showUntil, none running: never start", s, true, iso("2026-11-25T03:30:00Z"), [], []),
            ("past showUntil, an older check-in running: update, stale at once", s, true, iso("2026-11-25T03:30:00Z"),
             [RunningCounter(id: "a", state: other, staleAt: until)], [.update(id: "a", want, staleAt: until)]),
            ("past showUntil, two running: keep one stale, end the stray", s, true, iso("2026-11-25T03:30:00Z"),
             [RunningCounter(id: "a", state: want, staleAt: until), RunningCounter(id: "b", state: other, staleAt: until)],
             [.end(id: "b")]),
            ("past showUntil, out: end", PresenceShare(isIn: false), true, iso("2026-11-25T03:30:00Z"),
             [RunningCounter(id: "a", state: want, staleAt: until)], [.end(id: "a")]),
            ("past showUntil, signed out: end", .none, true, iso("2026-11-25T03:30:00Z"),
             [RunningCounter(id: "a", state: want, staleAt: until)], [.end(id: "a")]),
            ("past showUntil, switch off: end", s, false, iso("2026-11-25T03:30:00Z"),
             [RunningCounter(id: "a", state: want, staleAt: until)], [.end(id: "a")]),
        ]
        for t in tests {
            XCTAssertEqual(LiveCounter.plan(share: t.share, enabled: t.enabled, now: t.at, running: t.running), t.want, t.name)
        }
    }

    func testTheSwitchIsOffUntilTurnedOn() {
        XCTAssertFalse(LiveCounter.defaultOn)
    }

    // MARK: PresenceSync

    @MainActor
    func testSyncWritesReloadsAndStartsOne() {
        let counter = FakeCounter()
        var reloads = 0
        var on = true
        let sync = PresenceSync(store: PresenceShareStore(defaults: defaults), reload: { reloads += 1 }, counter: counter,
                                enabled: { on }, now: { iso("2026-11-24T20:00:00Z") })
        sync.apply(me(presence()), offset: 0)
        XCTAssertEqual(reloads, 1)
        XCTAssertEqual(counter.live.count, 1)
        XCTAssertEqual(counter.live.first?.staleAt, iso("2026-11-25T03:00:00Z"))

        // The same answer again: no reload, no second activity.
        sync.apply(me(presence()), offset: 0)
        XCTAssertEqual(reloads, 1)
        XCTAssertEqual(counter.live.count, 1)

        // Out: the widget reloads and the counter ends.
        sync.apply(me(nil), offset: 0)
        XCTAssertEqual(reloads, 2)
        XCTAssertTrue(counter.live.isEmpty)

        // In again, then the switch off.
        sync.apply(me(presence()), offset: 0)
        XCTAssertEqual(counter.live.count, 1)
        on = false
        sync.counterSwitched()
        XCTAssertTrue(counter.live.isEmpty)
        on = true
        sync.counterSwitched()
        XCTAssertEqual(counter.live.count, 1)
    }

    @MainActor
    func testClearEmptiesTheShareAndEndsTheCounter() {
        let counter = FakeCounter()
        var reloads = 0
        let store = PresenceShareStore(defaults: defaults)
        let sync = PresenceSync(store: store, reload: { reloads += 1 }, counter: counter, enabled: { true },
                                now: { iso("2026-11-24T20:00:00Z") })
        sync.apply(me(presence()), offset: 0)
        sync.clear()
        XCTAssertTrue(stored.isEmpty)
        XCTAssertEqual(store.read(), .none)
        XCTAssertTrue(counter.live.isEmpty)
        XCTAssertEqual(reloads, 2)
    }

    /// The system's own Live Activities switch is off: nothing is requested.
    @MainActor
    func testNoCounterWhenTheSystemSwitchIsOff() {
        let counter = FakeCounter()
        counter.allowed = false
        let sync = PresenceSync(store: PresenceShareStore(defaults: defaults), reload: {}, counter: counter, enabled: { true },
                                now: { iso("2026-11-24T20:00:00Z") })
        sync.apply(me(presence()), offset: 0)
        XCTAssertTrue(counter.live.isEmpty)
        XCTAssertFalse(sync.counterAllowed)
    }
}
